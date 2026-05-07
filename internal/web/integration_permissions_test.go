package web

import (
  "net/http"
  "net/url"
  "strings"
  "testing"
)

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

func TestDockerDependentEndpoints_Return503(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"username": {"testuser"}}
  resp := env.postForm(t, "/api/admin/container/start", "testadmin", form)
  if resp.StatusCode != 503 {
    t.Errorf("container/start: status=%d, want 503", resp.StatusCode)
  }
  resp.Body.Close()
}
