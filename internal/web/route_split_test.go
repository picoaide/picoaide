package web

import (
  "net/http"
  "net/http/httptest"
  "testing"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/config"
)

// registerInternalAPIRoutes 和 registerExternalAPIRoutes 将在实现时定义

// ============================================================
// Internal routes 注册验证
// ============================================================

func TestRegisterInternalAPIRoutes_hasHealth(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  found := false
  for _, ri := range r.Routes() {
    if ri.Method == "GET" && ri.Path == "/api/health" {
      found = true
      break
    }
  }
  if !found {
    t.Error("内部路由缺少 GET /api/health")
  }
}

func TestRegisterInternalAPIRoutes_hasMCPSSE(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  var getFound, postFound bool
  for _, ri := range r.Routes() {
    if ri.Method == "GET" && ri.Path == "/api/mcp/sse/:service" {
      getFound = true
    }
    if ri.Method == "POST" && ri.Path == "/api/mcp/sse/:service" {
      postFound = true
    }
  }
  if !getFound {
    t.Error("内部路由缺少 GET /api/mcp/sse/:service")
  }
  if !postFound {
    t.Error("内部路由缺少 POST /api/mcp/sse/:service")
  }
}

func TestRegisterInternalAPIRoutes_hasBrowserWS(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  for _, ri := range r.Routes() {
    if ri.Method == "GET" && ri.Path == "/api/browser/ws" {
      return
    }
  }
  t.Error("内部路由缺少 GET /api/browser/ws")
}

func TestRegisterInternalAPIRoutes_hasComputerWS(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  for _, ri := range r.Routes() {
    if ri.Method == "GET" && ri.Path == "/api/computer/ws" {
      return
    }
  }
  t.Error("内部路由缺少 GET /api/computer/ws")
}

func TestRegisterInternalAPIRoutes_hasPicoagentMe(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  for _, ri := range r.Routes() {
    if ri.Method == "GET" && ri.Path == "/api/picoagent/me" {
      return
    }
  }
  t.Error("内部路由缺少 GET /api/picoagent/me")
}

func TestRegisterInternalAPIRoutes_hasMCPToken(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  for _, ri := range r.Routes() {
    if ri.Method == "GET" && ri.Path == "/api/mcp/token" {
      return
    }
  }
  t.Error("内部路由缺少 GET /api/mcp/token")
}

func TestRegisterInternalAPIRoutes_hasMCPCookies(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  var getFound, postFound bool
  for _, ri := range r.Routes() {
    if ri.Method == "GET" && ri.Path == "/api/mcp/cookies" {
      getFound = true
    }
    if ri.Method == "POST" && ri.Path == "/api/mcp/cookies" {
      postFound = true
    }
  }
  if !getFound {
    t.Error("内部路由缺少 GET /api/mcp/cookies")
  }
  if !postFound {
    t.Error("内部路由缺少 POST /api/mcp/cookies")
  }
}

func TestRegisterInternalAPIRoutes_hasFiles(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  var listFound, uploadFound, downloadFound, deleteFound, mkdirFound, editGetFound, editPostFound bool
  for _, ri := range r.Routes() {
    switch ri.Method + " " + ri.Path {
    case "GET /api/files":
      listFound = true
    case "POST /api/files/upload":
      uploadFound = true
    case "GET /api/files/download":
      downloadFound = true
    case "POST /api/files/delete":
      deleteFound = true
    case "POST /api/files/mkdir":
      mkdirFound = true
    case "GET /api/files/edit":
      editGetFound = true
    case "POST /api/files/edit":
      editPostFound = true
    }
  }
  if !listFound {
    t.Error("内部路由缺少 GET /api/files")
  }
  if !uploadFound {
    t.Error("内部路由缺少 POST /api/files/upload")
  }
  if !downloadFound {
    t.Error("内部路由缺少 GET /api/files/download")
  }
  if !deleteFound {
    t.Error("内部路由缺少 POST /api/files/delete")
  }
  if !mkdirFound {
    t.Error("内部路由缺少 POST /api/files/mkdir")
  }
  if !editGetFound {
    t.Error("内部路由缺少 GET /api/files/edit")
  }
  if !editPostFound {
    t.Error("内部路由缺少 POST /api/files/edit")
  }
}

// ============================================================
// Internal routes 不应包含管理类路由
// ============================================================

func TestRegisterInternalAPIRoutes_noAdminRoutes(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  for _, ri := range r.Routes() {
    if len(ri.Path) >= 10 && ri.Path[:10] == "/api/admin" {
      t.Errorf("内部路由不应包含 admin 路径: %s %s", ri.Method, ri.Path)
    }
  }
}

func TestRegisterInternalAPIRoutes_noLogin(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  for _, ri := range r.Routes() {
    if len(ri.Path) >= 10 && ri.Path[:10] == "/api/login" {
      t.Errorf("内部路由不应包含 login 路径: %s %s", ri.Method, ri.Path)
    }
  }
}

func TestRegisterInternalAPIRoutes_noUserChat(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  for _, ri := range r.Routes() {
    if len(ri.Path) >= 15 && ri.Path[:15] == "/api/user/chat" {
      t.Errorf("内部路由不应包含 user/chat 路径: %s %s", ri.Method, ri.Path)
    }
  }
}

func TestRegisterInternalAPIRoutes_noConfig(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerInternalAPIRoutes(g)

  for _, ri := range r.Routes() {
    if ri.Path == "/api/config" {
      t.Errorf("内部路由不应包含 /api/config: %s %s", ri.Method, ri.Path)
    }
  }
}

// ============================================================
// External routes 包含全部路由
// ============================================================

func TestRegisterExternalAPIRoutes_includesAdmin(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerExternalAPIRoutes(g)

  var hasAdmin bool
  for _, ri := range r.Routes() {
    if ri.Path == "/api/admin/users" {
      hasAdmin = true
      break
    }
  }
  if !hasAdmin {
    t.Error("外部路由应包含 /api/admin/users")
  }
}

func TestRegisterExternalAPIRoutes_includesInternalRoutes(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerExternalAPIRoutes(g)

  for _, ri := range r.Routes() {
    if ri.Path == "/api/health" {
      return
    }
  }
  t.Error("外部路由应包含 /api/health（应继承内部路由）")
}

func TestRegisterExternalAPIRoutes_includesLogin(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  r := gin.New()
  g := r.Group("/api")
  s.registerExternalAPIRoutes(g)

  for _, ri := range r.Routes() {
    if ri.Path == "/api/login/mode" {
      return
    }
  }
  t.Error("外部路由应包含 /api/login/mode")
}

// ============================================================
// buildInternalHandler / buildExternalHandler
// ============================================================

func TestBuildInternalHandler_healthReturns200(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  handler := s.buildInternalHandler()

  req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)
  if w.Code != http.StatusOK {
    t.Errorf("GET /api/health 状态码 = %d, 期望 %d", w.Code, http.StatusOK)
  }
}

func TestBuildInternalHandler_adminReturns404(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  handler := s.buildInternalHandler()

  req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)
  if w.Code != http.StatusNotFound {
    t.Errorf("GET /api/admin/users 状态码 = %d, 期望 404", w.Code)
  }
}

func TestBuildInternalHandler_loginReturns404(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  handler := s.buildInternalHandler()

  req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)
  if w.Code != http.StatusNotFound {
    t.Errorf("POST /api/login 状态码 = %d, 期望 404", w.Code)
  }
}

func TestBuildInternalHandler_versionReturns200(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  handler := s.buildInternalHandler()

  req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)
  if w.Code != http.StatusOK {
    t.Errorf("GET /api/version 状态码 = %d, 期望 %d", w.Code, http.StatusOK)
  }
}

func TestBuildExternalHandler_healthReturns200(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  handler := s.buildExternalHandler()

  req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)
  if w.Code != http.StatusOK {
    t.Errorf("GET /api/health 状态码 = %d, 期望 %d", w.Code, http.StatusOK)
  }
}

func TestBuildExternalHandler_adminReturns404(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  handler := s.buildExternalHandler()

  req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)
  if w.Code != http.StatusUnauthorized {
    t.Errorf("GET /api/admin/users 状态码 = %d, 期望 401（需认证）", w.Code)
  }
}

func TestBuildExternalHandler_manageRedirects(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  handler := s.buildExternalHandler()

  req := httptest.NewRequest(http.MethodGet, "/manage", nil)
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)
  if w.Code != http.StatusMovedPermanently {
    t.Errorf("GET /manage 状态码 = %d, 期望 301", w.Code)
  }
}

func TestBuildExternalHandler_internalHealthAlsoWorks(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  handler := s.buildExternalHandler()

  req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)
  if w.Code != http.StatusOK {
    t.Errorf("外部 GET /api/health 状态码 = %d, 期望 %d", w.Code, http.StatusOK)
  }
}

func TestIsSandboxRequest_sandboxIP(t *testing.T) {
  r := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
  r.RemoteAddr = "100.64.0.2:12345"
  if !isSandboxRequest(r) {
    t.Error("100.64.0.2 应被识别为沙箱请求")
  }
}

func TestIsSandboxRequest_externalIP(t *testing.T) {
  r := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
  r.RemoteAddr = "203.0.113.1:12345"
  if isSandboxRequest(r) {
    t.Error("203.0.113.1 不应被识别为沙箱请求")
  }
}

func TestIsSandboxRequest_localhost(t *testing.T) {
  r := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
  r.RemoteAddr = "127.0.0.1:12345"
  if isSandboxRequest(r) {
    t.Error("127.0.0.1 不应被识别为沙箱请求")
  }
}

func TestSandboxAwareHandler_sandboxGetsInternal(t *testing.T) {
  internal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
  })
  external := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusForbidden)
  })
  handler := sandboxAwareHandler(internal, external)

  req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
  req.RemoteAddr = "100.64.0.2:12345"
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)
  if w.Code != http.StatusOK {
    t.Errorf("沙箱请求应走 internal handler, status=%d", w.Code)
  }
}

func TestSandboxAwareHandler_externalGetsExternal(t *testing.T) {
  internal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
  })
  external := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusForbidden)
  })
  handler := sandboxAwareHandler(internal, external)

  req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
  req.RemoteAddr = "203.0.113.1:12345"
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)
  if w.Code != http.StatusForbidden {
    t.Errorf("外部请求应走 external handler, status=%d", w.Code)
  }
}

// ============================================================
// TLS 重定向 handler 测试
// ============================================================

func TestTLSRedirectHandler_redirectsAllRequests(t *testing.T) {
  handler := redirectToHTTPSHandler()

  req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
  req.Host = "example.com"
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)

  if w.Code != http.StatusMovedPermanently {
    t.Errorf("状态码 = %d, 期望 301", w.Code)
  }
  loc := w.Header().Get("Location")
  expected := "https://example.com/api/health"
  if loc != expected {
    t.Errorf("Location = %q, 期望 %q", loc, expected)
  }
}

func TestTLSRedirectHandler_preservesQueryString(t *testing.T) {
  handler := redirectToHTTPSHandler()

  req := httptest.NewRequest(http.MethodGet, "/api/health?token=abc", nil)
  req.Host = "example.com"
  w := httptest.NewRecorder()
  handler.ServeHTTP(w, req)

  loc := w.Header().Get("Location")
  expected := "https://example.com/api/health?token=abc"
  if loc != expected {
    t.Errorf("Location = %q, 期望 %q", loc, expected)
  }
}

func TestBuildTLSServer_returnsNilWhenDisabled(t *testing.T) {
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  cfg := &config.GlobalConfig{}
  cfg.Web.TLS.Enabled = false
  s.cfg.Store(cfg)

  srv, err := s.buildTLSServer()
  if err != nil {
    t.Fatalf("buildTLSServer: %v", err)
  }
  if srv != nil {
    t.Error("TLS 禁用时应返回 nil server")
  }
}

func TestBuildTLSServer_returnsNilWhenNoCert(t *testing.T) {
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  cfg := &config.GlobalConfig{}
  cfg.Web.TLS.Enabled = true
  cfg.Web.TLS.CertPEM = ""
  cfg.Web.TLS.KeyPEM = ""
  s.cfg.Store(cfg)

  srv, err := s.buildTLSServer()
  if err != nil {
    t.Fatalf("buildTLSServer: %v", err)
  }
  if srv != nil {
    t.Error("无证书时应返回 nil server")
  }
}
