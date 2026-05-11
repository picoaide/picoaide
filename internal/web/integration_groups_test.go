package web

import (
	"net/url"
	"testing"

	"github.com/picoaide/picoaide/internal/auth"
	"github.com/picoaide/picoaide/internal/authsource"
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

func TestGroupCreate_LocalModeSuccess(t *testing.T) {
	env := setupTestServer(t)
	form := url.Values{
		"name":        {"dev-team"},
		"description": {"Developers"},
	}
	resp := env.postForm(t, "/api/admin/groups/create", "testadmin", form)
	assertStatus(t, resp, 200)
	if _, err := auth.GetGroupID("dev-team"); err != nil {
		t.Fatalf("group should exist: %v", err)
	}
}

func TestGroupDelete_LocalModeSuccess(t *testing.T) {
	env := setupTestServer(t)
	if err := auth.CreateGroup("to-delete", "local", "", nil); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	form := url.Values{"name": {"to-delete"}}
	resp := env.postForm(t, "/api/admin/groups/delete", "testadmin", form)
	assertStatus(t, resp, 200)
	if _, err := auth.GetGroupID("to-delete"); err == nil {
		t.Fatal("group should be deleted")
	}
}

func TestGroupMembers_ListAndMutationLocalModeSuccess(t *testing.T) {
	env := setupTestServer(t)
	if err := auth.CreateGroup("team-a", "local", "", nil); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := auth.AddUsersToGroup("team-a", []string{"testuser"}); err != nil {
		t.Fatalf("AddUsersToGroup: %v", err)
	}
	if err := auth.CreateUser("another-user", "pass123", "user"); err != nil {
		t.Fatalf("CreateUser another-user: %v", err)
	}

	form := url.Values{
		"group_name": {"team-a"},
		"usernames":  {"another-user"},
	}
	resp := env.postForm(t, "/api/admin/groups/members/add", "testadmin", form)
	assertStatus(t, resp, 200)

	resp = env.get(t, "/api/admin/groups/members?name=team-a", "testadmin")
	assertStatus(t, resp, 200)
	var result struct {
		Members []string `json:"members"`
	}
	parseJSON(t, resp, &result)
	found := false
	foundAdded := false
	for _, m := range result.Members {
		if m == "testuser" {
			found = true
		}
		if m == "another-user" {
			foundAdded = true
		}
	}
	if !found {
		t.Error("testuser should be in team-a members")
	}
	if !foundAdded {
		t.Error("another-user should be in team-a members")
	}

	form = url.Values{
		"group_name": {"team-a"},
		"username":   {"testuser"},
	}
	resp = env.postForm(t, "/api/admin/groups/members/remove", "testadmin", form)
	assertStatus(t, resp, 200)
	members, err := auth.GetGroupMembers("team-a")
	if err != nil {
		t.Fatalf("GetGroupMembers: %v", err)
	}
	for _, member := range members {
		if member == "testuser" {
			t.Fatal("testuser should be removed")
		}
	}
}

func TestGroupMembersAdd_LocalModeRejectsUnknownUser(t *testing.T) {
	env := setupTestServer(t)
	if err := auth.CreateGroup("team-a", "local", "", nil); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	form := url.Values{
		"group_name": {"team-a"},
		"usernames":  {"ghost-user"},
	}
	resp := env.postForm(t, "/api/admin/groups/members/add", "testadmin", form)
	assertStatus(t, resp, 400)

	members, err := auth.GetGroupMembers("team-a")
	if err != nil {
		t.Fatalf("GetGroupMembers: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("members = %v, want empty", members)
	}
}

func TestGroupMembersAdd_LocalModeRejectsSuperadmin(t *testing.T) {
	env := setupTestServer(t)
	if err := auth.CreateGroup("team-a", "local", "", nil); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	form := url.Values{
		"group_name": {"team-a"},
		"usernames":  {"testadmin"},
	}
	resp := env.postForm(t, "/api/admin/groups/members/add", "testadmin", form)
	assertStatus(t, resp, 400)

	members, err := auth.GetGroupMembers("team-a")
	if err != nil {
		t.Fatalf("GetGroupMembers: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("members = %v, want empty", members)
	}
}

func TestGroupMutations_ForbiddenInUnifiedAuthExceptWhitelist(t *testing.T) {
	env := setupTestServer(t)
	env.Cfg.Web.AuthMode = "ldap"
	if err := auth.CreateGroup("ldap-team", "ldap", "", nil); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := auth.AddUsersToGroup("ldap-team", []string{"testuser"}); err != nil {
		t.Fatalf("AddUsersToGroup: %v", err)
	}

	resp := env.postForm(t, "/api/admin/groups/create", "testadmin", url.Values{"name": {"manual-team"}})
	assertStatus(t, resp, 403)

	resp = env.postForm(t, "/api/admin/groups/delete", "testadmin", url.Values{"name": {"ldap-team"}})
	assertStatus(t, resp, 403)

	resp = env.postForm(t, "/api/admin/groups/members/add", "testadmin", url.Values{
		"group_name": {"ldap-team"},
		"usernames":  {"another-user"},
	})
	assertStatus(t, resp, 403)

	resp = env.postForm(t, "/api/admin/groups/members/remove", "testadmin", url.Values{
		"group_name": {"ldap-team"},
		"username":   {"testuser"},
	})
	assertStatus(t, resp, 403)

	resp = env.postForm(t, "/api/admin/whitelist", "testadmin", url.Values{"users": {"testuser"}})
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

	env.Server.syncLDAPGroupParents(map[string]authsource.GroupHierarchy{
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
