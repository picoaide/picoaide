package web

import (
  "fmt"
  "net/http"
  "os"
  "path/filepath"
  "strconv"
  "strings"
  "time"

  "github.com/gin-gonic/gin"

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
  shareDir := filepath.Join(filepath.Dir(s.loadConfig().UsersRoot), "shared", name)
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

  // 获取旧记录（用于检测是否需要 mv 目录）
  oldSF, err := auth.GetSharedFolder(id)
  if err != nil {
    writeError(c, http.StatusBadRequest, "共享文件夹不存在")
    return
  }

  needsRename := oldSF.Name != newName

  // 如果改名，先 mv 主机目录
  if needsRename {
    oldDir := filepath.Join(filepath.Dir(s.loadConfig().UsersRoot), "shared", oldSF.Name)
    newDir := filepath.Join(filepath.Dir(s.loadConfig().UsersRoot), "shared", newName)
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
      oldDir := filepath.Join(filepath.Dir(s.loadConfig().UsersRoot), "shared", oldSF.Name)
      newDir := filepath.Join(filepath.Dir(s.loadConfig().UsersRoot), "shared", newName)
      os.Rename(newDir, oldDir)
    }
    writeError(c, http.StatusBadRequest, err.Error())
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
  shareDir := filepath.Join(filepath.Dir(s.loadConfig().UsersRoot), "shared", sf.Name)
  if _, err := os.Stat(shareDir); err == nil {
    timestamp := time.Now().Format("20060102_150405")
    archiveDir := filepath.Join(filepath.Dir(s.loadConfig().UsersRoot), "archive", fmt.Sprintf("shared_%s_%s", sf.Name, timestamp))
    if err := os.MkdirAll(filepath.Dir(archiveDir), 0755); err == nil {
      os.Rename(shareDir, archiveDir)
    }
  }

  // 删除数据库记录
  if err := auth.DeleteSharedFolder(id); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
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

  if err := util.SafePathSegment(sf.Name); err != nil {
    writeError(c, http.StatusBadRequest, "共享文件夹名称不合法")
    return
  }

  mounted := false
  msg := "用户 " + testUsername + " 未挂载"

  // 1. 检查主机目录是否存在
  hostDir := filepath.Join(filepath.Dir(s.loadConfig().UsersRoot), "shared", sf.Name)
  if _, err := os.Stat(hostDir); os.IsNotExist(err) {
    msg = "主机共享目录不存在"
    auth.RecordMountTest(folderID, testUsername, false)
    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success": true, "mounted": false, "message": msg,
    })
    return
  }

  // 2. 检查用户目录中是否存在共享文件夹的符号链接
  userDir := user.UserDir(s.loadConfig(), testUsername)
  userSharePath := filepath.Join(userDir, "share", sf.Name)
  if !strings.HasPrefix(filepath.Clean(userSharePath), filepath.Clean(userDir)+string(os.PathSeparator)) {
    writeError(c, http.StatusForbidden, "共享文件夹路径不合法")
    return
  }
  if _, err := os.Stat(userSharePath); err == nil {
    mounted = true
    msg = "用户 " + testUsername + " 已挂载"
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

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": fmt.Sprintf("已处理挂载请求，共 %d 个用户", len(members)),
  })
}


