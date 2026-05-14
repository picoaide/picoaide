package web

import (
  "net/http"
  "net/url"
  "strings"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/user"
)

func TestMCPCookies_GetEmpty(t *testing.T) {
  env := setupTestServer(t)
  token := getMCPTokenForTest(t, env)

  resp := mcpGet(t, env, "/api/mcp/cookies?domain=example.com", token)
  assertStatus(t, resp, 200)
  data := getJSON(t, resp)
  if data["success"] != true {
    t.Errorf("success = %v, want true", data["success"])
  }
  if data["cookies"] != "" {
    t.Errorf("cookies = %q, want empty string", data["cookies"])
  }
  if data["domain"] != "example.com" {
    t.Errorf("domain = %q, want %q", data["domain"], "example.com")
  }
}

func TestMCPCookies_SetAndGet(t *testing.T) {
  env := setupTestServer(t)
  token := getMCPTokenForTest(t, env)

  // Set cookies via POST
  form := url.Values{"domain": {"example.com"}, "cookies": {"session=abc; token=xyz"}}
  resp := mcpPost(t, env, "/api/mcp/cookies", token, form)
  assertStatus(t, resp, 200)
  data := getJSON(t, resp)
  if data["success"] != true {
    t.Errorf("POST success = %v, want true", data["success"])
  }

  // Verify via GET
  resp = mcpGet(t, env, "/api/mcp/cookies?domain=example.com", token)
  assertStatus(t, resp, 200)
  data = getJSON(t, resp)
  if data["success"] != true {
    t.Errorf("GET success = %v, want true", data["success"])
  }
  if data["cookies"] != "session=abc; token=xyz" {
    t.Errorf("cookies = %q, want %q", data["cookies"], "session=abc; token=xyz")
  }
  if data["domain"] != "example.com" {
    t.Errorf("domain = %q, want %q", data["domain"], "example.com")
  }
}

func TestMCPCookies_Overwrite(t *testing.T) {
  env := setupTestServer(t)
  token := getMCPTokenForTest(t, env)

  form := url.Values{"domain": {"example.com"}, "cookies": {"old=value"}}
  mcpPost(t, env, "/api/mcp/cookies", token, form)

  form.Set("cookies", "new=value")
  mcpPost(t, env, "/api/mcp/cookies", token, form)

  resp := mcpGet(t, env, "/api/mcp/cookies?domain=example.com", token)
  data := getJSON(t, resp)
  if data["cookies"] != "new=value" {
    t.Errorf("cookies = %q, want %q", data["cookies"], "new=value")
  }
}

func TestMCPCookies_GetAll(t *testing.T) {
  env := setupTestServer(t)
  token := getMCPTokenForTest(t, env)

  mcpPost(t, env, "/api/mcp/cookies", token, url.Values{"domain": {"a.com"}, "cookies": {"a=1"}})
  mcpPost(t, env, "/api/mcp/cookies", token, url.Values{"domain": {"b.com"}, "cookies": {"b=2"}})

  // Without domain param, returns all
  resp := mcpGet(t, env, "/api/mcp/cookies", token)
  assertStatus(t, resp, 200)
  data := getJSON(t, resp)
  if data["success"] != true {
    t.Errorf("success = %v, want true", data["success"])
  }
  all, ok := data["cookies"].(map[string]interface{})
  if !ok {
    t.Fatalf("cookies is not a map: %T", data["cookies"])
  }
  if len(all) != 2 {
    t.Errorf("got %d domains, want 2", len(all))
  }
  if all["a.com"] != "a=1" {
    t.Errorf("a.com = %q", all["a.com"])
  }
  if all["b.com"] != "b=2" {
    t.Errorf("b.com = %q", all["b.com"])
  }
}

func TestMCPCookies_PostMissingDomain(t *testing.T) {
  env := setupTestServer(t)
  token := getMCPTokenForTest(t, env)

  form := url.Values{"cookies": {"val"}}
  resp := mcpPost(t, env, "/api/mcp/cookies", token, form)
  assertStatus(t, resp, 400)
}

func TestMCPCookies_PostMissingCookies(t *testing.T) {
  env := setupTestServer(t)
  token := getMCPTokenForTest(t, env)

  form := url.Values{"domain": {"example.com"}}
  resp := mcpPost(t, env, "/api/mcp/cookies", token, form)
  assertStatus(t, resp, 400)
}

func TestMCPCookies_AuthRequired(t *testing.T) {
  env := setupTestServer(t)

  // No token → 401
  resp := mcpGet(t, env, "/api/mcp/cookies?domain=x.com", "")
  assertStatus(t, resp, 401)
}

func TestMCPCookies_UserIsolation(t *testing.T) {
  env := setupTestServer(t)

  // Create second user
  if err := auth.CreateUser("user2", "pass2", "user"); err != nil {
    t.Fatal(err)
  }
  if err := user.InitUser(env.Cfg, "user2", ""); err != nil {
    t.Fatal(err)
  }

  // Get tokens for both users
  token1 := getMCPTokenForTest(t, env) // "testuser"
  token2 := getMCPTokenForTestAs(t, env, "user2")

  // testuser writes
  mcpPost(t, env, "/api/mcp/cookies", token1, url.Values{"domain": {"x.com"}, "cookies": {"from=testuser"}})

  // user2 reads — should be empty
  resp := mcpGet(t, env, "/api/mcp/cookies?domain=x.com", token2)
  data := getJSON(t, resp)
  if data["cookies"] != "" {
    t.Errorf("user2 should see empty, got %q", data["cookies"])
  }
}

// getMCPTokenForTestAs 获取指定用户的 MCP token
func getMCPTokenForTestAs(t *testing.T, env *testEnv, username string) string {
  t.Helper()
  resp := env.get(t, "/api/mcp/token", username)
  assertStatus(t, resp, 200)
  var result struct {
    Token string `json:"token"`
  }
  parseJSON(t, resp, &result)
  if result.Token == "" {
    t.Fatal("empty MCP token for " + username)
  }
  return result.Token
}

// mcpGet 发送带 MCP Bearer token 的 GET 请求
func mcpGet(t *testing.T, env *testEnv, path, token string) *http.Response {
  t.Helper()
  req, err := http.NewRequest("GET", env.HTTP.URL+path, nil)
  if err != nil {
    t.Fatalf("创建请求失败: %v", err)
  }
  if token != "" {
    req.Header.Set("Authorization", "Bearer "+token)
  }
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("发送请求失败: %v", err)
  }
  return resp
}

// mcpPost 发送带 MCP Bearer token 的 POST 请求
func mcpPost(t *testing.T, env *testEnv, path, token string, form url.Values) *http.Response {
  t.Helper()
  body := form.Encode()
  req, err := http.NewRequest("POST", env.HTTP.URL+path, strings.NewReader(body))
  if err != nil {
    t.Fatalf("创建请求失败: %v", err)
  }
  if token != "" {
    req.Header.Set("Authorization", "Bearer "+token)
  }
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("发送请求失败: %v", err)
  }
  return resp
}
