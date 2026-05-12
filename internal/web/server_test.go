package web

import (
  "io"
  "net/http"
  "net/http/httptest"
  "net/url"
  "reflect"
  "sort"
  "strings"
  "testing"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
)

func newTestServer(t *testing.T) *Server {
  t.Helper()
  auth.ResetDB()
  if err := auth.InitDB(t.TempDir()); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  if err := auth.CreateUser("admin", "admin123", "superadmin"); err != nil {
    t.Fatalf("CreateUser(admin): %v", err)
  }
  if err := auth.CreateUser("testuser", "user123", "user"); err != nil {
    t.Fatalf("CreateUser(testuser): %v", err)
  }
  return &Server{
    cfg:     &config.GlobalConfig{Web: config.WebConfig{AuthMode: "local"}},
    secret:  "test-secret-key-12345",
    csrfKey: "test-secret-key-12345-csrf",
  }
}

func TestSessionTokenRoundTrip(t *testing.T) {
  s := newTestServer(t)

  token := s.createSessionToken("admin")
  if token == "" {
    t.Fatal("token should not be empty")
  }

  username, ok := s.parseSessionToken(token)
  if !ok {
    t.Fatal("parseSessionToken should succeed")
  }
  if username != "admin" {
    t.Errorf("username = %q, want %q", username, "admin")
  }
}

func TestDockerNetworkRequest(t *testing.T) {
  req := httptest.NewRequest("GET", "/api/health", nil)
  req.RemoteAddr = "100.64.0.2:34567"
  if !isDockerNetworkRequest(req) {
    t.Fatal("100.64.0.2 should be treated as Docker network")
  }
}

func TestExternalNetworkRequest(t *testing.T) {
  req := httptest.NewRequest("GET", "/api/health", nil)
  req.RemoteAddr = "203.0.113.10:34567"
  if isDockerNetworkRequest(req) {
    t.Fatal("external IP should not be treated as Docker network")
  }
}

func TestHTTPSRedirectTarget(t *testing.T) {
  req := httptest.NewRequest("GET", "http://example.com/path?q=1", nil)
  if got := httpsRedirectTarget(req); got != "https://example.com/path?q=1" {
    t.Fatalf("httpsRedirectTarget = %q", got)
  }
}

func TestHTTPSRedirectTargetDropsDefaultHTTPPort(t *testing.T) {
  req := httptest.NewRequest("GET", "http://example.com:80/path?q=1", nil)
  if got := httpsRedirectTarget(req); got != "https://example.com/path?q=1" {
    t.Fatalf("httpsRedirectTarget = %q", got)
  }
}

func TestSortTagsForDisplay(t *testing.T) {
  tags := []string{"v0.2.7", "v0.2.8", "dev", "v0.2.5", "v0.2.6", "latest"}
  sortTagsForDisplay(tags)
  want := []string{"v0.2.8", "v0.2.7", "v0.2.6", "v0.2.5", "dev", "latest"}
  if !reflect.DeepEqual(tags, want) {
    t.Fatalf("tags = %#v, want %#v", tags, want)
  }
}

func TestCompareImageForDisplay(t *testing.T) {
  images := []struct {
    tags    []string
    created int64
  }{
    {[]string{"ghcr.io/picoaide/picoaide:v0.2.6"}, 30},
    {[]string{"ghcr.io/picoaide/picoaide:dev"}, 100},
    {[]string{"ghcr.io/picoaide/picoaide:v0.2.8"}, 10},
    {[]string{"ghcr.io/picoaide/picoaide:v0.2.7"}, 20},
  }
  sort.SliceStable(images, func(i, j int) bool {
    return compareImageForDisplay(images[i].tags, images[i].created, images[j].tags, images[j].created) < 0
  })
  got := []string{
    tagNameOnly(images[0].tags[0]),
    tagNameOnly(images[1].tags[0]),
    tagNameOnly(images[2].tags[0]),
    tagNameOnly(images[3].tags[0]),
  }
  want := []string{"v0.2.8", "v0.2.7", "v0.2.6", "dev"}
  if !reflect.DeepEqual(got, want) {
    t.Fatalf("image order = %#v, want %#v", got, want)
  }
}

func TestSessionTokenTampered(t *testing.T) {
  s := newTestServer(t)

  token := s.createSessionToken("admin")

  // 篡改用户名部分
  parts := strings.SplitN(token, ":", 3)
  tampered := "hacker:" + parts[1] + ":" + parts[2]

  _, ok := s.parseSessionToken(tampered)
  if ok {
    t.Error("tampered token should be rejected")
  }

  // 篡改签名
  tamperedSig := parts[0] + ":" + parts[1] + ":0000000000000000"
  _, ok = s.parseSessionToken(tamperedSig)
  if ok {
    t.Error("tampered signature should be rejected")
  }

  // 格式错误
  _, ok = s.parseSessionToken("invalid")
  if ok {
    t.Error("malformed token should be rejected")
  }

  // 空 token
  _, ok = s.parseSessionToken("")
  if ok {
    t.Error("empty token should be rejected")
  }
}

func TestCSRFTokenRoundTrip(t *testing.T) {
  s := newTestServer(t)

  token := s.csrfToken("admin")
  if token == "" {
    t.Fatal("CSRF token should not be empty")
  }
  if len(token) != 32 {
    t.Errorf("CSRF token length = %d, want 32", len(token))
  }

  // 同一用户同一时间窗口 token 应相同
  token2 := s.csrfToken("admin")
  if token != token2 {
    t.Error("CSRF token should be deterministic within same time window")
  }

  // 不同用户 token 应不同
  otherToken := s.csrfToken("otheruser")
  if token == otherToken {
    t.Error("different users should have different CSRF tokens")
  }
}

func TestCheckCSRF(t *testing.T) {
  s := newTestServer(t)

  // 创建带 session cookie 的请求
  sessionToken := s.createSessionToken("admin")
  csrfToken := s.csrfToken("admin")

  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  c.Request = httptest.NewRequest("POST", "/test", strings.NewReader(
    url.Values{"csrf_token": {csrfToken}}.Encode(),
  ))
  c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  c.Request.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})

  if !s.checkCSRF(c) {
    t.Error("checkCSRF should pass with valid token")
  }
}

func TestCheckCSRFInvalid(t *testing.T) {
  s := newTestServer(t)

  // 无 cookie
  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  c.Request = httptest.NewRequest("POST", "/test", nil)
  if s.checkCSRF(c) {
    t.Error("checkCSRF should fail without session")
  }

  // 错误 CSRF token
  sessionToken := s.createSessionToken("admin")
  w = httptest.NewRecorder()
  c, _ = gin.CreateTestContext(w)
  c.Request = httptest.NewRequest("POST", "/test", strings.NewReader(
    url.Values{"csrf_token": {"wrongtoken"}}.Encode(),
  ))
  c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  c.Request.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
  if s.checkCSRF(c) {
    t.Error("checkCSRF should fail with wrong token")
  }
}

func TestAllowedExtensionOrigin(t *testing.T) {
  s := newTestServer(t)
  if !s.allowedExtensionOrigin("chrome-extension://picoaide-test") {
    t.Fatal("extension origin should be allowed by default")
  }
  if s.allowedExtensionOrigin("https://example.com") {
    t.Fatal("non-extension origin should be rejected")
  }
}

func TestAllowedExtensionOriginEnvAllowlist(t *testing.T) {
  t.Setenv("PICOAIDE_ALLOWED_EXTENSION_ORIGINS", "chrome-extension://allowed")
  s := newTestServer(t)
  if !s.allowedExtensionOrigin("chrome-extension://allowed") {
    t.Fatal("configured extension origin should be allowed")
  }
  if s.allowedExtensionOrigin("chrome-extension://other") {
    t.Fatal("unconfigured extension origin should be rejected")
  }
}

func TestWebSocketOriginCheck(t *testing.T) {
  req := httptest.NewRequest("GET", "http://picoaide.test/api/browser/ws", nil)
  req.Header.Set("Origin", "http://picoaide.test")
  if !upgrader.CheckOrigin(req) {
    t.Fatal("same-origin websocket should be accepted")
  }

  req = httptest.NewRequest("GET", "http://picoaide.test/api/browser/ws", nil)
  req.Header.Set("Origin", "chrome-extension://picoaide-test")
  if !upgrader.CheckOrigin(req) {
    t.Fatal("extension websocket should be accepted")
  }

  req = httptest.NewRequest("GET", "http://picoaide.test/api/browser/ws", nil)
  req.Header.Set("Origin", "https://evil.example")
  if upgrader.CheckOrigin(req) {
    t.Fatal("cross-site websocket should be rejected")
  }
}

func TestEnsureSessionSecretPersistsGeneratedSecret(t *testing.T) {
  auth.ResetDB()
  tmpDir := t.TempDir()
  if err := auth.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }

  first, err := ensureSessionSecret()
  if err != nil {
    t.Fatalf("ensureSessionSecret first: %v", err)
  }
  second, err := ensureSessionSecret()
  if err != nil {
    t.Fatalf("ensureSessionSecret second: %v", err)
  }
  if first == "" || second == "" {
    t.Fatal("generated secrets should not be empty")
  }
  if first != second {
    t.Fatal("generated session secret should persist")
  }
  if len(first) != 64 {
    t.Fatalf("generated session secret should be 32 random bytes encoded as hex, got length %d", len(first))
  }
}

func TestEnsureSessionSecretDeletesLegacyWebPassword(t *testing.T) {
  auth.ResetDB()
  tmpDir := t.TempDir()
  if err := auth.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  engine, err := auth.GetEngine()
  if err != nil {
    t.Fatalf("GetEngine: %v", err)
  }
  if _, err := engine.Exec(
    "INSERT INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now','localtime'))",
    "web.password",
    "legacy-secret",
  ); err != nil {
    t.Fatalf("insert legacy web.password: %v", err)
  }

  secret, err := ensureSessionSecret()
  if err != nil {
    t.Fatalf("ensureSessionSecret: %v", err)
  }
  if secret == "" || secret == "legacy-secret" {
    t.Fatalf("ensureSessionSecret should generate a new secret, got %q", secret)
  }
  var setting auth.Setting
  has, err := engine.Where("key = ?", "web.password").Get(&setting)
  if err != nil {
    t.Fatalf("query legacy web.password: %v", err)
  }
  if has {
    t.Fatal("legacy web.password should be deleted")
  }
}

func TestGetSessionUser(t *testing.T) {
  s := newTestServer(t)

  // 无 cookie
  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  c.Request = httptest.NewRequest("GET", "/test", nil)
  if u := s.getSessionUser(c); u != "" {
    t.Errorf("getSessionUser without cookie = %q, want empty", u)
  }

  // 有效 cookie
  sessionToken := s.createSessionToken("testuser")
  w = httptest.NewRecorder()
  c, _ = gin.CreateTestContext(w)
  c.Request = httptest.NewRequest("GET", "/test", nil)
  c.Request.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
  if u := s.getSessionUser(c); u != "testuser" {
    t.Errorf("getSessionUser = %q, want %q", u, "testuser")
  }

  // 无效 cookie
  w = httptest.NewRecorder()
  c, _ = gin.CreateTestContext(w)
  c.Request = httptest.NewRequest("GET", "/test", nil)
  c.Request.AddCookie(&http.Cookie{Name: "session", Value: "invalid"})
  if u := s.getSessionUser(c); u != "" {
    t.Errorf("getSessionUser with invalid cookie = %q, want empty", u)
  }
}

func TestRootRedirectsToLogin(t *testing.T) {
  s := newTestServer(t)

  gin.SetMode(gin.TestMode)
  r := gin.New()
  s.registerUIRoutes(r)

  w := httptest.NewRecorder()
  req := httptest.NewRequest("GET", "/", nil)
  r.ServeHTTP(w, req)

  if w.Code != http.StatusFound {
    t.Fatalf("status=%d, want %d", w.Code, http.StatusFound)
  }
  if got := w.Header().Get("Location"); got != "/login" {
    t.Fatalf("Location=%q, want /login", got)
  }
}

func TestAdminRedirectsToDashboard(t *testing.T) {
  s := newTestServer(t)

  gin.SetMode(gin.TestMode)
  r := gin.New()
  s.registerUIRoutes(r)

  for _, path := range []string{"/admin", "/admin/"} {
    w := httptest.NewRecorder()
    req := httptest.NewRequest("GET", path, nil)
    r.ServeHTTP(w, req)

    if w.Code != http.StatusMovedPermanently {
      t.Fatalf("%s status=%d, want %d", path, w.Code, http.StatusMovedPermanently)
    }
    if got := w.Header().Get("Location"); got != "/admin/dashboard" {
      t.Fatalf("%s Location=%q, want /admin/dashboard", path, got)
    }
  }
}

func TestAdminSectionRoutesServeAdminShell(t *testing.T) {
  s := newTestServer(t)
  auth.ResetDB()
  tmpDir := t.TempDir()
  if err := auth.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  if err := auth.CreateUser("admin", "password123", "superadmin"); err != nil {
    t.Fatalf("CreateUser: %v", err)
  }

  gin.SetMode(gin.TestMode)
  r := gin.New()
  s.registerUIRoutes(r)

  for _, path := range []string{"/admin/dashboard", "/admin/users", "/admin/settings"} {
    w := httptest.NewRecorder()
    req := httptest.NewRequest("GET", path, nil)
    req.AddCookie(&http.Cookie{Name: "session", Value: s.createSessionToken("admin")})
    r.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
      t.Fatalf("%s status=%d, want %d", path, w.Code, http.StatusOK)
    }
    body, err := io.ReadAll(w.Body)
    if err != nil {
      t.Fatalf("ReadAll(%s): %v", path, err)
    }
    if !strings.Contains(string(body), "PicoAide 管理后台") {
      t.Fatalf("%s did not serve admin shell", path)
    }
  }
}

func TestUIStaticImagesRoute(t *testing.T) {
  s := newTestServer(t)

  gin.SetMode(gin.TestMode)
  r := gin.New()
  s.registerUIRoutes(r)

  w := httptest.NewRecorder()
  req := httptest.NewRequest("GET", "/images/logo-mark.svg", nil)
  r.ServeHTTP(w, req)

  if w.Code != http.StatusOK {
    t.Fatalf("status=%d, want %d", w.Code, http.StatusOK)
  }
  if got := w.Header().Get("Content-Type"); !strings.Contains(got, "image/svg+xml") {
    t.Fatalf("Content-Type=%q, want image/svg+xml", got)
  }
  if body := w.Body.String(); !strings.Contains(body, "<svg") {
    t.Fatal("logo response does not look like SVG")
  }
}

func TestAdminSectionRoutesRedirectUnauthenticated(t *testing.T) {
  s := newTestServer(t)

  gin.SetMode(gin.TestMode)
  r := gin.New()
  s.registerUIRoutes(r)

  w := httptest.NewRecorder()
  req := httptest.NewRequest("GET", "/admin/dashboard", nil)
  r.ServeHTTP(w, req)

  if w.Code != http.StatusFound {
    t.Fatalf("status=%d, want %d", w.Code, http.StatusFound)
  }
  if got := w.Header().Get("Location"); got != "/login" {
    t.Fatalf("Location=%q, want /login", got)
  }
}
