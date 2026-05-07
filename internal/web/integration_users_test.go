package web

import (
  "net/url"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
)

func TestAdminUsers_List(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/users", "testadmin")
  assertStatus(t, resp, 200)
  var result struct {
    Success bool                     `json:"success"`
    Users   []map[string]interface{} `json:"users"`
  }
  parseJSON(t, resp, &result)
  if !result.Success {
    t.Error("should succeed")
  }
  if len(result.Users) == 0 {
    t.Error("should have users")
  }
}

func TestAdminUsers_ForbiddenForRegularUser(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/users", "testuser")
  assertStatus(t, resp, 403)
}

func TestAdminUserCreate_InvalidUsername(t *testing.T) {
  env := setupTestServer(t)
  // 设置为本地模式，否则统一认证会先拒绝
  env.Cfg.Web.AuthMode = "local"
  form := url.Values{"username": {"bad/user"}}
  resp := env.postForm(t, "/api/admin/users/create", "testadmin", form)
  assertStatus(t, resp, 400)
}

func TestAdminUserCreate_Duplicate(t *testing.T) {
  env := setupTestServer(t)
  env.Cfg.Web.AuthMode = "local"
  form := url.Values{"username": {"testuser"}}
  resp := env.postForm(t, "/api/admin/users/create", "testadmin", form)
  // 用户已存在，应该失败
  if resp.StatusCode != 400 && resp.StatusCode != 500 {
    t.Errorf("status=%d, want 400 or 500", resp.StatusCode)
  }
}

func TestSuperadmins_List(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/superadmins", "testadmin")
  assertStatus(t, resp, 200)
  var result struct {
    Success bool     `json:"success"`
    Admins  []string `json:"admins"`
  }
  parseJSON(t, resp, &result)
  if len(result.Admins) == 0 {
    t.Error("should have superadmins")
  }
}

func TestSuperadminCreate_Success(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"username": {"newadmin"}}
  resp := env.postForm(t, "/api/admin/superadmins/create", "testadmin", form)
  assertStatus(t, resp, 200)
  var result struct {
    Success  bool   `json:"success"`
    Password string `json:"password"`
  }
  parseJSON(t, resp, &result)
  if !result.Success {
    t.Error("should succeed")
  }
  if result.Password == "" {
    t.Error("should return password")
  }
  // 验证角色
  if !auth.IsSuperadmin("newadmin") {
    t.Error("should be superadmin")
  }
}

func TestSuperadminCreate_Duplicate(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"username": {"testadmin"}}
  resp := env.postForm(t, "/api/admin/superadmins/create", "testadmin", form)
  if resp.StatusCode != 400 && resp.StatusCode != 500 {
    t.Errorf("status=%d, want error", resp.StatusCode)
  }
}

func TestSuperadminDelete_SelfDeletion(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"username": {"testadmin"}}
  resp := env.postForm(t, "/api/admin/superadmins/delete", "testadmin", form)
  // 自我删除应被拒绝
  assertStatus(t, resp, 400)
}

func TestSuperadminDelete_Success(t *testing.T) {
  env := setupTestServer(t)
  // 先创建另一个超管
  if err := auth.CreateUser("otheradmin", "pass123", "superadmin"); err != nil {
    t.Fatalf("CreateUser: %v", err)
  }
  form := url.Values{"username": {"otheradmin"}}
  resp := env.postForm(t, "/api/admin/superadmins/delete", "testadmin", form)
  assertStatus(t, resp, 200)
  if auth.IsSuperadmin("otheradmin") {
    t.Error("should be deleted")
  }
}

func TestSuperadminReset_Success(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"username": {"testadmin"}}
  resp := env.postForm(t, "/api/admin/superadmins/reset", "testadmin", form)
  assertStatus(t, resp, 200)
  var result struct {
    Success  bool   `json:"success"`
    Password string `json:"password"`
  }
  parseJSON(t, resp, &result)
  if result.Password == "" {
    t.Error("should return new password")
  }
}

func TestSuperadminReset_NotSuperadmin(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"username": {"testuser"}}
  resp := env.postForm(t, "/api/admin/superadmins/reset", "testadmin", form)
  if resp.StatusCode != 400 && resp.StatusCode != 404 {
    t.Errorf("status=%d, want error", resp.StatusCode)
  }
}
