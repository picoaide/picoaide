package web

import (
  "context"
  "crypto/hmac"
  "crypto/rand"
  "crypto/sha256"
  "crypto/tls"
  "encoding/hex"
  "fmt"
  "github.com/gin-gonic/gin"
  "log/slog"
  "net"
  "net/http"
  "net/url"
  "os"
  "os/signal"
  "path/filepath"
  "strconv"
  "strings"
  "sync"
  "sync/atomic"
  "syscall"
  "time"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/authsource"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/logger"
  "github.com/picoaide/picoaide/internal/sandbox"
  "github.com/picoaide/picoaide/internal/skill"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// Web 服务器
// ============================================================

// Server 是 Web 管理面板服务器
type Server struct {
  cfg              atomic.Pointer[config.GlobalConfig]
  configMu         sync.Mutex // 保护 Skills 等共享指针字段的写操作
  secret           string
  csrfKey          string
  loginLimiter     *rateLimiter
  syncCancel       context.CancelFunc
  syncMu           sync.Mutex
  agentIntegration *AgentIntegration
  tlsSrv           *http.Server // TLS 服务器，用于优雅关闭
}

// loadConfig 返回当前配置指针（原子读取）
func (s *Server) loadConfig() *config.GlobalConfig {
  return s.cfg.Load()
}

const sessionSecretSettingKey = "internal.session_secret"

func randomHex(bytesLen int) (string, error) {
  b := make([]byte, bytesLen)
  if _, err := rand.Read(b); err != nil {
    return "", err
  }
  return hex.EncodeToString(b), nil
}

func ensureSessionSecret() (string, error) {
  engine, err := auth.GetEngine()
  if err != nil {
    return "", err
  }
  if _, err := engine.Exec("DELETE FROM settings WHERE key = ?", "web.password"); err != nil {
    return "", err
  }
  var setting auth.Setting
  has, err := engine.Where("key = ?", sessionSecretSettingKey).Get(&setting)
  if err != nil {
    return "", err
  }
  if has && setting.Value != "" {
    return setting.Value, nil
  }

  secret, err := randomHex(32)
  if err != nil {
    return "", fmt.Errorf("生成 session 密钥失败: %w", err)
  }
  if _, err := engine.Exec(
    "INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now','localtime'))",
    sessionSecretSettingKey,
    secret,
  ); err != nil {
    return "", fmt.Errorf("保存 session 密钥失败: %w", err)
  }
  slog.Info("已生成持久化 session 密钥")
  return secret, nil
}

// syncAuto 自动同步用户目录和组（与手动同步相同的逻辑，含清理过期账号）
func (s *Server) syncAuto() {
  if !authsource.HasDirectoryProvider(s.loadConfig()) {
    return
  }

  authMode := s.loadConfig().AuthMode()

  // 同步用户目录（与手动同步相同的逻辑，含清理过期账号）
  result, err := s.syncUsersFromDirectory(true)
  if err != nil {
    slog.Error("自动同步用户失败", "auth_mode", authMode, "error", err)
  } else {
    slog.Info("自动同步用户完成", "auth_mode", authMode, "synced", result.LocalUserSynced, "allowed", result.AllowedUserCount, "initialized", result.InitializedCount, "archived", result.ArchivedStaleUsers, "deleted_local_auth", result.DeletedLocalAuth)
  }

  // 同步组
  groupResult, err := authsource.SyncGroups(authMode, s.loadConfig(), func(username string) error {
    if !auth.UserExists(username) {
      return s.initializeUser(username)
    }
    return nil
  })
  if err != nil {
    slog.Error("自动同步组失败", "auth_mode", authMode, "error", err)
    return
  }
  s.syncGroupParents(groupResult.Hierarchy)
  slog.Info("自动同步组完成", "auth_mode", authMode, "groups", groupResult.GroupCount, "members", groupResult.MemberCount)
}

// restartSyncTimer 重启定时同步任务，使用当前配置的间隔。保存配置时自动调用。
func (s *Server) restartSyncTimer() {
  s.syncMu.Lock()
  defer s.syncMu.Unlock()

  if s.syncCancel != nil {
    s.syncCancel()
    s.syncCancel = nil
  }

  interval := s.loadConfig().SyncIntervalDuration()
  if interval > 0 && s.loadConfig().UnifiedAuthEnabled() && authsource.HasDirectoryProvider(s.loadConfig()) {
    ctx, cancel := context.WithCancel(context.Background())
    s.syncCancel = cancel
    go func() {
      ticker := time.NewTicker(interval)
      defer ticker.Stop()
      slog.Info("定时同步已启动", "interval", interval, "auth_mode", s.loadConfig().AuthMode())
      // 启动时立即执行首次同步
      s.syncAuto()
      for {
        select {
        case <-ticker.C:
          s.syncAuto()
        case <-ctx.Done():
          slog.Info("定时同步已停止")
          return
        }
      }
    }()
  }
}

func (s *Server) syncGroupParents(groupMap authsource.GroupHierarchy) {
  for groupName := range groupMap {
    if err := auth.SetGroupParent(groupName, nil); err != nil {
      slog.Warn("清空组父级失败", "group", groupName, "error", err)
    }
  }
  for parentName, group := range groupMap {
    parentID, err := auth.GetGroupID(parentName)
    if err != nil {
      continue
    }
    for _, childName := range group.SubGroups {
      if _, ok := groupMap[childName]; !ok {
        continue
      }
      if err := auth.SetGroupParent(childName, &parentID); err != nil {
        slog.Warn("同步组父子关系失败", "parent", parentName, "child", childName, "error", err)
      }
    }
  }
}

// createSessionToken 生成 HMAC 签名的 session token
func (s *Server) createSessionToken(username string) string {
  ts := strconv.FormatInt(time.Now().Unix(), 10)
  mac := hmac.New(sha256.New, []byte(s.secret))
  mac.Write([]byte(username + ":" + ts))
  sig := hex.EncodeToString(mac.Sum(nil))
  return username + ":" + ts + ":" + sig
}

// parseSessionToken 验证并解析 session token，返回用户名
func (s *Server) parseSessionToken(token string) (string, bool) {
  parts := strings.SplitN(token, ":", 3)
  if len(parts) != 3 {
    return "", false
  }
  username, tsStr, sig := parts[0], parts[1], parts[2]

  mac := hmac.New(sha256.New, []byte(s.secret))
  mac.Write([]byte(username + ":" + tsStr))
  expectedSig := hex.EncodeToString(mac.Sum(nil))
  if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
    return "", false
  }

  ts, err := strconv.ParseInt(tsStr, 10, 64)
  if err != nil {
    return "", false
  }
  if time.Now().Unix()-ts > int64(config.SessionMaxAge) {
    return "", false
  }

  return username, true
}

func (s *Server) sessionUserAllowed(username string) bool {
  if username == "" {
    return false
  }
  if auth.IsSuperadmin(username) {
    return true
  }
  if !s.loadConfig().UnifiedAuthEnabled() {
    return auth.UserExists(username)
  }
  if s.loadConfig().AuthMode() == "local" {
    return false
  }
  if auth.UserExists(username) && !auth.IsExternalUser(username) {
    return false
  }
  if !user.AllowedByWhitelist(s.loadConfig(), s.loadConfig().AuthMode(), username) {
    return false
  }
  return auth.UserExists(username)
}

// getSessionUser 从请求的 cookie 中提取已登录的用户名
func (s *Server) getSessionUser(c *gin.Context) string {
  cookie, err := c.Cookie("session")
  if err != nil {
    return ""
  }
  username, ok := s.parseSessionToken(cookie)
  if !ok {
    return ""
  }
  if !s.sessionUserAllowed(username) {
    return ""
  }
  return username
}

// csrfToken 基于 session 用户名 + 时间窗口生成 CSRF token
func (s *Server) csrfToken(username string) string {
  window := time.Now().Unix() / 3600 // 1 小时窗口
  mac := hmac.New(sha256.New, []byte(s.csrfKey))
  mac.Write([]byte(username + ":" + strconv.FormatInt(window, 10)))
  return hex.EncodeToString(mac.Sum(nil))[:32]
}

// checkCSRF 验证请求中的 CSRF token 是否有效
func (s *Server) checkCSRF(c *gin.Context) bool {
  username := s.getSessionUser(c)
  if username == "" {
    return false
  }
  // 优先从 POST 表单获取，也检查 query 参数
  token := c.PostForm("csrf_token")
  if token == "" {
    token = c.Query("csrf_token")
  }
  return hmac.Equal([]byte(token), []byte(s.csrfToken(username)))
}

// maxBodyBytes 非 upload 端点的请求体大小上限（1 MB）
const maxBodyBytes = 1 << 20

// secureHeaders 安全 Header + 请求体大小限制 中间件
func (s *Server) secureHeaders() gin.HandlerFunc {
  return func(c *gin.Context) {
    origin := c.GetHeader("Origin")
    if s.allowedExtensionOrigin(origin) {
      c.Header("Access-Control-Allow-Origin", origin)
      c.Header("Access-Control-Allow-Credentials", "true")
      c.Header("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
      c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
    }
    // CORS preflight
    if c.Request.Method == "OPTIONS" {
      c.AbortWithStatus(http.StatusOK)
      return
    }
    c.Header("X-Content-Type-Options", "nosniff")
    c.Header("X-Frame-Options", "DENY")
    c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

    if (c.Request.Method == "POST" || c.Request.Method == "PUT" || c.Request.Method == "PATCH") &&
      !strings.HasSuffix(c.Request.URL.Path, "/files/upload") {
      c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
    }

    c.Next()
  }
}

func (s *Server) allowedExtensionOrigin(origin string) bool {
  if origin == "" {
    return false
  }
  if !strings.HasPrefix(origin, "chrome-extension://") && !strings.HasPrefix(origin, "moz-extension://") {
    return false
  }
  allowed := strings.TrimSpace(os.Getenv("PICOAIDE_ALLOWED_EXTENSION_ORIGINS"))
  if allowed == "" {
    return true
  }
  for _, item := range strings.Split(allowed, ",") {
    if strings.TrimSpace(item) == origin {
      return true
    }
  }
  return false
}

// registerInternalAPIRoutes 注册沙箱内部 API 路由（sandbox → host 通信所需的最小集）
func (s *Server) registerInternalAPIRoutes(g *gin.RouterGroup) {
  // 健康检查（无需认证）
  g.GET("/health", s.handleHealth)

  // PicoAgent 沙箱配置 API（token 认证，无需 session）
  g.GET("/picoagent/me", s.handlePicoAgentConfig)
  // 文件管理
  g.GET("/files", s.handleFiles)
  g.POST("/files/upload", s.handleFileUpload)
  g.GET("/files/download", s.handleFileDownload)
  g.POST("/files/delete", s.handleFileDelete)
  g.POST("/files/mkdir", s.handleFileMkdir)
  g.GET("/files/edit", s.handleFileEditGet)
  g.POST("/files/edit", s.handleFileEditSave)
  // Cookie 同步（写入数据库）
  g.POST("/cookies", s.handleCookies)
  // 用户 Cookie 授权管理
  g.GET("/user/cookies", s.handleUserCookies)
  g.POST("/user/cookies/delete", s.handleUserCookiesDelete)
  // 定时任务
  g.GET("/cron", s.handleCronList)
  g.POST("/cron/create", s.handleCronCreate)
  g.POST("/cron/update", s.handleCronUpdate)
  g.POST("/cron/delete", s.handleCronDelete)
  g.POST("/cron/toggle", s.handleCronToggle)
  // MCP token
  g.GET("/mcp/token", s.handleMCPToken)
  // MCP SSE 服务
  g.GET("/mcp/sse/:service", s.handleMCPSSEServiceGet)
  g.POST("/mcp/sse/:service", s.handleMCPSSEServicePost)
  // MCP Cookie API
  g.GET("/mcp/cookies", s.handleMCPCookiesGet)
  g.POST("/mcp/cookies", s.handleMCPCookiesPost)
  // Browser Extension WebSocket
  g.GET("/browser/ws", s.handleBrowserWS)
  // Computer 桌面代理 WebSocket
  g.GET("/computer/ws", s.handleComputerWS)
}

// registerExternalAPIRoutes 注册全部外部 API 路由（继承内部路由 + 外部特有路由）
func (s *Server) registerExternalAPIRoutes(g *gin.RouterGroup) {
  // 先注册内部沙箱路由
  s.registerInternalAPIRoutes(g)

  // 认证
  login := g.Group("/login")
  login.Use(s.rateLimitLogin())
  login.POST("", s.handleLogin)
  login.GET("/auth", s.handleAuthStart)
  login.GET("/callback", s.handleAuthCallback)
  g.GET("/login/mode", s.handleLoginMode)

  g.POST("/logout", s.handleLogout)
  g.GET("/user/info", s.handleUserInfo)
  g.GET("/user/init-status", s.handleUserInitStatus)
  g.POST("/user/password", s.handleChangePassword)
  // 对话
  g.GET("/user/chat/history", s.handleChatHistory)
  g.POST("/user/chat/send", s.handleChatSend)
  g.GET("/user/chat/stream", s.handleChatStream)
  g.POST("/user/chat/stop", s.handleChatStop)
  g.GET("/config", s.handleConfigGet)
  g.POST("/config", s.handleConfigSave)
  // CSRF token
  g.GET("/csrf", s.handleCSRF)
  // 渠道配置
  g.GET("/channels", s.handleUserChannelsGet)
  g.GET("/channels/config-fields", s.handleChannelConfigFieldsGet)
  g.POST("/channels/config-fields", s.handleChannelConfigFieldsSave)
  // 超管 API 路由组
  admin := g.Group("/admin")
  {
    admin.GET("/users", s.handleAdminUsers)
    admin.POST("/users/create", s.handleAdminUserCreate)
    admin.POST("/users/batch-create", s.handleAdminUserBatchCreate)
    admin.POST("/users/delete", s.handleAdminUserDelete)
    admin.GET("/superadmins", s.handleAdminSuperadmins)
    admin.POST("/superadmins/create", s.handleAdminSuperadminCreate)
    admin.POST("/superadmins/delete", s.handleAdminSuperadminDelete)
    admin.POST("/superadmins/reset", s.handleAdminSuperadminReset)
    admin.POST("/password", s.handleAdminChangePassword)
    admin.GET("/whitelist", s.handleAdminWhitelistGet)
    admin.POST("/whitelist", s.handleAdminWhitelistPost)
    admin.POST("/auth/test-ldap", s.handleAdminAuthTestLDAP)
    admin.GET("/auth/ldap-users", s.handleAdminAuthLDAPUsers)
    admin.POST("/auth/sync-users", s.handleAdminAuthSyncUsers)
    admin.POST("/auth/sync-groups", s.handleAdminAuthSyncGroups)
    admin.GET("/auth/providers", s.handleAdminAuthProviders)
    admin.GET("/groups", s.handleAdminGroups)
    admin.POST("/groups/create", s.handleAdminGroupCreate)
    admin.POST("/groups/delete", s.handleAdminGroupDelete)
    admin.GET("/groups/members", s.handleAdminGroupMembers)
    admin.POST("/groups/members/add", s.handleAdminGroupMembersAdd)
    admin.POST("/groups/members/remove", s.handleAdminGroupMembersRemove)
    admin.POST("/groups/skills/bind", s.handleAdminGroupSkillsBind)
    admin.POST("/groups/skills/unbind", s.handleAdminGroupSkillsUnbind)
    admin.GET("/skills", s.handleAdminSkills)
    admin.POST("/skills/deploy", s.handleAdminSkillsDeploy)
    admin.POST("/skills/remove", s.handleAdminSkillsRemove)
    admin.POST("/skills/user/bind", s.handleAdminSkillsUserBind)
    admin.POST("/skills/user/unbind", s.handleAdminSkillsUserUnbind)
    admin.GET("/skills/user/sources", s.handleAdminSkillsUserSources)
    admin.GET("/skills/sources", s.handleAdminSkillsSources)
    admin.POST("/skills/sources/git", s.handleAdminSkillsSourcesGitAdd)
    admin.POST("/skills/sources/remove", s.handleAdminSkillsSourcesRemove)
    admin.POST("/skills/sources/pull", s.handleAdminSkillsSourcesPull)
    admin.POST("/skills/sources/refresh", s.handleAdminSkillsSourcesRefresh)
    admin.GET("/skills/registry/list", s.handleAdminSkillsRegistryList)
    admin.POST("/skills/registry/install", s.handleAdminSkillsRegistryInstall)
    admin.GET("/skills/defaults", s.handleAdminSkillsDefaults)
    admin.POST("/skills/defaults/toggle", s.handleAdminSkillsDefaultsToggle)
    admin.GET("/shared-folders", s.handleAdminSharedFolders)
    admin.POST("/shared-folders/create", s.handleAdminSharedFoldersCreate)
    admin.POST("/shared-folders/update", s.handleAdminSharedFoldersUpdate)
    admin.POST("/shared-folders/delete", s.handleAdminSharedFoldersDelete)
    admin.POST("/shared-folders/groups/set", s.handleAdminSharedFoldersSetGroups)
    admin.POST("/shared-folders/test", s.handleAdminSharedFoldersTest)
    admin.POST("/shared-folders/mount", s.handleAdminSharedFoldersMount)
    admin.GET("/channels", s.handleAdminChannelsGet)
    admin.GET("/task/status", s.handleAdminTaskStatus)
    admin.GET("/skill-install-policy", s.handleAdminSkillInstallPolicyGet)
    admin.POST("/skill-install-policy", s.handleAdminSkillInstallPolicySet)
    admin.GET("/mcp/servers", s.handleAdminMCPServersList)
    admin.POST("/mcp/servers/create", s.handleAdminMCPServerCreate)
    admin.POST("/mcp/servers/update/:id", s.handleAdminMCPServerUpdate)
    admin.POST("/mcp/servers/delete/:id", s.handleAdminMCPServerDelete)
    admin.GET("/mcp/servers/grants", s.handleAdminMCPServerGrantsList)
    admin.POST("/mcp/servers/grants/add", s.handleAdminMCPServerGrantAdd)
    admin.POST("/mcp/servers/grants/remove/:id", s.handleAdminMCPServerGrantRemove)
    admin.POST("/mcp/servers/reload", s.handleAdminMCPServersReload)
    admin.GET("/mcp/servers/tools", s.handleAdminMCPServerTools)
    admin.POST("/model/test", s.handleAdminModelTest)
    admin.GET("/tls/status", s.handleAdminTLSStatus)
    admin.POST("/tls/upload", s.handleAdminTLSUpload)
    admin.POST("/tls/clear", s.handleAdminTLSClear)
  }
  // 普通用户 - 团队空间
  g.GET("/shared-folders", s.handleSharedFolders)
  // 普通用户 - 技能中心
  g.GET("/user/skills", s.handleUserSkills)
  g.POST("/user/skills/install", s.handleUserSkillsInstall)
  g.POST("/user/skills/uninstall", s.handleUserSkillsUninstall)
}

// RegisterRoutes 将所有 API 路由注册到 Gin 引擎
func (s *Server) RegisterRoutes(r *gin.Engine) {
  s.registerUIRoutes(r)
  s.registerExternalAPIRoutes(r.Group("/api"))
  s.registerExternalAPIRoutes(r.Group("/api/v1"))
  r.GET("/api/version", s.handleVersion)
}

// buildInternalHandler 创建仅包含沙箱内部 API 路由的 Gin engine
func (s *Server) buildInternalHandler() http.Handler {
  r := gin.New()
  r.Use(gin.Recovery())
  r.Use(s.secureHeaders())
  s.registerInternalAPIRoutes(r.Group("/api"))
  s.registerInternalAPIRoutes(r.Group("/api/v1"))
  r.GET("/api/version", s.handleVersion)
  return logger.AccessMiddleware(r)
}

// buildExternalHandler 创建包含全部路由（UI + 全部 API）的 Gin engine
func (s *Server) buildExternalHandler() http.Handler {
  r := gin.New()
  r.Use(gin.Recovery())
  r.Use(s.secureHeaders())
  s.RegisterRoutes(r)
  return logger.AccessMiddleware(r)
}

// redirectToHTTPSHandler 返回一个将 HTTP 请求 301 重定向到 HTTPS 的 handler
func redirectToHTTPSHandler() http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    u := url.URL{
      Scheme:   "https",
      Host:     r.Host,
      Path:     r.URL.Path,
      RawQuery: r.URL.RawQuery,
    }
    http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
  })
}

// sandboxAwareHandler 根据请求来源 IP 分发到 internalHandler（沙箱内网）或 externalHandler（外部）
func sandboxAwareHandler(internal, external http.Handler) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if isSandboxRequest(r) {
      internal.ServeHTTP(w, r)
    } else {
      external.ServeHTTP(w, r)
    }
  })
}

// sandboxCIDR 是沙箱内网网段
var sandboxCIDR = func() *net.IPNet {
  _, cidr, _ := net.ParseCIDR("100.64.0.0/16")
  return cidr
}()

// isSandboxRequest 判断请求是否来自沙箱内网
func isSandboxRequest(r *http.Request) bool {
  host, _, err := net.SplitHostPort(r.RemoteAddr)
  if err != nil {
    return false
  }
  ip := net.ParseIP(host)
  if ip == nil {
    return false
  }
  return sandboxCIDR.Contains(ip)
}

// buildTLSServer 根据配置创建 HTTPS 服务器。TLS 不可用时返回 nil, nil。
func (s *Server) buildTLSServer() (*http.Server, error) {
  cfg := s.loadConfig()
  if !cfg.Web.TLS.Enabled || cfg.Web.TLS.CertPEM == "" || cfg.Web.TLS.KeyPEM == "" {
    return nil, nil
  }
  cert, err := tls.X509KeyPair([]byte(cfg.Web.TLS.CertPEM), []byte(cfg.Web.TLS.KeyPEM))
  if err != nil {
    return nil, fmt.Errorf("加载 TLS 证书失败: %w", err)
  }
  return &http.Server{
    Addr:      ":443",
    Handler:   s.buildExternalHandler(),
    TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
  }, nil
}

// setSessionCookie 设置 session cookie
func (s *Server) setSessionCookie(c *gin.Context, value string, maxAge int) {
  cookie := &http.Cookie{
    Name:     "session",
    Value:    value,
    Path:     "/",
    HttpOnly: true,
    SameSite: http.SameSiteLaxMode,
    MaxAge:   maxAge,
  }
  if c.Request.TLS != nil {
    cookie.Secure = true
  }
  http.SetCookie(c.Writer, cookie)
}

// Serve 初始化并启动 Web 管理面板服务器（DB → 配置 → 日志 → 服务）
func Serve() error {
  wd, _ := os.Getwd()
  if wd != "" {
    os.MkdirAll(wd, 0755)
  }

  // 初始化数据库
  if err := auth.InitDB(wd); err != nil {
    os.MkdirAll(wd, 0755)
    if err := auth.InitDB(wd); err != nil {
      return fmt.Errorf("初始化数据库失败: %w", err)
    }
  }

  // 首次运行自动初始化
  count, _ := config.SettingsCount()
  if count == 0 {
    if err := config.InitDBDefaults(); err != nil {
      return fmt.Errorf("初始化默认配置失败: %w", err)
    }
  }

  cfg, err := config.LoadFromDB()
  if err != nil {
    return fmt.Errorf("加载配置失败: %w", err)
  }

  // 确保沙箱网桥存在（内网 listener 依赖 100.64.0.1 地址）
  sandbox.EnsureBridge()

  retention := cfg.Web.LogRetention
  if retention == "" {
    retention = "6m"
  }
  logger.Init(wd, retention, false, cfg.Web.LogLevel, cfg.Web.DebugMode)
  defer logger.Close()

  // 确保 users/ 和 archive/ 目录存在
  if err := user.EnsureUsersRoot(cfg); err != nil {
    slog.Warn("创建用户目录失败", "error", err)
  }

  // 自动创建超管（首次运行时）
  admins, err := auth.GetSuperadmins()
  if err != nil || len(admins) == 0 {
    password := auth.GenerateRandomPassword(16)
    if err := auth.CreateUser("admin", password, "superadmin"); err != nil {
      slog.Warn("自动创建超管失败", "error", err)
    } else {
      slog.Info("自动创建超管账户", "username", "admin")
      fmt.Fprintf(os.Stderr, "\n⚠ 超管账户已自动创建\n  用户名: admin\n  密码: %s\n  请立即登录并修改密码\n\n", password)
    }
  }

  secret, err := ensureSessionSecret()
  if err != nil {
    return err
  }
  csrfKey := secret + "-csrf"

  s := &Server{
    secret:       secret,
    csrfKey:      csrfKey,
    loginLimiter: newLoginRateLimiter(),
  }
  s.cfg.Store(cfg)

  // 定时同步（实时响应配置变更）
  s.restartSyncTimer()

  // 初始扫描技能目录（仅在启动时打印日志）
  initialSkills, _ := skill.ListAllSkills()
  slog.Info("技能目录扫描完成", "count", len(initialSkills))

  // 初始化 PicoAgent 集成层
  if ai, err := s.initAgentIntegration(); err != nil {
    slog.Warn("PicoAgent 集成初始化失败", "error", err)
  } else {
    s.agentIntegration = ai
    // 启动 IM 网关
    if err := ai.imGateway.Start(context.Background()); err != nil {
      slog.Warn("IM 网关启动失败", "error", err)
    } else {
      slog.Info("IM 网关已启动")
    }
    // 启动 Cron 调度器
    ai.cron.Start(context.Background())
  }

  // 加载第三方 MCP 服务器
  if err := LoadMCPServers(context.Background()); err != nil {
    slog.Warn("MCP 服务器加载失败", "error", err)
  } else {
    slog.Info("MCP 服务器已加载")
  }

  if cfg.Web.DebugMode {
    gin.SetMode(gin.DebugMode)
  } else {
    gin.SetMode(gin.ReleaseMode)
  }

  internalHandler := s.buildInternalHandler()
  externalHandler := s.buildExternalHandler()
  combinedHandler := sandboxAwareHandler(internalHandler, externalHandler)

  // 信号通道：监听 SIGTERM 和 SIGINT
  sigCh := make(chan os.Signal, 1)
  signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

  // 外部监听 :80（当 TLS 启用时做 301 跳转）
  tlsSrv, err := s.buildTLSServer()
  if err != nil {
    slog.Warn("TLS 服务器创建失败，回退到 HTTP", "error", err)
  }

  var extHandler http.Handler
  if tlsSrv != nil {
    extHandler = redirectToHTTPSHandler()
  } else {
    extHandler = combinedHandler
  }

  extSrv := &http.Server{
    Addr:    ":80",
    Handler: extHandler,
  }
  go func() {
    if tlsSrv != nil {
      slog.Info("外部 HTTP 已启动（301 跳转至 HTTPS）", "addr", ":80")
    } else {
      slog.Info("外部 HTTP 已启动", "addr", ":80")
    }
    if err := extSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
      slog.Error("外部 HTTP 服务失败", "error", err)
    }
  }()

  // TLS 服务器（仅当证书配置后启用）
  if tlsSrv != nil {
    s.tlsSrv = tlsSrv
    go func() {
      slog.Info("外部 HTTPS 已启动", "addr", ":443")
      if err := tlsSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
        slog.Error("外部 HTTPS 服务失败", "error", err)
      }
    }()
  }

  // Unix socket 监听（供沙箱内 picoagent 本地通信）
  sockPath := filepath.Join(config.WorkDir(), "picoaide.sock")
  os.Remove(sockPath)
  unixLis, err := net.Listen("unix", sockPath)
  if err != nil {
    slog.Warn("Unix socket 监听失败", "error", err)
  } else {
    os.Chmod(sockPath, 0700)
    go func() {
      slog.Info("Unix socket 已启动", "path", sockPath)
      if err := http.Serve(unixLis, internalHandler); err != nil && err != http.ErrServerClosed {
        slog.Error("Unix socket 服务失败", "error", err)
      }
    }()
  }

  <-sigCh
  slog.Info("收到终止信号，开始优雅关闭...")
  if unixLis != nil {
    unixLis.Close()
  }
  return s.gracefulShutdown(extSrv, sockPath)
}

// gracefulShutdown 优雅关闭 HTTP 服务器及相关资源
func (s *Server) gracefulShutdown(srv *http.Server, sockPath string) error {
  // 停止定时同步
  if s.syncCancel != nil {
    s.syncCancel()
  }

  // 停止速率限制器 goroutine
  if s.loginLimiter != nil {
    s.loginLimiter.Stop()
  }

  // 停止 IM 网关和 Cron 调度器
  if s.agentIntegration != nil {
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    s.agentIntegration.imGateway.Stop(shutdownCtx)
    s.agentIntegration.cron.Stop()
    cancel()
  }

  shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
  defer cancel()
  if err := srv.Shutdown(shutdownCtx); err != nil {
    slog.Error("外部 HTTP 服务器关闭失败", "error", err)
  }
  if s.tlsSrv != nil {
    if err := s.tlsSrv.Shutdown(shutdownCtx); err != nil {
      slog.Error("HTTPS 服务器关闭失败", "error", err)
    }
  }

  slog.Info("服务器已优雅关闭")
  os.Remove(sockPath)
  return nil
}

func contextWithTimeout(sec int) (context.Context, context.CancelFunc) {
  return context.WithTimeout(context.Background(), time.Duration(sec)*time.Second)
}
