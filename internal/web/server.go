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
  http.SetCookie(w, &http.Cookie{
    Name:     "session",
    Value:    value,
    Path:     "/",
    HttpOnly: true,
    SameSite: http.SameSiteLaxMode,
    MaxAge:   maxAge,
  })
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
  // 文件管理
  mux.HandleFunc("/api/files", s.secureHeaders(s.handleFiles))
  mux.HandleFunc("/api/files/upload", s.secureHeaders(s.handleFileUpload))
  mux.HandleFunc("/api/files/download", s.secureHeaders(s.handleFileDownload))
  mux.HandleFunc("/api/files/delete", s.secureHeaders(s.handleFileDelete))
  mux.HandleFunc("/api/files/mkdir", s.secureHeaders(s.handleFileMkdir))
  mux.HandleFunc("/api/files/edit", s.secureHeaders(s.handleFileEdit))
  // CSRF token
  mux.HandleFunc("/api/csrf", s.secureHeaders(s.handleCSRF))
  // 超管 - 用户管理
  mux.HandleFunc("/api/admin/users", s.secureHeaders(s.handleAdminUsers))
  mux.HandleFunc("/api/admin/users/create", s.secureHeaders(s.handleAdminUserCreate))
  mux.HandleFunc("/api/admin/users/delete", s.secureHeaders(s.handleAdminUserDelete))
  mux.HandleFunc("/api/admin/container/start", s.secureHeaders(s.handleAdminContainerStart))
  mux.HandleFunc("/api/admin/container/stop", s.secureHeaders(s.handleAdminContainerStop))
  mux.HandleFunc("/api/admin/container/restart", s.secureHeaders(s.handleAdminContainerRestart))
  // 超管 - 白名单
  mux.HandleFunc("/api/admin/whitelist", s.secureHeaders(s.handleAdminWhitelist))
  // 超管 - 认证配置
  mux.HandleFunc("/api/admin/auth/test-ldap", s.secureHeaders(s.handleAdminAuthTestLDAP))
  mux.HandleFunc("/api/admin/auth/ldap-users", s.secureHeaders(s.handleAdminAuthLDAPUsers))
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
  mux.HandleFunc("/api/admin/images/registry", s.secureHeaders(s.handleAdminImageRegistry))
  mux.HandleFunc("/api/admin/images/local-tags", s.secureHeaders(s.handleAdminLocalTags))

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

// handleAdminImages 列出本地镜像
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

  type ImageInfo struct {
    ID         string   `json:"id"`
    RepoTags   []string `json:"repo_tags"`
    Size       int64    `json:"size"`
    SizeStr    string   `json:"size_str"`
    Created    int64    `json:"created"`
  }

  var list []ImageInfo
  for _, img := range images {
    list = append(list, ImageInfo{
      ID:       img.ID,
      RepoTags: img.RepoTags,
      Size:     img.Size,
      SizeStr:  formatSize(img.Size),
      Created:  img.Created,
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
    writeJSON(w, http.StatusConflict, struct {
      Success bool     `json:"success"`
      Error   string   `json:"error"`
      Users   []string `json:"users"`
    }{false, "以下用户正在使用此镜像，请先升级或降级后再删除", dependentUsers})
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
  tags, err := dockerpkg.ListRegistryTags(ctx, "picoaide/picoaide")
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
