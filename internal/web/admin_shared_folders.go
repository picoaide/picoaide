package web

import (
  "context"
  "fmt"
  "net/http"
  "os"
  "path/filepath"
  "strconv"
  "strings"
  "time"

  "github.com/docker/docker/api/types/mount"
  "github.com/gin-gonic/gin"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 共享文件夹管理 — 超管 API
// ============================================================

// handleAdminSharedFolders 列表全部共享文件夹
func (s *Server) handleAdminSharedFolders(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  all, err := auth.ListSharedFolders()
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  result := make([]*auth.SharedFolderInfo, 0, len(all))
  for i := range all {
    info, err := auth.BuildSharedFolderInfo(&all[i])
    if err != nil {
      writeError(c, http.StatusInternalServerError, err.Error())
      return
    }
    result = append(result, info)
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "folders": result,
  })
}

// handleAdminSharedFoldersCreate 创建共享文件夹
func (s *Server) handleAdminSharedFoldersCreate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }
  username := s.getSessionUser(c)
  name := strings.TrimSpace(c.PostForm("name"))
  description := strings.TrimSpace(c.PostForm("description"))
  isPublic := c.PostForm("is_public") == "1"
  groupIDsStr := strings.TrimSpace(c.PostForm("group_ids"))

  if name == "" {
    writeError(c, http.StatusBadRequest, "名称不能为空")
    return
  }
  if err := util.SafePathSegment(name); err != nil {
    writeError(c, http.StatusBadRequest, "名称不合法: "+err.Error())
    return
  }

  // 创建主机目录
  shareDir := filepath.Join(filepath.Dir(s.cfg.UsersRoot), "shared", name)
  if err := os.MkdirAll(shareDir, 0755); err != nil {
    writeError(c, http.StatusInternalServerError, "创建共享目录失败: "+err.Error())
    return
  }

  // 创建数据库记录
  if err := auth.CreateSharedFolder(name, description, isPublic, username); err != nil {
    // 目录已创建，需要回滚
    os.RemoveAll(shareDir)
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 关联组
  if groupIDsStr != "" {
    sf, err := auth.GetSharedFolderByName(name)
    if err == nil {
      parts := strings.Split(groupIDsStr, ",")
      gids := make([]int64, 0, len(parts))
      for _, p := range parts {
        gid, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
        if err == nil {
          gids = append(gids, gid)
        }
      }
      if len(gids) > 0 {
        auth.SetSharedFolderGroups(sf.ID, gids)
      }
    }
  }

  writeSuccess(c, "共享文件夹「"+name+"」创建成功")
}

// handleAdminSharedFoldersUpdate 更新共享文件夹
func (s *Server) handleAdminSharedFoldersUpdate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }
  idStr := strings.TrimSpace(c.PostForm("id"))
  newName := strings.TrimSpace(c.PostForm("name"))
  description := strings.TrimSpace(c.PostForm("description"))
  newIsPublic := c.PostForm("is_public") == "1"

  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }
  if newName == "" {
    writeError(c, http.StatusBadRequest, "名称不能为空")
    return
  }
  if err := util.SafePathSegment(newName); err != nil {
    writeError(c, http.StatusBadRequest, "名称不合法: "+err.Error())
    return
  }

  // 获取旧记录（用于检测是否需要 mv 目录和重启容器）
  oldSF, err := auth.GetSharedFolder(id)
  if err != nil {
    writeError(c, http.StatusBadRequest, "共享文件夹不存在")
    return
  }

  needsRename := oldSF.Name != newName
  needsRestart := needsRename || oldSF.IsPublic != newIsPublic

  // 如果改名，先 mv 主机目录
  if needsRename {
    oldDir := filepath.Join(filepath.Dir(s.cfg.UsersRoot), "shared", oldSF.Name)
    newDir := filepath.Join(filepath.Dir(s.cfg.UsersRoot), "shared", newName)
    if _, err := os.Stat(oldDir); err == nil {
      if err := os.Rename(oldDir, newDir); err != nil {
        writeError(c, http.StatusInternalServerError, "重命名共享目录失败: "+err.Error())
        return
      }
    }
  }

  // 更新数据库
  if err := auth.UpdateSharedFolder(id, newName, description, newIsPublic); err != nil {
    // 回滚目录改名
    if needsRename {
      oldDir := filepath.Join(filepath.Dir(s.cfg.UsersRoot), "shared", oldSF.Name)
      newDir := filepath.Join(filepath.Dir(s.cfg.UsersRoot), "shared", newName)
      os.Rename(newDir, oldDir)
    }
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 需要重启容器
  if needsRestart {
    s.restartSharedFolderContainers(c, id, "更新共享文件夹后重启")
    return
  }

  writeSuccess(c, "共享文件夹已更新")
}

// handleAdminSharedFoldersDelete 删除共享文件夹
func (s *Server) handleAdminSharedFoldersDelete(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }
  idStr := strings.TrimSpace(c.PostForm("id"))
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }

  sf, err := auth.GetSharedFolder(id)
  if err != nil {
    writeError(c, http.StatusBadRequest, "共享文件夹不存在")
    return
  }

  // 归档主机目录
  shareDir := filepath.Join(filepath.Dir(s.cfg.UsersRoot), "shared", sf.Name)
  if _, err := os.Stat(shareDir); err == nil {
    timestamp := time.Now().Format("20060102_150405")
    archiveDir := filepath.Join(filepath.Dir(s.cfg.UsersRoot), "archive", fmt.Sprintf("shared_%s_%s", sf.Name, timestamp))
    if err := os.MkdirAll(filepath.Dir(archiveDir), 0755); err == nil {
      os.Rename(shareDir, archiveDir)
    }
  }

  // 获取关联用户用于后续重启
  members, _ := auth.GetSharedFolderMembers(id)

  // 删除数据库记录
  if err := auth.DeleteSharedFolder(id); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 重启容器
  if len(members) > 0 {
    taskID, err := enqueueTask("mount-shared-folder", members, func(username string) error {
      return s.recreateUserContainerWithSharedMounts(username)
    })
    if err == nil {
      writeJSON(c, http.StatusOK, map[string]interface{}{
        "success": true,
        "message": fmt.Sprintf("共享文件夹已删除，文件已归档，正在重启 %d 个用户容器", len(members)),
        "task_id": taskID,
      })
      return
    }
  }

  writeSuccess(c, "共享文件夹已删除")
}

// handleAdminSharedFoldersSetGroups 设置共享文件夹关联组
func (s *Server) handleAdminSharedFoldersSetGroups(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }
  folderIDStr := strings.TrimSpace(c.PostForm("folder_id"))
  groupIDsStr := strings.TrimSpace(c.PostForm("group_ids"))

  folderID, err := strconv.ParseInt(folderIDStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的文件夹 ID")
    return
  }

  // 获取旧成员
  oldMembers, _ := auth.GetSharedFolderMembers(folderID)

  // 解析新的组 ID 列表
  var gids []int64
  if groupIDsStr != "" {
    parts := strings.Split(groupIDsStr, ",")
    for _, p := range parts {
      gid, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
      if err != nil {
        writeError(c, http.StatusBadRequest, "无效的组 ID: "+p)
        return
      }
      gids = append(gids, gid)
    }
  }

  if err := auth.SetSharedFolderGroups(folderID, gids); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 计算成员变化
  newMembers, _ := auth.GetSharedFolderMembers(folderID)
  oldSet := make(map[string]bool)
  for _, m := range oldMembers {
    oldSet[m] = true
  }
  newSet := make(map[string]bool)
  for _, m := range newMembers {
    newSet[m] = true
  }
  // 需要重启 = 新增的用户 + 失去访问的用户
  changedUsers := make([]string, 0)
  for _, m := range newMembers {
    if !oldSet[m] {
      changedUsers = append(changedUsers, m)
    }
  }
  for _, m := range oldMembers {
    if !newSet[m] {
      changedUsers = append(changedUsers, m)
    }
  }
  changedUsers = uniqueStrings(changedUsers)

  if len(changedUsers) > 0 {
    taskID, err := enqueueTask("mount-shared-folder", changedUsers, func(username string) error {
      return s.recreateUserContainerWithSharedMounts(username)
    })
    if err == nil {
      writeJSON(c, http.StatusOK, map[string]interface{}{
        "success": true,
        "message": fmt.Sprintf("关联组已更新，正在重启 %d 个用户容器", len(changedUsers)),
        "task_id": taskID,
      })
      return
    }
  }

  writeSuccess(c, "关联组已更新")
}

// handleAdminSharedFoldersTest 测试用户挂载状态
func (s *Server) handleAdminSharedFoldersTest(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }
  folderIDStr := strings.TrimSpace(c.PostForm("folder_id"))
  testUsername := strings.TrimSpace(c.PostForm("username"))

  folderID, err := strconv.ParseInt(folderIDStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的文件夹 ID")
    return
  }
  if testUsername == "" {
    writeError(c, http.StatusBadRequest, "用户名不能为空")
    return
  }

  sf, err := auth.GetSharedFolder(folderID)
  if err != nil {
    writeError(c, http.StatusBadRequest, "共享文件夹不存在")
    return
  }

  mounted := false
  msg := "用户 " + testUsername + " 未挂载"

  // 1. 检查主机目录是否存在
  hostDir := filepath.Join(filepath.Dir(s.cfg.UsersRoot), "shared", sf.Name)
  if _, err := os.Stat(hostDir); os.IsNotExist(err) {
    msg = "主机共享目录不存在"
    auth.RecordMountTest(folderID, testUsername, false)
    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success": true, "mounted": false, "message": msg,
    })
    return
  }

  // 2. 检查容器是否在运行
  if s.dockerAvailable {
    containerPath := "/root/.picoclaw/workspace/share/" + sf.Name
    ok, err := dockerpkg.TestContainerDir(c.Request.Context(), "picoaide-"+testUsername, containerPath)
    if err == nil && ok {
      mounted = true
      msg = "用户 " + testUsername + " 已挂载"
    }
  } else {
    msg = "Docker 不可用，仅检查了主机目录"
  }

  now := time.Now().Format("2006-01-02 15:04:05")
  auth.RecordMountTest(folderID, testUsername, mounted)
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":    true,
    "mounted":    mounted,
    "message":    msg,
    "checked_at": now,
  })
}

// handleAdminSharedFoldersMount 一键挂载共享文件夹到所有关联用户
func (s *Server) handleAdminSharedFoldersMount(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }
  folderIDStr := strings.TrimSpace(c.PostForm("folder_id"))
  folderID, err := strconv.ParseInt(folderIDStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的文件夹 ID")
    return
  }

  if _, err := auth.GetSharedFolder(folderID); err != nil {
    writeError(c, http.StatusBadRequest, "共享文件夹不存在")
    return
  }

  members, err := auth.GetSharedFolderMembers(folderID)
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  if len(members) == 0 {
    writeError(c, http.StatusBadRequest, "该共享文件夹没有可挂载的用户")
    return
  }

  taskID, err := enqueueTask("mount-shared-folder", members, func(username string) error {
    return s.recreateUserContainerWithSharedMounts(username)
  })
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": fmt.Sprintf("已提交挂载任务，共 %d 个用户", len(members)),
    "task_id": taskID,
  })
}

// ============================================================
// 内部辅助
// ============================================================

// recreateUserContainerWithSharedMounts 停止→删除→重建→启动用户容器，附加所有共享挂载
func (s *Server) recreateUserContainerWithSharedMounts(username string) error {
  if !s.dockerAvailable {
    return nil
  }
  ctx := context.Background()

  rec, err := auth.GetContainerByUsername(username)
  if err != nil || rec == nil || rec.ContainerID == "" {
    return nil // 没有容器记录或记录不完整，跳过
  }

  // 计算所有共享挂载（确保路径为绝对路径）
  workDir, _ := filepath.Abs(filepath.Dir(s.cfg.UsersRoot))
  shareMounts, err := auth.GetSharedFolderMountsForUser(workDir, username)
  if err != nil {
    return fmt.Errorf("计算共享挂载失败: %w", err)
  }
  var extraMounts []mount.Mount
  for _, sm := range shareMounts {
    extraMounts = append(extraMounts, mount.Mount{
      Type:   mount.TypeBind,
      Source: sm.Source,
      Target: sm.Target,
    })
  }

  userDir := user.UserDir(s.cfg, username)

  // 停止并删除旧容器
  dockerpkg.Stop(ctx, rec.ContainerID)
  dockerpkg.Remove(ctx, rec.ContainerID)
  // 无论后续是否成功，先清除 DB 中的容器记录避免残留
  auth.UpdateContainerID(username, "")
  auth.UpdateContainerStatus(username, "stopped")

  // 重建容器
  containerID, err := dockerpkg.CreateContainerWithOptions(ctx, username, rec.Image,
    userDir, rec.IP, rec.CPULimit, rec.MemoryLimit, false, extraMounts)
  if err != nil {
    return fmt.Errorf("重建容器失败: %w", err)
  }

  if err := dockerpkg.Start(ctx, containerID); err != nil {
    return fmt.Errorf("启动容器失败: %w", err)
  }

  // 更新容器记录
  auth.UpsertContainer(&auth.ContainerRecord{
    Username:    username,
    ContainerID: containerID,
    Image:       rec.Image,
    Status:      "running",
    IP:          rec.IP,
    CPULimit:    rec.CPULimit,
    MemoryLimit: rec.MemoryLimit,
  })

  return nil
}

// restartSharedFolderContainers 重启某个共享文件夹的所有关联用户容器（异步）
func (s *Server) restartSharedFolderContainers(c *gin.Context, folderID int64, reason string) {
  members, err := auth.GetSharedFolderMembers(folderID)
  if err != nil || len(members) == 0 {
    writeSuccess(c, "共享文件夹已更新（无需重启容器）")
    return
  }
  taskID, err := enqueueTask("mount-shared-folder", members, func(username string) error {
    return s.recreateUserContainerWithSharedMounts(username)
  })
  if err != nil {
    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success": true,
      "message": fmt.Sprintf("共享文件夹已更新，但提交重启任务失败: %s", err.Error()),
    })
    return
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": fmt.Sprintf("共享文件夹已更新，正在重启 %d 个用户容器", len(members)),
    "task_id": taskID,
  })
}

// uniqueStrings 字符串去重
func uniqueStrings(s []string) []string {
  seen := make(map[string]bool)
  result := make([]string, 0, len(s))
  for _, item := range s {
    if !seen[item] {
      seen[item] = true
      result = append(result, item)
    }
  }
  return result
}
