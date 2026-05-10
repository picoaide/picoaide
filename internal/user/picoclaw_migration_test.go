package user

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testRulesJSON = `{
  "latest_supported_config_version": 3,
  "versions": [
    { "version": "0.2.4", "config_version": 1, "config_changed": false, "actions": [] },
    { "version": "0.2.5", "config_version": 2, "config_changed": true, "from_config": 1, "to_config": 2, "actions": [
      { "op": "move", "path": "channels.*.mention_only", "to": "channels.*.group_trigger.mention_only" },
      { "op": "infer_model_enabled" }
    ] },
    { "version": "0.2.6", "config_version": 2, "config_changed": false, "actions": [] },
    { "version": "0.2.7", "config_version": 3, "config_changed": true, "from_config": 2, "to_config": 3, "actions": [
      { "op": "set", "path": "version", "value": 3 },
      { "op": "delete", "path": "bindings" },
      { "op": "rename", "from": "channels", "to": "channel_list", "mode": "channels_to_nested" },
      { "op": "map", "path": "tools.mcp.servers", "field": "transport", "to": "type", "value": "sse" }
    ] },
    { "version": "0.2.8", "config_version": 3, "config_changed": false, "actions": [] }
  ]
}`

func TestMigratePicoClawConfigTo028(t *testing.T) {
	cfg := map[string]interface{}{
		"agents": map[string]interface{}{},
		"channels": map[string]interface{}{
			"telegram": map[string]interface{}{
				"enabled":      true,
				"token":        "secret",
				"mention_only": true,
			},
		},
		"tools": map[string]interface{}{
			"mcp": map[string]interface{}{
				"servers": map[string]interface{}{
					"browser": map[string]interface{}{
						"enabled":   true,
						"url":       "http://127.0.0.1/sse",
						"transport": "sse",
					},
				},
			},
		},
	}

	svc := testPicoClawMigrationService(t, testRulesJSON)
	if err := svc.Migrate(cfg, "v0.2.6", "v0.2.8"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if cfg["version"] != float64(3) {
		t.Fatalf("version = %v, want 3", cfg["version"])
	}
	if _, ok := cfg["channels"]; ok {
		t.Fatal("channels should be migrated to channel_list")
	}
	channelList, ok := cfg["channel_list"].(map[string]interface{})
	if !ok {
		t.Fatalf("channel_list type = %T, want map", cfg["channel_list"])
	}
	telegram := channelList["telegram"].(map[string]interface{})
	if telegram["type"] != "telegram" {
		t.Fatalf("telegram.type = %v, want telegram", telegram["type"])
	}
	settings := telegram["settings"].(map[string]interface{})
	if settings["token"] != "secret" {
		t.Fatalf("settings.token = %v, want secret", settings["token"])
	}

	server := cfg["tools"].(map[string]interface{})["mcp"].(map[string]interface{})["servers"].(map[string]interface{})["browser"].(map[string]interface{})
	if server["type"] != "sse" {
		t.Fatalf("server.type = %v, want sse", server["type"])
	}
	if _, ok := server["transport"]; ok {
		t.Fatal("server.transport should be removed")
	}
}

func TestApplyPicoClawCompatibilityFixupsTo028(t *testing.T) {
	cfg := map[string]interface{}{
		"channels": map[string]interface{}{
			"telegram": map[string]interface{}{
				"enabled":      true,
				"token":        "secret",
				"mention_only": true,
				"transport":    "sse",
			},
		},
	}

	if err := applyPicoClawCompatibilityFixups(cfg, "v0.2.8", ""); err != nil {
		t.Fatalf("applyPicoClawCompatibilityFixups() error = %v", err)
	}

	if _, ok := cfg["channels"]; ok {
		t.Fatal("channels should be removed for v0.2.8")
	}
	channelList, ok := cfg["channel_list"].(map[string]interface{})
	if !ok {
		t.Fatalf("channel_list type = %T, want map", cfg["channel_list"])
	}
	telegram := channelList["telegram"].(map[string]interface{})
	if telegram["type"] != "telegram" {
		t.Fatalf("telegram.type = %v, want telegram", telegram["type"])
	}
	if _, ok := telegram["group_trigger"].(map[string]interface{}); !ok {
		t.Fatal("telegram.group_trigger should be backfilled")
	}
	if _, ok := telegram["transport"]; ok {
		t.Fatal("transport should be removed from migrated channel")
	}
	settings := telegram["settings"].(map[string]interface{})
	if settings["token"] != "secret" {
		t.Fatalf("settings.token = %v, want secret", settings["token"])
	}
}

func TestApplyPicoClawCompatibilityFixupsUsesConfigVersion(t *testing.T) {
	cfg := map[string]interface{}{
		"version": float64(3),
		"channels": map[string]interface{}{
			"dingtalk": map[string]interface{}{"enabled": true},
		},
	}

	if err := applyPicoClawCompatibilityFixups(cfg, "", ""); err != nil {
		t.Fatalf("applyPicoClawCompatibilityFixups() error = %v", err)
	}
	if _, ok := cfg["channels"]; ok {
		t.Fatal("channels should be removed when config version is 3")
	}
	if _, ok := cfg["channel_list"]; !ok {
		t.Fatal("channel_list should be created when config version is 3")
	}
}

func TestApplyPicoClawCompatibilityFixupsBackfills028ChannelDefaults(t *testing.T) {
	cfg := map[string]interface{}{
		"version": float64(3),
		"model_list": []interface{}{
			map[string]interface{}{
				"model_name": "llama",
				"model":      "Qwen3.6-27B-UD-Q4_K_XL.gguf",
			},
		},
		"channel_list": map[string]interface{}{
			"dingtalk": map[string]interface{}{
				"enabled": true,
				"settings": map[string]interface{}{
					"client_id": "dingxxx",
				},
			},
		},
	}

	if err := applyPicoClawCompatibilityFixups(cfg, "", ""); err != nil {
		t.Fatalf("applyPicoClawCompatibilityFixups() error = %v", err)
	}

	channelList := cfg["channel_list"].(map[string]interface{})
	dingtalk := channelList["dingtalk"].(map[string]interface{})
	if dingtalk["type"] != "dingtalk" {
		t.Fatalf("dingtalk.type = %v, want dingtalk", dingtalk["type"])
	}
	if dingtalk["enabled"] != true {
		t.Fatalf("dingtalk.enabled should preserve user value")
	}
	if _, ok := dingtalk["group_trigger"].(map[string]interface{}); !ok {
		t.Fatal("dingtalk.group_trigger should be backfilled")
	}
	if placeholder, ok := dingtalk["placeholder"].(map[string]interface{}); !ok || placeholder["enabled"] != false {
		t.Fatalf("dingtalk.placeholder = %#v", dingtalk["placeholder"])
	}
	if _, ok := dingtalk["typing"].(map[string]interface{}); !ok {
		t.Fatal("dingtalk.typing should be backfilled")
	}
	settings := dingtalk["settings"].(map[string]interface{})
	if settings["client_id"] != "dingxxx" {
		t.Fatalf("client_id should be preserved")
	}
	if _, ok := channelList["telegram"].(map[string]interface{}); !ok {
		t.Fatal("telegram default channel should be backfilled")
	}
	model := cfg["model_list"].([]interface{})[0].(map[string]interface{})
	if model["enabled"] != true {
		t.Fatalf("model.enabled = %v, want true", model["enabled"])
	}
	defaults := cfg["agents"].(map[string]interface{})["defaults"].(map[string]interface{})
	if defaults["model_name"] != "llama" {
		t.Fatalf("agents.defaults.model_name = %v, want llama", defaults["model_name"])
	}
}

func TestApplyPicoClawCompatibilityFixupsPreservesExplicitDisabledModel(t *testing.T) {
	cfg := map[string]interface{}{
		"version": float64(3),
		"agents": map[string]interface{}{
			"defaults": map[string]interface{}{"model_name": "existing"},
		},
		"model_list": []interface{}{
			map[string]interface{}{
				"model_name": "disabled",
				"model":      "openai/disabled",
				"enabled":    false,
			},
		},
	}

	if err := applyPicoClawCompatibilityFixups(cfg, "", ""); err != nil {
		t.Fatalf("applyPicoClawCompatibilityFixups() error = %v", err)
	}

	model := cfg["model_list"].([]interface{})[0].(map[string]interface{})
	if model["enabled"] != false {
		t.Fatalf("model.enabled should preserve explicit false, got %v", model["enabled"])
	}
	defaults := cfg["agents"].(map[string]interface{})["defaults"].(map[string]interface{})
	if defaults["model_name"] != "existing" {
		t.Fatalf("agents.defaults.model_name should preserve explicit value, got %v", defaults["model_name"])
	}
}

func TestApplyPicoClawCompatibilityFixupsRemovesEmptyChannels(t *testing.T) {
	cfg := map[string]interface{}{
		"version":  float64(3),
		"channels": map[string]interface{}{},
	}

	if err := applyPicoClawCompatibilityFixups(cfg, "", ""); err != nil {
		t.Fatalf("applyPicoClawCompatibilityFixups() error = %v", err)
	}
	if _, ok := cfg["channels"]; ok {
		t.Fatal("empty channels should be removed when config version is 3")
	}
}

func TestMigratePicoClawConfigSameVersionDoesNothing(t *testing.T) {
	cfg := map[string]interface{}{
		"channels": map[string]interface{}{},
		"tools": map[string]interface{}{
			"mcp": map[string]interface{}{
				"servers": map[string]interface{}{
					"browser": map[string]interface{}{
						"transport": "sse",
					},
				},
			},
		},
	}

	svc := testPicoClawMigrationService(t, testRulesJSON)
	if err := svc.Migrate(cfg, "v0.2.7", "v0.2.7"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if _, ok := cfg["channels"]; !ok {
		t.Fatal("channels should remain for v0.2.7")
	}
	if _, ok := cfg["channel_list"]; ok {
		t.Fatal("channel_list should not be added for v0.2.7")
	}
	server := cfg["tools"].(map[string]interface{})["mcp"].(map[string]interface{})["servers"].(map[string]interface{})["browser"].(map[string]interface{})
	if _, ok := server["type"]; ok {
		t.Fatal("server.type should not be added for same version")
	}
	if server["transport"] != "sse" {
		t.Fatalf("server.transport = %v, want sse", server["transport"])
	}
}

func TestPicoClawMigrationBlocksUnsupportedVersion(t *testing.T) {
	svc := testPicoClawMigrationService(t, `{
    "latest_supported_config_version": 3,
    "versions": [
      { "version": "0.2.8", "config_version": 3, "config_changed": false, "actions": [] }
    ]
  }`)
	err := svc.EnsureUpgradeable("v0.2.8", "v0.2.9")
	if err == nil {
		t.Fatal("EnsureUpgradeable() error = nil, want unsupported error")
	}
	if !strings.Contains(err.Error(), PicoAideIssueURL) {
		t.Fatalf("error = %q, want issue URL", err.Error())
	}
}

func TestPicoClawMigrationNoConfigChangeIsNoop(t *testing.T) {
	cfg := map[string]interface{}{
		"version": float64(3),
		"channels": map[string]interface{}{
			"telegram": map[string]interface{}{"token": "secret"},
		},
	}
	svc := testPicoClawMigrationService(t, testRulesJSON)
	if err := svc.Migrate(cfg, "v0.2.7", "v0.2.8"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if cfg["version"] != float64(3) {
		t.Fatalf("version = %v, want 3", cfg["version"])
	}
	if _, ok := cfg["channel_list"]; ok {
		t.Fatal("channel_list should not be added when release has no config change")
	}
	if _, ok := cfg["channels"]; !ok {
		t.Fatal("channels should remain when release has no config change")
	}
}

func TestReleasePicoClawMigrationRulesCache(t *testing.T) {
	cacheDir := t.TempDir()
	if err := ReleasePicoClawMigrationRulesCache(cacheDir); err != nil {
		t.Fatalf("ReleasePicoClawMigrationRulesCache() error = %v", err)
	}
	pkg, err := NewPicoClawAdapterPackage(cacheDir)
	if err != nil {
		t.Fatalf("NewPicoClawAdapterPackage() error = %v", err)
	}
	rules := pkg.ToMigrationRuleSet()
	if len(rules.Versions) == 0 {
		t.Fatal("cached rules should not be empty")
	}
}

func TestPicoClawMigrationAllowsRegisteredReleaseWithoutConfigChange(t *testing.T) {
	cfg := map[string]interface{}{}
	svc := testPicoClawMigrationService(t, testRulesJSON)
	if err := svc.EnsureUpgradeable("v0.2.6", "v0.2.8"); err != nil {
		t.Fatalf("EnsureUpgradeable() error = %v", err)
	}
	if err := svc.Migrate(cfg, "v0.2.6", "v0.2.8"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if cfg["version"] != float64(3) {
		t.Fatalf("version = %v, want 3", cfg["version"])
	}
}

func TestPicoClawMigrationBlocksNewConfigVersionBeyondPicoAideSupport(t *testing.T) {
	svc := testPicoClawMigrationService(t, `{
    "latest_supported_config_version": 4,
    "versions": [
      { "version": "0.2.8", "config_version": 3, "config_changed": false, "actions": [] },
      { "version": "0.2.10", "config_version": 4, "config_changed": true, "from_config": 3, "to_config": 4, "actions": [
        { "op": "set", "path": "version", "value": 4 }
      ] }
    ]
  }`)
	err := svc.EnsureUpgradeable("v0.2.8", "v0.2.10")
	if err == nil {
		t.Fatal("EnsureUpgradeable() error = nil, want unsupported config version error")
	}
	if !strings.Contains(err.Error(), "只支持到 3") {
		t.Fatalf("error = %q, want supported config version message", err.Error())
	}
}

func TestSavePicoClawMigrationRulesRejectsUnsupportedConfigVersion(t *testing.T) {
	_, err := SavePicoClawMigrationRules(t.TempDir(), []byte(`{
    "latest_supported_config_version": 4,
    "versions": [
      { "version": "0.2.10", "config_version": 4, "config_changed": false, "actions": [] }
    ]
  }`))
	if err == nil {
		t.Fatal("SavePicoClawMigrationRules() error = nil, want unsupported config version error")
	}
	if !strings.Contains(err.Error(), "只支持到 3") {
		t.Fatalf("error = %q, want supported config version message", err.Error())
	}
}

func TestSavePicoClawMigrationRulesWritesLocalCache(t *testing.T) {
	cacheDir := t.TempDir()
	info, err := SavePicoClawMigrationRules(cacheDir, []byte(testRulesJSON))
	if err != nil {
		t.Fatalf("SavePicoClawMigrationRules() error = %v", err)
	}
	if info.PicoAideSupportedConfigVersion != 3 {
		t.Fatalf("PicoAideSupportedConfigVersion = %d, want 3", info.PicoAideSupportedConfigVersion)
	}
	if info.CachePath == "" || info.UpdatedAt == "" {
		t.Fatalf("info should include cache path and updated_at: %+v", info)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, picoClawMigrationCacheFile)); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	loaded, err := LoadPicoClawMigrationRulesInfo(cacheDir)
	if err != nil {
		t.Fatalf("LoadPicoClawMigrationRulesInfo() error = %v", err)
	}
	if len(loaded.Versions) != 5 {
		t.Fatalf("loaded versions = %d, want 5", len(loaded.Versions))
	}
}

func TestPicoClawTagAtLeast(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		{"v0.2.7", false},
		{"0.2.8", true},
		{"v0.2.8", true},
		{"v0.2.9", true},
		{"v0.3.0", true},
		{"latest", false},
	}

	for _, tt := range cases {
		if got := picoclawTagAtLeast(tt.tag, 0, 2, 8); got != tt.want {
			t.Fatalf("picoclawTagAtLeast(%q) = %v, want %v", tt.tag, got, tt.want)
		}
	}
}

func testPicoClawMigrationService(t *testing.T, rulesJSON string) *PicoClawMigrationService {
	t.Helper()
	rules, err := parsePicoClawMigrationRules([]byte(rulesJSON))
	if err != nil {
		t.Fatalf("parsePicoClawMigrationRules() error = %v", err)
	}
	return &PicoClawMigrationService{rules: rules}
}
