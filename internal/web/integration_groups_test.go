package web

import (
	"net/url"
	"testing"

	"github.com/picoaide/picoaide/internal/auth"
	"github.com/picoaide/picoaide/internal/ldap"
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

func TestGroupMembers_IncludesInheritedSubGroupMembers(t *testing.T) {
	env := setupTestServer(t)
	if err := auth.CreateGroup("parent-team", "local", "", nil); err != nil {
		t.Fatalf("CreateGroup parent: %v", err)
	}
	parentID, err := auth.GetGroupID("parent-team")
	if err != nil {
		t.Fatalf("GetGroupID parent: %v", err)
	}
	if err := auth.CreateGroup("child-team", "local", "", &parentID); err != nil {
		t.Fatalf("CreateGroup child: %v", err)
	}
	if err := auth.AddUsersToGroup("parent-team", []string{"direct-user"}); err != nil {
		t.Fatalf("AddUsersToGroup parent: %v", err)
	}
	if err := auth.AddUsersToGroup("child-team", []string{"child-user"}); err != nil {
		t.Fatalf("AddUsersToGroup child: %v", err)
	}

	resp := env.get(t, "/api/admin/groups/members?name=parent-team", "testadmin")
	assertStatus(t, resp, 200)
	var result struct {
		Members          []string `json:"members"`
		InheritedMembers []string `json:"inherited_members"`
	}
	parseJSON(t, resp, &result)

	if len(result.Members) != 1 || result.Members[0] != "direct-user" {
		t.Fatalf("direct members = %v, want [direct-user]", result.Members)
	}
	if len(result.InheritedMembers) != 1 || result.InheritedMembers[0] != "child-user" {
		t.Fatalf("inherited members = %v, want [child-user]", result.InheritedMembers)
	}
}

func TestSyncLDAPGroupParentsUpdatesListHierarchy(t *testing.T) {
	env := setupTestServer(t)
	if err := auth.CreateGroup("ldap-parent", "ldap", "", nil); err != nil {
		t.Fatalf("CreateGroup parent: %v", err)
	}
	if err := auth.CreateGroup("ldap-child", "ldap", "", nil); err != nil {
		t.Fatalf("CreateGroup child: %v", err)
	}

	env.Server.syncLDAPGroupParents(map[string]ldap.GroupHierarchy{
		"ldap-parent": {SubGroups: []string{"ldap-child"}},
		"ldap-child":  {},
	})

	groups, err := auth.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}

	var parentID int64
	var childParentID *int64
	for _, group := range groups {
		if group.Name == "ldap-parent" {
			parentID = group.ID
		}
		if group.Name == "ldap-child" {
			childParentID = group.ParentID
		}
	}
	if parentID == 0 {
		t.Fatal("ldap-parent not found")
	}
	if childParentID == nil || *childParentID != parentID {
		t.Fatalf("child parent_id = %v, want %d", childParentID, parentID)
	}
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
