package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/picoaide/picoaide/internal/auth"
	"github.com/picoaide/picoaide/internal/config"
	dockerpkg "github.com/picoaide/picoaide/internal/docker"
	"github.com/picoaide/picoaide/internal/ldap"
	"github.com/picoaide/picoaide/internal/logger"
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
}

const sessionSecretSettingKey = "internal.session_secret"

func randomHex(bytesLen int) (string, error) {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ensureSessionSecret(cfg *config.GlobalConfig) (string, error) {
	if cfg.Web.Password != "" {
		return cfg.Web.Password, nil
	}

	engine, err := auth.GetEngine()
	if err != nil {
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

// startLDAPSyncScheduler 定时同步 LDAP 组
func (s *Server) startLDAPSyncScheduler(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	slog.Info("LDAP 定时同步已启动", "interval", interval)
	for range ticker.C {
		s.syncLDAPGroups()
	}
}

// syncLDAPGroups 执行 LDAP 组同步
func (s *Server) syncLDAPGroups() {
	if !s.cfg.LDAPEnabled() {
		return
	}
	if s.cfg.LDAP.GroupSearchMode == "" {
		return
	}
	groupMap, err := ldap.FetchAllGroupsWithMembers(s.cfg)
	if err != nil {
		slog.Error("LDAP 定时同步失败", "error", err)
		return
	}
	whitelist, _ := user.LoadWhitelist()
	groupCount := 0
	for groupName, members := range groupMap {
		auth.CreateGroup(groupName, "ldap", "", nil)
		groupCount++
		var filtered []string
		for _, m := range members {
			if whitelist == nil || whitelist[m] {
				filtered = append(filtered, m)
			}
		}
		if len(filtered) > 0 {
			auth.AddUsersToGroup(groupName, filtered)
		}
	}
	slog.Info("LDAP 定时同步完成", "groups", groupCount)
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
			c.Request.URL.Path != "/api/admin/migration-rules/upload" &&
			c.Request.URL.Path != "/api/admin/skills/upload" {
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

// RegisterRoutes 将所有 API 路由注册到 Gin 引擎
func (s *Server) RegisterRoutes(r *gin.Engine) {
	s.registerUIRoutes(r)

	// 健康检查（无需认证）
	r.GET("/api/health", s.handleHealth)

	// 认证
	login := r.Group("/api/login")
	login.Use(s.rateLimitLogin())
	login.POST("", s.handleLogin)

	r.POST("/api/logout", s.handleLogout)
	r.GET("/api/user/info", s.handleUserInfo)
	r.POST("/api/user/password", s.handleChangePassword)
	// 钉钉配置
	r.GET("/api/dingtalk", s.handleDingTalkGet)
	r.POST("/api/dingtalk", s.handleDingTalkSave)
	// 配置管理（超管）
	r.GET("/api/config", s.handleConfigGet)
	r.POST("/api/config", s.handleConfigSave)
	r.POST("/api/admin/config/apply", s.handleAdminConfigApply)
	r.GET("/api/admin/migration-rules", s.handleAdminMigrationRulesGet)
	r.POST("/api/admin/migration-rules/refresh", s.handleAdminMigrationRulesRefresh)
	r.POST("/api/admin/migration-rules/upload", s.handleAdminMigrationRulesUpload)
	r.GET("/api/admin/picoclaw/channels", s.handlePicoClawAdminChannelsGet)
	r.GET("/api/picoclaw/channels", s.handlePicoClawChannelsGet)
	r.GET("/api/picoclaw/config-fields", s.handlePicoClawConfigFieldsGet)
	r.POST("/api/picoclaw/config-fields", s.handlePicoClawConfigFieldsSave)
	r.GET("/api/admin/task/status", s.handleAdminTaskStatus)
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
	// 超管 - 用户管理
	r.GET("/api/admin/users", s.handleAdminUsers)
	r.POST("/api/admin/users/create", s.handleAdminUserCreate)
	r.POST("/api/admin/users/delete", s.handleAdminUserDelete)
	// 超管 - 超管账户管理
	r.GET("/api/admin/superadmins", s.handleAdminSuperadmins)
	r.POST("/api/admin/superadmins/create", s.handleAdminSuperadminCreate)
	r.POST("/api/admin/superadmins/delete", s.handleAdminSuperadminDelete)
	r.POST("/api/admin/superadmins/reset", s.handleAdminSuperadminReset)
	r.POST("/api/admin/container/start", s.handleAdminContainerStart)
	r.POST("/api/admin/container/stop", s.handleAdminContainerStop)
	r.POST("/api/admin/container/restart", s.handleAdminContainerRestart)
	r.POST("/api/admin/container/debug", s.handleAdminContainerDebug)
	r.GET("/api/admin/container/logs", s.handleAdminContainerLogs)
	// 超管 - 白名单
	r.GET("/api/admin/whitelist", s.handleAdminWhitelistGet)
	r.POST("/api/admin/whitelist", s.handleAdminWhitelistPost)
	// 超管 - 认证配置
	r.POST("/api/admin/auth/test-ldap", s.handleAdminAuthTestLDAP)
	r.GET("/api/admin/auth/ldap-users", s.handleAdminAuthLDAPUsers)
	r.POST("/api/admin/auth/sync-groups", s.handleAdminAuthSyncGroups)
	// 超管 - 用户组
	r.GET("/api/admin/groups", s.handleAdminGroups)
	r.POST("/api/admin/groups/create", s.handleAdminGroupCreate)
	r.POST("/api/admin/groups/delete", s.handleAdminGroupDelete)
	r.GET("/api/admin/groups/members", s.handleAdminGroupMembers)
	r.POST("/api/admin/groups/members/add", s.handleAdminGroupMembersAdd)
	r.POST("/api/admin/groups/members/remove", s.handleAdminGroupMembersRemove)
	r.POST("/api/admin/groups/skills/bind", s.handleAdminGroupSkillsBind)
	r.POST("/api/admin/groups/skills/unbind", s.handleAdminGroupSkillsUnbind)
	// 超管 - 技能库
	r.GET("/api/admin/skills", s.handleAdminSkills)
	r.POST("/api/admin/skills/deploy", s.handleAdminSkillsDeploy)
	r.GET("/api/admin/skills/download", s.handleAdminSkillsDownload)
	r.POST("/api/admin/skills/remove", s.handleAdminSkillsRemove)
	r.POST("/api/admin/skills/upload", s.handleAdminSkillsUpload)
	// 超管 - 技能仓库
	r.GET("/api/admin/skills/repos/list", s.handleAdminSkillsReposList)
	r.POST("/api/admin/skills/repos/add", s.handleAdminSkillsReposAdd)
	r.POST("/api/admin/skills/repos/save", s.handleAdminSkillsReposSave)
	r.POST("/api/admin/skills/repos/pull", s.handleAdminSkillsReposPull)
	r.POST("/api/admin/skills/repos/remove", s.handleAdminSkillsReposRemove)
	r.POST("/api/admin/skills/install", s.handleAdminSkillsInstall)
	// 超管 - 镜像管理
	r.GET("/api/admin/images", s.handleAdminImages)
	r.POST("/api/admin/images/pull", s.handleAdminImagePull)
	r.POST("/api/admin/images/delete", s.handleAdminImageDelete)
	r.POST("/api/admin/images/migrate", s.handleAdminImageMigrate)
	r.POST("/api/admin/images/upgrade", s.handleAdminImageUpgrade)
	r.GET("/api/admin/images/registry", s.handleAdminImageRegistry)
	r.GET("/api/admin/images/local-tags", s.handleAdminLocalTags)
	r.GET("/api/admin/images/upgrade-candidates", s.handleAdminImageUpgradeCandidates)
	r.GET("/api/admin/images/users", s.handleAdminImageUsers)
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
func Serve(cfg *config.GlobalConfig, listenAddr string) error {
	if listenAddr == "" {
		listenAddr = cfg.Web.Listen
	}
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

	secret, err := ensureSessionSecret(cfg)
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

	// LDAP 定时同步
	var ldapStop chan struct{}
	if interval := cfg.SyncIntervalDuration(); interval > 0 && cfg.LDAPEnabled() {
		ldapStop = make(chan struct{})
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			slog.Info("LDAP 定时同步已启动", "interval", interval)
			for {
				select {
				case <-ticker.C:
					s.syncLDAPGroups()
				case <-ldapStop:
					slog.Info("LDAP 定时同步已停止")
					return
				}
			}
		}()
	}

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
		return gracefulShutdown(srv, redirectServer, dockerOK, ldapStop)
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
	return gracefulShutdown(srv, redirectServer, dockerOK, ldapStop)
}

// gracefulShutdown 优雅关闭 HTTP 服务器及相关资源
func gracefulShutdown(srv, redirectSrv *http.Server, dockerOK bool, ldapStop chan struct{}) error {
	if ldapStop != nil {
		close(ldapStop)
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

// ============================================================
// 镜像管理 Handler
// ============================================================

// handleAdminImages 列出本地镜像（含 Image ID、创建时间、用户依赖）
func (s *Server) handleAdminImages(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if !s.dockerAvailable {
		writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
		return
	}

	ctx := contextWithTimeout(10)
	images, err := dockerpkg.ListLocalImages(ctx, s.cfg.Image.Name)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取镜像列表失败: "+err.Error())
		return
	}

	// 查询所有用户的镜像引用，统计每个镜像的依赖用户
	containers, _ := auth.GetAllContainers()
	imageUsers := make(map[string][]string)
	for _, c := range containers {
		if c.Image != "" {
			imageUsers[c.Image] = append(imageUsers[c.Image], c.Username)
		}
	}

	type ImageInfo struct {
		ID         string   `json:"id"`
		FullID     string   `json:"full_id"`
		RepoTags   []string `json:"repo_tags"`
		Size       int64    `json:"size"`
		SizeStr    string   `json:"size_str"`
		Created    int64    `json:"created"`
		CreatedStr string   `json:"created_str"`
		UserCount  int      `json:"user_count"`
		Users      []string `json:"users"`
	}

	var list []ImageInfo
	for _, img := range images {
		// 短 ID（去掉 sha256: 前缀，取 12 位）
		shortID := img.ID
		if strings.HasPrefix(shortID, "sha256:") {
			shortID = shortID[7:]
		}
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}

		createdStr := ""
		if img.Created > 0 {
			createdStr = time.Unix(img.Created, 0).Format("2006-01-02 15:04")
		}

		// 统计此镜像所有 tag 的用户依赖
		var users []string
		for _, tag := range img.RepoTags {
			users = append(users, imageUsers[tag]...)
		}

		list = append(list, ImageInfo{
			ID:         shortID,
			FullID:     img.ID,
			RepoTags:   img.RepoTags,
			Size:       img.Size,
			SizeStr:    formatSize(img.Size),
			Created:    img.Created,
			CreatedStr: createdStr,
			UserCount:  len(users),
			Users:      users,
		})
	}
	if list == nil {
		list = []ImageInfo{}
	}

	writeJSON(c, http.StatusOK, struct {
		Success bool        `json:"success"`
		Images  []ImageInfo `json:"images"`
	}{true, list})
}

// handleAdminImagePull 拉取镜像（SSE 流式推送）
func (s *Server) handleAdminImagePull(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if !s.dockerAvailable {
		writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	tag := c.PostForm("tag")
	if tag == "" {
		writeError(c, http.StatusBadRequest, "标签参数不能为空")
		return
	}

	pullRef := s.cfg.Image.PullRef(tag)
	unifiedRef := s.cfg.Image.UnifiedRef(tag)

	// SSE 响应
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	flush := func() {
		c.Writer.Flush()
	}

	ctx := context.Background()
	reader, err := dockerpkg.ImagePull(ctx, pullRef)
	if err != nil {
		fmt.Fprintf(c.Writer, "data: {\"status\":\"error\",\"error\":\"%s\"}\n\n", err.Error())
		flush()
		return
	}
	defer reader.Close()

	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			fmt.Fprintf(c.Writer, "data: %s\n\n", buf[:n])
			flush()
		}
		if err != nil {
			break
		}
	}

	// 腾讯云模式：拉取后 retag 为统一名称
	if s.cfg.Image.IsTencent() && pullRef != unifiedRef {
		fmt.Fprintf(c.Writer, "data: {\"status\":\"重命名镜像: %s -> %s\"}\n\n", pullRef, unifiedRef)
		flush()
		if err := dockerpkg.RetagImage(ctx, pullRef, unifiedRef); err != nil {
			fmt.Fprintf(c.Writer, "data: {\"status\":\"error\",\"error\":\"重命名失败: %s\"}\n\n", err.Error())
			flush()
			return
		}
	}

	fmt.Fprintf(c.Writer, "data: {\"status\":\"done\"}\n\n")
	flush()
}

// handleAdminImageDelete 删除本地镜像（检查用户依赖）
func (s *Server) handleAdminImageDelete(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if !s.dockerAvailable {
		writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	imageRef := c.PostForm("image")
	if imageRef == "" {
		writeError(c, http.StatusBadRequest, "镜像参数不能为空")
		return
	}

	// 检查是否有用户依赖此镜像
	containers, err := auth.GetAllContainers()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "查询用户列表失败: "+err.Error())
		return
	}
	var dependentUsers []string
	for _, ctr := range containers {
		if ctr.Image == imageRef {
			dependentUsers = append(dependentUsers, ctr.Username)
		}
	}
	if len(dependentUsers) > 0 {
		// 查找本地其他可用镜像作为迁移目标
		ctxList := contextWithTimeout(10)
		localImgs, _ := dockerpkg.ListLocalImages(ctxList, s.cfg.Image.Name)
		var alternatives []string
		for _, img := range localImgs {
			for _, t := range img.RepoTags {
				if t != imageRef {
					alternatives = append(alternatives, t)
				}
			}
		}
		if alternatives == nil {
			alternatives = []string{}
		}
		writeJSON(c, http.StatusConflict, struct {
			Success      bool     `json:"success"`
			Error        string   `json:"error"`
			Users        []string `json:"users"`
			Alternatives []string `json:"alternatives"`
		}{false, "以下用户正在使用此镜像", dependentUsers, alternatives})
		return
	}

	// 检查镜像是否存在
	ctx := contextWithTimeout(30)
	if !dockerpkg.ImageExists(ctx, imageRef) {
		writeError(c, http.StatusNotFound, "镜像 "+imageRef+" 不存在")
		return
	}

	if err := dockerpkg.RemoveImage(ctx, imageRef); err != nil {
		writeError(c, http.StatusInternalServerError, "删除镜像失败: "+err.Error())
		return
	}

	writeSuccess(c, "镜像 "+imageRef+" 已删除")
}

// handleAdminImageMigrate 将用户从旧镜像迁移到新镜像（更新 DB + 重建容器）
func (s *Server) handleAdminImageMigrate(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if !s.dockerAvailable {
		writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	oldImage := c.PostForm("image")
	newImage := c.PostForm("target")
	if oldImage == "" || newImage == "" {
		writeError(c, http.StatusBadRequest, "必须指定旧镜像和新镜像")
		return
	}
	if oldImage == newImage {
		writeError(c, http.StatusBadRequest, "新旧镜像不能相同")
		return
	}
	migrator, err := user.NewPicoClawMigrationService(config.RuleCacheDir())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := migrator.EnsureUpgradeable(imageTagFromRef(oldImage), imageTagFromRef(newImage)); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 检查新镜像是否存在
	ctx := context.Background()
	if !dockerpkg.ImageExists(ctx, newImage) {
		writeError(c, http.StatusBadRequest, "新镜像 "+newImage+" 不存在，请先拉取")
		return
	}

	// 找出依赖旧镜像的用户
	containers, err := auth.GetAllContainers()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "查询用户列表失败")
		return
	}

	// 支持指定特定用户列表
	userFilter := c.PostForm("users")
	var targetUsers []string
	for _, ctr := range containers {
		if ctr.Image != oldImage {
			continue
		}
		if userFilter != "" {
			found := false
			for _, u := range strings.Split(userFilter, ",") {
				if strings.TrimSpace(u) == ctr.Username {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		targetUsers = append(targetUsers, ctr.Username)
	}

	if len(targetUsers) == 0 {
		writeError(c, http.StatusBadRequest, "没有用户使用旧镜像 "+oldImage)
		return
	}

	var success []string
	var failed []string

	for _, username := range targetUsers {
		fromTag := imageTagFromRef(oldImage)
		// 更新 DB 中的镜像引用
		if err := auth.UpdateContainerImage(username, newImage); err != nil {
			failed = append(failed, username+"(更新失败)")
			continue
		}

		// 如果容器正在运行，重建容器以使用新镜像
		rec, _ := auth.GetContainerByUsername(username)
		if rec == nil {
			failed = append(failed, username+"(记录不存在)")
			continue
		}
		if rec.ContainerID != "" {
			_ = dockerpkg.Stop(ctx, rec.ContainerID)
			_ = dockerpkg.Remove(ctx, rec.ContainerID)
			auth.UpdateContainerID(username, "")
		}
		// 只有原来是 running 状态才重新启动
		if rec.Status == "running" {
			ud := user.UserDir(s.cfg, username)
			cid, createErr := dockerpkg.CreateContainer(ctx, username, newImage, ud, rec.IP, rec.CPULimit, rec.MemoryLimit)
			if createErr != nil {
				failed = append(failed, username+"(创建失败)")
				continue
			}
			auth.UpdateContainerID(username, cid)
			if err := s.applyConfigForUpgrade(username, fromTag, imageTagFromRef(newImage)); err != nil {
				failed = append(failed, username+"(配置失败)")
				continue
			}
			if err := dockerpkg.Start(ctx, cid); err != nil {
				failed = append(failed, username+"(启动失败)")
				continue
			}
			auth.UpdateContainerStatus(username, "running")
		}
		success = append(success, username)
	}

	msg := fmt.Sprintf("迁移完成：%d 成功", len(success))
	if len(failed) > 0 {
		msg += fmt.Sprintf("，%d 失败：%s", len(failed), strings.Join(failed, ", "))
	}
	writeSuccess(c, msg)
}

// handleAdminImageUpgradeCandidates 查询可升级到指定版本的用户和分组
func (s *Server) handleAdminImageUpgradeCandidates(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}

	targetTag := c.Query("tag")
	if targetTag == "" {
		writeError(c, http.StatusBadRequest, "必须指定目标版本标签")
		return
	}
	newImage := s.cfg.Image.UnifiedRef(targetTag)

	containers, err := auth.GetAllContainers()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "查询用户列表失败")
		return
	}

	type userInfo struct {
		Username string `json:"username"`
		Image    string `json:"image"`
		Status   string `json:"status"`
		Groups   string `json:"groups"`
	}

	var users []userInfo
	for _, ctr := range containers {
		if ctr.Image == newImage {
			continue
		}
		if !strings.Contains(ctr.Image, s.cfg.Image.RepoName()) {
			continue
		}
		groups, _ := auth.GetGroupsForUser(ctr.Username)
		groupStr := strings.Join(groups, ", ")
		users = append(users, userInfo{
			Username: ctr.Username,
			Image:    ctr.Image,
			Status:   ctr.Status,
			Groups:   groupStr,
		})
	}

	allGroups, _ := auth.ListGroups()
	type groupInfo struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	groupCount := map[string]int{}
	for _, u := range users {
		gs, _ := auth.GetGroupsForUser(u.Username)
		seen := map[string]bool{}
		for _, g := range gs {
			if !seen[g] {
				groupCount[g]++
				seen[g] = true
			}
		}
	}
	var groups []groupInfo
	for _, g := range allGroups {
		if groupCount[g.Name] > 0 {
			groups = append(groups, groupInfo{Name: g.Name, Count: groupCount[g.Name]})
		}
	}

	writeJSON(c, http.StatusOK, map[string]interface{}{
		"success": true,
		"target":  newImage,
		"users":   users,
		"groups":  groups,
	})
}

// handleAdminImageUpgrade 拉取新版本镜像，然后用任务队列逐步升级用户
func (s *Server) handleAdminImageUpgrade(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if !s.dockerAvailable {
		writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	targetTag := c.PostForm("tag")
	if targetTag == "" {
		writeError(c, http.StatusBadRequest, "必须指定目标版本标签")
		return
	}
	newImage := s.cfg.Image.UnifiedRef(targetTag)

	// 解析目标用户列表
	var targetUsers []string
	if userList := c.PostForm("users"); userList != "" {
		for _, u := range strings.Split(userList, ",") {
			if v := strings.TrimSpace(u); v != "" {
				targetUsers = append(targetUsers, v)
			}
		}
	}

	if len(targetUsers) == 0 {
		writeError(c, http.StatusBadRequest, "必须指定要升级的用户")
		return
	}

	// 过滤出真正需要升级的用户
	var upgradeUsers []string
	fromTags := make(map[string]string)
	for _, username := range targetUsers {
		rec, _ := auth.GetContainerByUsername(username)
		if rec == nil {
			continue
		}
		if rec.Image == newImage {
			continue
		}
		if !strings.Contains(rec.Image, s.cfg.Image.RepoName()) {
			continue
		}
		fromTags[username] = imageTagFromRef(rec.Image)
		upgradeUsers = append(upgradeUsers, username)
	}

	if len(upgradeUsers) == 0 {
		writeError(c, http.StatusBadRequest, "没有需要升级的用户")
		return
	}
	migrator, err := user.NewPicoClawMigrationService(config.RuleCacheDir())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	for _, username := range upgradeUsers {
		if err := migrator.EnsureUpgradeable(fromTags[username], targetTag); err != nil {
			writeError(c, http.StatusBadRequest, username+": "+err.Error())
			return
		}
	}

	// SSE 响应
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	flush := func() {
		c.Writer.Flush()
	}
	sendStatus := func(status string) {
		fmt.Fprintf(c.Writer, "data: {\"status\":%q}\n\n", status)
		flush()
	}

	// 1. 同步拉取新镜像
	sendStatus("正在拉取镜像 " + newImage + " ...")
	pullRef := s.cfg.Image.PullRef(targetTag)
	ctx := context.Background()
	reader, err := dockerpkg.ImagePull(ctx, pullRef)
	if err != nil {
		fmt.Fprintf(c.Writer, "data: {\"status\":\"error\",\"error\":\"拉取失败: %s\"}\n\n", err.Error())
		flush()
		return
	}
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			c.Writer.Write(buf[:n])
			flush()
		}
		if err != nil {
			break
		}
	}

	// 腾讯云模式 retag
	if s.cfg.Image.IsTencent() && pullRef != newImage {
		sendStatus("重命名镜像 " + pullRef + " -> " + newImage)
		if err := dockerpkg.RetagImage(ctx, pullRef, newImage); err != nil {
			fmt.Fprintf(c.Writer, "data: {\"status\":\"error\",\"error\":\"重命名失败: %s\"}\n\n", err.Error())
			flush()
			return
		}
	}

	// 2. 提交到任务队列逐步执行
	img := newImage // 闭包捕获
	taskFn := func(username string) error {
		fromTag := fromTags[username]
		if err := auth.UpdateContainerImage(username, img); err != nil {
			return fmt.Errorf("更新失败: %w", err)
		}
		rec, _ := auth.GetContainerByUsername(username)
		if rec == nil {
			return fmt.Errorf("记录不存在")
		}
		if rec.ContainerID != "" {
			_ = dockerpkg.Stop(ctx, rec.ContainerID)
			_ = dockerpkg.Remove(ctx, rec.ContainerID)
			auth.UpdateContainerID(username, "")
		}
		if rec.Status == "running" {
			ud := user.UserDir(s.cfg, username)
			cid, createErr := dockerpkg.CreateContainer(ctx, username, img, ud, rec.IP, rec.CPULimit, rec.MemoryLimit)
			if createErr != nil {
				return fmt.Errorf("创建失败: %w", createErr)
			}
			auth.UpdateContainerID(username, cid)
			if err := s.applyConfigForUpgrade(username, fromTag, targetTag); err != nil {
				return fmt.Errorf("配置失败: %w", err)
			}
			if err := dockerpkg.Start(ctx, cid); err != nil {
				return fmt.Errorf("启动失败: %w", err)
			}
			auth.UpdateContainerStatus(username, "running")
		}
		return nil
	}

	taskID, err := enqueueTask("upgrade", upgradeUsers, taskFn)
	if err != nil {
		fmt.Fprintf(c.Writer, "data: {\"status\":\"error\",\"error\":\"%s\"}\n\n", err.Error())
		flush()
		return
	}

	sendStatus(fmt.Sprintf("镜像就绪，已提交升级任务 %s，共 %d 个用户排队中（每 2 秒处理一个）", taskID, len(upgradeUsers)))
	fmt.Fprintf(c.Writer, "data: {\"status\":\"done\",\"message\":\"镜像拉取完成，%d 个用户已进入升级队列\",\"task_id\":%q,\"total\":%d}\n\n", len(upgradeUsers), taskID, len(upgradeUsers))
	flush()
}

// handleAdminImageRegistry 列出 PicoAide 远程仓库标签
func (s *Server) handleAdminImageRegistry(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if !s.dockerAvailable {
		writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
		return
	}

	// 远程标签始终从 GitHub Container Registry 获取
	ctx := contextWithTimeout(15)
	tags, err := dockerpkg.ListRegistryTags(ctx, s.cfg.Image.RepoName())
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取远程标签失败: "+err.Error())
		return
	}
	if tags == nil {
		tags = []string{}
	}

	writeJSON(c, http.StatusOK, struct {
		Success bool     `json:"success"`
		Tags    []string `json:"tags"`
	}{true, tags})
}

// handleAdminLocalTags 列出本地镜像的所有标签
func (s *Server) handleAdminLocalTags(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if !s.dockerAvailable {
		writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
		return
	}

	ctx := contextWithTimeout(10)
	tags, err := dockerpkg.ListLocalTags(ctx, s.cfg.Image.Name)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取本地标签失败: "+err.Error())
		return
	}
	if tags == nil {
		tags = []string{}
	}

	writeJSON(c, http.StatusOK, struct {
		Success bool     `json:"success"`
		Tags    []string `json:"tags"`
	}{true, tags})
}

// handleAdminImageUsers 返回使用指定镜像的用户列表
func (s *Server) handleAdminImageUsers(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}

	image := c.Query("image")
	if image == "" {
		writeError(c, http.StatusBadRequest, "缺少 image 参数")
		return
	}

	containers, _ := auth.GetAllContainers()
	var users []string
	for _, ctr := range containers {
		if ctr.Image == image {
			users = append(users, ctr.Username)
		}
	}
	if users == nil {
		users = []string{}
	}

	writeJSON(c, http.StatusOK, map[string]interface{}{
		"success": true,
		"users":   users,
	})
}

func imageTagFromRef(imageRef string) string {
	idx := strings.LastIndex(imageRef, ":")
	if idx < 0 || idx == len(imageRef)-1 {
		return ""
	}
	slashIdx := strings.LastIndex(imageRef, "/")
	if slashIdx > idx {
		return ""
	}
	return imageRef[idx+1:]
}
