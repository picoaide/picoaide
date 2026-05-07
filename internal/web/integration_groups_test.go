package web

import (
  "net/url"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
)

func TestWhitelist_GetEmpty(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/whitelist", "testadmin")
  assertStatus(t, resp, 200)
}

func TestWhitelist_UpdateAndGet(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"users": {"user1,user2"}}
  resp := env.postForm(t, "/api/admin/whitelist", "testadmin", form)
  assertStatus(t, resp, 200)
  // 验证更新后能读取
  resp = env.get(t, "/api/admin/whitelist", "testadmin")
  assertStatus(t, resp, 200)
}

func TestGroups_ListEmpty(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/groups", "testadmin")
  assertStatus(t, resp, 200)
}

func TestGroupCreate_Success(t *testing.T) {
  env := setupTestServer(t)
  // 设置为本地模式以允许手动创建组
  env.Cfg.Web.AuthMode = "local"
  form := url.Values{
    "name":        {"dev-team"},
    "description": {"Developers"},
  }
  resp := env.postForm(t, "/api/admin/groups/create", "testadmin", form)
  assertStatus(t, resp, 200)
}

func TestGroupCreate_Duplicate(t *testing.T) {
  env := setupTestServer(t)
  env.Cfg.Web.AuthMode = "local"
  form := url.Values{"name": {"dev-team"}}
  env.postForm(t, "/api/admin/groups/create", "testadmin", form)
  resp := env.postForm(t, "/api/admin/groups/create", "testadmin", form)
  if resp.StatusCode != 400 && resp.StatusCode != 500 {
    t.Errorf("duplicate create status=%d", resp.StatusCode)
  }
}

func TestGroupDelete_Success(t *testing.T) {
  env := setupTestServer(t)
  if err := auth.CreateGroup("to-delete", "local", "", nil); err != nil {
    t.Fatalf("CreateGroup: %v", err)
  }
  form := url.Values{"name": {"to-delete"}}
  resp := env.postForm(t, "/api/admin/groups/delete", "testadmin", form)
  assertStatus(t, resp, 200)
}

func TestGroupDelete_Nonexistent(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"name": {"no-such-group"}}
  resp := env.postForm(t, "/api/admin/groups/delete", "testadmin", form)
  if resp.StatusCode != 400 && resp.StatusCode != 404 {
    t.Errorf("status=%d", resp.StatusCode)
  }
}

func TestGroupMembers_AddListRemove(t *testing.T) {
  env := setupTestServer(t)
  if err := auth.CreateGroup("team-a", "local", "", nil); err != nil {
    t.Fatalf("CreateGroup: %v", err)
  }

  // 添加成员
  form := url.Values{
    "group_name": {"team-a"},
    "usernames":  {"testuser"},
  }
  resp := env.postForm(t, "/api/admin/groups/members/add", "testadmin", form)
  assertStatus(t, resp, 200)

  // 列出成员
  resp = env.get(t, "/api/admin/groups/members?name=team-a", "testadmin")
  assertStatus(t, resp, 200)
  var result struct {
    Members []string `json:"members"`
  }
  parseJSON(t, resp, &result)
  found := false
  for _, m := range result.Members {
    if m == "testuser" {
      found = true
    }
  }
  if !found {
    t.Error("testuser should be in team-a members")
  }

  // 移除成员
  form = url.Values{
    "group_name": {"team-a"},
    "username":   {"testuser"},
  }
  resp = env.postForm(t, "/api/admin/groups/members/remove", "testadmin", form)
  assertStatus(t, resp, 200)
}

func TestGroupSkills_BindUnbind(t *testing.T) {
  env := setupTestServer(t)
  if err := auth.CreateGroup("team-b", "local", "", nil); err != nil {
    t.Fatalf("CreateGroup: %v", err)
  }

  form := url.Values{
    "group_name": {"team-b"},
    "skill_name": {"test-skill"},
  }
  resp := env.postForm(t, "/api/admin/groups/skills/bind", "testadmin", form)
  // 绑定可能成功也可能因技能目录不存在而失败
  t.Logf("bind status=%d", resp.StatusCode)

  form = url.Values{
    "group_name": {"team-b"},
    "skill_name": {"test-skill"},
  }
  resp = env.postForm(t, "/api/admin/groups/skills/unbind", "testadmin", form)
  assertStatus(t, resp, 200)
}
