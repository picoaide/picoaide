package web

import (
  "encoding/json"
  "io"
  "net/http"
  "net/url"
  "testing"
)

func TestConfig_Get_Superadmin(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/config", "testadmin")
  assertStatus(t, resp, 200)
}

func TestConfig_Get_RegularUser(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/config", "testuser")
  assertStatus(t, resp, 403)
}

func TestConfig_Get_Unauthenticated(t *testing.T) {
  env := setupTestServer(t)
  resp, _ := http.Get(env.HTTP.URL + "/api/config")
  assertStatus(t, resp, 401)
}

func TestConfig_SaveAndGet(t *testing.T) {
  env := setupTestServer(t)
  // 获取当前配置
  resp := env.get(t, "/api/config", "testadmin")
  assertStatus(t, resp, 200)
  body, _ := io.ReadAll(resp.Body)
  resp.Body.Close()

  // 修改并保存
  var cfg map[string]interface{}
  if err := json.Unmarshal(body, &cfg); err != nil {
    t.Fatalf("JSON unmarshal failed: %v", err)
  }
  cfg["users_root"] = "./custom-users"

  configJSON, _ := json.Marshal(cfg)
  form := url.Values{"config": {string(configJSON)}}
  resp = env.postForm(t, "/api/config", "testadmin", form)
  assertStatus(t, resp, 200)

  // 重新加载并验证
  resp = env.get(t, "/api/config", "testadmin")
  assertStatus(t, resp, 200)
  body, _ = io.ReadAll(resp.Body)
  resp.Body.Close()
  if err := json.Unmarshal(body, &cfg); err != nil {
    t.Fatalf("JSON unmarshal failed: %v", err)
  }
  if cfg["users_root"] != "./custom-users" {
    t.Errorf("users_root=%v", cfg["users_root"])
  }
}
