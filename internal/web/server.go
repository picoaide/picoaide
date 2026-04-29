package web

import (
  "context"
  "crypto/hmac"
  "crypto/sha256"
  "encoding/hex"
  "fmt"
  "net/http"
  "os"
  "strconv"
  "strings"
  "time"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// Web 服务器
// ============================================================

// Server 是 Web 管理面板服务器
type Server struct {
  cfg            *config.GlobalConfig
  secret         string
  csrfKey        string
  dockerAvailable bool
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
func (s *Server) getSessionUser(r *http.Request) string {
  cookie, err := r.Cookie("session")
  if err != nil {
    return ""
  }
  username, ok := s.parseSessionToken(cookie.Value)
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
func (s *Server) checkCSRF(r *http.Request) bool {
  username := s.getSessionUser(r)
  if username == "" {
    return false
  }
  token := r.FormValue("csrf_token")
  return hmac.Equal([]byte(token), []byte(s.csrfToken(username)))
}

// secureHeaders 安全 Header 中间件
func (s *Server) secureHeaders(next http.HandlerFunc) http.HandlerFunc {
  return func(w http.ResponseWriter, r *http.Request) {
    origin := r.Header.Get("Origin")
    if strings.HasPrefix(origin, "chrome-extension://") || strings.HasPrefix(origin, "moz-extension://") {
      w.Header().Set("Access-Control-Allow-Origin", origin)
      w.Header().Set("Access-Control-Allow-Credentials", "true")
      w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
      w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
    }
    // CORS preflight
    if r.Method == "OPTIONS" {
      w.WriteHeader(http.StatusOK)
      return
    }
    w.Header().Set("X-Content-Type-Options", "nosniff")
    w.Header().Set("X-Frame-Options", "DENY")
    w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
    next(w, r)
  }
}

// setSessionCookie 设置 session cookie
func (s *Server) setSessionCookie(w http.ResponseWriter, value string, maxAge int) {
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
  http.SetCookie(w, cookie)
}

// Serve 创建并启动 Web 管理面板服务器
func Serve(cfg *config.GlobalConfig, listenAddr string) error {
  if listenAddr == "" {
    listenAddr = cfg.Web.Listen
  }
  if listenAddr == "" {
    listenAddr = ":80"
  }

  secret := cfg.Web.Password
  if secret == "" {
    secret = config.SessionSecret
    fmt.Fprintln(os.Stderr, "警告: 未配置 web.password，使用默认 session 密钥，请尽快修改")
  }

  csrfKey := secret + "-csrf"

  // 确保工作目录存在
  wd, _ := os.Getwd()
  if wd != "" {
    os.MkdirAll(wd, 0755)
  }

  // 确保 users/ 和 archive/ 目录存在
  if err := user.EnsureUsersRoot(cfg); err != nil {
    fmt.Fprintf(os.Stderr, "警告: 创建用户目录失败: %v\n", err)
  }

  // 初始化本地用户数据库（失败时重试一次）
  if err := auth.InitDB(wd); err != nil {
    fmt.Fprintf(os.Stderr, "警告: 数据库初始化失败，正在重试: %v\n", err)
    os.MkdirAll(wd, 0755)
    if err := auth.InitDB(wd); err != nil {
      return fmt.Errorf("初始化用户数据库失败: %w", err)
    }
  }

  // 初始化 Docker 客户端
  dockerOK := false
  if err := dockerpkg.InitClient(); err != nil {
    fmt.Fprintf(os.Stderr, "警告: Docker 不可用，容器操作将被禁用: %v\n", err)
  } else {
    defer dockerpkg.CloseClient()
    ctx := contextWithTimeout(5)
    if err := dockerpkg.EnsureNetwork(ctx); err != nil {
      fmt.Fprintf(os.Stderr, "警告: 网络初始化失败: %v\n", err)
    }
    dockerOK = true
  }

  s := &Server{
    cfg:            cfg,
    secret:         secret,
    csrfKey:        csrfKey,
    dockerAvailable: dockerOK,
  }

  mux := http.NewServeMux()
  // 认证
  mux.HandleFunc("/api/login", s.secureHeaders(s.handleLogin))
  mux.HandleFunc("/api/logout", s.secureHeaders(s.handleLogout))
  mux.HandleFunc("/api/user/info", s.secureHeaders(s.handleUserInfo))
  mux.HandleFunc("/api/user/password", s.secureHeaders(s.handleChangePassword))
  // 钉钉配置
  mux.HandleFunc("/api/dingtalk", s.secureHeaders(s.handleDingTalk))
  // 配置管理（超管）
  mux.HandleFunc("/api/config", s.secureHeaders(s.handleConfig))
  mux.HandleFunc("/api/admin/config/apply", s.secureHeaders(s.handleAdminConfigApply))
  // 文件管理
  mux.HandleFunc("/api/files", s.secureHeaders(s.handleFiles))
  mux.HandleFunc("/api/files/upload", s.secureHeaders(s.handleFileUpload))
  mux.HandleFunc("/api/files/download", s.secureHeaders(s.handleFileDownload))
  mux.HandleFunc("/api/files/delete", s.secureHeaders(s.handleFileDelete))
  mux.HandleFunc("/api/files/mkdir", s.secureHeaders(s.handleFileMkdir))
  mux.HandleFunc("/api/files/edit", s.secureHeaders(s.handleFileEdit))
  // Cookie 同步（写入用户 .security.yml）
  mux.HandleFunc("/api/cookies", s.secureHeaders(s.handleCookies))
  // CSRF token
  mux.HandleFunc("/api/csrf", s.secureHeaders(s.handleCSRF))
  // MCP token（Extension 获取认证 token）
  mux.HandleFunc("/api/mcp/token", s.secureHeaders(s.handleMCPToken))
  // Browser MCP Server（SSE + JSON-RPC）
  mux.HandleFunc("/api/browser/mcp/sse", s.secureHeaders(s.handleBrowserMCPSSE))
  mux.HandleFunc("/api/browser/mcp", s.secureHeaders(s.handleBrowserMCPMessage))
  // Browser Extension WebSocket
  mux.HandleFunc("/api/browser/ws", s.secureHeaders(s.handleBrowserWS))
  // 超管 - 用户管理
  mux.HandleFunc("/api/admin/users", s.secureHeaders(s.handleAdminUsers))
  mux.HandleFunc("/api/admin/users/create", s.secureHeaders(s.handleAdminUserCreate))
  mux.HandleFunc("/api/admin/users/delete", s.secureHeaders(s.handleAdminUserDelete))
  // 超管 - 超管账户管理
  mux.HandleFunc("/api/admin/superadmins", s.secureHeaders(s.handleAdminSuperadmins))
  mux.HandleFunc("/api/admin/superadmins/create", s.secureHeaders(s.handleAdminSuperadminCreate))
  mux.HandleFunc("/api/admin/superadmins/delete", s.secureHeaders(s.handleAdminSuperadminDelete))
  mux.HandleFunc("/api/admin/superadmins/reset", s.secureHeaders(s.handleAdminSuperadminReset))
  mux.HandleFunc("/api/admin/container/start", s.secureHeaders(s.handleAdminContainerStart))
  mux.HandleFunc("/api/admin/container/stop", s.secureHeaders(s.handleAdminContainerStop))
  mux.HandleFunc("/api/admin/container/restart", s.secureHeaders(s.handleAdminContainerRestart))
  mux.HandleFunc("/api/admin/container/logs", s.secureHeaders(s.handleAdminContainerLogs))
  // 超管 - 白名单
  mux.HandleFunc("/api/admin/whitelist", s.secureHeaders(s.handleAdminWhitelist))
  // 超管 - 认证配置
  mux.HandleFunc("/api/admin/auth/test-ldap", s.secureHeaders(s.handleAdminAuthTestLDAP))
  mux.HandleFunc("/api/admin/auth/ldap-users", s.secureHeaders(s.handleAdminAuthLDAPUsers))
  mux.HandleFunc("/api/admin/auth/sync-groups", s.secureHeaders(s.handleAdminAuthSyncGroups))
  // 超管 - 用户组
  mux.HandleFunc("/api/admin/groups", s.secureHeaders(s.handleAdminGroups))
  mux.HandleFunc("/api/admin/groups/create", s.secureHeaders(s.handleAdminGroupCreate))
  mux.HandleFunc("/api/admin/groups/delete", s.secureHeaders(s.handleAdminGroupDelete))
  mux.HandleFunc("/api/admin/groups/members", s.secureHeaders(s.handleAdminGroupMembers))
  mux.HandleFunc("/api/admin/groups/members/add", s.secureHeaders(s.handleAdminGroupMembersAdd))
  mux.HandleFunc("/api/admin/groups/members/remove", s.secureHeaders(s.handleAdminGroupMembersRemove))
  mux.HandleFunc("/api/admin/groups/skills/bind", s.secureHeaders(s.handleAdminGroupSkillsBind))
  mux.HandleFunc("/api/admin/groups/skills/unbind", s.secureHeaders(s.handleAdminGroupSkillsUnbind))
  // 超管 - 技能库
  mux.HandleFunc("/api/admin/skills", s.secureHeaders(s.handleAdminSkills))
  mux.HandleFunc("/api/admin/skills/deploy", s.secureHeaders(s.handleAdminSkillsDeploy))
  mux.HandleFunc("/api/admin/skills/download", s.secureHeaders(s.handleAdminSkillsDownload))
  mux.HandleFunc("/api/admin/skills/remove", s.secureHeaders(s.handleAdminSkillsRemove))
  // 超管 - 技能仓库
  mux.HandleFunc("/api/admin/skills/repos/list", s.secureHeaders(s.handleAdminSkillsReposList))
  mux.HandleFunc("/api/admin/skills/repos/add", s.secureHeaders(s.handleAdminSkillsReposAdd))
  mux.HandleFunc("/api/admin/skills/repos/pull", s.secureHeaders(s.handleAdminSkillsReposPull))
  mux.HandleFunc("/api/admin/skills/repos/remove", s.secureHeaders(s.handleAdminSkillsReposRemove))
  mux.HandleFunc("/api/admin/skills/install", s.secureHeaders(s.handleAdminSkillsInstall))
  // 超管 - 镜像管理
  mux.HandleFunc("/api/admin/images", s.secureHeaders(s.handleAdminImages))
  mux.HandleFunc("/api/admin/images/pull", s.secureHeaders(s.handleAdminImagePull))
  mux.HandleFunc("/api/admin/images/delete", s.secureHeaders(s.handleAdminImageDelete))
  mux.HandleFunc("/api/admin/images/migrate", s.secureHeaders(s.handleAdminImageMigrate))
  mux.HandleFunc("/api/admin/images/registry", s.secureHeaders(s.handleAdminImageRegistry))
  mux.HandleFunc("/api/admin/images/local-tags", s.secureHeaders(s.handleAdminLocalTags))

  if cfg.Web.TLS.Enabled && cfg.Web.TLS.CertFile != "" && cfg.Web.TLS.KeyFile != "" {
    if _, err := os.Stat(cfg.Web.TLS.CertFile); err != nil {
      return fmt.Errorf("证书文件不存在: %s", cfg.Web.TLS.CertFile)
    }
    if _, err := os.Stat(cfg.Web.TLS.KeyFile); err != nil {
      return fmt.Errorf("私钥文件不存在: %s", cfg.Web.TLS.KeyFile)
    }

    // 如果监听 443，额外启动 80 端口 HTTP→HTTPS 重定向
    if strings.HasSuffix(listenAddr, ":443") {
      go func() {
        redirectMux := http.NewServeMux()
        redirectMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
          target := "https://" + r.Host + r.URL.Path
          if r.URL.RawQuery != "" {
            target += "?" + r.URL.RawQuery
          }
          http.Redirect(w, r, target, http.StatusMovedPermanently)
        })
        fmt.Fprintf(os.Stderr, "HTTP→HTTPS 重定向: :80\n")
        if err := http.ListenAndServe(":80", redirectMux); err != nil {
          fmt.Fprintf(os.Stderr, "重定向服务错误: %v\n", err)
        }
      }()
    }

    fmt.Printf("PicoClaw 管理面板启动: https://%s\n", listenAddr)
    return http.ListenAndServeTLS(listenAddr, cfg.Web.TLS.CertFile, cfg.Web.TLS.KeyFile, mux)
  }

  fmt.Printf("PicoClaw 管理面板启动: http://%s\n", listenAddr)
  return http.ListenAndServe(listenAddr, mux)
}

func contextWithTimeout(sec int) context.Context {
  ctx, _ := context.WithTimeout(context.Background(), time.Duration(sec)*time.Second)
  return ctx
}

// ============================================================
// 镜像管理 Handler
// ============================================================

// handleAdminImages 列出本地镜像（含 Image ID、创建时间、用户依赖）
func (s *Server) handleAdminImages(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(w, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  ctx := contextWithTimeout(10)
  images, err := dockerpkg.ListLocalImages(ctx, s.cfg.Image.Name)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "获取镜像列表失败: "+err.Error())
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

  writeJSON(w, http.StatusOK, struct {
    Success bool        `json:"success"`
    Images  []ImageInfo `json:"images"`
  }{true, list})
}

// handleAdminImagePull 拉取镜像（SSE 流式推送）
func (s *Server) handleAdminImagePull(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(w, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
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

  tag := r.FormValue("tag")
  if tag == "" {
    writeError(w, http.StatusBadRequest, "标签参数不能为空")
    return
  }

  pullRef := s.cfg.Image.PullRef(tag)
  unifiedRef := s.cfg.Image.UnifiedRef(tag)

  // SSE 响应
  w.Header().Set("Content-Type", "text/event-stream")
  w.Header().Set("Cache-Control", "no-cache")
  w.Header().Set("Connection", "keep-alive")

  flush := func() {
    if f, ok := w.(http.Flusher); ok {
      f.Flush()
    }
  }

  ctx := context.Background()
  reader, err := dockerpkg.ImagePull(ctx, pullRef)
  if err != nil {
    fmt.Fprintf(w, "data: {\"status\":\"error\",\"error\":\"%s\"}\n\n", err.Error())
    flush()
    return
  }
  defer reader.Close()

  buf := make([]byte, 4096)
  for {
    n, err := reader.Read(buf)
    if n > 0 {
      fmt.Fprintf(w, "data: %s\n\n", buf[:n])
      flush()
    }
    if err != nil {
      break
    }
  }

  // 腾讯云模式：拉取后 retag 为统一名称
  if s.cfg.Image.IsTencent() && pullRef != unifiedRef {
    fmt.Fprintf(w, "data: {\"status\":\"重命名镜像: %s -> %s\"}\n\n", pullRef, unifiedRef)
    flush()
    if err := dockerpkg.RetagImage(ctx, pullRef, unifiedRef); err != nil {
      fmt.Fprintf(w, "data: {\"status\":\"error\",\"error\":\"重命名失败: %s\"}\n\n", err.Error())
      flush()
      return
    }
  }

  fmt.Fprintf(w, "data: {\"status\":\"done\"}\n\n")
  flush()
}

// handleAdminImageDelete 删除本地镜像（检查用户依赖）
func (s *Server) handleAdminImageDelete(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(w, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
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

  imageRef := r.FormValue("image")
  if imageRef == "" {
    writeError(w, http.StatusBadRequest, "镜像参数不能为空")
    return
  }

  // 检查是否有用户依赖此镜像
  containers, err := auth.GetAllContainers()
  if err != nil {
    writeError(w, http.StatusInternalServerError, "查询用户列表失败: "+err.Error())
    return
  }
  var dependentUsers []string
  for _, c := range containers {
    if c.Image == imageRef {
      dependentUsers = append(dependentUsers, c.Username)
    }
  }
  if len(dependentUsers) > 0 {
    // 查找本地其他可用镜像作为迁移目标
    ctxList := contextWithTimeout(10)
    localImgs, _ := dockerpkg.ListLocalImages(ctxList, s.cfg.Image.Name)
    var alternatives []string
    for _, img := range localImgs {
      for _, tag := range img.RepoTags {
        if tag != imageRef {
          alternatives = append(alternatives, tag)
        }
      }
    }
    if alternatives == nil {
      alternatives = []string{}
    }
    writeJSON(w, http.StatusConflict, struct {
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
    writeError(w, http.StatusNotFound, "镜像 "+imageRef+" 不存在")
    return
  }

  if err := dockerpkg.RemoveImage(ctx, imageRef); err != nil {
    writeError(w, http.StatusInternalServerError, "删除镜像失败: "+err.Error())
    return
  }

  writeSuccess(w, "镜像 "+imageRef+" 已删除")
}

// handleAdminImageMigrate 将用户从旧镜像迁移到新镜像（更新 DB + 重建容器）
func (s *Server) handleAdminImageMigrate(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(w, http.StatusServiceUnavailable, "Docker 服务不可用")
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

  oldImage := r.FormValue("image")
  newImage := r.FormValue("target")
  if oldImage == "" || newImage == "" {
    writeError(w, http.StatusBadRequest, "必须指定旧镜像和新镜像")
    return
  }
  if oldImage == newImage {
    writeError(w, http.StatusBadRequest, "新旧镜像不能相同")
    return
  }

  // 检查新镜像是否存在
  ctx := context.Background()
  if !dockerpkg.ImageExists(ctx, newImage) {
    writeError(w, http.StatusBadRequest, "新镜像 "+newImage+" 不存在，请先拉取")
    return
  }

  // 找出依赖旧镜像的用户
  containers, err := auth.GetAllContainers()
  if err != nil {
    writeError(w, http.StatusInternalServerError, "查询用户列表失败")
    return
  }

  // 支持指定特定用户列表
  userFilter := r.FormValue("users")
  var targetUsers []string
  for _, c := range containers {
    if c.Image != oldImage {
      continue
    }
    if userFilter != "" {
      found := false
      for _, u := range strings.Split(userFilter, ",") {
        if strings.TrimSpace(u) == c.Username {
          found = true
          break
        }
      }
      if !found {
        continue
      }
    }
    targetUsers = append(targetUsers, c.Username)
  }

  if len(targetUsers) == 0 {
    writeError(w, http.StatusBadRequest, "没有用户使用旧镜像 "+oldImage)
    return
  }

  var success []string
  var failed []string

  for _, username := range targetUsers {
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
  writeSuccess(w, msg)
}

// handleAdminImageRegistry 列出 PicoAide 远程仓库标签
func (s *Server) handleAdminImageRegistry(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(w, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  // 固定查询 picoaide/picoaide 仓库
  ctx := contextWithTimeout(15)
  tags, err := dockerpkg.ListRegistryTagsForConfig(ctx, "picoaide/picoaide", s.cfg.Image.Registry)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "获取远程标签失败: "+err.Error())
    return
  }
  if tags == nil {
    tags = []string{}
  }

  writeJSON(w, http.StatusOK, struct {
    Success bool     `json:"success"`
    Tags    []string `json:"tags"`
  }{true, tags})
}

// handleAdminLocalTags 列出本地镜像的所有标签
func (s *Server) handleAdminLocalTags(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(w, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  ctx := contextWithTimeout(10)
  tags, err := dockerpkg.ListLocalTags(ctx, s.cfg.Image.Name)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "获取本地标签失败: "+err.Error())
    return
  }
  if tags == nil {
    tags = []string{}
  }

  writeJSON(w, http.StatusOK, struct {
    Success bool     `json:"success"`
    Tags    []string `json:"tags"`
  }{true, tags})
}
