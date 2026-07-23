package web

import (
  "fmt"
  "net/http"
  "os"
  "path/filepath"
  "strings"
  "time"
  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/store"
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
  all, err := store.ListSharedFolders()
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  result := make([]*store.SharedFolderInfo, 0, len(all))
  for i := range all {
    info, err := store.BuildSharedFolderInfo(&all[i])
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

type adminSharedFolderCreateReq struct {
  Name        string  `json:"name"`
  Description string  `json:"description"`
  IsPublic    bool    `json:"is_public"`
  GroupIDs    []int64 `json:"group_ids"`
}

// handleAdminSharedFoldersCreate 创建共享文件夹
func (s *Server) handleAdminSharedFoldersCreate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  username := s.getSessionUser(c)
  var req adminSharedFolderCreateReq
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  name := strings.TrimSpace(req.Name)
  description := strings.TrimSpace(req.Description)

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
  if err := store.CreateSharedFolder(name, description, req.IsPublic, username); err != nil {
    // 目录已创建，需要回滚
    os.RemoveAll(shareDir)
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 关联组
  if len(req.GroupIDs) > 0 {
    sf, err := store.GetSharedFolderByName(name)
    if err == nil {
      store.SetSharedFolderGroups(sf.ID, req.GroupIDs)
    }
  }

  writeSuccess(c, "共享文件夹「"+name+"」创建成功")
}

// handleAdminSharedFoldersUpdate 更新共享文件夹
func (s *Server) handleAdminSharedFoldersUpdate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  var req struct {
    ID          int64  `json:"id"`
    Name        string `json:"name"`
    Description string `json:"description"`
    IsPublic    bool   `json:"is_public"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  id := req.ID
  newName := strings.TrimSpace(req.Name)
  description := strings.TrimSpace(req.Description)
  if newName == "" {
    writeError(c, http.StatusBadRequest, "名称不能为空")
    return
  }
  if err := util.SafePathSegment(newName); err != nil {
    writeError(c, http.StatusBadRequest, "名称不合法: "+err.Error())
    return
  }

  // 获取旧记录（用于检测是否需要 mv 目录）
  oldSF, err := store.GetSharedFolder(id)
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
  if err := store.UpdateSharedFolder(id, newName, description, req.IsPublic); err != nil {
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

  var req struct {
    ID int64 `json:"id"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  id := req.ID

  sf, err := store.GetSharedFolder(id)
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
  if err := store.DeleteSharedFolder(id); err != nil {
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

  var req struct {
    FolderID int64   `json:"folder_id"`
    GroupIDs []int64 `json:"group_ids"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  folderID := req.FolderID

  // 获取旧成员
  oldMembers, _ := store.GetSharedFolderMembers(folderID)

  if err := store.SetSharedFolderGroups(folderID, req.GroupIDs); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 计算成员变化
  newMembers, _ := store.GetSharedFolderMembers(folderID)
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

  var req struct {
    FolderID int64  `json:"folder_id"`
    Username string `json:"username"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  folderID := req.FolderID
  testUsername := strings.TrimSpace(req.Username)
  if testUsername == "" {
    writeError(c, http.StatusBadRequest, "用户名不能为空")
    return
  }

  sf, err := store.GetSharedFolder(folderID)
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
    store.RecordMountTest(folderID, testUsername, false)
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
  store.RecordMountTest(folderID, testUsername, mounted)
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

  var req struct {
    FolderID int64 `json:"folder_id"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  folderID := req.FolderID

  if _, err := store.GetSharedFolder(folderID); err != nil {
    writeError(c, http.StatusBadRequest, "共享文件夹不存在")
    return
  }

  members, err := store.GetSharedFolderMembers(folderID)
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


