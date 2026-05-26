package web

import (
  "bufio"
  "encoding/json"
  "fmt"
  "net/http"
  "os"
  "path/filepath"
  "strconv"
  "strings"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/authsource"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/logger"
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
func writeJSON(c *gin.Context, statusCode int, v interface{}) {
  c.JSON(statusCode, v)
}

// writeSuccess 返回成功响应
func writeSuccess(c *gin.Context, message string) {
  writeJSON(c, http.StatusOK, apiResponse{Success: true, Message: message})
}

// writeError 返回错误响应
func writeError(c *gin.Context, statusCode int, errMsg string) {
  writeJSON(c, statusCode, apiError{Success: false, Error: errMsg})
}

// requireAuth 检查登录状态，返回用户名；未登录时自动返回 401
func (s *Server) requireAuth(c *gin.Context) string {
  username := s.getSessionUser(c)
  if username == "" {
    writeError(c, http.StatusUnauthorized, "未登录")
    return ""
  }
  return username
}

func (s *Server) requireNonSuperadmin(c *gin.Context) string {
  username := s.requireAuth(c)
  if username == "" {
    return ""
  }
  if auth.IsSuperadmin(username) {
    writeError(c, http.StatusForbidden, "超管用户不允许登录插件，使用普通用户登录")
    return ""
  }
  return username
}

func (s *Server) requireRegularUser(c *gin.Context) string {
  username := s.requireAuth(c)
  if username == "" {
    return ""
  }
  if auth.IsSuperadmin(username) {
    writeError(c, http.StatusForbidden, "管理员没有普通用户配置权限，请进入管理后台")
    return ""
  }
  return username
}

func (s *Server) isExtensionRequest(c *gin.Context) bool {
  origin := c.GetHeader("Origin")
  return strings.HasPrefix(origin, "chrome-extension://") || strings.HasPrefix(origin, "moz-extension://")
}

// ============================================================
// 健康检查 Handler
// ============================================================

func (s *Server) handleHealth(c *gin.Context) {
  writeJSON(c, http.StatusOK, struct {
    Status  string `json:"status"`
    Version string `json:"version"`
  }{
    Status:  "ok",
    Version: config.Version,
  })
}

// handleVersion 返回 API 版本信息
func (s *Server) handleVersion(c *gin.Context) {
  writeJSON(c, http.StatusOK, gin.H{
    "current":   "v1",
    "supported": []string{"v1"},
    "version":   config.Version,
  })
}

// ============================================================
// 认证 Handler
// ============================================================

func (s *Server) handleLoginMode(c *gin.Context) {
  writeJSON(c, http.StatusOK, struct {
    Success  bool                  `json:"success"`
    AuthMode string                `json:"auth_mode"`
    Provider authsource.ProviderMeta `json:"provider"`
  }{true, s.loadConfig().AuthMode(), authsource.ActiveProviderMeta(s.loadConfig())})
}

// handleLogin 处理用户名密码登录请求
// 流程：认证 → 超管逃生通道 → 本地/外部用户分流，不依赖具体认证源名称
func (s *Server) handleLogin(c *gin.Context) {
  username := c.PostForm("username")
  password := c.PostForm("password")
  logger.DebugRecv("POST", "/api/login", "username", username)
  if username == "" || password == "" {
    writeError(c, http.StatusBadRequest, "请输入用户名和密码")
    return
  }

  isSuperadmin := auth.IsSuperadmin(username)
  if s.isExtensionRequest(c) && isSuperadmin {
    logger.DebugSend("POST", "/api/login", http.StatusForbidden, "reason", "superadmin_extension_blocked")
    writeError(c, http.StatusForbidden, "超管用户不允许登录插件，使用普通用户登录")
    return
  }

  // 1. 通过当前认证源认证（local/ldap/任何 PasswordProvider）
  logger.DebugProcess("authenticate", "username", username, "auth_mode", s.loadConfig().AuthMode())
  authenticated := authsource.Authenticate(s.loadConfig(), username, password)

  // 2. 超管逃生通道：当前认证源认证失败时，尝试本地密码
  if !authenticated && isSuperadmin {
    logger.DebugProcess("superadmin_fallback", "username", username)
    ok, _, err := auth.AuthenticateLocal(username, password)
    authenticated = (err == nil && ok)
  }

  if !authenticated {
    if !authsource.HasPasswordProvider(s.loadConfig()) {
      writeError(c, http.StatusBadRequest, "当前认证方式不支持密码登录，请使用 SSO 登录")
      return
    }
    logger.Audit("user.login_failed", "username", username, "reason", "invalid_credentials")
    writeError(c, http.StatusUnauthorized, "用户名或密码错误")
    return
  }

  authMode := s.loadConfig().AuthMode()

  // 场景 A：本地模式或超管 → 直接登录
  if isSuperadmin || !s.loadConfig().UnifiedAuthEnabled() {
    if !isSuperadmin {
      logger.DebugProcess("init_user", "username", username)
      if err := s.initializeUser(username); err != nil {
        writeError(c, http.StatusInternalServerError, "初始化用户失败: "+err.Error())
        return
      }
    }
    s.setSessionCookie(c, s.createSessionToken(username), 86400)
    logger.Audit("user.login", "username", username, "method", "local")
    logger.DebugSend("POST", "/api/login", http.StatusOK, "username", username, "method", "local")

    // 超管首次登录成功，删除 secret 文件
    if isSuperadmin {
      if wd, err := os.Getwd(); err == nil {
        os.Remove(filepath.Join(wd, "secret"))
      }
    }

    writeJSON(c, http.StatusOK, struct {
      Success  bool   `json:"success"`
      Username string `json:"username"`
    }{
      Success:  true,
      Username: username,
    })
    return
  }

  // 场景 B：统一认证模式下的外部用户
  logger.DebugProcess("whitelist_check", "username", username, "auth_mode", authMode)
  if !user.AllowedByWhitelist(s.loadConfig(), authMode, username) {
    logger.DebugSend("POST", "/api/login", http.StatusForbidden, "reason", "whitelist_denied")
    writeError(c, http.StatusForbidden, "请联系管理员添加白名单")
    return
  }
  logger.DebugProcess("ensure_external_user", "username", username, "auth_mode", authMode)
  if err := auth.EnsureExternalUser(username, "user", authMode); err != nil {
    writeError(c, http.StatusInternalServerError, "同步用户失败: "+err.Error())
    return
  }

  if err := s.initializeUser(username); err != nil {
    logger.Audit("user.init_failed", "username", username, "method", authMode, "error", err.Error())
    writeError(c, http.StatusInternalServerError, "登录成功但初始化账号失败: "+err.Error())
    return
  }

  // 异步同步用户的组（所有支持 DirectoryProvider 的认证源通用）
  if authsource.HasDirectoryProvider(s.loadConfig()) {
    go func() {
      if groups, err := authsource.FetchUserGroups(s.loadConfig(), username); err == nil && len(groups) > 0 {
        auth.SyncUserGroups(username, groups, authMode)
      }
    }()
  }

  s.setSessionCookie(c, s.createSessionToken(username), 86400)
  logger.Audit("user.login", "username", username, "method", authMode)
  logger.DebugSend("POST", "/api/login", http.StatusOK, "username", username, "method", authMode)
  writeJSON(c, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Username string `json:"username"`
  }{
    Success:  true,
    Username: username,
  })
}

// handleAuthStart 启动浏览器认证流程（统一入口，替代 OIDC 专属路径）
func (s *Server) handleAuthStart(c *gin.Context) {
  if !authsource.HasBrowserProvider(s.loadConfig()) {
    writeError(c, http.StatusBadRequest, "当前认证方式不支持浏览器登录")
    return
  }
  state, err := randomHex(16)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "生成 state 失败")
    return
  }
  authURL, err := authsource.AuthURL(s.loadConfig(), state)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  http.SetCookie(c.Writer, &http.Cookie{
    Name:     "auth_state",
    Value:    state,
    Path:     "/api/login/callback",
    MaxAge:   600,
    HttpOnly: true,
    Secure:   s.loadConfig().Web.TLS.Enabled || c.GetHeader("X-Forwarded-Proto") == "https",
  })
  c.Redirect(http.StatusFound, authURL)
}

// handleAuthCallback 处理浏览器认证回调（统一入口，替代 OIDC 专属回调）
func (s *Server) handleAuthCallback(c *gin.Context) {
  if !authsource.HasBrowserProvider(s.loadConfig()) {
    writeError(c, http.StatusBadRequest, "当前认证方式不支持浏览器回调")
    return
  }
  stateCookie, err := c.Cookie("auth_state")
  if err != nil || stateCookie == "" || stateCookie != c.Query("state") {
    writeError(c, http.StatusForbidden, "state 无效")
    return
  }
  http.SetCookie(c.Writer, &http.Cookie{
    Name: "auth_state", Value: "", Path: "/api/login/callback", MaxAge: -1, HttpOnly: true, Secure: s.loadConfig().Web.TLS.Enabled || c.GetHeader("X-Forwarded-Proto") == "https",
  })

  identity, err := authsource.CompleteLogin(c.Request.Context(), s.loadConfig(), c.Query("code"))
  if err != nil {
    writeError(c, http.StatusUnauthorized, err.Error())
    return
  }

  authMode := s.loadConfig().AuthMode()
  if err := user.ValidateUsername(identity.Username); err != nil {
    writeError(c, http.StatusBadRequest, "用户名不合法: "+err.Error())
    return
  }
  if !user.AllowedByWhitelist(s.loadConfig(), authMode, identity.Username) {
    writeError(c, http.StatusForbidden, "请联系管理员添加白名单")
    return
  }
  if err := auth.EnsureExternalUser(identity.Username, "user", authMode); err != nil {
    writeError(c, http.StatusInternalServerError, "同步用户失败: "+err.Error())
    return
  }
  if len(identity.Groups) > 0 {
    _ = auth.SyncUserGroups(identity.Username, identity.Groups, authMode)
  }

  if err := s.initializeUser(identity.Username); err != nil {
    writeError(c, http.StatusInternalServerError, "登录成功，但初始化账号失败: "+err.Error())
    return
  }

  s.setSessionCookie(c, s.createSessionToken(identity.Username), 86400)
  logger.Audit("user.login", "username", identity.Username, "method", authMode)
  c.Redirect(http.StatusFound, "/manage")
}

// handleLogout 处理登出请求
func (s *Server) handleLogout(c *gin.Context) {
  username := s.getSessionUser(c)
  logger.DebugRecv("POST", "/api/logout", "username", username)
  s.setSessionCookie(c, "", -1)
  if username != "" {
    logger.Audit("user.logout", "username", username)
  }
  logger.DebugSend("POST", "/api/logout", http.StatusOK)
  writeSuccess(c, "已登出")
}

// handleCSRF 返回当前用户的 CSRF token
func (s *Server) handleCSRF(c *gin.Context) {
  username := s.requireAuth(c)
  if username == "" {
    return
  }
  writeJSON(c, http.StatusOK, struct {
    Success   bool   `json:"success"`
    CSRFToken string `json:"csrf_token"`
  }{
    Success:   true,
    CSRFToken: s.csrfToken(username),
  })
}

// ============================================================
// Cookie 同步 Handler
// ============================================================

// handleCookies 将当前页面的 Cookie 写入数据库
func (s *Server) handleCookies(c *gin.Context) {
  username := s.requireNonSuperadmin(c)
  if username == "" {
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  domain := strings.TrimSpace(c.PostForm("domain"))
  cookieStr := strings.TrimSpace(c.PostForm("cookies"))

  if domain == "" || cookieStr == "" {
    writeError(c, http.StatusBadRequest, "域名和 Cookie 不能为空")
    return
  }

  if err := user.SyncCookies(s.loadConfig(), username, domain, cookieStr); err != nil {
    writeError(c, http.StatusInternalServerError, "同步失败: "+err.Error())
    return
  }

  writeSuccess(c, "已同步 "+domain+" 的登录状态")
}

// handleUserCookies 返回当前用户所有已授权的域名列表
func (s *Server) handleUserCookies(c *gin.Context) {
  username := s.requireAuth(c)
  if username == "" {
    return
  }

  entries, err := auth.ListCookieDomains(username)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "读取失败")
    return
  }

  // 不暴露 cookie 值给前端
  type cookieInfo struct {
    Domain    string `json:"domain"`
    UpdatedAt string `json:"updated_at"`
  }
  list := make([]cookieInfo, len(entries))
  for i, e := range entries {
    list[i] = cookieInfo{Domain: e.Domain, UpdatedAt: e.UpdatedAt}
  }

  writeJSON(c, http.StatusOK, gin.H{
    "success": true,
    "list":    list,
  })
}

// handleUserCookiesDelete 取消某个域名的授权
func (s *Server) handleUserCookiesDelete(c *gin.Context) {
  username := s.requireAuth(c)
  if username == "" {
    return
  }

  domain := strings.TrimSpace(c.PostForm("domain"))
  if domain == "" {
    writeError(c, http.StatusBadRequest, "域名不能为空")
    return
  }

  if err := auth.DeleteCookie(username, domain); err != nil {
    writeError(c, http.StatusInternalServerError, "删除失败: "+err.Error())
    return
  }

  writeSuccess(c, "已取消 "+domain+" 的授权")
}

// ============================================================
// 钉钉配置 Handler
// ============================================================

// ============================================================
// 用户信息 & 配置管理 Handler
// ============================================================

// handleUserInfo 返回当前登录用户的信息（角色等）
func (s *Server) handleUserInfo(c *gin.Context) {
  username := s.requireAuth(c)
  if username == "" {
    return
  }
  c.JSON(200, gin.H{
    "success":  true,
    "username": username,
    "role":     auth.GetUserRole(username),
    "source":   auth.GetUserSource(username),
  })
}

// handleUserInitStatus 返回用户目录初始化状态
func (s *Server) handleUserInitStatus(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }
  c.JSON(200, gin.H{
    "success": true,
    "ready":   true,
    "status":  "running",
  })
}

type chatMessage struct {
  Role    string `json:"role"`
  Content string `json:"content"`
}

// handleChatHistory 返回当前用户的完整对话历史
func (s *Server) handleChatHistory(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  workspace := filepath.Join(config.WorkDir(), "users", username)
  user.InitializeUser(filepath.Join(config.WorkDir(), "user-template"), filepath.Join(config.WorkDir(), "users"), username)

  // 读取最新会话的 live.jsonl
  var messages []chatMessage
  sessDir := filepath.Join(workspace, "sessions")
  if !strings.HasPrefix(filepath.Clean(sessDir), filepath.Clean(config.WorkDir())+string(os.PathSeparator)) {
    writeError(c, http.StatusForbidden, "访问被拒绝")
    return
  }
  if entries, err := os.ReadDir(sessDir); err == nil {
    for _, entry := range entries {
      if !entry.IsDir() {
        continue
      }
      liveFile := filepath.Join(sessDir, entry.Name(), "live.jsonl")
      if !strings.HasPrefix(filepath.Clean(liveFile), sessDir+string(os.PathSeparator)) {
        continue
      }
      f, err := os.Open(liveFile)
      if err != nil {
        continue
      }
      scanner := bufio.NewScanner(f)
      for scanner.Scan() {
        var msg chatMessage
        if err := json.Unmarshal([]byte(scanner.Text()), &msg); err == nil {
          messages = append(messages, msg)
        }
      }
      f.Close()
      break // 只读第一个会话目录
    }
  }
  if messages == nil {
    messages = []chatMessage{}
  }

  writeJSON(c, http.StatusOK, struct {
    Success  bool          `json:"success"`
    Ready    bool          `json:"ready"`
    Messages []chatMessage `json:"messages"`
  }{
    Success:  true,
    Ready:    true,
    Messages: messages,
  })
}

// handleChangePassword 处理用户修改密码（仅本地模式）
func (s *Server) handleChangePassword(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  if s.loadConfig().UnifiedAuthEnabled() {
    writeError(c, http.StatusForbidden, "非本地用户不支持修改密码，请联系管理员在公司认证中心修改")
    return
  }

  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  oldPassword := c.PostForm("old_password")
  newPassword := c.PostForm("new_password")
  if oldPassword == "" || newPassword == "" {
    writeError(c, http.StatusBadRequest, "请输入旧密码和新密码")
    return
  }
  if len(newPassword) < 6 {
    writeError(c, http.StatusBadRequest, "新密码至少 6 个字符")
    return
  }

  // 验证旧密码
  ok, _, err := auth.AuthenticateLocal(username, oldPassword)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "验证密码失败")
    return
  }
  if !ok {
    writeError(c, http.StatusUnauthorized, "旧密码错误")
    return
  }

  if err := auth.ChangePassword(username, newPassword); err != nil {
    writeError(c, http.StatusInternalServerError, "修改密码失败: "+err.Error())
    return
  }

  writeSuccess(c, "密码修改成功")
  logger.Audit("password.change", "username", username)
}

// handleConfigGet 从数据库读取配置并返回为 JSON
func (s *Server) handleConfigGet(c *gin.Context) {
  username := s.requireAuth(c)
  if username == "" {
    return
  }

  // 检查超管权限
  if !auth.IsSuperadmin(username) {
    writeError(c, http.StatusForbidden, "仅超级管理员可访问")
    return
  }

  raw, err := config.LoadRawFromDB()
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  data, err := json.MarshalIndent(raw, "", "  ")
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

// handleConfigSave 从 JSON 保存配置到数据库
func (s *Server) handleConfigSave(c *gin.Context) {
  username := s.requireAuth(c)
  if username == "" {
    return
  }
  logger.DebugRecv("POST", "/api/config", "operator", username)

  // 检查超管权限
  if !auth.IsSuperadmin(username) {
    writeError(c, http.StatusForbidden, "仅超级管理员可访问")
    return
  }

  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  jsonStr := c.PostForm("config")
  if jsonStr == "" {
    writeError(c, http.StatusBadRequest, "配置内容不能为空")
    return
  }

  var raw map[string]interface{}
  if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
    writeError(c, http.StatusBadRequest, fmt.Sprintf("JSON 格式错误: %v", err))
    return
  }

  oldMode := s.loadConfig().AuthMode()
  oldCfg := *s.loadConfig()
  newMode := authModeFromRaw(raw, oldMode)
  if !validAuthMode(newMode) {
    writeError(c, http.StatusBadRequest, "不支持的认证方式: "+newMode)
    return
  }
  normalizeAuthModeInRaw(raw, newMode)

  changedBy := s.getSessionUser(c)
  if changedBy == "" {
    changedBy = "admin"
  }
  logger.DebugProcess("save_config", "operator", changedBy, "auth_mode_change", oldMode != newMode, "old_mode", oldMode, "new_mode", newMode)
  if err := config.SaveRawToDB(raw, changedBy); err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }

  // 重新加载内存配置，确保后续下发操作使用最新值
  if newCfg, err := config.LoadFromDB(); err == nil {
    s.cfg.Store(newCfg)
    // 运行时切换 debug 模式
    if newCfg.Web.DebugMode {
      logger.EnableDebug()
    } else {
      logger.DisableDebug()
    }
  }

  var cleanup *authProviderSwitchCleanupResult
  if oldMode != newMode {
    var err error
    cleanup, err = s.purgeOrdinaryAuthProviderStateForConfig(&oldCfg)
    if err != nil {
      writeError(c, http.StatusInternalServerError, "认证方式已保存，但清理旧认证数据失败: "+err.Error())
      return
    }
    logger.Audit("auth.provider_switch", "operator", changedBy, "old_mode", oldMode, "new_mode", newMode, "users_removed", cleanup.UsersRemoved)
  }

  // 实时重启定时同步（间隔或认证模式改变后立即生效，无需重启服务）
  s.restartSyncTimer()

  if cleanup != nil {
    writeJSON(c, http.StatusOK, struct {
      Success bool                             `json:"success"`
      Message string                           `json:"message"`
      Cleanup *authProviderSwitchCleanupResult `json:"cleanup"`
    }{true, "配置已保存，认证方式已切换并清空旧认证数据", cleanup})
  } else {
    writeSuccess(c, "配置已保存")
  }
  logger.Audit("config.save", "operator", changedBy)
}

func authModeFromRaw(raw map[string]interface{}, fallback string) string {
  web, ok := raw["web"].(map[string]interface{})
  if !ok {
    return fallback
  }
  if mode, ok := web["auth_mode"].(string); ok && strings.TrimSpace(mode) != "" {
    return strings.ToLower(strings.TrimSpace(mode))
  }
  if enabled, ok := web["ldap_enabled"].(bool); ok {
    if enabled {
      return "ldap"
    }
    return "local"
  }
  if enabled, ok := web["ldap_enabled"].(string); ok {
    b, _ := strconv.ParseBool(enabled)
    if b {
      return "ldap"
    }
    return "local"
  }
  return fallback
}

func validAuthMode(mode string) bool {
  for _, name := range authsource.RegisteredProviderNames() {
    if name == mode {
      return true
    }
  }
  return false
}

func normalizeAuthModeInRaw(raw map[string]interface{}, mode string) {
  web, ok := raw["web"].(map[string]interface{})
  if !ok {
    web = map[string]interface{}{}
    raw["web"] = web
  }
  web["auth_mode"] = mode
  web["ldap_enabled"] = mode == "ldap"
}
