package web

import (
  "net/http"
  "strconv"
  "strings"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/store"
)

// ============================================================
// 知识库管理 — 超管 API
// ============================================================

// handleAdminKBList 列表全部知识库
func (s *Server) handleAdminKBList(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  kbs, err := store.ListKnowledgeBases()
  if err != nil {
    writeError(c, 500, "获取知识库列表失败")
    return
  }
  writeJSON(c, 200, gin.H{"success": true, "data": kbs})
}

type adminKBCreateReq struct {
  Name        string `json:"name"`
  Description string `json:"description"`
}

// handleAdminKBCreate 创建知识库
func (s *Server) handleAdminKBCreate(c *gin.Context) {
  username := s.requireSuperadmin(c)
  if username == "" {
    return
  }
  var req adminKBCreateReq
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  name := strings.TrimSpace(req.Name)
  desc := strings.TrimSpace(req.Description)
  if name == "" {
    writeError(c, http.StatusBadRequest, "名称不能为空")
    return
  }
  kb, err := store.CreateKnowledgeBase(name, desc, username)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": kb})
}

type adminKBUpdateReq struct {
  Name        string `json:"name"`
  Description string `json:"description"`
}

// handleAdminKBUpdate 更新知识库
func (s *Server) handleAdminKBUpdate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  idStr := c.Param("id")
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }
  var req adminKBUpdateReq
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  name := strings.TrimSpace(req.Name)
  desc := strings.TrimSpace(req.Description)
  if name == "" {
    writeError(c, http.StatusBadRequest, "名称不能为空")
    return
  }
  if err := store.UpdateKnowledgeBase(id, name, desc); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}

// handleAdminKBDelete 删除知识库
func (s *Server) handleAdminKBDelete(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  idStr := c.Param("id")
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }
  if err := store.DeleteKnowledgeBase(id); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}

// handleAdminFolderTree 获取文件夹树（扁平列表，前端构造树）
func (s *Server) handleAdminFolderTree(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  idStr := c.Param("id")
  kbID, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }
  folders, err := store.GetFolderTree(kbID)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "获取文件夹树失败")
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": folders})
}

type adminFolderCreateReq struct {
  ParentID int64  `json:"parent_id"`
  Name     string `json:"name"`
}

// handleAdminFolderCreate 创建文件夹
func (s *Server) handleAdminFolderCreate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  idStr := c.Param("id")
  kbID, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }
  var req adminFolderCreateReq
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  name := strings.TrimSpace(req.Name)
  if name == "" {
    writeError(c, http.StatusBadRequest, "名称不能为空")
    return
  }
  var parentID *int64
  if req.ParentID > 0 {
    parentID = &req.ParentID
  }
  folder, err := store.CreateFolder(kbID, parentID, name)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": folder})
}

type adminFolderUpdateReq struct {
  Name     string `json:"name"`
  ParentID *int64 `json:"parent_id"`
}

// handleAdminFolderUpdate 更新文件夹
func (s *Server) handleAdminFolderUpdate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  idStr := c.Param("id")
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }
  var req adminFolderUpdateReq
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  name := strings.TrimSpace(req.Name)
  if name == "" {
    writeError(c, http.StatusBadRequest, "名称不能为空")
    return
  }
  if err := store.UpdateFolder(id, name, req.ParentID); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}

// handleAdminFolderDelete 删除文件夹
func (s *Server) handleAdminFolderDelete(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  idStr := c.Param("id")
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }
  if err := store.DeleteFolder(id); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}

// handleAdminFolderPermissions 获取文件夹权限
func (s *Server) handleAdminFolderPermissions(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  idStr := c.Param("id")
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }
  users, groups, err := store.GetFolderPermissions(id)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "获取权限失败")
    return
  }
  var folder store.KBFolder
  if e, eErr := store.GetEngine(); eErr == nil {
    _, _ = e.Where("id = ?", id).Get(&folder)
  }
  permSet := folder.PermissionsSet
  writeJSON(c, http.StatusOK, gin.H{
    "success":         true,
    "users":           users,
    "groups":          groups,
    "permissions_set": permSet,
  })
}

type adminFolderSetPermissionsReq struct {
  Users          []string `json:"users"`
  Groups         []int64  `json:"groups"`
  PermissionsSet int      `json:"permissions_set"`
}

// handleAdminFolderSetPermissions 设置文件夹权限
func (s *Server) handleAdminFolderSetPermissions(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  idStr := c.Param("id")
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }
  var req adminFolderSetPermissionsReq
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }

  // Clear existing permissions and set new ones in a transaction
  e, err := store.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }
  session := e.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    writeError(c, http.StatusInternalServerError, "事务开始失败")
    return
  }
  if _, err := session.Where("folder_id = ?", id).Delete(&store.KBFolderUser{}); err != nil {
    session.Rollback()
    writeError(c, http.StatusInternalServerError, "清除用户权限失败")
    return
  }
  if _, err := session.Where("folder_id = ?", id).Delete(&store.KBFolderGroup{}); err != nil {
    session.Rollback()
    writeError(c, http.StatusInternalServerError, "清除组权限失败")
    return
  }
  for _, u := range req.Users {
    if _, err := session.Insert(&store.KBFolderUser{FolderID: id, Username: u}); err != nil {
      session.Rollback()
      writeError(c, http.StatusInternalServerError, "添加用户权限失败")
      return
    }
  }
  for _, g := range req.Groups {
    if _, err := session.Insert(&store.KBFolderGroup{FolderID: id, GroupID: g}); err != nil {
      session.Rollback()
      writeError(c, http.StatusInternalServerError, "添加组权限失败")
      return
    }
  }
  if _, err := session.Where("id = ?", id).Cols("permissions_set").Update(&store.KBFolder{PermissionsSet: req.PermissionsSet}); err != nil {
    session.Rollback()
    writeError(c, http.StatusInternalServerError, "更新权限标记失败")
    return
  }
  if err := session.Commit(); err != nil {
    writeError(c, http.StatusInternalServerError, "事务提交失败")
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}

// handleAdminFolderDocuments 获取文件夹内的文档列表
func (s *Server) handleAdminFolderDocuments(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  idStr := c.Param("id")
  folderID, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }
  e, err := store.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }
  rows, err := e.SQL("SELECT id, kb_id, folder_id, title, file_type, file_size, created_by, created_at, updated_at FROM kb_documents WHERE folder_id = ? ORDER BY title", folderID).QueryString()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "获取文档列表失败")
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": rows})
}
