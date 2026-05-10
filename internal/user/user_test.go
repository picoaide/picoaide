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
	if got := containerBaseURL(cfg); got != "https://100.64.0.1:8443" {
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
			Tag:  "v0.2.10",
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
	if err := auth.UpsertContainer(&auth.ContainerRecord{Username: "alice", Image: "picoaide/picoaide:v0.2.10", Status: "stopped", IP: "100.64.0.2"}); err != nil {
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
