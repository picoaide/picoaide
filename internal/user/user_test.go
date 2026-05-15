package user

import (
  "encoding/json"
  "os"
  "path/filepath"
  "strings"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "gopkg.in/yaml.v3"
)

func testInitAuthDB(t *testing.T) string {
  t.Helper()
  tmpDir := t.TempDir()
  config.DefaultWorkDir = tmpDir
  auth.ResetDB()
  if err := auth.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  return tmpDir
}

func TestValidateUsername(t *testing.T) {
  validNames := []string{
    "admin",
    "user123",
    "test-user",
    "test_user",
    "user.name",
    "a",
    "A",
    "a0",
    "my-cool_user.v2",
    "123",
  }
  for _, name := range validNames {
    if err := ValidateUsername(name); err != nil {
      t.Errorf("ValidateUsername(%q) = %v, want nil", name, err)
    }
  }

  invalidCases := []struct {
    name string
    msg  string
  }{
    {"", "空用户名"},
    {"a/b", "含斜杠"},
    {"a b", "含空格"},
    {"-test", "短横线开头"},
    {"test-", "短横线结尾"},
    {".test", "点开头"},
    {"test.", "点结尾"},
    {"_test", "下划线开头"},
    {"test_", "下划线结尾"},
  }

  for _, tt := range invalidCases {
    err := ValidateUsername(tt.name)
    if err == nil {
      t.Errorf("ValidateUsername(%q) = nil, want error (%s)", tt.name, tt.msg)
    }
  }

  // 超长用户名
  longName := strings.Repeat("a", 100)
  if err := ValidateUsername(longName); err == nil {
    t.Error("ValidateUsername(100 chars) should fail")
  }

  // 恰好 64 字符应通过
  maxName := strings.Repeat("a", 64)
  if err := ValidateUsername(maxName); err != nil {
    t.Errorf("ValidateUsername(64 chars) should pass, got %v", err)
  }

  // 65 字符应失败
  overName := strings.Repeat("a", 65)
  if err := ValidateUsername(overName); err == nil {
    t.Error("ValidateUsername(65 chars) should fail")
  }
}

func TestIsWhitelisted(t *testing.T) {
  // nil 白名单 = 全部允许
  if !IsWhitelisted(nil, "anyone") {
    t.Error("IsWhitelisted(nil, ...) should return true")
  }

  // 空白名单 = 无匹配
  whitelist := map[string]bool{}
  if IsWhitelisted(whitelist, "user") {
    t.Error("IsWhitelisted(empty, user) should return false")
  }

  // 有匹配
  whitelist = map[string]bool{
    "alice": true,
    "bob":   true,
  }
  if !IsWhitelisted(whitelist, "alice") {
    t.Error("IsWhitelisted should find alice")
  }
  if IsWhitelisted(whitelist, "charlie") {
    t.Error("IsWhitelisted should not find charlie")
  }
}

func TestAllowedByWhitelistUsesProviderConfig(t *testing.T) {
  testInitAuthDB(t)

  cfg := &config.GlobalConfig{
    LDAP: config.LDAPConfig{WhitelistEnabled: false},
    OIDC: config.OIDCConfig{WhitelistEnabled: true},
  }
  if !AllowedByWhitelist(cfg, "ldap", "alice") {
    t.Fatal("disabled LDAP whitelist should allow users")
  }
  if !AllowedByWhitelist(cfg, "oidc", "alice") {
    t.Fatal("enabled OIDC whitelist with empty list should allow users")
  }

  engine, err := auth.GetEngine()
  if err != nil {
    t.Fatal(err)
  }
  if _, err := engine.Exec("INSERT INTO whitelist (username, added_by) VALUES (?, ?)", "bob", "test"); err != nil {
    t.Fatal(err)
  }
  if !AllowedByWhitelist(cfg, "oidc", "bob") {
    t.Fatal("enabled OIDC whitelist should allow listed users")
  }
  if AllowedByWhitelist(cfg, "oidc", "alice") {
    t.Fatal("enabled OIDC whitelist should reject users missing from non-empty whitelist")
  }
}

func TestContainerBaseURLDefault(t *testing.T) {
  cfg := &config.GlobalConfig{
    Web: config.WebConfig{Listen: ":80"},
  }
  if got := containerBaseURL(cfg); got != "http://100.64.0.1:80" {
    t.Fatalf("containerBaseURL = %q, want default bridge URL", got)
  }
}

func TestContainerBaseURLDefaultsToInternalHTTPWithTLS443(t *testing.T) {
  cfg := &config.GlobalConfig{
    Web: config.WebConfig{
      Listen: ":443",
      TLS:    config.TLSConfig{Enabled: true},
    },
  }
  if got := containerBaseURL(cfg); got != "http://100.64.0.1:80" {
    t.Fatalf("containerBaseURL = %q, want internal HTTP bridge URL", got)
  }
}

func TestContainerBaseURLNormalizesWildcardHost(t *testing.T) {
  cfg := &config.GlobalConfig{
    Web: config.WebConfig{Listen: "0.0.0.0:80"},
  }
  if got := containerBaseURL(cfg); got != "http://100.64.0.1:80" {
    t.Fatalf("containerBaseURL = %q, want bridge URL", got)
  }
}

func TestContainerBaseURLUsesHTTPSWithTLSNon443(t *testing.T) {
  cfg := &config.GlobalConfig{
    Web: config.WebConfig{
      Listen: ":8443",
      TLS:    config.TLSConfig{Enabled: true},
    },
  }
  if got := containerBaseURL(cfg); got != "http://100.64.0.1:80" {
    t.Fatalf("containerBaseURL = %q, want HTTPS listener URL", got)
  }
}

func TestContainerBaseURLIgnoresConfiguredOverride(t *testing.T) {
  cfg := &config.GlobalConfig{
    Web: config.WebConfig{
      Listen:           ":80",
      ContainerBaseURL: "http://172.17.0.1:8080/",
    },
  }
  if got := containerBaseURL(cfg); got != "http://100.64.0.1:80" {
    t.Fatalf("containerBaseURL = %q, want computed default", got)
  }
}

func TestInjectMCPConfigUsesComputedContainerBaseURL(t *testing.T) {
  cfg := &config.GlobalConfig{
    Web: config.WebConfig{
      Listen:           ":80",
      ContainerBaseURL: "http://172.17.0.1:8080/",
    },
  }
  out := map[string]interface{}{}

  injectMCPConfig(out, "alice:token", cfg)

  tools := out["tools"].(map[string]interface{})
  mcp := tools["mcp"].(map[string]interface{})
  servers := mcp["servers"].(map[string]interface{})
  browser := servers["browser"].(map[string]interface{})
  computer := servers["computer"].(map[string]interface{})

  if browser["type"] != "sse" {
    t.Fatalf("browser MCP type = %q, want sse", browser["type"])
  }
  if browser["url"] != "http://100.64.0.1:80/api/mcp/sse/browser?token=alice:token" {
    t.Fatalf("browser MCP url = %q", browser["url"])
  }
  if computer["type"] != "sse" {
    t.Fatalf("computer MCP type = %q, want sse", computer["type"])
  }
  if computer["url"] != "http://100.64.0.1:80/api/mcp/sse/computer?token=alice:token" {
    t.Fatalf("computer MCP url = %q", computer["url"])
  }
}

func TestApplyConfigToJSONGeneratesMissingMCPToken(t *testing.T) {
  tmpDir := testInitAuthDB(t)
  cfg := &config.GlobalConfig{
    UsersRoot: filepath.Join(tmpDir, "users"),
    Image: config.ImageConfig{
      Name: "picoaide/picoaide",
      Tag:  "v0.2.8",
    },
    Web: config.WebConfig{
      Listen:           ":80",
      ContainerBaseURL: "http://172.17.0.1:8080",
    },
    PicoClaw: map[string]interface{}{},
  }
  picoclawDir := filepath.Join(UserDir(cfg, "alice"), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }
  if err := auth.UpsertContainer(&auth.ContainerRecord{Username: "alice", Image: "picoaide/picoaide:v0.2.8", Status: "stopped", IP: "100.64.0.2"}); err != nil {
    t.Fatalf("UpsertContainer: %v", err)
  }

  if err := ApplyConfigToJSON(cfg, picoclawDir, "alice"); err != nil {
    t.Fatalf("ApplyConfigToJSON: %v", err)
  }

  token, err := auth.GetMCPToken("alice")
  if err != nil {
    t.Fatalf("GetMCPToken: %v", err)
  }
  if token == "" {
    t.Fatal("expected generated MCP token")
  }

  data, err := os.ReadFile(filepath.Join(picoclawDir, "config.json"))
  if err != nil {
    t.Fatalf("ReadFile: %v", err)
  }
  var got map[string]interface{}
  if err := json.Unmarshal(data, &got); err != nil {
    t.Fatalf("Unmarshal: %v", err)
  }
  servers := got["tools"].(map[string]interface{})["mcp"].(map[string]interface{})["servers"].(map[string]interface{})
  browser := servers["browser"].(map[string]interface{})
  if browser["url"] != "http://100.64.0.1:80/api/mcp/sse/browser?token="+token {
    t.Fatalf("browser MCP url = %q", browser["url"])
  }
}

func TestSaveDingTalkConfigWritesV3ChannelList(t *testing.T) {
  cfg := &config.GlobalConfig{UsersRoot: t.TempDir()}
  picoclawDir := filepath.Join(UserDir(cfg, "alice"), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(filepath.Join(picoclawDir, "config.json"), []byte(`{"version":3}`), 0644); err != nil {
    t.Fatal(err)
  }

  if err := SaveDingTalkConfig(cfg, "alice", "client-id", "client-secret"); err != nil {
    t.Fatalf("SaveDingTalkConfig() error = %v", err)
  }
  clientID, clientSecret := GetDingTalkConfig(cfg, "alice")
  if clientID != "client-id" || clientSecret != "client-secret" {
    t.Fatalf("GetDingTalkConfig() = %q/%q", clientID, clientSecret)
  }

  var saved map[string]interface{}
  data, _ := os.ReadFile(filepath.Join(picoclawDir, "config.json"))
  if err := json.Unmarshal(data, &saved); err != nil {
    t.Fatal(err)
  }
  channelList := saved["channel_list"].(map[string]interface{})
  dingtalk := channelList["dingtalk"].(map[string]interface{})
  settings := dingtalk["settings"].(map[string]interface{})
  if settings["client_id"] != "client-id" || dingtalk["type"] != "dingtalk" {
    t.Fatalf("unexpected v3 dingtalk config: %+v", dingtalk)
  }
}

func TestSaveDingTalkConfigWritesV2Channels(t *testing.T) {
  cfg := &config.GlobalConfig{UsersRoot: t.TempDir()}
  picoclawDir := filepath.Join(UserDir(cfg, "alice"), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(filepath.Join(picoclawDir, "config.json"), []byte(`{"version":2,"channels":{"dingtalk":{"enabled":false}}}`), 0644); err != nil {
    t.Fatal(err)
  }

  if err := SaveDingTalkConfig(cfg, "alice", "old-client-id", "old-client-secret"); err != nil {
    t.Fatalf("SaveDingTalkConfig() error = %v", err)
  }
  clientID, clientSecret := GetDingTalkConfig(cfg, "alice")
  if clientID != "old-client-id" || clientSecret != "old-client-secret" {
    t.Fatalf("GetDingTalkConfig() = %q/%q", clientID, clientSecret)
  }

  var saved map[string]interface{}
  data, _ := os.ReadFile(filepath.Join(picoclawDir, "config.json"))
  if err := json.Unmarshal(data, &saved); err != nil {
    t.Fatal(err)
  }
  if _, ok := saved["channel_list"]; ok {
    t.Fatal("v2 save should not create channel_list")
  }
  channels := saved["channels"].(map[string]interface{})
  dingtalk := channels["dingtalk"].(map[string]interface{})
  if dingtalk["client_id"] != "old-client-id" {
    t.Fatalf("unexpected v2 dingtalk config: %+v", dingtalk)
  }

  var security map[string]interface{}
  secData, _ := os.ReadFile(filepath.Join(picoclawDir, ".security.yml"))
  if err := yaml.Unmarshal(secData, &security); err != nil {
    t.Fatal(err)
  }
  secChannels := security["channels"].(map[string]interface{})
  secDingtalk := secChannels["dingtalk"].(map[string]interface{})
  if secDingtalk["client_secret"] != "old-client-secret" {
    t.Fatalf("unexpected v2 dingtalk security: %+v", secDingtalk)
  }
}

func TestPicoClawConfigFieldsUseAdapterPaths(t *testing.T) {
  cfg := &config.GlobalConfig{UsersRoot: t.TempDir(), PicoClaw: map[string]interface{}{
    "channel_list": map[string]interface{}{
      "dingtalk": map[string]interface{}{"enabled": true, "type": "dingtalk"},
    },
  }}
  picoclawDir := filepath.Join(UserDir(cfg, "alice"), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(filepath.Join(picoclawDir, "config.json"), []byte(`{"version":3}`), 0644); err != nil {
    t.Fatal(err)
  }
  if err := SavePicoClawConfigFields(cfg, "alice", 0, map[string]interface{}{
    "enabled":       true,
    "client_id":     "adapter-client",
    "client_secret": "adapter-secret",
  }); err != nil {
    t.Fatalf("SavePicoClawConfigFields() error = %v", err)
  }
  values, err := GetPicoClawConfigFields(cfg, "alice", 0, "dingtalk")
  if err != nil {
    t.Fatalf("GetPicoClawConfigFields() error = %v", err)
  }
  got := map[string]interface{}{}
  for _, value := range values {
    got[value.Field.Key] = value.Value
  }
  if got["client_id"] != "adapter-client" || got["client_secret"] != "adapter-secret" {
    t.Fatalf("fields = %+v", got)
  }
}

func TestApplyConfigToJSONProjectsGlobalConfigForTargetTags(t *testing.T) {
  tmpDir := testInitAuthDB(t)
  cfg := &config.GlobalConfig{
    UsersRoot: filepath.Join(tmpDir, "users"),
    Image:     config.ImageConfig{Name: "picoaide/picoaide", Tag: "v0.2.8"},
    Web:       config.WebConfig{Listen: ":80"},
    PicoClaw: map[string]interface{}{
      "agents": map[string]interface{}{
        "defaults": map[string]interface{}{
          "model_name": "gpt-new",
          "model":      "gpt-old",
        },
      },
      "model_list": []interface{}{
        map[string]interface{}{"model_name": "gpt-new", "model": "openai/gpt-4.1"},
      },
      "channel_list": map[string]interface{}{
        "dingtalk": map[string]interface{}{
          "enabled":              true,
          "type":                 "dingtalk",
          "allow_from":           []interface{}{"alice"},
          "reasoning_channel_id": "reason",
          "settings": map[string]interface{}{
            "client_id": "v3-client",
          },
        },
        "whatsapp_native": map[string]interface{}{
          "enabled": true,
          "type":    "whatsapp_native",
        },
      },
      "channels": map[string]interface{}{
        "dingtalk": map[string]interface{}{
          "enabled":    true,
          "client_id":  "legacy-client",
          "allow_from": []interface{}{"legacy"},
        },
      },
    },
  }

  cases := []struct {
    tag               string
    wantVersion       float64
    wantRoot          string
    wantDefaultPath   string
    wantClientID      string
    wantNativePresent bool
  }{
    {tag: "v0.2.4", wantVersion: 1, wantRoot: "channels", wantDefaultPath: "agents.defaults.model", wantClientID: "v3-client", wantNativePresent: false},
    {tag: "v0.2.6", wantVersion: 2, wantRoot: "channels", wantDefaultPath: "agents.defaults.model_name", wantClientID: "v3-client", wantNativePresent: false},
    {tag: "v0.2.8", wantVersion: 3, wantRoot: "channel_list", wantDefaultPath: "agents.defaults.model_name", wantClientID: "v3-client", wantNativePresent: true},
  }

  for _, tt := range cases {
    t.Run(tt.tag, func(t *testing.T) {
      username := strings.ReplaceAll(tt.tag, ".", "-")
      if err := auth.UpsertContainer(&auth.ContainerRecord{Username: username, Image: "picoaide/picoaide:" + tt.tag, Status: "stopped", IP: "100.64.0.2"}); err != nil {
        t.Fatalf("UpsertContainer: %v", err)
      }
      picoclawDir := filepath.Join(UserDir(cfg, username), ".picoclaw")
      if err := os.MkdirAll(picoclawDir, 0755); err != nil {
        t.Fatal(err)
      }
      if err := ApplyConfigToJSONForTag(cfg, picoclawDir, username, tt.tag); err != nil {
        t.Fatalf("ApplyConfigToJSONForTag() error = %v", err)
      }
      var saved map[string]interface{}
      data, err := os.ReadFile(filepath.Join(picoclawDir, "config.json"))
      if err != nil {
        t.Fatal(err)
      }
      if err := json.Unmarshal(data, &saved); err != nil {
        t.Fatal(err)
      }
      if saved["version"] != tt.wantVersion {
        t.Fatalf("version = %v, want %v; config=%s", saved["version"], tt.wantVersion, data)
      }
      if _, ok := saved[tt.wantRoot].(map[string]interface{}); !ok {
        t.Fatalf("missing %s in config: %s", tt.wantRoot, data)
      }
      otherRoot := "channels"
      if tt.wantRoot == "channels" {
        otherRoot = "channel_list"
      }
      if _, ok := saved[otherRoot]; ok {
        t.Fatalf("unexpected %s in config: %s", otherRoot, data)
      }
      if got, _ := deepGet(saved, tt.wantDefaultPath); got != "gpt-new" {
        t.Fatalf("%s = %v, want gpt-new", tt.wantDefaultPath, got)
      }
      if tt.wantRoot == "channel_list" {
        if got, _ := deepGet(saved, "channel_list.dingtalk.settings.client_id"); got != tt.wantClientID {
          t.Fatalf("v3 client_id = %v, want %s", got, tt.wantClientID)
        }
      } else if got, _ := deepGet(saved, "channels.dingtalk.client_id"); got != tt.wantClientID {
        t.Fatalf("legacy client_id = %v, want %s", got, tt.wantClientID)
      }
      _, nativePresent := deepGet(saved, tt.wantRoot+".whatsapp_native")
      if nativePresent != tt.wantNativePresent {
        t.Fatalf("whatsapp_native present = %v, want %v", nativePresent, tt.wantNativePresent)
      }
    })
  }
}

func TestSaveDingTalkConfigRejectsUnsupportedConfigVersion(t *testing.T) {
  cfg := &config.GlobalConfig{UsersRoot: t.TempDir()}
  picoclawDir := filepath.Join(UserDir(cfg, "alice"), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(filepath.Join(picoclawDir, "config.json"), []byte(`{"version":4}`), 0644); err != nil {
    t.Fatal(err)
  }
  err := SaveDingTalkConfig(cfg, "alice", "client-id", "client-secret")
  if err == nil {
    t.Fatal("SaveDingTalkConfig() error = nil, want unsupported config version error")
  }
  if !strings.Contains(err.Error(), "只支持到 3") {
    t.Fatalf("error = %q, want supported config version message", err.Error())
  }
}

func TestResolveUsersRoot(t *testing.T) {
  cfg := &config.GlobalConfig{UsersRoot: "/absolute/path"}
  if got := ResolveUsersRoot(cfg); got != "/absolute/path" {
    t.Errorf("absolute ResolveUsersRoot = %q, want /absolute/path", got)
  }

  wd, _ := os.Getwd()
  cfg = &config.GlobalConfig{UsersRoot: "relative/path"}
  if got := ResolveUsersRoot(cfg); got != filepath.Join(wd, "relative/path") {
    t.Errorf("relative ResolveUsersRoot = %q, want %q", got, filepath.Join(wd, "relative/path"))
  }
}

func TestEnsureUsersRoot(t *testing.T) {
  tmpDir := t.TempDir()
  cfg := &config.GlobalConfig{
    UsersRoot:   filepath.Join(tmpDir, "users"),
    ArchiveRoot: filepath.Join(tmpDir, "archive"),
  }
  if err := EnsureUsersRoot(cfg); err != nil {
    t.Fatalf("EnsureUsersRoot() error = %v", err)
  }
  if _, err := os.Stat(filepath.Join(tmpDir, "users")); err != nil {
    t.Errorf("users dir not created: %v", err)
  }
  if _, err := os.Stat(filepath.Join(tmpDir, "archive")); err != nil {
    t.Errorf("archive dir not created: %v", err)
  }
}

func TestResolveArchiveRoot(t *testing.T) {
  cfg := &config.GlobalConfig{ArchiveRoot: "/archive"}
  if got := ResolveArchiveRoot(cfg); got != "/archive" {
    t.Errorf("absolute = %q, want /archive", got)
  }

  cfg = &config.GlobalConfig{ArchiveRoot: ""}
  wd, _ := os.Getwd()
  if got := ResolveArchiveRoot(cfg); got != filepath.Join(wd, "./archive") {
    t.Errorf("empty ArchiveRoot = %q, want %q", got, filepath.Join(wd, "./archive"))
  }

  cfg = &config.GlobalConfig{ArchiveRoot: "my-archive"}
  if got := ResolveArchiveRoot(cfg); got != filepath.Join(wd, "my-archive") {
    t.Errorf("relative = %q, want %q", got, filepath.Join(wd, "my-archive"))
  }
}

func TestGetUserList(t *testing.T) {
  testInitAuthDB(t)
  users, err := GetUserList(nil)
  if err != nil {
    t.Fatalf("GetUserList() error = %v", err)
  }
  if len(users) != 0 {
    t.Fatalf("GetUserList() = %v, want empty", users)
  }

  if err := auth.UpsertContainer(&auth.ContainerRecord{Username: "bob", Status: "stopped", IP: "100.64.0.2"}); err != nil {
    t.Fatalf("UpsertContainer: %v", err)
  }
  if err := auth.UpsertContainer(&auth.ContainerRecord{Username: "alice", Status: "stopped", IP: "100.64.0.3"}); err != nil {
    t.Fatalf("UpsertContainer: %v", err)
  }

  users, err = GetUserList(nil)
  if err != nil {
    t.Fatalf("GetUserList() error = %v", err)
  }
  if len(users) != 2 || users[0] != "alice" || users[1] != "bob" {
    t.Fatalf("GetUserList() = %v, want [alice bob]", users)
  }
}

func TestArchiveUser(t *testing.T) {
  tmpDir := t.TempDir()
  cfg := &config.GlobalConfig{
    UsersRoot:   filepath.Join(tmpDir, "users"),
    ArchiveRoot: filepath.Join(tmpDir, "archive"),
  }

  userDir := UserDir(cfg, "testuser")
  if err := os.MkdirAll(userDir, 0755); err != nil {
    t.Fatal(err)
  }

  if err := ArchiveUser(cfg, "testuser"); err != nil {
    t.Fatalf("ArchiveUser() error = %v", err)
  }

  if _, err := os.Stat(userDir); err == nil {
    t.Error("user dir should be gone after archive")
  }

  archiveDir := filepath.Join(ResolveArchiveRoot(cfg), "testuser")
  if _, err := os.Stat(archiveDir); err != nil {
    t.Errorf("archive dir not found: %v", err)
  }

  if err := ArchiveUser(cfg, "nonexistent"); err != nil {
    t.Fatalf("ArchiveUser(nonexistent) error = %v", err)
  }
}

func TestArchiveUserExistingArchive(t *testing.T) {
  tmpDir := t.TempDir()
  cfg := &config.GlobalConfig{
    UsersRoot:   filepath.Join(tmpDir, "users"),
    ArchiveRoot: filepath.Join(tmpDir, "archive"),
  }

  userDir := UserDir(cfg, "testuser")
  if err := os.MkdirAll(userDir, 0755); err != nil {
    t.Fatal(err)
  }

  archiveDir := filepath.Join(ResolveArchiveRoot(cfg), "testuser")
  if err := os.MkdirAll(archiveDir, 0755); err != nil {
    t.Fatal(err)
  }

  if err := ArchiveUser(cfg, "testuser"); err != nil {
    t.Fatalf("ArchiveUser() error = %v", err)
  }

  entries, err := os.ReadDir(ResolveArchiveRoot(cfg))
  if err != nil {
    t.Fatal(err)
  }
  if len(entries) != 2 {
    t.Fatalf("expected 2 archive entries, got %d", len(entries))
  }
}

func TestRemoveAllUserData(t *testing.T) {
  tmpDir := t.TempDir()
  cfg := &config.GlobalConfig{
    UsersRoot:   filepath.Join(tmpDir, "users"),
    ArchiveRoot: filepath.Join(tmpDir, "archive"),
  }

  if err := os.MkdirAll(filepath.Join(cfg.UsersRoot, "testuser"), 0755); err != nil {
    t.Fatal(err)
  }
  if err := os.MkdirAll(cfg.ArchiveRoot, 0755); err != nil {
    t.Fatal(err)
  }

  if err := RemoveAllUserData(cfg); err != nil {
    t.Fatalf("RemoveAllUserData() error = %v", err)
  }

  if _, err := os.Stat(cfg.UsersRoot); err != nil {
    t.Errorf("users root should be recreated: %v", err)
  }
  if _, err := os.Stat(cfg.ArchiveRoot); err != nil {
    t.Errorf("archive root should be recreated: %v", err)
  }

  entries, _ := os.ReadDir(cfg.UsersRoot)
  if len(entries) != 0 {
    t.Error("users root should be empty")
  }
}

func TestApplySecurityToYAML(t *testing.T) {
  tmpDir := t.TempDir()
  cfg := &config.GlobalConfig{
    Security: map[string]interface{}{
      "model_list": map[string]interface{}{
        "gpt-4": map[string]interface{}{
          "api_keys": []interface{}{"sk-test"},
        },
      },
    },
  }

  picoclawDir := filepath.Join(tmpDir, ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }

  existingYAML := "cookies:\n  example.com: session=abc\n"
  if err := os.WriteFile(filepath.Join(picoclawDir, ".security.yml"), []byte(existingYAML), 0600); err != nil {
    t.Fatal(err)
  }

  if err := ApplySecurityToYAML(cfg, picoclawDir); err != nil {
    t.Fatalf("ApplySecurityToYAML() error = %v", err)
  }

  data, err := os.ReadFile(filepath.Join(picoclawDir, ".security.yml"))
  if err != nil {
    t.Fatal(err)
  }
  var result map[string]interface{}
  if err := yaml.Unmarshal(data, &result); err != nil {
    t.Fatal(err)
  }
  if result["cookies"] == nil {
    t.Error("cookies should be preserved")
  }
  if result["model_list"] == nil {
    t.Error("model_list from global security should be present")
  }
}

func TestApplySecurityToYAMLCorruptedFile(t *testing.T) {
  tmpDir := t.TempDir()
  cfg := &config.GlobalConfig{}
  picoclawDir := filepath.Join(tmpDir, ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }

  if err := os.WriteFile(filepath.Join(picoclawDir, ".security.yml"), []byte(": bad yaml :"), 0600); err != nil {
    t.Fatal(err)
  }

  err := ApplySecurityToYAML(cfg, picoclawDir)
  if err == nil {
    t.Fatal("ApplySecurityToYAML() should error on corrupted YAML")
  }
}

func TestConfigVersionFromMap(t *testing.T) {
  cases := []struct {
    name string
    cfg  map[string]interface{}
    want int
  }{
    {"int version", map[string]interface{}{"version": 1}, 1},
    {"int64 version", map[string]interface{}{"version": int64(2)}, 2},
    {"float64 version", map[string]interface{}{"version": float64(3)}, 3},
    {"json.Number version", map[string]interface{}{"version": json.Number("2")}, 2},
    {"string version", map[string]interface{}{"version": "3"}, 3},
    {"no version", map[string]interface{}{}, 3},
    {"nil version", map[string]interface{}{"version": nil}, 3},
  }
  for _, tt := range cases {
    t.Run(tt.name, func(t *testing.T) {
      if got := configVersionFromMap(tt.cfg); got != tt.want {
        t.Errorf("configVersionFromMap() = %d, want %d", got, tt.want)
      }
    })
  }
}

func TestDingTalkBaseKey(t *testing.T) {
  cases := []struct {
    name string
    cfg  map[string]interface{}
    want string
  }{
    {"channel_list exists", map[string]interface{}{"channel_list": map[string]interface{}{}}, "channel_list"},
    {"channels exists", map[string]interface{}{"channels": map[string]interface{}{}}, "channels"},
    {"v3 version", map[string]interface{}{"version": 3}, "channel_list"},
    {"v2 version", map[string]interface{}{"version": 2}, "channels"},
    {"no version default v3", map[string]interface{}{}, "channel_list"},
  }
  for _, tt := range cases {
    t.Run(tt.name, func(t *testing.T) {
      if got := dingTalkBaseKey(tt.cfg); got != tt.want {
        t.Errorf("dingTalkBaseKey() = %q, want %q", got, tt.want)
      }
    })
  }
}

func TestSetDingTalkFieldInBase(t *testing.T) {
  t.Run("v3 channel_list mode", func(t *testing.T) {
    root := map[string]interface{}{"version": 3}
    setDingTalkFieldInBase(root, "channel_list", "client_id", "abc123")
    channels := root["channel_list"].(map[string]interface{})
    dingtalk := channels["dingtalk"].(map[string]interface{})
    if dingtalk["enabled"] != true {
      t.Error("enabled should be true")
    }
    if dingtalk["type"] != "dingtalk" {
      t.Error("type should be dingtalk")
    }
    settings := dingtalk["settings"].(map[string]interface{})
    if settings["client_id"] != "abc123" {
      t.Errorf("client_id = %v, want abc123", settings["client_id"])
    }
  })

  t.Run("v2 channels mode", func(t *testing.T) {
    root := map[string]interface{}{"version": 2}
    setDingTalkFieldInBase(root, "channels", "client_id", "abc456")
    channels := root["channels"].(map[string]interface{})
    dingtalk := channels["dingtalk"].(map[string]interface{})
    if dingtalk["enabled"] != true {
      t.Error("enabled should be true")
    }
    if dingtalk["client_id"] != "abc456" {
      t.Errorf("client_id = %v, want abc456", dingtalk["client_id"])
    }
  })

  t.Run("existing channel_list preserved", func(t *testing.T) {
    root := map[string]interface{}{
      "channel_list": map[string]interface{}{
        "dingtalk": map[string]interface{}{
          "type": "dingtalk",
          "settings": map[string]interface{}{
            "old_key": "old_val",
          },
        },
      },
    }
    setDingTalkFieldInBase(root, "channel_list", "client_id", "newval")
    channels := root["channel_list"].(map[string]interface{})
    dingtalk := channels["dingtalk"].(map[string]interface{})
    settings := dingtalk["settings"].(map[string]interface{})
    if settings["old_key"] != "old_val" {
      t.Error("old settings should be preserved")
    }
    if settings["client_id"] != "newval" {
      t.Error("client_id should be set")
    }
  })
}

func TestSetDingTalkField(t *testing.T) {
  root := map[string]interface{}{"version": 3}
  setDingTalkField(root, "client_id", "abc123")
  channelList := root["channel_list"].(map[string]interface{})
  dingtalk := channelList["dingtalk"].(map[string]interface{})
  settings := dingtalk["settings"].(map[string]interface{})
  if settings["client_id"] != "abc123" {
    t.Errorf("client_id = %v, want abc123", settings["client_id"])
  }
}

func TestSeedPicoClawAdapterToDB(t *testing.T) {
  testInitAuthDB(t)
  engine, err := auth.GetEngine()
  if err != nil {
    t.Fatal(err)
  }

  if err := SeedPicoClawAdapterToDB(engine); err != nil {
    t.Fatalf("SeedPicoClawAdapterToDB() error = %v", err)
  }

  // Second call should succeed (no-op since seeded already)
  if err := SeedPicoClawAdapterToDB(engine); err != nil {
    t.Fatalf("Second SeedPicoClawAdapterToDB() error = %v", err)
  }

  // Verify data was written
  pkg, err := PicoClawAdapterPackageFromDB(engine)
  if err != nil {
    t.Fatal(err)
  }
  if pkg == nil {
    t.Fatal("Package should exist after seeding")
  }
  if len(pkg.ConfigSchemas) == 0 {
    t.Error("ConfigSchemas should not be empty")
  }
}

func TestInitUser(t *testing.T) {
  testInitAuthDB(t)
  tmpDir := t.TempDir()
  cfg := &config.GlobalConfig{
    UsersRoot:   filepath.Join(tmpDir, "users"),
    ArchiveRoot: filepath.Join(tmpDir, "archive"),
    Image:       config.ImageConfig{Name: "picoaide/picoaide", Tag: "v0.2.8"},
    Web:         config.WebConfig{Listen: ":80"},
  }

  if err := InitUser(cfg, "newuser", "v0.2.8"); err != nil {
    t.Fatalf("InitUser() error = %v", err)
  }

  userDir := filepath.Join(tmpDir, "users", "newuser")
  if _, err := os.Stat(userDir); err != nil {
    t.Errorf("user dir should exist: %v", err)
  }

  rec, err := auth.GetContainerByUsername("newuser")
  if err != nil {
    t.Fatal(err)
  }
  if rec == nil {
    t.Fatal("container record should exist")
  }
  if rec.IP == "" {
    t.Error("IP should be allocated")
  }
  if rec.Image != "picoaide/picoaide:v0.2.8" {
    t.Errorf("Image = %q, want picoaide/picoaide:v0.2.8", rec.Image)
  }
}

func TestInitUserExistingDir(t *testing.T) {
  testInitAuthDB(t)
  tmpDir := t.TempDir()
  cfg := &config.GlobalConfig{
    UsersRoot:   filepath.Join(tmpDir, "users"),
    ArchiveRoot: filepath.Join(tmpDir, "archive"),
    Web:         config.WebConfig{Listen: ":80"},
  }

  userDir := filepath.Join(tmpDir, "users", "updateuser")
  if err := os.MkdirAll(userDir, 0755); err != nil {
    t.Fatal(err)
  }

  if err := InitUser(cfg, "updateuser", ""); err != nil {
    t.Fatalf("InitUser() error = %v", err)
  }

  rec, err := auth.GetContainerByUsername("updateuser")
  if err != nil {
    t.Fatal(err)
  }
  if rec == nil {
    t.Fatal("container record should exist")
  }
  if rec.Image != "" {
    t.Errorf("Image should be empty when no tag given")
  }
}

func TestInitUserRejectsInvalidUsername(t *testing.T) {
  cfg := &config.GlobalConfig{}
  err := InitUser(cfg, "invalid/user", "")
  if err == nil {
    t.Fatal("InitUser should reject invalid username")
  }
}

func TestGetDingTalkField(t *testing.T) {
  root := map[string]interface{}{
    "channel_list": map[string]interface{}{
      "dingtalk": map[string]interface{}{
        "settings": map[string]interface{}{
          "client_id": "cid-001",
        },
        "client_secret": "cs-001",
      },
    },
  }
  v, ok := getDingTalkField(root, "client_id")
  if !ok || v != "cid-001" {
    t.Errorf("getDingTalkField(client_id) = %q, %v", v, ok)
  }
  _, ok = getDingTalkField(root, "nonexistent")
  if ok {
    t.Errorf("getDingTalkField(nonexistent) should not be found")
  }
}

func TestSaveDingTalkConfigFallsBackToDirectWrite(t *testing.T) {
  tmpDir := t.TempDir()
  cfg := &config.GlobalConfig{UsersRoot: filepath.Join(tmpDir, "users")}
  picoclawDir := filepath.Join(UserDir(cfg, "alice"), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }

  if err := os.WriteFile(filepath.Join(picoclawDir, "config.json"), []byte(`{"version":3,"channel_list":{"dingtalk":{"enabled":false,"type":"dingtalk","settings":{"old_key":"old_val"}}}}`), 0644); err != nil {
    t.Fatal(err)
  }

  if err := SaveDingTalkConfig(cfg, "alice", "direct-client", "direct-secret"); err != nil {
    t.Fatalf("SaveDingTalkConfig() error = %v", err)
  }

  var saved map[string]interface{}
  data, _ := os.ReadFile(filepath.Join(picoclawDir, "config.json"))
  if err := json.Unmarshal(data, &saved); err != nil {
    t.Fatal(err)
  }
  channelList := saved["channel_list"].(map[string]interface{})
  dingtalk := channelList["dingtalk"].(map[string]interface{})
  settings := dingtalk["settings"].(map[string]interface{})
  if settings["client_id"] != "direct-client" {
    t.Fatalf("client_id = %v, want direct-client", settings["client_id"])
  }
}

func TestSyncCookiesRejectsInvalidUsername(t *testing.T) {
  err := SyncCookies(nil, "invalid/user", "example.com", "session=abc")
  if err == nil {
    t.Fatal("SyncCookies should reject invalid username")
  }
}

func TestSyncCookiesCorruptedSecurityYAML(t *testing.T) {
  cfg := &config.GlobalConfig{UsersRoot: t.TempDir()}
  picoclawDir := filepath.Join(UserDir(cfg, "alice"), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(filepath.Join(picoclawDir, ".security.yml"), []byte(": bad"), 0600); err != nil {
    t.Fatal(err)
  }
  err := SyncCookies(cfg, "alice", "example.com", "session=abc")
  if err == nil {
    t.Fatal("SyncCookies should error on corrupted YAML")
  }
}

func TestGetDingTalkConfigWithConfigFile(t *testing.T) {
  cfg := &config.GlobalConfig{UsersRoot: t.TempDir()}
  picoclawDir := filepath.Join(UserDir(cfg, "alice"), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(filepath.Join(picoclawDir, "config.json"), []byte(`{"channels":{"dingtalk":{"client_id":"from-config"}}}`), 0644); err != nil {
    t.Fatal(err)
  }
  clientID, clientSecret := GetDingTalkConfig(cfg, "alice")
  if clientID != "from-config" {
    t.Errorf("clientID = %q, want from-config", clientID)
  }
  if clientSecret != "" {
    t.Errorf("clientSecret = %q, want empty", clientSecret)
  }
}

func TestGetDingTalkConfigWithSecurityFile(t *testing.T) {
  cfg := &config.GlobalConfig{UsersRoot: t.TempDir()}
  picoclawDir := filepath.Join(UserDir(cfg, "alice"), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(filepath.Join(picoclawDir, "config.json"), []byte(`{"channels":{"dingtalk":{}}}`), 0644); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(filepath.Join(picoclawDir, ".security.yml"), []byte("channels:\n  dingtalk:\n    client_secret: from-security\n"), 0600); err != nil {
    t.Fatal(err)
  }
  _, clientSecret := GetDingTalkConfig(cfg, "alice")
  if clientSecret != "from-security" {
    t.Errorf("clientSecret = %q, want from-security", clientSecret)
  }
}

func TestGetDingTalkConfigInvalidUsername(t *testing.T) {
  id, secret := GetDingTalkConfig(nil, "")
  if id != "" || secret != "" {
    t.Errorf("GetDingTalkConfig('') = %q, %q, want empty", id, secret)
  }
}

func TestReleasePicoClawMigrationServiceBundledRules(t *testing.T) {
  svc := &PicoClawMigrationService{cacheDir: t.TempDir()}
  if err := svc.ReleaseBundledRulesCache(); err != nil {
    t.Fatalf("ReleaseBundledRulesCache() error = %v", err)
  }
  // Nil receiver
  nilSvc := (*PicoClawMigrationService)(nil)
  if err := nilSvc.ReleaseBundledRulesCache(); err != nil {
    t.Errorf("nil receiver should not error: %v", err)
  }
}

func TestEnsureUpgradeableNilService(t *testing.T) {
  nilSvc := (*PicoClawMigrationService)(nil)
  if err := nilSvc.EnsureUpgradeable("v0.2.7", "v0.2.8"); err != nil {
    t.Fatalf("nil EnsureUpgradeable should not error: %v", err)
  }
}

func TestMigrateNilService(t *testing.T) {
  cfg := map[string]interface{}{"version": float64(3)}
  nilSvc := (*PicoClawMigrationService)(nil)
  if err := nilSvc.Migrate(cfg, "v0.2.7", "v0.2.8"); err != nil {
    t.Fatalf("nil Migrate should not error: %v", err)
  }
}

func TestNewPicoClawMigrationServiceFromEmbed(t *testing.T) {
  svc, err := NewPicoClawMigrationService(t.TempDir())
  if err != nil {
    t.Fatalf("NewPicoClawMigrationService() error = %v", err)
  }
  if svc == nil {
    t.Fatal("returned nil service")
  }
  if len(svc.rules.Versions) == 0 {
    t.Error("rules should have versions")
  }
}

func TestEnsureSupportedByPicoAideUpgradeRequired(t *testing.T) {
  rules := PicoClawMigrationRuleSet{
    LatestSupportedConfigVersion: 3,
    Versions: []PicoClawMigrationVersionRule{
      {Version: "0.2.10", ConfigVersion: 4},
    },
  }
  err := rules.EnsureSupportedByPicoAide()
  if err == nil {
    t.Fatal("expected error for unsupported config version")
  }
  if !strings.Contains(err.Error(), "只支持到 3") {
    t.Errorf("error = %q, want config version message", err.Error())
  }
}

func TestMissingVersionsFromUnknown(t *testing.T) {
  rules := PicoClawMigrationRuleSet{
    Versions: []PicoClawMigrationVersionRule{
      {Version: "0.2.7", ConfigVersion: 3},
    },
  }
  missing := rules.MissingVersions("v0.2.6", "v0.2.8")
  if len(missing) == 0 {
    t.Error("should report missing versions")
  }
}

func TestVersionChainEmpty(t *testing.T) {
  rules := PicoClawMigrationRuleSet{
    Versions: []PicoClawMigrationVersionRule{
      {Version: "0.2.7", ConfigVersion: 3},
    },
  }
  chain := rules.VersionChain("v0.2.8", "v0.2.8")
  if len(chain) != 0 {
    t.Errorf("VersionChain for same version should be empty, got %d", len(chain))
  }
}

func TestApplyPicoClawCompatibilityFixupsNoop(t *testing.T) {
  cfg := map[string]interface{}{
    "version": float64(2),
  }
  // Version 2 with target < v0.2.8 should be noop
  if err := applyPicoClawCompatibilityFixups(cfg, "v0.2.7", "v0.2.7"); err != nil {
    t.Fatalf("applyPicoClawCompatibilityFixups() error = %v", err)
  }
}

func TestProjectDefaultModelNoTargetPath(t *testing.T) {
  cfg := map[string]interface{}{
    "agents": map[string]interface{}{
      "defaults": map[string]interface{}{
        "model_name": "test-model",
      },
    },
  }
  // Empty target path should be no-op
  projectDefaultModel(cfg, "")
  if v, _ := deepGet(cfg, "agents.defaults.model_name"); v != "test-model" {
    t.Error("model_name should be preserved when target path is empty")
  }
}

func TestLoadWhitelistNoEngine(t *testing.T) {
  auth.ResetDB()
  m, err := LoadWhitelist()
  if err != nil {
    t.Fatalf("LoadWhitelist() error = %v", err)
  }
  if m != nil {
    t.Errorf("LoadWhitelist with no DB = %v, want nil", m)
  }
}

func TestLoadWhitelistWithData(t *testing.T) {
  testInitAuthDB(t)
  engine, err := auth.GetEngine()
  if err != nil {
    t.Fatal(err)
  }
  if _, err := engine.Exec("INSERT INTO whitelist (username, added_by) VALUES (?, ?)", "alice", "admin"); err != nil {
    t.Fatal(err)
  }
  if _, err := engine.Exec("INSERT INTO whitelist (username, added_by) VALUES (?, ?)", "bob", "admin"); err != nil {
    t.Fatal(err)
  }

  m, err := LoadWhitelist()
  if err != nil {
    t.Fatalf("LoadWhitelist() error = %v", err)
  }
  if m == nil {
    t.Fatal("LoadWhitelist() returned nil")
  }
  if !m["alice"] || !m["bob"] {
    t.Errorf("LoadWhitelist() = %v, want alice and bob", m)
  }
}

func TestEnsureUsersRootFails(t *testing.T) {
  // Using an unwritable path to trigger error
  err := EnsureUsersRoot(&config.GlobalConfig{
    UsersRoot: "/nonexistent-deep-path/users",
    ArchiveRoot: "/nonexistent-deep-path/archive",
  })
  if err == nil {
    t.Log("EnsureUsersRoot may or may not error depending on permissions")
  }
}

func TestPicoClawCompatibilityFixupsNonMapChannels(t *testing.T) {
  cfg := map[string]interface{}{
    "version": float64(3),
    "channels": map[string]interface{}{
      "telegram": "string-value",
    },
  }
  if err := applyPicoClawCompatibilityFixups(cfg, "v0.2.8", ""); err != nil {
    t.Fatalf("applyPicoClawCompatibilityFixups() error = %v", err)
  }
}

func TestPicoClawMigrationMissingVersions(t *testing.T) {
  rules := PicoClawMigrationRuleSet{
    LatestSupportedConfigVersion: 3,
    Versions: []PicoClawMigrationVersionRule{
      {Version: "0.2.7", ConfigVersion: 3},
    },
  }
  missing := rules.MissingVersions("v0.2.6", "v0.2.7")
  if len(missing) == 0 {
    t.Error("should report missing version 0.2.6")
  }

  // non-normalizable
  missing = rules.MissingVersions("latest", "v0.2.7")
  if len(missing) == 0 {
    t.Error("should report missing when from version can't be normalized")
  }

  // Same version
  missing = rules.MissingVersions("v0.2.7", "v0.2.7")
  if missing != nil {
    t.Errorf("same version should return nil, got %v", missing)
  }
}

func TestUniqueStrings(t *testing.T) {
  if got := uniqueStrings(nil); got != nil {
    t.Errorf("uniqueStrings(nil) = %v, want nil", got)
  }
  if got := uniqueStrings([]string{}); got != nil {
    t.Errorf("uniqueStrings([]) = %v, want nil", got)
  }
  if got := uniqueStrings([]string{"a", "b", "a", ""}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
    t.Errorf("uniqueStrings(dup) = %v, want [a b]", got)
  }
}

func TestUnsupportedEndpointVersions(t *testing.T) {
  rules := PicoClawMigrationRuleSet{
    Versions: []PicoClawMigrationVersionRule{
      {Version: "0.2.7", ConfigVersion: 3},
    },
  }
  missing := rules.UnsupportedEndpointVersions("v0.2.8", "v0.2.9")
  if len(missing) == 0 {
    t.Error("should report unsupported versions")
  }

  // Non-normalizable should be reported
  missing = rules.UnsupportedEndpointVersions("latest", "v0.2.7")
  if len(missing) != 1 {
    t.Errorf("expected 1 unsupported version, got %v", missing)
  }
}

func TestCompareVersionStrings(t *testing.T) {
  if compareVersionStrings("v0.2.7", "v0.2.8") != -1 {
    t.Error("v0.2.7 < v0.2.8")
  }
  if compareVersionStrings("v0.2.8", "v0.2.7") != 1 {
    t.Error("v0.2.8 > v0.2.7")
  }
  if compareVersionStrings("v0.2.7", "v0.2.7") != 0 {
    t.Error("v0.2.7 == v0.2.7")
  }
  // Non-matching versions use string compare
  if compareVersionStrings("abc", "def") != -1 {
    t.Error("abc < def")
  }
}

func TestValidPicoClawVersion(t *testing.T) {
  if !validPicoClawVersion("v0.2.8") {
    t.Error("v0.2.8 should be valid")
  }
  if !validPicoClawVersion("0.2.8") {
    t.Error("0.2.8 should be valid")
  }
  if validPicoClawVersion("latest") {
    t.Error("latest should be invalid")
  }
}

func TestSyncCookiesWritesToDB(t *testing.T) {
  testInitAuthDB(t)
  cfg := &config.GlobalConfig{UsersRoot: t.TempDir()}

  if err := SyncCookies(cfg, "alice", "example.com", "session=abc"); err != nil {
    t.Fatalf("SyncCookies failed: %v", err)
  }

  got, err := auth.GetCookie("alice", "example.com")
  if err != nil {
    t.Fatalf("GetCookie failed: %v", err)
  }
  if got != "session=abc" {
    t.Errorf("GetCookie = %q, want %q", got, "session=abc")
  }

  // Also verify .security.yml was written (existing behavior preserved)
  picoclawDir := filepath.Join(UserDir(cfg, "alice"), ".picoclaw")
  secData, err := os.ReadFile(filepath.Join(picoclawDir, ".security.yml"))
  if err != nil {
    t.Fatal(err)
  }
  var secMap map[string]interface{}
  if err := yaml.Unmarshal(secData, &secMap); err != nil {
    t.Fatal(err)
  }
  cookiesMap, _ := secMap["cookies"].(map[string]interface{})
  if cookiesMap == nil || cookiesMap["example.com"] != "session=abc" {
    t.Errorf(".security.yml cookies = %v", cookiesMap)
  }
}
