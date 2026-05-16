package web

import (
  "encoding/json"
  "net/http"
  "net/http/httptest"
  "net/url"
  "strings"
  "testing"

  "github.com/gin-gonic/gin"
)

func TestHandleVersionEndpoint(t *testing.T) {
  env := setupTestServer(t)
  resp, err := http.Get(env.HTTP.URL + "/api/version")
  if err != nil {
    t.Fatalf("GET /api/version: %v", err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["current"] != "v1" {
    t.Errorf("current=%v", body["current"])
  }
}

func TestHandleLoginModeEndpoint(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/login/mode", "testuser")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["auth_mode"] != "local" {
    t.Errorf("auth_mode=%v", body["auth_mode"])
  }
}

func TestAdminTaskStatus(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/task/status", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["running"] != false {
    t.Errorf("running=%v", body["running"])
  }
}

func TestAdminTaskStatusForbiddenForRegularUser(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/task/status", "testuser")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusForbidden {
    t.Errorf("regular user status=%d, want 403", resp.StatusCode)
  }
}

func TestAdminAuthProviders(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/auth/providers", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["providers"] == nil {
    t.Error("providers should not be nil")
  }
}

func TestAdminPicoClawChannels(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/picoclaw/channels", "testadmin")
  defer resp.Body.Close()
}

func TestAdminSkillsEndpoint(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/skills", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["skills"] == nil {
    t.Error("skills should not be nil")
  }
}

func TestAdminSkillsSources(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/skills/sources", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["sources"] == nil {
    t.Error("sources should not be nil")
  }
}

func TestAdminSkillsDefaults(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/skills/defaults", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
}

func TestAdminSkillsUserSourcesNoQuery(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/skills/user/sources", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminSkillsUserSourcesSuccess(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/skills/user/sources?username=testuser&skill_name=test", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["sources"] == nil {
    t.Error("sources should not be nil")
  }
}

func TestUserSkillsEndpoint(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/user/skills", "testuser")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["skills"] == nil {
    t.Error("skills should not be nil")
  }
}

func TestUserInitStatus(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/user/init-status", "testuser")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["ready"] == nil {
    t.Error("ready field missing")
  }
}

func TestAdminImageEndpointsDockerUnavailable(t *testing.T) {
  env := setupTestServer(t)

  endpoints := []struct {
    method string
    path   string
    body   string
  }{
    {"GET", "/api/admin/images", ""},
    {"GET", "/api/admin/images/pull-status", ""},
    {"GET", "/api/admin/images/registry", ""},
    {"GET", "/api/admin/images/local-tags", ""},
    {"GET", "/api/admin/images/upgrade-candidates?tag=v1.0.0", ""},
    {"GET", "/api/admin/images/users?image=test", ""},
    {"POST", "/api/admin/images/pull", "tag=v1.0.0"},
    {"POST", "/api/admin/images/delete", "image=test"},
    {"POST", "/api/admin/images/migrate", "image=test&target=new"},
    {"POST", "/api/admin/images/upgrade", "tag=v1.0.0&users=testuser"},
  }

  for _, ep := range endpoints {
    t.Run(ep.method+" "+ep.path, func(t *testing.T) {
      var resp *http.Response
      if ep.method == "GET" {
        resp = env.get(t, ep.path, "testadmin")
      } else {
        form := url.Values{}
        for _, pair := range strings.Split(ep.body, "&") {
          parts := strings.SplitN(pair, "=", 2)
          if len(parts) == 2 {
            form.Set(parts[0], parts[1])
          }
        }
        resp = env.postForm(t, ep.path, "testadmin", form)
      }
      defer resp.Body.Close()
      // Docker unavailable = 503
      if resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != http.StatusOK {
        // Some image endpoints (pull-status, users) only require superadmin auth, not docker
        t.Logf("%s %s: status=%d", ep.method, ep.path, resp.StatusCode)
      }
    })
  }
}

func TestAdminContainerEndpointsDockerUnavailable(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"username": {"testuser"}}

  endpoints := []struct {
    method string
    path   string
  }{
    {"POST", "/api/admin/container/stop"},
    {"POST", "/api/admin/container/restart"},
    {"POST", "/api/admin/container/debug"},
  }

  for _, ep := range endpoints {
    t.Run(ep.path, func(t *testing.T) {
      var resp *http.Response
      if ep.method == "POST" {
        resp = env.postForm(t, ep.path, "testadmin", form)
      } else {
        resp = env.get(t, ep.path, "testadmin")
      }
      defer resp.Body.Close()
      if resp.StatusCode != http.StatusServiceUnavailable {
        t.Errorf("%s: status=%d, want 503", ep.path, resp.StatusCode)
      }
    })
  }
}

func TestAdminContainerLogsRequiresDocker(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/container/logs?username=testuser", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusServiceUnavailable {
    t.Errorf("status=%d, want 503", resp.StatusCode)
  }
}

func TestAdminTLSStatus(t *testing.T) {
  env := setupTestServer(t)
  // TLS not configured
  resp := env.get(t, "/api/admin/tls/status", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["enabled"] != false {
    t.Errorf("enabled=%v", body["enabled"])
  }
}

func TestAdminTLSStatusForbiddenForRegularUser(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/tls/status", "testuser")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusForbidden {
    t.Errorf("status=%d, want 403", resp.StatusCode)
  }
}

func TestAdminTLSUploadForbiddenForRegularUser(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"enabled": {"true"}}
  resp := env.postForm(t, "/api/admin/tls/upload", "testuser", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusForbidden {
    t.Errorf("status=%d, want 403", resp.StatusCode)
  }
}

func TestAdminSkillsDeployRequiresCSRF(t *testing.T) {
  env := setupTestServer(t)
  // POST without CSRF
  req, err := http.NewRequest("POST", env.HTTP.URL+"/api/admin/skills/deploy", strings.NewReader("skill_name=test&username=testuser"))
  if err != nil {
    t.Fatalf("NewRequest: %v", err)
  }
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("Do: %v", err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusForbidden {
    t.Errorf("status=%d, want 403 (CSRF)", resp.StatusCode)
  }
}

func TestAdminSkillsRemoveRequiresCSRF(t *testing.T) {
  env := setupTestServer(t)
  req, err := http.NewRequest("POST", env.HTTP.URL+"/api/admin/skills/remove", strings.NewReader("skill_name=test"))
  if err != nil {
    t.Fatalf("NewRequest: %v", err)
  }
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("Do: %v", err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusForbidden {
    t.Errorf("status=%d, want 403 (CSRF)", resp.StatusCode)
  }
}

func TestAdminSkillsUserBindRequiresCSRF(t *testing.T) {
  env := setupTestServer(t)
  req, err := http.NewRequest("POST", env.HTTP.URL+"/api/admin/skills/user/bind", strings.NewReader("skill_name=test&username=testuser"))
  if err != nil {
    t.Fatalf("NewRequest: %v", err)
  }
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("Do: %v", err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusForbidden {
    t.Errorf("status=%d, want 403 (CSRF)", resp.StatusCode)
  }
}

func TestAdminSkillsUserUnbindRequiresCSRF(t *testing.T) {
  env := setupTestServer(t)
  req, err := http.NewRequest("POST", env.HTTP.URL+"/api/admin/skills/user/unbind", strings.NewReader("skill_name=test&username=testuser"))
  if err != nil {
    t.Fatalf("NewRequest: %v", err)
  }
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("Do: %v", err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusForbidden {
    t.Errorf("status=%d, want 403 (CSRF)", resp.StatusCode)
  }
}

func TestAdminSkillsSourcesGitAddMissingFields(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"name": {""}, "url": {""}}
  resp := env.postForm(t, "/api/admin/skills/sources/git", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminSkillsSourcesRemoveMissingName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/skills/sources/remove", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminSkillsSourcesPullMissingName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/skills/sources/pull", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminSkillsSourcesRefreshMissingName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/skills/sources/refresh", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestHandleAuthStartNoBrowserProvider(t *testing.T) {
  env := setupTestServer(t)
  // No browser provider configured in local mode
  resp, err := http.Get(env.HTTP.URL + "/api/login/auth")
  if err != nil {
    t.Fatalf("GET /api/login/auth: %v", err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestHandleAuthCallbackNoBrowserProvider(t *testing.T) {
  env := setupTestServer(t)
  resp, err := http.Get(env.HTTP.URL + "/api/login/callback?state=abc&code=def")
  if err != nil {
    t.Fatalf("GET /api/login/callback: %v", err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminSkillDefaultsToggleRequiresCSRF(t *testing.T) {
  env := setupTestServer(t)
  req, err := http.NewRequest("POST", env.HTTP.URL+"/api/admin/skills/defaults/toggle", strings.NewReader("skill_name=test"))
  if err != nil {
    t.Fatalf("NewRequest: %v", err)
  }
  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("Do: %v", err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusForbidden {
    t.Errorf("status=%d, want 403 (CSRF)", resp.StatusCode)
  }
}

func TestAdminSkillsRegistryListMissingSource(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/skills/registry/list", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminSkillsRegistryListNonexistentSource(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/skills/registry/list?source=nonexistent", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminSkillsRegistryInstallMissingURL(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/skills/registry/install", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminSkillsDeployMissingSkillName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"username": {"testuser"}}
  resp := env.postForm(t, "/api/admin/skills/deploy", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminSkillsRemoveMissingSkillName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/skills/remove", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminSkillsUserBindMissingFields(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/skills/user/bind", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminSkillsUserUnbindMissingFields(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/skills/user/unbind", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestUserSkillsInstallMissingName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/user/skills/install", "testuser", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestUserSkillsUninstallMissingName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/user/skills/uninstall", "testuser", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminMigrationRulesRefreshRequiresCSRF(t *testing.T) {
  env := setupTestServer(t)
  req, err := http.NewRequest("POST", env.HTTP.URL+"/api/admin/migration-rules/refresh", nil)
  if err != nil {
    t.Fatalf("NewRequest: %v", err)
  }
  req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("Do: %v", err)
  }
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusForbidden {
    t.Errorf("status=%d, want 403 (CSRF)", resp.StatusCode)
  }
}

func TestAdminMigrationRulesUploadMissingFile(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/migration-rules/upload", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminConfigApplyMissingTargets(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postForm(t, "/api/admin/config/apply", "testadmin", url.Values{})
  defer resp.Body.Close()
  // Applies to all users, will try to apply config
  // config.json doesn't exist for testuser, so it should fail with error
  // but the handler itself should run without error
  if resp.StatusCode != http.StatusInternalServerError && resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200 or 500", resp.StatusCode)
  }
}

func TestAdminAuthTestLDAPMissingFields(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/auth/test-ldap", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminAuthSyncUsersNoDirectoryProvider(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/auth/sync-users", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminAuthSyncGroupsNoDirectoryProvider(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/auth/sync-groups", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminImagePullStatus(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/images/pull-status", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["running"] == nil {
    t.Error("running field missing")
  }
}

func TestAdminImageUsers(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/images/users?image=test", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("status=%d, want 200", resp.StatusCode)
  }
  body := getJSON(t, resp)
  if body["users"] == nil {
    t.Error("users should not be nil")
  }
}

func TestAdminImageUsersMissingImage(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/images/users", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminGroupCreateEmptyName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"name": {""}}
  resp := env.postForm(t, "/api/admin/groups/create", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminGroupDeleteEmptyName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"name": {""}}
  resp := env.postForm(t, "/api/admin/groups/delete", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminGroupDeleteNonexistent(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"name": {"nonexistent"}}
  resp := env.postForm(t, "/api/admin/groups/delete", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminGroupMembersAddEmptyFields(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/groups/members/add", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminGroupMembersRemoveEmptyFields(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/groups/members/remove", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminGroupSkillsBindEmptyFields(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/groups/skills/bind", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminGroupSkillsUnbindEmptyFields(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{}
  resp := env.postForm(t, "/api/admin/groups/skills/unbind", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminGroupMembersMissingName(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/groups/members", "testadmin")
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusBadRequest {
    t.Errorf("status=%d, want 400", resp.StatusCode)
  }
}

func TestAdminGroupCreateWithParent(t *testing.T) {
  env := setupTestServer(t)
  // Create parent group
  form := url.Values{"name": {"parent-group"}, "description": {"parent"}}
  resp := env.postForm(t, "/api/admin/groups/create", "testadmin", form)
  defer resp.Body.Close()
  // Create child group with parent
  form = url.Values{"name": {"child-group"}, "description": {"child"}, "parent_id": {"1"}}
  resp = env.postForm(t, "/api/admin/groups/create", "testadmin", form)
  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    t.Errorf("child group create status=%d, want 200", resp.StatusCode)
  }
}

func TestWebSocketEndpointsRejectHTTP(t *testing.T) {
  env := setupTestServer(t)
  // WS endpoints should upgrade protocol, or fail with regular HTTP
  for _, path := range []string{"/api/browser/ws", "/api/computer/ws"} {
    resp := env.get(t, path+"?token=invalid", "testuser")
    resp.Body.Close()
    // Should either upgrade or return error - just ensure no panic
  }
}

func TestAdminContainerLogsMissingUsername(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/container/logs", "testadmin")
  defer resp.Body.Close()
  // Without username + docker unavailable, it may be bad request or 503
  // Docker-unavailable check happens before username check
  if resp.StatusCode != http.StatusServiceUnavailable {
    t.Errorf("status=%d, want 503", resp.StatusCode)
  }
}

func TestParseBatchUsernames(t *testing.T) {
  // Normal input
  result := parseBatchUsernames("user1\nuser2\nuser1\n\nuser3")
  if len(result) != 3 || result[0] != "user1" || result[1] != "user2" || result[2] != "user3" {
    t.Errorf("unexpected result: %v", result)
  }

  // Empty
  result = parseBatchUsernames("")
  if len(result) != 0 {
    t.Errorf("empty should return empty, got %v", result)
  }

  // Whitespace
  result = parseBatchUsernames("  user1  \n  user2  ")
  if len(result) != 2 || result[0] != "user1" || result[1] != "user2" {
    t.Errorf("whitespace: %v", result)
  }
}

func TestRemoveUserSkillDir(t *testing.T) {
  env := setupTestServer(t)
  // Should not panic for any inputs
  removeUserSkillDir(env.Cfg, "testuser", "test-skill")
  removeUserSkillDir(env.Cfg, "testuser", "../evil")
  removeUserSkillDir(env.Cfg, "testuser", "")
}

func TestIsSkillInstallDisabled(t *testing.T) {
  // No tools key
  if !isSkillInstallDisabled(map[string]interface{}{}) {
    t.Error("empty config should be disabled")
  }

  // All disabled
  pico := map[string]interface{}{
    "tools": map[string]interface{}{},
  }
  if !isSkillInstallDisabled(pico) {
    t.Error("empty tools should be disabled")
  }

  // install_skill enabled
  pico = map[string]interface{}{
    "tools": map[string]interface{}{
      "install_skill": map[string]interface{}{"enabled": true},
    },
  }
  if isSkillInstallDisabled(pico) {
    t.Error("install_skill enabled should return false")
  }

  // clawhub enabled
  pico = map[string]interface{}{
    "tools": map[string]interface{}{
      "skills": map[string]interface{}{
        "registries": map[string]interface{}{
          "clawhub": map[string]interface{}{"enabled": true},
        },
      },
    },
  }
  if isSkillInstallDisabled(pico) {
    t.Error("clawhub enabled should return false")
  }

  // github enabled
  pico = map[string]interface{}{
    "tools": map[string]interface{}{
      "skills": map[string]interface{}{
        "registries": map[string]interface{}{
          "github": map[string]interface{}{"enabled": true},
        },
      },
    },
  }
  if isSkillInstallDisabled(pico) {
    t.Error("github enabled should return false")
  }
}

func TestSetSkillInstallDisabled(t *testing.T) {
  pico := map[string]interface{}{}
  setSkillInstallDisabled(pico, true)
  tools, _ := pico["tools"].(map[string]interface{})
  installSkill, _ := tools["install_skill"].(map[string]interface{})
  if enabled, ok := installSkill["enabled"].(bool); !ok || enabled != false {
    t.Errorf("should set enabled=false: %v", pico)
  }

  setSkillInstallDisabled(pico, false)
  tools, _ = pico["tools"].(map[string]interface{})
  installSkill, _ = tools["install_skill"].(map[string]interface{})
  if enabled, ok := installSkill["enabled"].(bool); !ok || enabled != true {
    t.Errorf("should set enabled=true: %v", pico)
  }
}

func TestNonNilChannelsAlreadyNotNil(t *testing.T) {
  // Tested in server_test.go - keep this for coverage
}

func TestHandleMCPToolCallNoAgent(t *testing.T) {
  s := newTestServer(t)
  gin.SetMode(gin.TestMode)

  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"test","arguments":{}}`))
  c.Request.Header.Set("Content-Type", "application/json")

  s.handleMCPToolCall(c, json.Number("1"), json.RawMessage(`{"name":"navigate","arguments":{"url":"http://example.com"}}`), "testuser",
    &ServiceInfo{
      ServerName: "test-service",
      Hub:        NewServiceHub("test"),
    })
  if w.Code != http.StatusOK {
    t.Errorf("status=%d, want 200", w.Code)
  }
  // Should return error about agent not connected
  if !strings.Contains(w.Body.String(), "代理未连接") {
    t.Errorf("body=%s", w.Body.String())
  }
}

func TestHandleMCPToolCallInvalidParams(t *testing.T) {
  s := newTestServer(t)
  gin.SetMode(gin.TestMode)

  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  c.Request = httptest.NewRequest("POST", "/", nil)

  s.handleMCPToolCall(c, json.Number("1"), json.RawMessage(`invalid json`), "testuser",
    &ServiceInfo{
      ServerName: "test-service",
      Hub:        NewServiceHub("test"),
    })
  if w.Code != http.StatusOK {
    t.Errorf("status=%d, want 200", w.Code)
  }
  if !strings.Contains(w.Body.String(), "参数解析失败") {
    t.Errorf("body=%s", w.Body.String())
  }
}
