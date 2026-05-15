package web

import (
  "crypto/tls"
  "encoding/json"
  "io"
  "net/http"
  "net/http/httptest"
  "net/url"
  "reflect"
  "sort"
  "strings"
  "testing"
  "time"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/user"
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

func TestParsePagination(t *testing.T) {
  gin.SetMode(gin.TestMode)
  tests := []struct {
    name    string
    query   string
    want    paginationQuery
  }{
    {"no params", "", paginationQuery{Page: 1, PageSize: 0, Search: "", Enabled: false}},
    {"page only", "page=2", paginationQuery{Page: 2, PageSize: 20, Search: "", Enabled: true}},
    {"page and page_size", "page=3&page_size=10", paginationQuery{Page: 3, PageSize: 10, Search: "", Enabled: true}},
    {"search only", "search=foo", paginationQuery{Page: 1, PageSize: 20, Search: "foo", Enabled: true}},
    {"invalid page", "page=-1", paginationQuery{Page: 1, PageSize: 0, Search: "", Enabled: false}},
    {"zero page", "page=0", paginationQuery{Page: 1, PageSize: 0, Search: "", Enabled: false}},
    {"oversized page_size clamped", "page=1&page_size=500", paginationQuery{Page: 1, PageSize: 100, Search: "", Enabled: true}},
    {"exact max page_size", "page=1&page_size=100", paginationQuery{Page: 1, PageSize: 100, Search: "", Enabled: true}},
    {"search with uppercase", "search=HELLO", paginationQuery{Page: 1, PageSize: 20, Search: "hello", Enabled: true}},
    {"search trimmed", "search=foo", paginationQuery{Page: 1, PageSize: 20, Search: "foo", Enabled: true}},
  }

  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      w := httptest.NewRecorder()
      c, _ := gin.CreateTestContext(w)
      c.Request = httptest.NewRequest("GET", "/?"+tt.query, nil)
      got := parsePagination(c, 20, 100)
      if !reflect.DeepEqual(got, tt.want) {
        t.Errorf("parsePagination = %+v, want %+v", got, tt.want)
      }
    })
  }
}

func TestPaginateSlice(t *testing.T) {
  items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

  // disabled pagination returns all
  sliced, total, totalPages, page, pageSize := paginateSlice(items, paginationQuery{Enabled: false})
  if len(sliced) != 10 || total != 10 || totalPages != 1 || page != 1 || pageSize != 0 {
    t.Errorf("disabled: sliced=%d total=%d pages=%d page=%d size=%d", len(sliced), total, totalPages, page, pageSize)
  }

  // empty slice disabled
  sliced, _, _, _, _ = paginateSlice([]int{}, paginationQuery{Enabled: false})
  if len(sliced) != 0 {
    t.Errorf("empty disabled: len=%d", len(sliced))
  }

  // page 1, size 3
  sliced, total, totalPages, page, pageSize = paginateSlice(items, paginationQuery{Enabled: true, Page: 1, PageSize: 3})
  if len(sliced) != 3 || sliced[0] != 1 || total != 10 || totalPages != 4 || page != 1 || pageSize != 3 {
    t.Errorf("page1: sliced=%v total=%d pages=%d", sliced, total, totalPages)
  }

  // page 2, size 3
  sliced, _, _, _, _ = paginateSlice(items, paginationQuery{Enabled: true, Page: 2, PageSize: 3})
  if len(sliced) != 3 || sliced[0] != 4 {
    t.Errorf("page2: sliced=%v", sliced)
  }

  // page beyond total
  sliced, _, _, page, _ = paginateSlice(items, paginationQuery{Enabled: true, Page: 100, PageSize: 3})
  if len(sliced) != 1 || sliced[0] != 10 || page != 4 {
    t.Errorf("beyond: sliced=%v page=%d", sliced, page)
  }

  // exact page boundary
  sliced, _, totalPages, _, _ = paginateSlice(items, paginationQuery{Enabled: true, Page: 2, PageSize: 5})
  if len(sliced) != 5 || sliced[0] != 6 || totalPages != 2 {
    t.Errorf("exact: sliced=%v page=%d totalPages=%d", sliced, page, totalPages)
  }
}

func TestClientIPFromRequest(t *testing.T) {
  // X-Real-IP
  req := httptest.NewRequest("GET", "/", nil)
  req.Header.Set("X-Real-IP", "10.0.0.1")
  if ip := clientIPFromRequest(req); ip != "10.0.0.1" {
    t.Errorf("X-Real-IP: got %q", ip)
  }

  // X-Forwarded-For (single)
  req = httptest.NewRequest("GET", "/", nil)
  req.Header.Set("X-Forwarded-For", "10.0.0.2")
  if ip := clientIPFromRequest(req); ip != "10.0.0.2" {
    t.Errorf("X-Forwarded-For single: got %q", ip)
  }

  // X-Forwarded-For (multiple)
  req = httptest.NewRequest("GET", "/", nil)
  req.Header.Set("X-Forwarded-For", "10.0.0.3, 10.0.0.4")
  if ip := clientIPFromRequest(req); ip != "10.0.0.3" {
    t.Errorf("X-Forwarded-For multi: got %q", ip)
  }

  // RemoteAddr fallback
  req = httptest.NewRequest("GET", "/", nil)
  req.RemoteAddr = "192.168.1.1:12345"
  if ip := clientIPFromRequest(req); ip != "192.168.1.1" {
    t.Errorf("RemoteAddr: got %q", ip)
  }

  // RemoteAddr without port
  req = httptest.NewRequest("GET", "/", nil)
  req.RemoteAddr = "bad-addr"
  if ip := clientIPFromRequest(req); ip != "bad-addr" {
    t.Errorf("bad RemoteAddr: got %q", ip)
  }
}

func TestStringsIndex(t *testing.T) {
  if idx := stringsIndex("hello world", "world"); idx != 6 {
    t.Errorf("expected 6, got %d", idx)
  }
  if idx := stringsIndex("hello world", "xyz"); idx != -1 {
    t.Errorf("expected -1, got %d", idx)
  }
  if idx := stringsIndex("aaaa", "aa"); idx != 0 {
    t.Errorf("expected 0, got %d", idx)
  }
  if idx := stringsIndex("", "a"); idx != -1 {
    t.Errorf("expected -1, got %d", idx)
  }
}

func TestImageTagFromRef(t *testing.T) {
  if tag := imageTagFromRef("ghcr.io/picoaide/picoaide:v1.2.3"); tag != "v1.2.3" {
    t.Errorf("got %q", tag)
  }
  if tag := imageTagFromRef("picoaide:latest"); tag != "latest" {
    t.Errorf("got %q", tag)
  }
  // no colon
  if tag := imageTagFromRef("picoaide"); tag != "" {
    t.Errorf("got %q", tag)
  }
  // colon at end
  if tag := imageTagFromRef("picoaide:"); tag != "" {
    t.Errorf("got %q", tag)
  }
  // colon before slash
  if tag := imageTagFromRef("http://example.com/image"); tag != "" {
    t.Errorf("got %q", tag)
  }
}

func TestFormatSize(t *testing.T) {
  if s := formatSize(500); s != "500 B" {
    t.Errorf("got %q", s)
  }
  if s := formatSize(2048); s != "2.0 KB" {
    t.Errorf("got %q", s)
  }
  if s := formatSize(3145728); s != "3.0 MB" {
    t.Errorf("got %q", s)
  }
  if s := formatSize(0); s != "0 B" {
    t.Errorf("got %q", s)
  }
}

func TestPicoclawWorkspaceTag(t *testing.T) {
  tests := []struct {
    name  string
    isDir bool
    want  string
  }{
    {"sessions", true, "会话"},
    {"memory", true, "记忆"},
    {"state", true, "状态"},
    {"cron", true, "定时"},
    {"skills", true, "技能"},
    {"unknown", true, ""},
    {"AGENT.md", false, "行为"},
    {"HEARTBEAT.md", false, "心跳"},
    {"IDENTITY.md", false, "身份"},
    {"SOUL.md", false, "灵魂"},
    {"USER.md", false, "偏好"},
    {"readme.md", false, ""},
  }
  for _, tt := range tests {
    if got := picoclawWorkspaceTag(tt.name, tt.isDir); got != tt.want {
      t.Errorf("picoclawWorkspaceTag(%q, %v) = %q, want %q", tt.name, tt.isDir, got, tt.want)
    }
  }
}

func TestExtractToken(t *testing.T) {
  // from query
  req := httptest.NewRequest("GET", "/?token=mytoken", nil)
  if tok := extractToken(req); tok != "mytoken" {
    t.Errorf("query: got %q", tok)
  }

  // from bearer
  req = httptest.NewRequest("GET", "/", nil)
  req.Header.Set("Authorization", "Bearer bearertoken")
  if tok := extractToken(req); tok != "bearertoken" {
    t.Errorf("bearer: got %q", tok)
  }

  // no token
  req = httptest.NewRequest("GET", "/", nil)
  if tok := extractToken(req); tok != "" {
    t.Errorf("empty: got %q", tok)
  }

  // query takes precedence
  req = httptest.NewRequest("GET", "/?token=fromquery", nil)
  req.Header.Set("Authorization", "Bearer fromheader")
  if tok := extractToken(req); tok != "fromquery" {
    t.Errorf("query precedence: got %q", tok)
  }
}

func TestMCPError(t *testing.T) {
  err := mcpError(json.Number("1"), -32700, "Parse error")
  if err == nil {
    t.Fatal("mcpError returned nil")
  }
  e, ok := err["error"].(map[string]interface{})
  if !ok {
    t.Fatalf("unexpected format: %v", err)
  }
  code, ok := e["code"].(int)
  if !ok || code != -32700 {
    t.Errorf("code = %v (type %T), want -32700", e["code"], e["code"])
  }
  msg, ok := e["message"].(string)
  if !ok || msg != "Parse error" {
    t.Errorf("message = %v", e["message"])
  }
  if err["id"] != json.Number("1") {
    t.Errorf("id = %v", err["id"])
  }
}

func TestFormatMCPResult(t *testing.T) {
  // nil result
  r := formatMCPResult(nil)
  if content, ok := r["content"].([]map[string]interface{}); !ok || content[0]["text"] != "执行成功" {
    t.Errorf("nil: %v", r)
  }

  // result with content
  r = formatMCPResult(map[string]interface{}{
    "content": []interface{}{
      map[string]interface{}{"type": "text", "text": "hello"},
    },
  })
  if content, ok := r["content"].([]interface{}); !ok || len(content) != 1 {
    t.Errorf("pre-formatted: %v", r)
  }

  // string result
  r = formatMCPResult("simple string")
  if content, ok := r["content"].([]map[string]interface{}); !ok || content[0]["text"] != "simple string" {
    t.Errorf("string: %v", r)
  }

  // map without content
  r = formatMCPResult(map[string]interface{}{"key": "value"})
  if content, ok := r["content"].([]map[string]interface{}); !ok {
    t.Errorf("map: %v", r)
  } else if !strings.Contains(content[0]["text"].(string), "key") {
    t.Errorf("map text should contain key: %v", content[0]["text"])
  }
}

func TestContextWithTimeout(t *testing.T) {
  ctx, cancel := contextWithTimeout(1)
  defer cancel()
  if ctx == nil {
    t.Fatal("contextWithTimeout returned nil")
  }
  select {
  case <-ctx.Done():
    // already cancelled (shouldn't happen since we just created it)
    t.Error("context should not be done immediately")
  case <-time.After(10 * time.Millisecond):
    // ok
  }
}

func TestGetImagePullTaskFunctions(t *testing.T) {
  // start
  startImagePull("v1.0.0")
  status := getImagePullStatus()
  if !status.Running || status.Tag != "v1.0.0" || status.Message != "正在拉取..." {
    t.Errorf("start: %+v", status)
  }

  // update
  updateImagePull("下载中...")
  status = getImagePullStatus()
  if status.Message != "下载中..." {
    t.Errorf("update: %+v", status)
  }

  // update when not running
  finishImagePull()
  updateImagePull("不应该更新")
  status = getImagePullStatus()
  if !strings.Contains(status.Message, "完成") {
    t.Errorf("finish then update: %+v", status)
  }

  // fail
  startImagePull("v2.0.0")
  failImagePull("网络错误")
  status = getImagePullStatus()
  if status.Running || status.Error != "网络错误" {
    t.Errorf("fail: %+v", status)
  }
}

func TestGetTaskStatus(t *testing.T) {
  status := getTaskStatus()
  if status == nil {
    t.Fatal("getTaskStatus returned nil")
  }
  if status.Running {
    t.Error("expected no running task")
  }
}

func TestValidAuthMode(t *testing.T) {
  if !validAuthMode("local") {
    t.Error("local should be valid")
  }
  if validAuthMode("nonexistent") {
    t.Error("nonexistent should not be valid")
  }
}

func TestAuthModeFromRaw(t *testing.T) {
  // from auth_mode field
  raw := map[string]interface{}{"web": map[string]interface{}{"auth_mode": "ldap"}}
  if mode := authModeFromRaw(raw, "local"); mode != "ldap" {
    t.Errorf("got %q", mode)
  }

  // from ldap_enabled bool
  raw = map[string]interface{}{"web": map[string]interface{}{"ldap_enabled": true}}
  if mode := authModeFromRaw(raw, "local"); mode != "ldap" {
    t.Errorf("ldap_enabled bool: got %q", mode)
  }

  // from ldap_enabled string
  raw = map[string]interface{}{"web": map[string]interface{}{"ldap_enabled": "true"}}
  if mode := authModeFromRaw(raw, "local"); mode != "ldap" {
    t.Errorf("ldap_enabled string: got %q", mode)
  }

  // no web key
  raw = map[string]interface{}{}
  if mode := authModeFromRaw(raw, "fallback"); mode != "fallback" {
    t.Errorf("no web: got %q", mode)
  }

  // empty auth_mode
  raw = map[string]interface{}{"web": map[string]interface{}{"auth_mode": ""}}
  if mode := authModeFromRaw(raw, "fallback"); mode != "fallback" {
    t.Errorf("empty auth_mode: got %q", mode)
  }
}

func TestNormalizeAuthModeInRaw(t *testing.T) {
  raw := map[string]interface{}{}
  normalizeAuthModeInRaw(raw, "ldap")
  web, ok := raw["web"].(map[string]interface{})
  if !ok || web["auth_mode"] != "ldap" || web["ldap_enabled"] != true {
    t.Errorf("ldap mode: %v", raw)
  }

  // existing web key
  raw = map[string]interface{}{"web": map[string]interface{}{"other": "value"}}
  normalizeAuthModeInRaw(raw, "local")
  web, ok = raw["web"].(map[string]interface{})
  if !ok || web["auth_mode"] != "local" || web["ldap_enabled"] != false {
    t.Errorf("local mode: %v", raw)
  }
}

func TestNonNilChannels(t *testing.T) {
  result := nonNilChannels(nil)
  if result == nil || len(result) != 0 {
    t.Errorf("nil input should return empty slice, got %v", result)
  }
  result = nonNilChannels([]user.PicoClawChannelInfo{})
  if result == nil {
    t.Error("empty slice input should return empty slice, not nil")
  }
}

func TestUserEnvironmentReady(t *testing.T) {
  s := newTestServer(t)

  // bad username
  if s.userEnvironmentReady("") {
    t.Error("empty username should not be ready")
  }

  // user without container record
  if s.userEnvironmentReady("nonexistent") {
    t.Error("nonexistent user should not be ready")
  }
}

func TestTagNameOnly(t *testing.T) {
  if got := tagNameOnly("ghcr.io/picoaide/picoaide:v1.0.0"); got != "v1.0.0" {
    t.Errorf("got %q", got)
  }
  if got := tagNameOnly("picoaide:latest"); got != "latest" {
    t.Errorf("got %q", got)
  }
  if got := tagNameOnly("no-tag"); got != "no-tag" {
    t.Errorf("got %q", got)
  }
  if got := tagNameOnly("tag:"); got != "tag:" {
    t.Errorf("got %q", got)
  }
}

func TestPrimaryDisplayTag(t *testing.T) {
  // empty tags
  if got := primaryDisplayTag([]string{}); got != "" {
    t.Errorf("empty: got %q", got)
  }

  // single tag
  if got := primaryDisplayTag([]string{"v1.0.0"}); got != "v1.0.0" {
    t.Errorf("single: got %q", got)
  }

  // multiple tags - should pick highest version
  got := primaryDisplayTag([]string{"v1.0.0", "v2.0.0", "dev"})
  if got != "v2.0.0" {
    t.Errorf("should pick v2.0.0, got %q", got)
  }

  // full refs
  got = primaryDisplayTag([]string{"ghcr.io/picoaide/picoaide:v1.0.0", "ghcr.io/picoaide/picoaide:v2.0.0"})
  if got != "v2.0.0" {
    t.Errorf("full refs: got %q", got)
  }
}

func TestParseVersionTag(t *testing.T) {
  // valid
  v, ok := parseVersionTag("v1.2.3")
  if !ok || v != [3]int{1, 2, 3} {
    t.Errorf("v1.2.3: %v %v", v, ok)
  }

  // without v prefix
  v, ok = parseVersionTag("1.2.3")
  if !ok || v != [3]int{1, 2, 3} {
    t.Errorf("1.2.3: %v %v", v, ok)
  }

  // invalid
  _, ok = parseVersionTag("latest")
  if ok {
    t.Error("latest should not be valid")
  }

  // too few parts
  _, ok = parseVersionTag("v1.2")
  if ok {
    t.Error("v1.2 should not be valid")
  }

  // non-numeric
  _, ok = parseVersionTag("v1.2.abc")
  if ok {
    t.Error("v1.2.abc should not be valid")
  }
}

func TestCompareTagsForDisplay(t *testing.T) {
  // v2.0.0 should come before v1.0.0 in display order (descending)
  if compareTagsForDisplay("v2.0.0", "v1.0.0") >= 0 {
    t.Error("v2 should come before v1 (returns negative)")
  }
  if compareTagsForDisplay("v1.0.0", "v2.0.0") <= 0 {
    t.Error("v1 should come before v2 (returns positive)")
  }
  if compareTagsForDisplay("dev", "latest") != strings.Compare("dev", "latest") {
    t.Error("non-version tags should compare lexicographically")
  }
  // version tags sort before non-version tags
  if compareTagsForDisplay("v1.0.0", "dev") >= 0 {
    t.Error("version tags should come before non-version tags (returns negative)")
  }
  if compareTagsForDisplay("dev", "v1.0.0") <= 0 {
    t.Error("non-version should come after version (returns positive)")
  }
}

func TestIsStreamableMCPRequest(t *testing.T) {
  req := httptest.NewRequest("GET", "/", nil)
  if isStreamableMCPRequest(req) {
    t.Error("no headers should not be streamable")
  }

  req.Header.Set("Mcp-Protocol-Version", "2024-11-05")
  if !isStreamableMCPRequest(req) {
    t.Error("Mcp-Protocol-Version should make it streamable")
  }

  req = httptest.NewRequest("GET", "/", nil)
  req.Header.Set("Mcp-Session-Id", "abc123")
  if !isStreamableMCPRequest(req) {
    t.Error("Mcp-Session-Id should make it streamable")
  }
}

func TestIsStreamableMCPPost(t *testing.T) {
  req := httptest.NewRequest("POST", "/", nil)
  if isStreamableMCPPost(req) {
    t.Error("no Accept header should not be streamable")
  }

  req.Header.Set("Accept", "text/event-stream")
  if !isStreamableMCPPost(req) {
    t.Error("text/event-stream Accept should be streamable")
  }
}

func TestStreamableSessionID(t *testing.T) {
  id := streamableSessionID("testuser", "browser")
  if id == "" {
    t.Error("should return non-empty session ID")
  }
  // Should be hex encoded (32 chars for 16 bytes)
  if len(id) != 32 && !strings.Contains(id, "testuser") {
    t.Errorf("unexpected session ID format: %q", id)
  }
}

func TestNegotiatedMCPProtocolVersion(t *testing.T) {
  // with params
  params := json.RawMessage(`{"protocolVersion":"2025-01-01"}`)
  if v := negotiatedMCPProtocolVersion(params); v != "2025-01-01" {
    t.Errorf("got %q", v)
  }

  // empty params
  params = json.RawMessage(`{}`)
  if v := negotiatedMCPProtocolVersion(params); v != "2024-11-05" {
    t.Errorf("got %q", v)
  }

  // invalid params
  params = json.RawMessage(`not-json`)
  if v := negotiatedMCPProtocolVersion(params); v != "2024-11-05" {
    t.Errorf("got %q", v)
  }
}

func TestServiceHub(t *testing.T) {
  hub := NewServiceHub("test")
  if hub.name != "test" {
    t.Errorf("name = %q", hub.name)
  }

  // GetConnection on empty hub
  _, ok := hub.GetConnection("nobody")
  if ok {
    t.Error("should not find connection for nobody")
  }

  // Unregister on empty hub
  hub.Unregister("nobody") // should not panic
}

func TestWebSocketUpgrader(t *testing.T) {
  // CheckOrigin with empty origin
  req := httptest.NewRequest("GET", "/", nil)
  if !upgrader.CheckOrigin(req) {
    t.Error("empty origin should be allowed")
  }

  // CheckOrigin chrome extension
  req = httptest.NewRequest("GET", "/", nil)
  req.Header.Set("Origin", "chrome-extension://abc123")
  if !upgrader.CheckOrigin(req) {
    t.Error("chrome extension should be allowed")
  }

  // CheckOrigin moz extension
  req = httptest.NewRequest("GET", "/", nil)
  req.Header.Set("Origin", "moz-extension://abc123")
  if !upgrader.CheckOrigin(req) {
    t.Error("moz extension should be allowed")
  }

  // CheckOrigin same origin http
  req = httptest.NewRequest("GET", "http://example.com/api", nil)
  req.Header.Set("Origin", "http://example.com")
  if !upgrader.CheckOrigin(req) {
    t.Error("same origin http should be allowed")
  }

  // CheckOrigin same origin https
  req = httptest.NewRequest("GET", "https://example.com/api", nil)
  req.TLS = &tls.ConnectionState{}
  req.Header.Set("Origin", "https://example.com")
  if !upgrader.CheckOrigin(req) {
    t.Error("same origin https should be allowed")
  }

  // CheckOrigin cross-site
  req = httptest.NewRequest("GET", "http://example.com/api", nil)
  req.Header.Set("Origin", "https://evil.com")
  if upgrader.CheckOrigin(req) {
    t.Error("cross-site should be rejected")
  }
}

func TestRateLimiter(t *testing.T) {
  rl := newRateLimiter(3, 100*time.Millisecond)

  // allow up to limit
  if !rl.allow("1.2.3.4") {
    t.Error("first request should be allowed")
  }
  if !rl.allow("1.2.3.4") {
    t.Error("second request should be allowed")
  }
  if !rl.allow("1.2.3.4") {
    t.Error("third request should be allowed")
  }
  if rl.allow("1.2.3.4") {
    t.Error("fourth request should be blocked")
  }

  // different IP should be allowed
  if !rl.allow("5.6.7.8") {
    t.Error("different IP should be allowed")
  }
}

func TestIsExtensionRequest(t *testing.T) {
  s := newTestServer(t)

  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  c.Request = httptest.NewRequest("GET", "/", nil)
  c.Request.Header.Set("Origin", "chrome-extension://abc123")
  if !s.isExtensionRequest(c) {
    t.Error("chrome-extension should be detected")
  }

  c.Request.Header.Set("Origin", "moz-extension://abc123")
  if !s.isExtensionRequest(c) {
    t.Error("moz-extension should be detected")
  }

  c.Request.Header.Set("Origin", "https://example.com")
  if s.isExtensionRequest(c) {
    t.Error("https origin should not be detected as extension")
  }
}

func TestUpdateSourceLastPull(t *testing.T) {
  sources := []config.SkillsSourceWrapper{
    {Name: "src1", Git: &config.GitSource{URL: "https://example.com/repo.git"}},
    {Name: "src2", Reg: &config.RegistrySource{IndexURL: "https://example.com/index.json"}},
  }
  result := updateSourceLastPull(sources, "src1")
  if result[0].Git.LastPull == "" {
    t.Error("src1 LastPull should be updated")
  }
  if result[1].Reg != nil && result[1].Reg.LastRefresh != "" {
    t.Error("src2 should not be affected")
  }
  // nonexistent source
  result = updateSourceLastPull(sources, "nonexistent")
  if len(result) != 2 {
    t.Errorf("expected 2 sources, got %d", len(result))
  }
}

func TestUpdateSourceLastRefresh(t *testing.T) {
  sources := []config.SkillsSourceWrapper{
    {Name: "src1", Git: &config.GitSource{URL: "https://example.com/repo.git"}},
    {Name: "src2", Reg: &config.RegistrySource{IndexURL: "https://example.com/index.json"}},
  }
  result := updateSourceLastRefresh(sources, "src2")
  if result[1].Reg.LastRefresh == "" {
    t.Error("src2 LastRefresh should be updated")
  }
  if result[0].Git != nil && result[0].Git.LastPull != "" {
    t.Error("src1 should not be affected")
  }
}

func TestTimeNow(t *testing.T) {
  now := timeNow()
  if len(now) != 19 {
    t.Errorf("expected 19 chars (2006-01-02 15:04:05), got %q (len %d)", now, len(now))
  }
}

func TestSaveSkillsConfig(t *testing.T) {
  auth.ResetDB()
  tmpDir := t.TempDir()
  if err := auth.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  if err := config.InitDBDefaults(); err != nil {
    t.Fatalf("InitDBDefaults: %v", err)
  }
  cfg, err := config.LoadFromDB()
  if err != nil {
    t.Fatalf("LoadFromDB: %v", err)
  }
  s := &Server{cfg: cfg, secret: "test", csrfKey: "test-csrf"}
  s.saveSkillsConfig()
  // should not panic or error
}

func TestSecureHeaders(t *testing.T) {
  s := newTestServer(t)
  gin.SetMode(gin.TestMode)
  r := gin.New()
  r.Use(s.secureHeaders())
  r.GET("/test", func(c *gin.Context) {
    c.String(200, "ok")
  })

  w := httptest.NewRecorder()
  req := httptest.NewRequest("GET", "/test", nil)
  r.ServeHTTP(w, req)

  if w.Code != 200 {
    t.Fatalf("status=%d", w.Code)
  }
  if w.Header().Get("X-Content-Type-Options") != "nosniff" {
    t.Error("X-Content-Type-Options should be nosniff")
  }
  if w.Header().Get("X-Frame-Options") != "DENY" {
    t.Error("X-Frame-Options should be DENY")
  }
}

func TestUniqueStrings(t *testing.T) {
  input := []string{"a", "b", "a", "c", "b", "d"}
  result := uniqueStrings(input)
  expected := []string{"a", "b", "c", "d"}
  if len(result) != len(expected) {
    t.Errorf("len=%d, want %d, got %v", len(result), len(expected), result)
    return
  }
  for i, v := range expected {
    if result[i] != v {
      t.Errorf("result[%d]=%q, want %q", i, result[i], v)
    }
  }
}

func TestImageRequiredMiddleware(t *testing.T) {
  s := newTestServer(t)
  s.dockerAvailable = false
  gin.SetMode(gin.TestMode)
  handler := s.imageRequiredMiddleware()

  // GET requests should pass through
  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  c.Request = httptest.NewRequest("GET", "/api/admin/users", nil)
  handler(c)
  if w.Code != 200 && !c.IsAborted() {
    t.Error("GET should pass through")
  }

  // POST to image endpoints should pass through
  w = httptest.NewRecorder()
  c, _ = gin.CreateTestContext(w)
  c.Request = httptest.NewRequest("POST", "/api/admin/images", nil)
  handler(c)
  if c.IsAborted() {
    t.Error("POST to /admin/images should pass through")
  }
}

func TestHandleFileUploadNoAuth(t *testing.T) {
  env := setupTestServer(t)
  // POST without session
  resp, err := http.Post(env.HTTP.URL+"/api/files/upload", "multipart/form-data", nil)
  if err != nil {
    t.Fatalf("POST upload: %v", err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusUnauthorized {
    t.Errorf("status=%d, want 401", resp.StatusCode)
  }
}

func TestHandleFileUploadNoCSRF(t *testing.T) {
  env := setupTestServer(t)
  req, err := http.NewRequest("POST", env.HTTP.URL+"/api/files/upload", nil)
  if err != nil {
    t.Fatalf("NewRequest: %v", err)
  }
  req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testuser")})
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("Do: %v", err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusForbidden {
    t.Errorf("status=%d, want 403 (CSRF)", resp.StatusCode)
  }
}
