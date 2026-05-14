package web

import (
  "net/url"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/user"
)

func TestUserCookies_ListEmpty(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/user/cookies", "testuser")
  assertStatus(t, resp, 200)
  data := getJSON(t, resp)
  if data["success"] != true {
    t.Errorf("success = %v, want true", data["success"])
  }
  list, ok := data["list"].([]interface{})
  if !ok {
    t.Fatalf("list is not array: %T", data["list"])
  }
  if len(list) != 0 {
    t.Errorf("list = %v, want empty", list)
  }
}

func TestUserCookies_ListWithData(t *testing.T) {
  env := setupTestServer(t)

  // Insert cookies via DB directly (same as browser extension would)
  if err := auth.SetCookie("testuser", "example.com", "session=abc"); err != nil {
    t.Fatal(err)
  }
  if err := auth.SetCookie("testuser", "other.com", "token=xyz"); err != nil {
    t.Fatal(err)
  }

  resp := env.get(t, "/api/user/cookies", "testuser")
  assertStatus(t, resp, 200)
  data := getJSON(t, resp)
  list, ok := data["list"].([]interface{})
  if !ok {
    t.Fatalf("list is not array: %T", data["list"])
  }
  if len(list) != 2 {
    t.Errorf("got %d entries, want 2", len(list))
  }
  // Verify each entry has domain and updated_at, but NOT cookies
  for _, item := range list {
    entry, ok := item.(map[string]interface{})
    if !ok {
      t.Errorf("entry is not map: %T", item)
      continue
    }
    if entry["domain"] == nil {
      t.Error("entry missing domain")
    }
    if entry["updated_at"] == nil {
      t.Error("entry missing updated_at")
    }
    if entry["cookies"] != nil {
      t.Error("entry should not expose cookies value")
    }
  }
}

func TestUserCookies_Delete(t *testing.T) {
  env := setupTestServer(t)

  if err := auth.SetCookie("testuser", "example.com", "session=abc"); err != nil {
    t.Fatal(err)
  }

  form := url.Values{"domain": {"example.com"}}
  resp := env.postForm(t, "/api/user/cookies/delete", "testuser", form)
  assertStatus(t, resp, 200)
  data := getJSON(t, resp)
  if data["success"] != true {
    t.Errorf("success = %v, want true", data["success"])
  }

  // Verify it's gone
  got, _ := auth.GetCookie("testuser", "example.com")
  if got != "" {
    t.Errorf("cookie still exists: %q", got)
  }
}

func TestUserCookies_DeleteMissingDomain(t *testing.T) {
  env := setupTestServer(t)

  form := url.Values{"domain": {"nonexistent.com"}}
  resp := env.postForm(t, "/api/user/cookies/delete", "testuser", form)
  assertStatus(t, resp, 200)
}

func TestUserCookies_DeleteRequiresAuth(t *testing.T) {
  env := setupTestServer(t)
  // 未认证用户无法访问 API
  resp := env.get(t, "/api/user/cookies", "")
  assertStatus(t, resp, 401)
}

func TestUserCookies_UserIsolation(t *testing.T) {
  env := setupTestServer(t)

  if err := auth.CreateUser("user2", "pass2", "user"); err != nil {
    t.Fatal(err)
  }
  if err := user.InitUser(env.Cfg, "user2", ""); err != nil {
    t.Fatal(err)
  }

  auth.SetCookie("testuser", "example.com", "from_testuser")
  auth.SetCookie("user2", "other.com", "from_user2")

  // testuser should only see their own
  resp := env.get(t, "/api/user/cookies", "testuser")
  data := getJSON(t, resp)
  list := data["list"].([]interface{})
  if len(list) != 1 {
    t.Errorf("testuser sees %d entries, want 1", len(list))
  }
}
