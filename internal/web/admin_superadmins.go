package web

import (
  "net/http"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/logger"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// 超管账户管理
// ============================================================

// handleAdminSuperadmins 返回超管列表
func (s *Server) handleAdminSuperadmins(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method == "GET" {
    list, err := auth.GetSuperadmins()
    if err != nil {
      writeError(c, http.StatusInternalServerError, err.Error())
      return
    }
    if list == nil {
      list = []string{}
    }
    writeJSON(c, http.StatusOK, struct {
      Success bool     `json:"success"`
      Admins  []string `json:"admins"`
    }{true, list})
    return
  }
  writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
}

// handleAdminSuperadminCreate 创建超管账户
func (s *Server) handleAdminSuperadminCreate(c *gin.Context) {
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

  username := c.PostForm("username")
  if err := user.ValidateUsername(username); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  if auth.UserExists(username) {
    writeError(c, http.StatusBadRequest, "用户 "+username+" 已存在")
    return
  }

  password := auth.GenerateRandomPassword(12)
  if err := auth.CreateUser(username, password, "superadmin"); err != nil {
    writeError(c, http.StatusInternalServerError, "创建超管失败: "+err.Error())
    return
  }

  writeJSON(c, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Message  string `json:"message"`
    Username string `json:"username"`
    Password string `json:"password"`
  }{true, "超管创建成功", username, password})
  logger.Audit("superadmin.create", "username", username, "operator", s.getSessionUser(c))
}

// handleAdminSuperadminDelete 删除超管账户（至少保留一个）
func (s *Server) handleAdminSuperadminDelete(c *gin.Context) {
  currentUser := s.requireSuperadmin(c)
  if currentUser == "" {
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

  username := c.PostForm("username")
  if username == "" {
    writeError(c, http.StatusBadRequest, "用户名不能为空")
    return
  }
  if username == currentUser {
    writeError(c, http.StatusBadRequest, "不能删除自己")
    return
  }
  if err := user.ValidateUsername(username); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  if !auth.IsSuperadmin(username) {
    writeError(c, http.StatusBadRequest, username+" 不是超管")
    return
  }

  admins, _ := auth.GetSuperadmins()
  if len(admins) <= 1 {
    writeError(c, http.StatusBadRequest, "至少保留一个超管账户")
    return
  }

  if err := auth.DeleteUser(username); err != nil {
    writeError(c, http.StatusInternalServerError, "删除失败: "+err.Error())
    return
  }

  writeSuccess(c, "超管 "+username+" 已删除")
  logger.Audit("superadmin.delete", "username", username, "operator", currentUser)
}

// handleAdminSuperadminReset 重置超管密码
func (s *Server) handleAdminSuperadminReset(c *gin.Context) {
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

  username := c.PostForm("username")
  if username == "" {
    writeError(c, http.StatusBadRequest, "用户名不能为空")
    return
  }
  if err := user.ValidateUsername(username); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  if !auth.IsSuperadmin(username) {
    writeError(c, http.StatusBadRequest, username+" 不是超管")
    return
  }

  password := auth.GenerateRandomPassword(12)
  if err := auth.ChangePassword(username, password); err != nil {
    writeError(c, http.StatusInternalServerError, "重置密码失败: "+err.Error())
    return
  }

  writeJSON(c, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Message  string `json:"message"`
    Password string `json:"password"`
  }{true, "密码已重置", password})
  logger.Audit("password.reset", "username", username, "operator", s.getSessionUser(c))
}
