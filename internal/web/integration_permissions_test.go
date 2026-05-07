package web

import (
  "fmt"
  "io"
  "net/http"
  "net/http/httptest"
  "net/url"
  "strings"
  "testing"
)

func TestRequestBodySizeLimit_RejectsOversizedBody(t *testing.T) {
  env := setupTestServer(t)

  bigBody := strings.NewReader(strings.Repeat("x", 2<<20))
  req, _ := http.NewRequest("POST", env.HTTP.URL+"/api/login", bigBody)
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatal(err)
  }
  defer resp.Body.Close()

  if resp.StatusCode == 200 {
    t.Fatalf("expected non-200 for oversized body, got %d", resp.StatusCode)
  }
}

func TestRequestBodySizeLimit_SmallBodyPasses(t *testing.T) {
  s := newTestServer(t)
  handler := s.secureHeaders(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("ok"))
  })

  smallBody := strings.NewReader("small")
  req := httptest.NewRequest("POST", "/api/test", smallBody)
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  rec := httptest.NewRecorder()
  handler(rec, req)

  if rec.Code != 200 {
    t.Fatalf("small body should pass, got %d", rec.Code)
  }
}

func TestRequestBodySizeLimit_BigBody413(t *testing.T) {
  s := newTestServer(t)
  handler := s.secureHeaders(func(w http.ResponseWriter, r *http.Request) {
    _, err := io.ReadAll(r.Body)
    if err != nil {
      http.Error(w, "请求体过大", http.StatusRequestEntityTooLarge)
      return
    }
    w.WriteHeader(http.StatusOK)
  })

  bigBody := strings.NewReader(strings.Repeat("x", 2<<20))
  req := httptest.NewRequest("POST", "/api/test", bigBody)
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  rec := httptest.NewRecorder()
  handler(rec, req)

  if rec.Code != 413 {
    t.Fatalf("expected 413, got %d", rec.Code)
  }
}

func TestRequestBodySizeLimit_SkipsUploadPath(t *testing.T) {
  s := newTestServer(t)
  handler := s.secureHeaders(func(w http.ResponseWriter, r *http.Request) {
    buf := make([]byte, 2<<20+1)
    n, _ := r.Body.Read(buf)
    w.WriteHeader(http.StatusOK)
    fmt.Fprintf(w, "read %d", n)
  })

  bigBody := strings.NewReader(strings.Repeat("x", 2<<20))
  req := httptest.NewRequest("POST", "/api/files/upload", bigBody)
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  rec := httptest.NewRecorder()
  handler(rec, req)

  if rec.Code != 200 {
    t.Fatalf("upload path should not be limited, got %d", rec.Code)
  }
}

func TestRequestBodySizeLimit_SkipsGetRequests(t *testing.T) {
  s := newTestServer(t)
  handler := s.secureHeaders(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
  })

  req := httptest.NewRequest("GET", "/api/test", nil)
  rec := httptest.NewRecorder()
  handler(rec, req)

  if rec.Code != 200 {
    t.Fatalf("GET should not be limited, got %d", rec.Code)
  }
}

func TestUnauthenticatedAccess_Returns401(t *testing.T) {
  env := setupTestServer(t)
  endpoints := []struct {
    method, path string
  }{
    {"GET", "/api/user/info"},
    {"GET", "/api/files"},
    {"GET", "/api/csrf"},
    {"GET", "/api/mcp/token"},
    {"GET", "/api/dingtalk"},
    {"GET", "/api/admin/users"},
    {"GET", "/api/admin/superadmins"},
    {"GET", "/api/admin/groups"},
    {"GET", "/api/admin/whitelist"},
    {"GET", "/api/config"},
  }
  for _, ep := range endpoints {
    req, _ := http.NewRequest(ep.method, env.HTTP.URL+ep.path, nil)
    resp, _ := http.DefaultClient.Do(req)
    if resp.StatusCode != 401 {
      t.Errorf("%s %s: status=%d, want 401", ep.method, ep.path, resp.StatusCode)
    }
    resp.Body.Close()
  }
}

func TestRegularUser_AdminEndpoints_Returns403(t *testing.T) {
  env := setupTestServer(t)
  endpoints := []struct {
    method, path string
  }{
    {"GET", "/api/admin/users"},
    {"GET", "/api/admin/superadmins"},
    {"GET", "/api/admin/groups"},
    {"GET", "/api/admin/whitelist"},
    {"GET", "/api/config"},
  }
  for _, ep := range endpoints {
    resp := env.get(t, ep.path, "testuser")
    if resp.StatusCode != 403 {
      t.Errorf("%s %s as testuser: status=%d, want 403", ep.method, ep.path, resp.StatusCode)
    }
    resp.Body.Close()
  }
}

func TestPostWithoutCSRF_Returns403(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "old_password": {"user123"},
    "new_password": {"newpass123"},
  }
  req, _ := http.NewRequest("POST", env.HTTP.URL+"/api/user/password",
    strings.NewReader(form.Encode()))
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  req.AddCookie(&http.Cookie{
    Name:  "session",
    Value: env.Server.createSessionToken("testuser"),
  })
  resp, _ := http.DefaultClient.Do(req)
  if resp.StatusCode != 403 {
    t.Errorf("status=%d, want 403", resp.StatusCode)
  }
  resp.Body.Close()
}

func TestLoginRateLimit_Returns429(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"username": {"testuser"}, "password": {"wrong"}}
  for i := 0; i < 10; i++ {
    req, _ := http.NewRequest("POST", env.HTTP.URL+"/api/login", strings.NewReader(form.Encode()))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
      t.Fatal(err)
    }
    resp.Body.Close()
  }
  req, _ := http.NewRequest("POST", env.HTTP.URL+"/api/login", strings.NewReader(form.Encode()))
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatal(err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != 429 {
    t.Fatalf("expected 429 after 10 attempts, got %d", resp.StatusCode)
  }
}

func TestDockerDependentEndpoints_Return503(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"username": {"testuser"}}
  resp := env.postForm(t, "/api/admin/container/start", "testadmin", form)
  if resp.StatusCode != 503 {
    t.Errorf("container/start: status=%d, want 503", resp.StatusCode)
  }
  resp.Body.Close()
}
