package web

import (
  "encoding/json"
  "fmt"
  "net/http"
  "strings"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
  "github.com/picoaide/picoaide/internal/ldap"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// JSON 响应辅助
// ============================================================

// apiResponse 是统一的 JSON 响应结构
type apiResponse struct {
  Success bool   `json:"success"`
  Message string `json:"message,omitempty"`
}

// apiError 是带 error 字段的 JSON 响应
type apiError struct {
  Success bool   `json:"success"`
  Error   string `json:"error"`
}

// writeJSON 将 v 序列化为 JSON 并写入响应
func writeJSON(w http.ResponseWriter, statusCode int, v interface{}) {
  w.Header().Set("Content-Type", "application/json; charset=utf-8")
  w.WriteHeader(statusCode)
  json.NewEncoder(w).Encode(v)
}

// writeSuccess 返回成功响应
func writeSuccess(w http.ResponseWriter, message string) {
  writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: message})
}

// writeError 返回错误响应
func writeError(w http.ResponseWriter, statusCode int, errMsg string) {
  writeJSON(w, statusCode, apiError{Success: false, Error: errMsg})
}

// requireAuth 检查登录状态，返回用户名；未登录时自动返回 401
func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) string {
  username := s.getSessionUser(r)
  if username == "" {
    writeError(w, http.StatusUnauthorized, "未登录")
    return ""
  }
  return username
}

// ============================================================
// 认证 Handler
// ============================================================

// handleLogin 处理登录请求（先本地认证，再 LDAP）
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }

  username := r.FormValue("username")
  password := r.FormValue("password")
  if username == "" || password == "" {
    writeError(w, http.StatusBadRequest, "请输入用户名和密码")
    return
  }

  // 1. 本地认证（LDAP 模式下仅允许超管）
  if ok, _, err := auth.AuthenticateLocal(username, password); err == nil && ok {
    if s.cfg.UnifiedAuthEnabled() && !auth.IsSuperadmin(username) {
      // 统一认证模式下，非超管本地用户禁止登录
    } else {
      s.setSessionCookie(w, s.createSessionToken(username), 86400)
      writeJSON(w, http.StatusOK, struct {
        Success  bool   `json:"success"`
        Username string `json:"username"`
      }{
        Success:  true,
        Username: username,
      })
      return
    }
  }

  // 2. LDAP 认证（如果启用）
  if !s.cfg.LDAPEnabled() {
    writeError(w, http.StatusUnauthorized, "用户名或密码错误")
    return
  }

  if !ldap.Authenticate(s.cfg, username, password) {
    writeError(w, http.StatusUnauthorized, "用户名或密码错误")
    return
  }

  whitelist, _ := user.LoadWhitelist()
  if !user.IsWhitelisted(whitelist, username) {
    writeError(w, http.StatusForbidden, "请联系管理员添加白名单")
    return
  }

  s.setSessionCookie(w, s.createSessionToken(username), 86400)
  writeJSON(w, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Username string `json:"username"`
  }{
    Success:  true,
    Username: username,
  })
}

// handleLogout 处理登出请求
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  s.setSessionCookie(w, "", -1)
  writeSuccess(w, "已登出")
}

// handleCSRF 返回当前用户的 CSRF token
func (s *Server) handleCSRF(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  writeJSON(w, http.StatusOK, struct {
    Success   bool   `json:"success"`
    CSRFToken string `json:"csrf_token"`
  }{
    Success:   true,
    CSRFToken: s.csrfToken(username),
  })
}

// ============================================================
// 钉钉配置 Handler
// ============================================================

// handleDingTalk 处理钉钉配置的 GET（读取）和 POST（保存）
func (s *Server) handleDingTalk(w http.ResponseWriter, r *http.Request) {
  if r.Method == "GET" {
    s.handleDingTalkGet(w, r)
    return
  }
  if r.Method == "POST" {
    s.handleDingTalkSave(w, r)
    return
  }
  writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 和 POST 方法")
}

// handleDingTalkGet 返回当前用户的钉钉配置
func (s *Server) handleDingTalkGet(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
    return
  }

  clientID, clientSecret := user.GetDingTalkConfig(s.cfg, username)
  writeJSON(w, http.StatusOK, struct {
    Success      bool   `json:"success"`
    ClientID     string `json:"client_id"`
    ClientSecret string `json:"client_secret"`
  }{
    Success:      true,
    ClientID:     clientID,
    ClientSecret: clientSecret,
  })
}

// handleDingTalkSave 保存钉钉配置并重启容器
func (s *Server) handleDingTalkSave(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
    return
  }

  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  clientID := strings.TrimSpace(r.FormValue("client_id"))
  clientSecret := strings.TrimSpace(r.FormValue("client_secret"))

  if clientID == "" || clientSecret == "" {
    writeError(w, http.StatusBadRequest, "Client ID 和 Client Secret 不能为空")
    return
  }

  if err := user.SaveDingTalkConfig(s.cfg, username, clientID, clientSecret); err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }

  // 重启容器
  rec, _ := auth.GetContainerByUsername(username)
  if rec != nil && rec.ContainerID != "" {
    _ = dockerpkg.Restart(r.Context(), rec.ContainerID)
  }

  writeSuccess(w, "配置已保存，容器正在重启中，请稍候片刻即可使用。")
}

// ============================================================
// 用户信息 & 配置管理 Handler
// ============================================================

// handleUserInfo 返回当前登录用户的信息（角色等）
func (s *Server) handleUserInfo(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  role := auth.GetUserRole(username)
  if role == "" {
    role = "user"
  }

  writeJSON(w, http.StatusOK, struct {
    Success     bool   `json:"success"`
    Username    string `json:"username"`
    Role        string `json:"role"`
    AuthMode    string `json:"auth_mode"`
    UnifiedAuth bool   `json:"unified_auth"`
  }{
    Success:     true,
    Username:    username,
    Role:        role,
    AuthMode:    s.cfg.AuthMode(),
    UnifiedAuth: s.cfg.UnifiedAuthEnabled(),
  })
}

// handleChangePassword 处理用户修改密码（仅本地模式）
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
    return
  }

  if s.cfg.UnifiedAuthEnabled() {
    writeError(w, http.StatusForbidden, "统一认证模式下不允许修改密码，请联系管理员")
    return
  }

  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }

  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  oldPassword := r.FormValue("old_password")
  newPassword := r.FormValue("new_password")
  if oldPassword == "" || newPassword == "" {
    writeError(w, http.StatusBadRequest, "请输入旧密码和新密码")
    return
  }
  if len(newPassword) < 6 {
    writeError(w, http.StatusBadRequest, "新密码至少 6 个字符")
    return
  }

  // 验证旧密码
  ok, _, err := auth.AuthenticateLocal(username, oldPassword)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "验证密码失败")
    return
  }
  if !ok {
    writeError(w, http.StatusUnauthorized, "旧密码错误")
    return
  }

  if err := auth.ChangePassword(username, newPassword); err != nil {
    writeError(w, http.StatusInternalServerError, "修改密码失败: "+err.Error())
    return
  }

  writeSuccess(w, "密码修改成功")
}

// isSuperadmin 检查请求的用户是否是超管
func (s *Server) isSuperadmin(r *http.Request) bool {
  username := s.getSessionUser(r)
  if username == "" {
    return false
  }
  return auth.IsSuperadmin(username)
}

// handleConfig 处理配置文件的 GET（读取）和 POST（保存），仅超管可用
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
    return
  }

  // 检查超管权限
  if !auth.IsSuperadmin(username) {
    writeError(w, http.StatusForbidden, "仅超级管理员可访问")
    return
  }

  if r.Method == "GET" {
    s.handleConfigGet(w, r)
    return
  }
  if r.Method == "POST" {
    s.handleConfigSave(w, r)
    return
  }
  writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 和 POST 方法")
}

// handleConfigGet 从数据库读取配置并返回为 JSON
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
  raw, err := config.LoadRawFromDB()
  if err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }
  data, err := json.MarshalIndent(raw, "", "  ")
  if err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }
  w.Header().Set("Content-Type", "application/json; charset=utf-8")
  w.Write(data)
}

// handleConfigSave 从 JSON 保存配置到数据库
func (s *Server) handleConfigSave(w http.ResponseWriter, r *http.Request) {
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  if err := r.ParseForm(); err != nil {
    writeError(w, http.StatusBadRequest, "解析请求失败")
    return
  }

  jsonStr := r.FormValue("config")
  if jsonStr == "" {
    writeError(w, http.StatusBadRequest, "配置内容不能为空")
    return
  }

  var raw map[string]interface{}
  if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
    writeError(w, http.StatusBadRequest, fmt.Sprintf("JSON 格式错误: %v", err))
    return
  }

  changedBy := s.getSessionUser(r)
  if changedBy == "" {
    changedBy = "admin"
  }
  if err := config.SaveRawToDB(raw, changedBy); err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }

  writeSuccess(w, "配置已保存")
}
