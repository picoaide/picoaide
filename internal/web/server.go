package web

import (
  "context"
  "crypto/hmac"
  "crypto/rand"
  "crypto/sha256"
  "encoding/hex"
  "fmt"
  "github.com/gin-gonic/gin"
  "log/slog"
  "net"
  "net/http"
  "os"
  "os/signal"
  "strconv"
  "strings"
  "sync"
  "syscall"
  "time"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/authsource"
  "github.com/picoaide/picoaide/internal/config"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
  "github.com/picoaide/picoaide/internal/logger"
  "github.com/picoaide/picoaide/internal/skill"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// Web 服务器
// ============================================================

// Server 是 Web 管理面板服务器
type Server struct {
  cfg             *config.GlobalConfig
  secret          string
  csrfKey         string
  dockerAvailable bool
  loginLimiter    *rateLimiter
  syncCancel      context.CancelFunc
  syncMu          sync.Mutex
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
  if !authsource.HasDirectoryProvider(s.cfg) {
    return
  }

  authMode := s.cfg.AuthMode()

  // 同步用户目录（与手动同步相同的逻辑，含清理过期账号）
  result, err := s.syncUsersFromDirectory(true)
  if err != nil {
    slog.Error("自动同步用户失败", "auth_mode", authMode, "error", err)
  } else {
    slog.Info("自动同步用户完成", "auth_mode", authMode, "synced", result.LocalUserSynced, "allowed", result.AllowedUserCount, "initialized", result.InitializedCount, "archived", result.ArchivedStaleUsers, "deleted_local_auth", result.DeletedLocalAuth)
  }

  // 同步组
  groupResult, err := authsource.SyncGroups(authMode, s.cfg, func(username string) error {
    if rec, _ := auth.GetContainerByUsername(username); rec == nil {
      return s.initExternalUser(username)
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

  interval := s.cfg.SyncIntervalDuration()
  if interval > 0 && s.cfg.UnifiedAuthEnabled() && authsource.HasDirectoryProvider(s.cfg) {
    ctx, cancel := context.WithCancel(context.Background())
    s.syncCancel = cancel
    go func() {
      ticker := time.NewTicker(interval)
      defer ticker.Stop()
      slog.Info("定时同步已启动", "interval", interval, "auth_mode", s.cfg.AuthMode())
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
  if !s.cfg.UnifiedAuthEnabled() {
    return auth.UserExists(username)
  }
  if s.cfg.AuthMode() == "local" {
    return false
  }
  if auth.UserExists(username) && !auth.IsExternalUser(username) {
    return false
  }
  if !user.AllowedByWhitelist(s.cfg, s.cfg.AuthMode(), username) {
    return false
  }
  rec, _ := auth.GetContainerByUsername(username)
  return rec != nil
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
      c.Header("Access-Control-Allow-Headers", "Content-Type")
      c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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
      c.Request.URL.Path != "/api/files/upload" &&
      c.Request.URL.Path != "/api/admin/migration-rules/upload" {
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
    // Backward-compatible default: allow extension origins, but only extension
    // code that can fetch a CSRF token can complete mutating requests.
    return true
  }
  for _, item := range strings.Split(allowed, ",") {
    if strings.TrimSpace(item) == origin {
      return true
    }
  }
  return false
}

// ensureImageAvailable 检查本地是否有 picoclaw 镜像，无则自动拉取最新版本
func (s *Server) ensureImageAvailable() {
  for {
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    images, err := dockerpkg.ListLocalImages(ctx, s.cfg.Image.Name)
    cancel()
    if err == nil && len(images) > 0 {
      slog.Info("本地已有镜像，跳过自动拉取", "count", len(images))
      return
    }

    // 从 GitHub 获取最新标签列表
    ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
    tags, err := dockerpkg.ListRegistryTagsForConfig(ctx2, s.cfg.Image.RepoName(), "github")
    cancel2()
    if err != nil || len(tags) == 0 {
      slog.Error("获取远程标签失败，30 秒后重试", "error", err)
      time.Sleep(30 * time.Second)
      continue
    }

    sortTagsForDisplay(tags)
    latestTag := tags[0]
    // 按当前配置的仓库地址拉取（默认腾讯云，可改 GitHub）
    pullRef := s.cfg.Image.PullRef(latestTag)
    unifiedRef := s.cfg.Image.UnifiedRef(latestTag)
    startImagePull(latestTag)
    slog.Info("正在拉取镜像", "tag", pullRef)

    pullCtx := context.Background()
    reader, err := dockerpkg.ImagePull(pullCtx, pullRef)
    if err != nil {
      slog.Error("镜像拉取失败", "error", err)
      failImagePull(err.Error())
      time.Sleep(30 * time.Second)
      continue
    }
    buf := make([]byte, 4096)
    for {
      n, readErr := reader.Read(buf)
      if n > 0 {
        updateImagePull(string(buf[:n]))
      }
      if readErr != nil {
        break
      }
    }
    reader.Close()

    // 腾讯云模式：拉取后 retag 为统一名称
    if s.cfg.Image.IsTencent() && pullRef != unifiedRef {
      slog.Info("重命名镜像", "from", pullRef, "to", unifiedRef)
      if err := dockerpkg.RetagImage(pullCtx, pullRef, unifiedRef); err != nil {
        slog.Error("重命名失败", "error", err)
      }
    }

    ctx3, cancel3 := context.WithTimeout(context.Background(), 10*time.Second)
    images, err = dockerpkg.ListLocalImages(ctx3, s.cfg.Image.Name)
    cancel3()
    if err == nil && len(images) > 0 {
      finishImagePull()
      slog.Info("镜像拉取完成", "tag", unifiedRef)
      return
    }
    slog.Error("镜像拉取后未检测到本地镜像，30 秒后重试")
    failImagePull("拉取后未检测到本地镜像")
    time.Sleep(30 * time.Second)
  }
}

// imageRequiredMiddleware 拦截无本地镜像时的超管写操作
func (s *Server) imageRequiredMiddleware() gin.HandlerFunc {
  return func(c *gin.Context) {
    if c.Request.Method != "POST" {
      c.Next()
      return
    }
    if strings.HasPrefix(c.Request.URL.Path, "/api/admin/images") {
      c.Next()
      return
    }
    if !s.dockerAvailable {
      c.Next()
      return
    }
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    images, err := dockerpkg.ListLocalImages(ctx, s.cfg.Image.Name)
    cancel()
    if err == nil && len(images) > 0 {
      c.Next()
      return
    }
    if getImagePullStatus().Running {
      c.Next()
      return
    }
    writeError(c, http.StatusPreconditionFailed, "本地无可用镜像，请先到镜像管理拉取镜像")
    c.Abort()
  }
}

// RegisterRoutes 将所有 API 路由注册到 Gin 引擎
func (s *Server) RegisterRoutes(r *gin.Engine) {
  s.registerUIRoutes(r)

  // 健康检查（无需认证）
  r.GET("/api/health", s.handleHealth)

  // 认证
  login := r.Group("/api/login")
  login.Use(s.rateLimitLogin())
  login.POST("", s.handleLogin)
  login.GET("/auth", s.handleAuthStart)
  login.GET("/callback", s.handleAuthCallback)
  r.GET("/api/login/mode", s.handleLoginMode)

  r.POST("/api/logout", s.handleLogout)
  r.GET("/api/user/info", s.handleUserInfo)
  r.GET("/api/user/init-status", s.handleUserInitStatus)
  r.POST("/api/user/password", s.handleChangePassword)
  // 钉钉配置
  r.GET("/api/dingtalk", s.handleDingTalkGet)
  r.POST("/api/dingtalk", s.handleDingTalkSave)
  // 配置管理（超管）
  r.GET("/api/config", s.handleConfigGet)
  r.POST("/api/config", s.handleConfigSave)
  r.GET("/api/picoclaw/channels", s.handlePicoClawChannelsGet)
  r.GET("/api/picoclaw/config-fields", s.handlePicoClawConfigFieldsGet)
  r.POST("/api/picoclaw/config-fields", s.handlePicoClawConfigFieldsSave)
  // 文件管理
  r.GET("/api/files", s.handleFiles)
  r.POST("/api/files/upload", s.handleFileUpload)
  r.GET("/api/files/download", s.handleFileDownload)
  r.POST("/api/files/delete", s.handleFileDelete)
  r.POST("/api/files/mkdir", s.handleFileMkdir)
  r.GET("/api/files/edit", s.handleFileEditGet)
  r.POST("/api/files/edit", s.handleFileEditSave)
  // Cookie 同步（写入用户 .security.yml）
  r.POST("/api/cookies", s.handleCookies)
  // CSRF token
  r.GET("/api/csrf", s.handleCSRF)
  // MCP token（Extension 获取认证 token）
  r.GET("/api/mcp/token", s.handleMCPToken)
  // MCP SSE 服务（参数化路由，支持 browser、computer 等服务）
  r.GET("/api/mcp/sse/:service", s.handleMCPSSEServiceGet)
  r.POST("/api/mcp/sse/:service", s.handleMCPSSEServicePost)
  // Browser Extension WebSocket
  r.GET("/api/browser/ws", s.handleBrowserWS)
  // Computer 桌面代理 WebSocket
  r.GET("/api/computer/ws", s.handleComputerWS)
  // 超管 API 路由组（含镜像拦截中间件）
  admin := r.Group("/api/admin")
  admin.Use(s.imageRequiredMiddleware())
  {
    admin.GET("/users", s.handleAdminUsers)
    admin.POST("/users/create", s.handleAdminUserCreate)
    admin.POST("/users/batch-create", s.handleAdminUserBatchCreate)
    admin.POST("/users/delete", s.handleAdminUserDelete)
    admin.GET("/superadmins", s.handleAdminSuperadmins)
    admin.POST("/superadmins/create", s.handleAdminSuperadminCreate)
    admin.POST("/superadmins/delete", s.handleAdminSuperadminDelete)
    admin.POST("/superadmins/reset", s.handleAdminSuperadminReset)
    admin.POST("/container/start", s.handleAdminContainerStart)
    admin.POST("/container/stop", s.handleAdminContainerStop)
    admin.POST("/container/restart", s.handleAdminContainerRestart)
    admin.POST("/container/debug", s.handleAdminContainerDebug)
    admin.GET("/container/logs", s.handleAdminContainerLogs)
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
    admin.GET("/skills/download", s.handleAdminSkillsDownload)
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
    // 镜像管理（中间件内部已放行 /api/admin/images 前缀）
    admin.GET("/images", s.handleAdminImages)
    admin.POST("/images/pull", s.handleAdminImagePull)
    admin.POST("/images/delete", s.handleAdminImageDelete)
    admin.POST("/images/migrate", s.handleAdminImageMigrate)
    admin.POST("/images/upgrade", s.handleAdminImageUpgrade)
    admin.GET("/images/registry", s.handleAdminImageRegistry)
    admin.GET("/images/local-tags", s.handleAdminLocalTags)
    admin.GET("/images/upgrade-candidates", s.handleAdminImageUpgradeCandidates)
    admin.GET("/images/users", s.handleAdminImageUsers)
    admin.GET("/images/pull-status", s.handleAdminImagePullStatus)
    admin.GET("/shared-folders", s.handleAdminSharedFolders)
    admin.POST("/shared-folders/create", s.handleAdminSharedFoldersCreate)
    admin.POST("/shared-folders/update", s.handleAdminSharedFoldersUpdate)
    admin.POST("/shared-folders/delete", s.handleAdminSharedFoldersDelete)
    admin.POST("/shared-folders/groups/set", s.handleAdminSharedFoldersSetGroups)
    admin.POST("/shared-folders/test", s.handleAdminSharedFoldersTest)
    admin.POST("/shared-folders/mount", s.handleAdminSharedFoldersMount)
    admin.POST("/config/apply", s.handleAdminConfigApply)
    admin.GET("/migration-rules", s.handleAdminMigrationRulesGet)
    admin.POST("/migration-rules/refresh", s.handleAdminMigrationRulesRefresh)
    admin.POST("/migration-rules/upload", s.handleAdminMigrationRulesUpload)
    admin.GET("/picoclaw/channels", s.handlePicoClawAdminChannelsGet)
    admin.GET("/task/status", s.handleAdminTaskStatus)
  }
  // 普通用户 - 团队空间
  r.GET("/api/shared-folders", s.handleSharedFolders)
  // 普通用户 - 技能中心
  r.GET("/api/user/skills", s.handleUserSkills)
  r.POST("/api/user/skills/install", s.handleUserSkillsInstall)
  r.POST("/api/user/skills/uninstall", s.handleUserSkillsUninstall)
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
  if s.cfg.Web.TLS.Enabled {
    cookie.Secure = true
  }
  http.SetCookie(c.Writer, cookie)
}

func isDockerNetworkRequest(r *http.Request) bool {
  host, _, err := net.SplitHostPort(r.RemoteAddr)
  if err != nil {
    host = r.RemoteAddr
  }
  ip := net.ParseIP(host)
  if ip == nil {
    return false
  }
  _, subnet, err := net.ParseCIDR(dockerpkg.NetworkSubnet)
  if err != nil {
    return false
  }
  return subnet.Contains(ip)
}

func httpsRedirectTarget(r *http.Request) string {
  host := r.Host
  if h, p, err := net.SplitHostPort(r.Host); err == nil && p == "80" {
    host = h
  }
  target := "https://" + host + r.URL.Path
  if r.URL.RawQuery != "" {
    target += "?" + r.URL.RawQuery
  }
  return target
}

// Serve 创建并启动 Web 管理面板服务器
func Serve(cfg *config.GlobalConfig) error {
  listenAddr := cfg.Web.Listen
  if listenAddr == "" {
    listenAddr = ":80"
  }

  // 确保工作目录存在
  wd, _ := os.Getwd()
  if wd != "" {
    os.MkdirAll(wd, 0755)
  }

  // 确保 users/ 和 archive/ 目录存在
  if err := user.EnsureUsersRoot(cfg); err != nil {
    slog.Warn("创建用户目录失败", "error", err)
  }

  // 初始化本地用户数据库（失败时重试一次）
  if err := auth.InitDB(wd); err != nil {
    slog.Warn("数据库初始化失败，正在重试", "error", err)
    os.MkdirAll(wd, 0755)
    if err := auth.InitDB(wd); err != nil {
      return fmt.Errorf("初始化用户数据库失败: %w", err)
    }
  }

  secret, err := ensureSessionSecret()
  if err != nil {
    return err
  }
  csrfKey := secret + "-csrf"

  // 初始化 Docker 客户端
  dockerOK := false
  if err := dockerpkg.InitClient(); err != nil {
    slog.Warn("Docker 不可用，容器操作将被禁用", "error", err)
  } else {
    ctx := contextWithTimeout(5)
    if err := dockerpkg.EnsureNetwork(ctx); err != nil {
      slog.Warn("网络初始化失败", "error", err)
    }
    dockerOK = true
  }

  s := &Server{
    cfg:             cfg,
    secret:          secret,
    csrfKey:         csrfKey,
    dockerAvailable: dockerOK,
    loginLimiter:    newLoginRateLimiter(),
  }

  // 后台自动拉取镜像（无本地镜像时）
  if dockerOK {
    go s.ensureImageAvailable()
  }

  // 定时同步（实时响应配置变更）
  s.restartSyncTimer()

  // 初始扫描技能目录（仅在启动时打印日志）
  initialSkills, _ := skill.ListAllSkills()
  slog.Info("技能目录扫描完成", "count", len(initialSkills))

  gin.SetMode(gin.ReleaseMode)
  r := gin.New()
  r.Use(s.secureHeaders())
  s.RegisterRoutes(r)
  appHandler := logger.AccessMiddleware(r)

  // 信号通道：监听 SIGTERM 和 SIGINT
  sigCh := make(chan os.Signal, 1)
  signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

  var redirectServer *http.Server

  if cfg.Web.TLS.Enabled && cfg.Web.TLS.CertFile != "" && cfg.Web.TLS.KeyFile != "" {
    if _, err := os.Stat(cfg.Web.TLS.CertFile); err != nil {
      return fmt.Errorf("证书文件不存在: %s", cfg.Web.TLS.CertFile)
    }
    if _, err := os.Stat(cfg.Web.TLS.KeyFile); err != nil {
      return fmt.Errorf("私钥文件不存在: %s", cfg.Web.TLS.KeyFile)
    }

    if strings.HasSuffix(listenAddr, ":443") {
      redirectMux := http.NewServeMux()
      redirectMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        if isDockerNetworkRequest(r) {
          appHandler.ServeHTTP(w, r)
          return
        }
        http.Redirect(w, r, httpsRedirectTarget(r), http.StatusMovedPermanently)
      })
      redirectServer = &http.Server{Addr: ":80", Handler: redirectMux}
      go func() {
        slog.Info("HTTP 入口已启动", "listen", ":80", "internal", dockerpkg.NetworkSubnet, "external", "redirect-to-https")
        if err := redirectServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
          slog.Error("HTTP 入口服务错误", "error", err)
        }
      }()
    }

    srv := &http.Server{
      Addr:    listenAddr,
      Handler: appHandler,
    }
    go func() {
      slog.Info("管理面板启动", "url", "https://"+listenAddr)
      if err := srv.ListenAndServeTLS(cfg.Web.TLS.CertFile, cfg.Web.TLS.KeyFile); err != nil && err != http.ErrServerClosed {
        slog.Error("服务启动失败", "error", err)
      }
    }()

    <-sigCh
    slog.Info("收到终止信号，开始优雅关闭...")
    return gracefulShutdown(srv, redirectServer, dockerOK, s.syncCancel)
  }

  srv := &http.Server{
    Addr:    listenAddr,
    Handler: appHandler,
  }
  go func() {
    slog.Info("管理面板启动", "url", "http://"+listenAddr)
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
      slog.Error("服务启动失败", "error", err)
    }
  }()

  <-sigCh
  slog.Info("收到终止信号，开始优雅关闭...")
  return gracefulShutdown(srv, redirectServer, dockerOK, s.syncCancel)
}

// gracefulShutdown 优雅关闭 HTTP 服务器及相关资源
func gracefulShutdown(srv, redirectSrv *http.Server, dockerOK bool, syncCancel context.CancelFunc) error {
  if syncCancel != nil {
    syncCancel()
  }

  if redirectSrv != nil {
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    redirectSrv.Shutdown(shutdownCtx)
    cancel()
  }

  shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
  defer cancel()
  if err := srv.Shutdown(shutdownCtx); err != nil {
    slog.Error("服务器关闭失败", "error", err)
    return err
  }

  if dockerOK {
    dockerpkg.CloseClient()
  }

  slog.Info("服务器已优雅关闭")
  return nil
}

func contextWithTimeout(sec int) context.Context {
  ctx, cancel := context.WithTimeout(context.Background(), time.Duration(sec)*time.Second)
  go func() {
    <-ctx.Done()
    cancel()
  }()
  return ctx
}
