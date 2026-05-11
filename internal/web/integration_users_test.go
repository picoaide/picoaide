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
	for _, u := range result.Users {
		if u["role"] == "superadmin" {
			t.Fatalf("superadmin should not be listed in user management: %+v", u)
		}
	}
}

func TestAdminUsers_TotalExcludesSuperadmins(t *testing.T) {
	env := setupTestServer(t)
	resp := env.get(t, "/api/admin/users?runtime=false", "testadmin")
	assertStatus(t, resp, 200)
	var result struct {
		Success bool `json:"success"`
		Total   int  `json:"total"`
		Users   []struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"users"`
	}
	parseJSON(t, resp, &result)
	if !result.Success {
		t.Fatal("should succeed")
	}
	if result.Total != len(result.Users) {
		t.Fatalf("total = %d, len(users) = %d", result.Total, len(result.Users))
	}
	for _, u := range result.Users {
		if u.Role == "superadmin" {
			t.Fatalf("superadmin should not be counted in user management: %+v", u)
		}
	}
}

func TestAdminUsers_ForbiddenForRegularUser(t *testing.T) {
	env := setupTestServer(t)
	resp := env.get(t, "/api/admin/users", "testuser")
	assertStatus(t, resp, 403)
}

func TestDeletedUserSessionInvalidated(t *testing.T) {
	env := setupTestServer(t)
	if err := auth.DeleteUser("testuser"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	resp := env.get(t, "/api/user/info", "testuser")
	assertStatus(t, resp, 401)
}

func TestLocalUserSessionInvalidatedInLDAPMode(t *testing.T) {
	env := setupTestServer(t)
	env.Cfg.Web.AuthMode = "ldap"

	resp := env.get(t, "/api/user/info", "testuser")
	assertStatus(t, resp, 401)
}

func TestAdminUsers_LDAPModeUsesSyncedLocalUsers(t *testing.T) {
	env := setupTestServer(t)
	env.Cfg.Web.AuthMode = "ldap"
	env.Cfg.LDAP.Host = ""

	if err := auth.EnsureExternalUser("ldapuser", "user", "ldap"); err != nil {
		t.Fatalf("EnsureExternalUser: %v", err)
	}

	resp := env.get(t, "/api/admin/users", "testadmin")
	assertStatus(t, resp, 200)

	var result struct {
		Success bool `json:"success"`
		Users   []struct {
			Username string `json:"username"`
			Source   string `json:"source"`
			Status   string `json:"status"`
		} `json:"users"`
	}
	parseJSON(t, resp, &result)
	if !result.Success {
		t.Fatal("should succeed")
	}
	for _, u := range result.Users {
		if u.Username == "ldapuser" {
			if u.Source != "ldap" {
				t.Fatalf("ldapuser source = %q, want ldap", u.Source)
			}
			if u.Status != "未初始化" {
				t.Fatalf("ldapuser status = %q, want 未初始化", u.Status)
			}
			return
		}
	}
	t.Fatal("synced LDAP user not listed")
}

func TestAdminAuthLDAPUsers_UnifiedModeUsesSyncedLocalUsers(t *testing.T) {
	env := setupTestServer(t)
	env.Cfg.Web.AuthMode = "ldap"
	env.Cfg.LDAP.Host = ""

	if err := auth.EnsureExternalUser("ldapuser", "user", "ldap"); err != nil {
		t.Fatalf("EnsureExternalUser: %v", err)
	}

	resp := env.get(t, "/api/admin/auth/ldap-users", "testadmin")
	assertStatus(t, resp, 200)

	var result struct {
		Success bool     `json:"success"`
		Users   []string `json:"users"`
	}
	parseJSON(t, resp, &result)
	if !result.Success {
		t.Fatal("should succeed")
	}
	if len(result.Users) != 1 || result.Users[0] != "ldapuser" {
		t.Fatalf("users = %v, want [ldapuser]", result.Users)
	}
}

func TestAdminAuthLDAPUsers_DirectorySourceUsesLDAP(t *testing.T) {
	env := setupTestServer(t)
	env.Cfg.Web.AuthMode = "ldap"
	env.Cfg.LDAP.Host = ""

	resp := env.get(t, "/api/admin/auth/ldap-users?source=directory", "testadmin")
	if resp.StatusCode == 200 {
		t.Fatalf("status=200, want LDAP connection error when source=directory")
	}
}

func TestAdminUserCreate_LocalModeSuccess(t *testing.T) {
	env := setupTestServer(t)
	form := url.Values{
		"username":  {"newuser"},
		"image_tag": {"test-tag"},
	}
	resp := env.postForm(t, "/api/admin/users/create", "testadmin", form)
	assertStatus(t, resp, 200)
	var result struct {
		Success  bool   `json:"success"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	parseJSON(t, resp, &result)
	if !result.Success {
		t.Fatal("should succeed")
	}
	if result.Username != "newuser" {
		t.Fatalf("username = %q, want newuser", result.Username)
	}
	if result.Password == "" {
		t.Fatal("should return initial password")
	}
	if !auth.UserExists("newuser") {
		t.Fatal("newuser should exist")
	}
}

func TestAdminUserCreate_LocalModeInvalidUsername(t *testing.T) {
	env := setupTestServer(t)
	form := url.Values{"username": {"bad/user"}}
	resp := env.postForm(t, "/api/admin/users/create", "testadmin", form)
	assertStatus(t, resp, 400)
}

func TestAdminUserCreate_UnifiedModeForbidden(t *testing.T) {
	env := setupTestServer(t)
	env.Cfg.Web.AuthMode = "ldap"
	form := url.Values{"username": {"newuser"}}
	resp := env.postForm(t, "/api/admin/users/create", "testadmin", form)
	assertStatus(t, resp, 403)
}

func TestAdminUserBatchCreate_LocalModeMixedResults(t *testing.T) {
	env := setupTestServer(t)
	form := url.Values{
		"usernames": {"batch1\nbad/user\ntestuser\nbatch2"},
		"image_tag": {"test-tag"},
	}
	resp := env.postForm(t, "/api/admin/users/batch-create", "testadmin", form)
	assertStatus(t, resp, 200)
	var result struct {
		Success bool `json:"success"`
		Created int  `json:"created"`
		Failed  int  `json:"failed"`
		Results []struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Success  bool   `json:"success"`
			Error    string `json:"error"`
		} `json:"results"`
	}
	parseJSON(t, resp, &result)
	if result.Success {
		t.Fatal("batch should report partial failure")
	}
	if result.Created != 2 || result.Failed != 2 {
		t.Fatalf("created=%d failed=%d, want 2/2", result.Created, result.Failed)
	}
	if !auth.UserExists("batch1") || !auth.UserExists("batch2") {
		t.Fatal("successful batch users should exist")
	}
}

func TestAdminUserBatchCreate_UnifiedModeForbidden(t *testing.T) {
	env := setupTestServer(t)
	env.Cfg.Web.AuthMode = "ldap"
	form := url.Values{
		"usernames": {"batch1\nbatch2"},
		"image_tag": {"test-tag"},
	}
	resp := env.postForm(t, "/api/admin/users/batch-create", "testadmin", form)
	assertStatus(t, resp, 403)
}

func TestAdminUserDelete_LocalModeSuccess(t *testing.T) {
	env := setupTestServer(t)
	form := url.Values{"username": {"testuser"}}
	resp := env.postForm(t, "/api/admin/users/delete", "testadmin", form)
	assertStatus(t, resp, 200)
	if auth.UserExists("testuser") {
		t.Fatal("testuser should be deleted")
	}
}

func TestAdminUserDelete_LocalModeRejectsSuperadmin(t *testing.T) {
	env := setupTestServer(t)
	form := url.Values{"username": {"testadmin"}}
	resp := env.postForm(t, "/api/admin/users/delete", "testadmin", form)
	assertStatus(t, resp, 400)
	if !auth.IsSuperadmin("testadmin") {
		t.Fatal("testadmin should still exist")
	}
}

func TestAdminUserDelete_UnifiedModeForbidden(t *testing.T) {
	env := setupTestServer(t)
	env.Cfg.Web.AuthMode = "ldap"
	form := url.Values{"username": {"testuser"}}
	resp := env.postForm(t, "/api/admin/users/delete", "testadmin", form)
	assertStatus(t, resp, 403)
	if !auth.UserExists("testuser") {
		t.Fatal("testuser should not be deleted in unified mode")
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
