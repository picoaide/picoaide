package web

import (
	"net/http"
	"net/url"
	"strings"
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

func TestLogin_SuperadminAllowedFromWeb(t *testing.T) {
	env := setupTestServer(t)
	resp, _ := http.PostForm(env.HTTP.URL+"/api/login", url.Values{
		"username": {"testadmin"},
		"password": {"admin123"},
	})
	assertStatus(t, resp, 200)
}

func TestLogin_SuperadminBlockedFromExtension(t *testing.T) {
	env := setupTestServer(t)
	form := url.Values{
		"username": {"testadmin"},
		"password": {"admin123"},
	}
	req, err := http.NewRequest("POST", env.HTTP.URL+"/api/login", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "chrome-extension://picoaide-test")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, resp, 403)
	var result struct {
		Error string `json:"error"`
	}
	parseJSON(t, resp, &result)
	if result.Error != "超管用户不允许登录插件，使用普通用户登录" {
		t.Errorf("error=%q", result.Error)
	}
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
	// Gin 按方法注册路由，GET /api/login 未注册故返回 404（而非 405）
	assertStatus(t, resp, 404)
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

func TestExtensionOnlyAPIs_BlockSuperadmin(t *testing.T) {
	env := setupTestServer(t)
	resp := env.get(t, "/api/mcp/token", "testadmin")
	assertStatus(t, resp, 403)
	var tokenResult struct {
		Error string `json:"error"`
	}
	parseJSON(t, resp, &tokenResult)
	if tokenResult.Error != "超管用户不允许登录插件，使用普通用户登录" {
		t.Errorf("mcp error=%q", tokenResult.Error)
	}

	form := url.Values{
		"domain":  {"example.com"},
		"cookies": {"sid=1"},
	}
	resp = env.postForm(t, "/api/cookies", "testadmin", form)
	assertStatus(t, resp, 403)
}

func TestCookiesRequiresCSRF(t *testing.T) {
	env := setupTestServer(t)
	form := url.Values{
		"domain":  {"example.com"},
		"cookies": {"sid=1"},
	}
	req, err := http.NewRequest("POST", env.HTTP.URL+"/api/cookies", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{
		Name:  "session",
		Value: env.Server.createSessionToken("testuser"),
	})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, resp, 403)
}

func TestCookiesWithCSRF(t *testing.T) {
	env := setupTestServer(t)
	form := url.Values{
		"domain":  {"example.com"},
		"cookies": {"sid=1"},
	}
	resp := env.postForm(t, "/api/cookies", "testuser", form)
	assertStatus(t, resp, 200)
}

func TestRegularUserAPIs_BlockSuperadmin(t *testing.T) {
	env := setupTestServer(t)
	for _, path := range []string{"/api/dingtalk", "/api/files"} {
		resp := env.get(t, path, "testadmin")
		assertStatus(t, resp, 403)
	}
}

func TestUIRoutesRequireRole(t *testing.T) {
	env := setupTestServer(t)

	resp, err := http.Get(env.HTTP.URL + "/admin/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, resp, 200)
	if got := resp.Request.URL.Path; got != "/login" {
		t.Fatalf("unauthenticated admin final path=%q, want /login", got)
	}

	resp = env.get(t, "/admin/dashboard", "testuser")
	assertStatus(t, resp, 200)
	if got := resp.Request.URL.Path; got != "/manage" {
		t.Fatalf("regular user admin final path=%q, want /manage", got)
	}

	resp = env.get(t, "/manage", "testadmin")
	assertStatus(t, resp, 200)
	if got := resp.Request.URL.Path; got != "/admin/dashboard" {
		t.Fatalf("superadmin manage final path=%q, want /admin/dashboard", got)
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

func TestHealth_NoAuth(t *testing.T) {
	env := setupTestServer(t)

	// 健康检查端点不需要认证
	resp, err := http.Get(env.HTTP.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	parseJSON(t, resp, &result)
	if result["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", result["status"])
	}
	if result["version"] == "" {
		t.Fatal("expected non-empty version")
	}
}
