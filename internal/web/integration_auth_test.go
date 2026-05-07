package web

import (
  "net/http"
  "net/url"
  "testing"
)

func TestLogin_Success(t *testing.T) {
  env := setupTestServer(t)
  resp, _ := http.PostForm(env.HTTP.URL+"/api/login", url.Values{
    "username": {"testuser"},
    "password": {"user123"},
  })
  assertStatus(t, resp, 200)
  var result struct {
    Success  bool   `json:"success"`
    Username string `json:"username"`
  }
  parseJSON(t, resp, &result)
  if !result.Success {
    t.Error("should succeed")
  }
  if result.Username != "testuser" {
    t.Errorf("username=%q, want %q", result.Username, "testuser")
  }
}

func TestLogin_WrongPassword(t *testing.T) {
  env := setupTestServer(t)
  resp, _ := http.PostForm(env.HTTP.URL+"/api/login", url.Values{
    "username": {"testuser"},
    "password": {"wrong"},
  })
  assertStatus(t, resp, 401)
}

func TestLogin_MissingFields(t *testing.T) {
  env := setupTestServer(t)
  resp, _ := http.PostForm(env.HTTP.URL+"/api/login", url.Values{
    "username": {""},
    "password": {""},
  })
  assertStatus(t, resp, 400)
}

func TestLogin_WrongMethod(t *testing.T) {
  env := setupTestServer(t)
  resp, _ := http.Get(env.HTTP.URL + "/api/login")
  assertStatus(t, resp, 405)
}

func TestLogout_Success(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postForm(t, "/api/logout", "testuser", url.Values{})
  assertStatus(t, resp, 200)
  // 验证 session cookie 被清除（MaxAge < 0）
  var cookieFound bool
  for _, c := range resp.Cookies() {
    if c.Name == "session" && c.MaxAge < 0 {
      cookieFound = true
    }
  }
  if !cookieFound {
    t.Error("session cookie should be cleared")
  }
}

func TestUserInfo_Superadmin(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/user/info", "testadmin")
  assertStatus(t, resp, 200)
  var result struct {
    Success bool   `json:"success"`
    Role    string `json:"role"`
  }
  parseJSON(t, resp, &result)
  if result.Role != "superadmin" {
    t.Errorf("role=%q, want %q", result.Role, "superadmin")
  }
}

func TestUserInfo_RegularUser(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/user/info", "testuser")
  assertStatus(t, resp, 200)
  var result struct {
    Role string `json:"role"`
  }
  parseJSON(t, resp, &result)
  if result.Role != "user" {
    t.Errorf("role=%q, want %q", result.Role, "user")
  }
}

func TestUserInfo_Unauthenticated(t *testing.T) {
  env := setupTestServer(t)
  resp, _ := http.Get(env.HTTP.URL + "/api/user/info")
  assertStatus(t, resp, 401)
}

func TestCSRF_Get(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/csrf", "testuser")
  assertStatus(t, resp, 200)
  var result struct {
    CSRFToken string `json:"csrf_token"`
  }
  parseJSON(t, resp, &result)
  if result.CSRFToken == "" {
    t.Error("expected non-empty CSRF token")
  }
}

func TestChangePassword_Success(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "old_password": {"user123"},
    "new_password": {"newpass123"},
  }
  resp := env.postForm(t, "/api/user/password", "testuser", form)
  assertStatus(t, resp, 200)

  // 验证新密码可以登录
  loginResp, _ := http.PostForm(env.HTTP.URL+"/api/login", url.Values{
    "username": {"testuser"},
    "password": {"newpass123"},
  })
  assertStatus(t, loginResp, 200)
}

func TestChangePassword_WrongOld(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "old_password": {"wrongpass"},
    "new_password": {"newpass123"},
  }
  resp := env.postForm(t, "/api/user/password", "testuser", form)
  assertStatus(t, resp, 401)
}

func TestChangePassword_TooShort(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "old_password": {"user123"},
    "new_password": {"abc"},
  }
  resp := env.postForm(t, "/api/user/password", "testuser", form)
  assertStatus(t, resp, 400)
}

func TestChangePassword_UnifiedAuth(t *testing.T) {
  env := setupTestServer(t)
  // 切换为 LDAP 统一认证模式（setupTestServer 默认 local）
  env.Cfg.Web.AuthMode = "ldap"
  form := url.Values{
    "old_password": {"user123"},
    "new_password": {"newpass123"},
  }
  resp := env.postForm(t, "/api/user/password", "testuser", form)
  assertStatus(t, resp, 403)
}
