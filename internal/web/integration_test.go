package web

import (
  "encoding/json"
  "io"
  "net/http"
  "net/http/httptest"
  "net/url"
  "os"
  "path/filepath"
  "strings"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// 集成测试基础设施
// ============================================================

// testEnv 封装测试服务器及其依赖
type testEnv struct {
  Server *Server
  HTTP   *httptest.Server
  Cfg    *config.GlobalConfig
}

// setupTestServer 创建完整的测试环境（数据库、用户、HTTP 服务器）
func setupTestServer(t *testing.T) *testEnv {
  t.Helper()

  // 关闭已有数据库连接
  auth.ResetDB()

  // 创建临时目录
  tmpDir := t.TempDir()

  // 初始化数据库
  if err := auth.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }

  // 写入默认配置到 settings 表
  if err := config.InitDBDefaults(); err != nil {
    t.Fatalf("InitDBDefaults: %v", err)
  }

  // 从数据库加载配置
  cfg, err := config.LoadFromDB()
  if err != nil {
    t.Fatalf("LoadFromDB: %v", err)
  }

  // 设置用户目录和归档目录到临时目录
  cfg.UsersRoot = filepath.Join(tmpDir, "users")
  cfg.ArchiveRoot = filepath.Join(tmpDir, "archive")

  // 使用本地认证模式（测试环境不依赖 LDAP）
  cfg.Web.AuthMode = "local"

  // 确保用户根目录存在
  if err := user.EnsureUsersRoot(cfg); err != nil {
    t.Fatalf("EnsureUsersRoot: %v", err)
  }

  // 创建超管
  if err := auth.CreateUser("testadmin", "admin123", "superadmin"); err != nil {
    t.Fatalf("CreateUser(testadmin): %v", err)
  }

  // 创建普通用户
  if err := auth.CreateUser("testuser", "user123", "user"); err != nil {
    t.Fatalf("CreateUser(testuser): %v", err)
  }

  // 初始化普通用户的工作目录和容器记录
  if err := user.InitUser(cfg, "testuser", ""); err != nil {
    t.Fatalf("InitUser(testuser): %v", err)
  }

  // 确保工作区目录存在（文件管理 API 依赖此目录）
  workspaceDir := filepath.Join(user.UserDir(cfg, "testuser"), ".picoclaw", "workspace")
  if err := os.MkdirAll(workspaceDir, 0755); err != nil {
    t.Fatalf("MkdirAll workspace: %v", err)
  }

  // 创建 Server 实例（Docker 不可用）
  s := &Server{
    cfg:            cfg,
    secret:         "test-integration-secret",
    csrfKey:        "test-integration-secret-csrf",
    dockerAvailable: false,
  }

  // 注册所有路由
  mux := buildTestMux(s)

  // 创建 HTTP 测试服务器
  httpServer := httptest.NewServer(mux)
  t.Cleanup(httpServer.Close)

  return &testEnv{
    Server: s,
    HTTP:   httpServer,
    Cfg:    cfg,
  }
}

// buildTestMux 注册所有 API 路由（与 Serve() 一致）
func buildTestMux(s *Server) *http.ServeMux {
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
  mux.HandleFunc("/api/admin/task/status", s.secureHeaders(s.handleAdminTaskStatus))
  // 文件管理
  mux.HandleFunc("/api/files", s.secureHeaders(s.handleFiles))
  mux.HandleFunc("/api/files/upload", s.secureHeaders(s.handleFileUpload))
  mux.HandleFunc("/api/files/download", s.secureHeaders(s.handleFileDownload))
  mux.HandleFunc("/api/files/delete", s.secureHeaders(s.handleFileDelete))
  mux.HandleFunc("/api/files/mkdir", s.secureHeaders(s.handleFileMkdir))
  mux.HandleFunc("/api/files/edit", s.secureHeaders(s.handleFileEdit))
  // Cookie 同步
  mux.HandleFunc("/api/cookies", s.secureHeaders(s.handleCookies))
  // CSRF token
  mux.HandleFunc("/api/csrf", s.secureHeaders(s.handleCSRF))
  // MCP token
  mux.HandleFunc("/api/mcp/token", s.secureHeaders(s.handleMCPToken))
  // MCP SSE 服务
  mux.HandleFunc("/api/mcp/sse/{service}", func(w http.ResponseWriter, r *http.Request) {
    s.secureHeaders(func(w http.ResponseWriter, r *http.Request) {
      s.handleMCPSSEService(w, r, r.PathValue("service"))
    })(w, r)
  })
  // Browser Extension WebSocket
  mux.HandleFunc("/api/browser/ws", s.secureHeaders(s.handleBrowserWS))
  // Computer 桌面代理 WebSocket
  mux.HandleFunc("/api/computer/ws", s.secureHeaders(s.handleComputerWS))
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
  mux.HandleFunc("/api/admin/images/upgrade", s.secureHeaders(s.handleAdminImageUpgrade))
  mux.HandleFunc("/api/admin/images/registry", s.secureHeaders(s.handleAdminImageRegistry))
  mux.HandleFunc("/api/admin/images/local-tags", s.secureHeaders(s.handleAdminLocalTags))
  mux.HandleFunc("/api/admin/images/upgrade-candidates", s.secureHeaders(s.handleAdminImageUpgradeCandidates))
  mux.HandleFunc("/api/admin/images/users", s.secureHeaders(s.handleAdminImageUsers))

  return mux
}

// ============================================================
// testEnv 辅助方法
// ============================================================

// authRequest 发送带 session cookie 和 CSRF token 的 HTTP 请求
func (env *testEnv) authRequest(t *testing.T, method, path, username string, form url.Values) *http.Response {
  t.Helper()

  // POST 请求需要 CSRF token
  if method == "POST" && form == nil {
    form = url.Values{}
  }

  var bodyReader io.Reader
  if form != nil {
    // 在编码前注入 CSRF token
    form.Set("csrf_token", env.Server.csrfToken(username))
    bodyReader = strings.NewReader(form.Encode())
  }

  req, err := http.NewRequest(method, env.HTTP.URL+path, bodyReader)
  if err != nil {
    t.Fatalf("创建请求失败: %v", err)
  }

  if form != nil {
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  }

  // 添加 session cookie
  req.AddCookie(&http.Cookie{
    Name:  "session",
    Value: env.Server.createSessionToken(username),
  })

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("发送请求失败: %v", err)
  }
  return resp
}

// get 发送带认证的 GET 请求
func (env *testEnv) get(t *testing.T, path, username string) *http.Response {
  t.Helper()
  return env.authRequest(t, "GET", path, username, nil)
}

// postForm 发送带认证和 CSRF token 的 POST 请求
func (env *testEnv) postForm(t *testing.T, path, username string, form url.Values) *http.Response {
  t.Helper()
  return env.authRequest(t, "POST", path, username, form)
}

// ============================================================
// 通用断言和解析辅助函数
// ============================================================

// assertStatus 检查 HTTP 响应状态码
func assertStatus(t *testing.T, resp *http.Response, expected int) {
  t.Helper()
  if resp.StatusCode != expected {
    t.Errorf("status=%d, want %d", resp.StatusCode, expected)
  }
}

// parseJSON 解码 JSON 响应体到 v
func parseJSON(t *testing.T, resp *http.Response, v interface{}) {
  t.Helper()
  defer resp.Body.Close()
  if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
}

// getJSON 解码 JSON 响应体到通用 map
func getJSON(t *testing.T, resp *http.Response) map[string]interface{} {
  t.Helper()
  defer resp.Body.Close()
  var result map[string]interface{}
  if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  return result
}
