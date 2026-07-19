package web

import (
  "bytes"
  "encoding/json"
  "io"
  "mime/multipart"
  "net/http"
  "net/http/httptest"
  "net/url"
  "os"
  "path/filepath"
  "testing"
  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/store"
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
  store.ResetDB()

  // 创建临时目录
  tmpDir := t.TempDir()
  config.DefaultWorkDir = tmpDir
  t.Setenv("PICOAIDE_RULE_CACHE_DIR", filepath.Join(tmpDir, "rules"))

  // 初始化数据库
  if err := store.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  config.SetEngineProvider(store.GetEngine)

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
  if err := store.CreateUser("testadmin", "admin123", "superadmin"); err != nil {
    t.Fatalf("CreateUser(testadmin): %v", err)
  }

  // 创建普通用户
  if err := store.CreateUser("testuser", "user123", "user"); err != nil {
    t.Fatalf("CreateUser(testuser): %v", err)
  }

  // 初始化普通用户的工作目录和容器记录
  if err := user.InitUser(cfg, "testuser"); err != nil {
    t.Fatalf("InitUser(testuser): %v", err)
  }

  // 确保工作区目录存在（文件管理 API 依赖此目录）
  workspaceDir := user.UserDir(cfg, "testuser")
  if err := os.MkdirAll(workspaceDir, 0755); err != nil {
    t.Fatalf("MkdirAll workspace: %v", err)
  }

  // 创建 Server 实例
  s := &Server{
    secret:       "test-integration-secret",
    csrfKey:      "test-integration-secret-csrf",
    loginLimiter: newLoginRateLimiter(),
  }
  s.cfg.Store(cfg)

  // 注册所有路由到 Gin 引擎
  gin.SetMode(gin.TestMode)
  r := gin.New()
  r.Use(s.secureHeaders())
  s.RegisterRoutes(r)

  // 创建 HTTP 测试服务器
  httpServer := httptest.NewServer(r)
  t.Cleanup(httpServer.Close)

  return &testEnv{
    Server: s,
    HTTP:   httpServer,
    Cfg:    cfg,
  }
}

// ============================================================
// testEnv 辅助方法
// ============================================================

// urlValuesToJSON 将 url.Values 转为适合 JSON 的 map，自动推断 bool
func urlValuesToJSON(form url.Values) map[string]interface{} {
  m := make(map[string]interface{}, len(form))
  for k, v := range form {
    val := v[0]
    if val == "true" || val == "false" {
      m[k] = val == "true"
    } else if len(v) > 1 {
      m[k] = v
    } else {
      m[k] = val
    }
  }
  return m
}

// authRequest 发送带 session cookie 和 CSRF token 的 HTTP 请求
func (env *testEnv) authRequest(t *testing.T, method, path, username string, form url.Values) *http.Response {
  t.Helper()

  var bodyReader io.Reader
  var contentType string
  if method == "POST" {
    if form != nil {
      m := urlValuesToJSON(form)
      data, _ := json.Marshal(m)
      bodyReader = bytes.NewReader(data)
      contentType = "application/json"
    }
  }

  req, err := http.NewRequest(method, env.HTTP.URL+path, bodyReader)
  if err != nil {
    t.Fatalf("创建请求失败: %v", err)
  }

  if contentType != "" {
    req.Header.Set("Content-Type", contentType)
  }
  if method == "POST" {
    req.Header.Set("X-CSRF-Token", env.Server.csrfToken(username))
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

// postForm 发送带认证和 CSRF token 的 POST 请求（表单编码）
func (env *testEnv) postForm(t *testing.T, path, username string, form url.Values) *http.Response {
  t.Helper()
  return env.authRequest(t, "POST", path, username, form)
}

// postMap 发送带认证和 CSRF token 的 JSON POST 请求（map 类型）
func (env *testEnv) postMap(t *testing.T, path, username string, body map[string]interface{}) *http.Response {
  t.Helper()
  data, _ := json.Marshal(body)
  req, err := http.NewRequest("POST", env.HTTP.URL+path, bytes.NewReader(data))
  if err != nil {
    t.Fatalf("创建请求失败: %v", err)
  }
  req.Header.Set("Content-Type", "application/json")
  req.Header.Set("X-CSRF-Token", env.Server.csrfToken(username))
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

// postJSON 发送带认证和 CSRF token 的 JSON POST 请求
func (env *testEnv) postJSON(t *testing.T, path, username string, body interface{}) *http.Response {
  t.Helper()
  var reqBody io.Reader
  if body != nil {
    data, err := json.Marshal(body)
    if err != nil {
      t.Fatalf("JSON 序列化失败: %v", err)
    }
    reqBody = bytes.NewReader(data)
  }
  req, err := http.NewRequest("POST", env.HTTP.URL+path, reqBody)
  if err != nil {
    t.Fatalf("创建请求失败: %v", err)
  }
  if body != nil {
    req.Header.Set("Content-Type", "application/json")
  }
  req.Header.Set("X-CSRF-Token", env.Server.csrfToken(username))
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

func (env *testEnv) postMultipartFile(t *testing.T, path, username, fieldName, fileName string, data []byte) *http.Response {
  t.Helper()
  var body bytes.Buffer
  writer := multipart.NewWriter(&body)
  fw, err := writer.CreateFormFile(fieldName, fileName)
  if err != nil {
    t.Fatalf("CreateFormFile: %v", err)
  }
  if _, err := fw.Write(data); err != nil {
    t.Fatalf("write multipart file: %v", err)
  }
  if err := writer.WriteField("csrf_token", env.Server.csrfToken(username)); err != nil {
    t.Fatalf("WriteField csrf_token: %v", err)
  }
  if err := writer.Close(); err != nil {
    t.Fatalf("multipart close: %v", err)
  }
  req, err := http.NewRequest("POST", env.HTTP.URL+path, &body)
  if err != nil {
    t.Fatalf("创建请求失败: %v", err)
  }
  req.Header.Set("Content-Type", writer.FormDataContentType())
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
