package web

import (
  "net/http"
  "net/http/httptest"
  "net/url"
  "strings"
  "testing"

  "github.com/picoaide/picoaide/internal/config"
)

func newTestServer(t *testing.T) *Server {
  t.Helper()
  return &Server{
    cfg:     &config.GlobalConfig{},
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

  req := httptest.NewRequest("POST", "/test", strings.NewReader(
    url.Values{"csrf_token": {csrfToken}}.Encode(),
  ))
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})

  if !s.checkCSRF(req) {
    t.Error("checkCSRF should pass with valid token")
  }
}

func TestCheckCSRFInvalid(t *testing.T) {
  s := newTestServer(t)

  // 无 cookie
  req := httptest.NewRequest("POST", "/test", nil)
  if s.checkCSRF(req) {
    t.Error("checkCSRF should fail without session")
  }

  // 错误 CSRF token
  sessionToken := s.createSessionToken("admin")
  req = httptest.NewRequest("POST", "/test", strings.NewReader(
    url.Values{"csrf_token": {"wrongtoken"}}.Encode(),
  ))
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
  if s.checkCSRF(req) {
    t.Error("checkCSRF should fail with wrong token")
  }
}

func TestGetSessionUser(t *testing.T) {
  s := newTestServer(t)

  // 无 cookie
  req := httptest.NewRequest("GET", "/test", nil)
  if u := s.getSessionUser(req); u != "" {
    t.Errorf("getSessionUser without cookie = %q, want empty", u)
  }

  // 有效 cookie
  sessionToken := s.createSessionToken("testuser")
  req = httptest.NewRequest("GET", "/test", nil)
  req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
  if u := s.getSessionUser(req); u != "testuser" {
    t.Errorf("getSessionUser = %q, want %q", u, "testuser")
  }

  // 无效 cookie
  req = httptest.NewRequest("GET", "/test", nil)
  req.AddCookie(&http.Cookie{Name: "session", Value: "invalid"})
  if u := s.getSessionUser(req); u != "" {
    t.Errorf("getSessionUser with invalid cookie = %q, want empty", u)
  }
}
