package user

import (
  "strings"
  "testing"
)

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

  svc := testPicoClawMigrationService(t, testRules())
  if err := svc.Migrate(cfg, "v0.2.6", "v0.2.8"); err != nil {
    t.Fatalf("Migrate() error = %v", err)
  }

  if !numericEqual(cfg["version"], 3) {
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

  svc := testPicoClawMigrationService(t, testRules())
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
  svc := testPicoClawMigrationService(t, PicoClawMigrationRuleSet{
    LatestSupportedConfigVersion: 3,
    Versions: []PicoClawMigrationVersionRule{
      {Version: "0.2.8", ConfigVersion: 3, Actions: []PicoClawMigrationRule{}},
    },
  })
  err := svc.EnsureUpgradeable("v0.2.8", "v0.2.9")
  if err == nil {
    t.Fatal("EnsureUpgradeable() error = nil, want unsupported error")
  }
  if !strings.Contains(err.Error(), PicoAideIssueURL) {
    t.Fatalf("error = %q, want issue URL", err.Error())
  }
}

func TestPicoClawMigrationBlocksUnsupportedDowngradeEndpoint(t *testing.T) {
  svc := testPicoClawMigrationService(t, testRules())
  err := svc.EnsureUpgradeable("v0.2.9", "v0.2.8")
  if err == nil {
    t.Fatal("EnsureUpgradeable() error = nil, want unsupported downgrade endpoint error")
  }
  if !strings.Contains(err.Error(), "v0.2.9") {
    t.Fatalf("error = %q, want unsupported version", err.Error())
  }
}

func TestPicoClawMigrationAllowsSupportedDowngrade(t *testing.T) {
  svc := testPicoClawMigrationService(t, testRules())
  if err := svc.EnsureUpgradeable("v0.2.8", "v0.2.4"); err != nil {
    t.Fatalf("EnsureUpgradeable() error = %v", err)
  }
  cfg := map[string]interface{}{"version": float64(3)}
  if err := svc.Migrate(cfg, "v0.2.8", "v0.2.4"); err != nil {
    t.Fatalf("Migrate() error = %v", err)
  }
  if cfg["version"] != float64(3) {
    t.Fatalf("downgrade migration should be noop, got config %+v", cfg)
  }
}

func TestPicoClawMigrationNoConfigChangeIsNoop(t *testing.T) {
  cfg := map[string]interface{}{
    "version": float64(3),
    "channels": map[string]interface{}{
      "telegram": map[string]interface{}{"token": "secret"},
    },
  }
  svc := testPicoClawMigrationService(t, testRules())
  if err := svc.Migrate(cfg, "v0.2.7", "v0.2.8"); err != nil {
    t.Fatalf("Migrate() error = %v", err)
  }
  if !numericEqual(cfg["version"], 3) {
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
  svc := testPicoClawMigrationService(t, testRules())
  if err := svc.EnsureUpgradeable("v0.2.6", "v0.2.8"); err != nil {
    t.Fatalf("EnsureUpgradeable() error = %v", err)
  }
  if err := svc.Migrate(cfg, "v0.2.6", "v0.2.8"); err != nil {
    t.Fatalf("Migrate() error = %v", err)
  }
  if !numericEqual(cfg["version"], 3) {
    t.Fatalf("version = %v, want 3", cfg["version"])
  }
}

func TestPicoClawMigrationBlocksNewConfigVersionBeyondPicoAideSupport(t *testing.T) {
  svc := testPicoClawMigrationService(t, PicoClawMigrationRuleSet{
    LatestSupportedConfigVersion: 4,
    Versions: []PicoClawMigrationVersionRule{
      {Version: "0.2.8", ConfigVersion: 3, Actions: []PicoClawMigrationRule{}},
      {
        Version:       "0.2.10",
        ConfigVersion: 4,
        ConfigChanged: true,
        FromConfig:    3,
        ToConfig:      4,
        Actions:       []PicoClawMigrationRule{{Op: "set", Path: "version", Value: 4}},
      },
    },
  })
  err := svc.EnsureUpgradeable("v0.2.8", "v0.2.10")
  if err == nil {
    t.Fatal("EnsureUpgradeable() error = nil, want unsupported config version error")
  }
  if !strings.Contains(err.Error(), "只支持到 3") {
    t.Fatalf("error = %q, want supported config version message", err.Error())
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

func testRules() PicoClawMigrationRuleSet {
  return PicoClawMigrationRuleSet{
    LatestSupportedConfigVersion: 3,
    Versions: []PicoClawMigrationVersionRule{
      {Version: "0.2.4", ConfigVersion: 1, Actions: []PicoClawMigrationRule{}},
      {
        Version:       "0.2.5",
        ConfigVersion: 2,
        ConfigChanged: true,
        FromConfig:    1,
        ToConfig:      2,
        Actions: []PicoClawMigrationRule{
          {Op: "move", Path: "channels.*.mention_only", To: "channels.*.group_trigger.mention_only"},
          {Op: "infer_model_enabled"},
        },
      },
      {Version: "0.2.6", ConfigVersion: 2, Actions: []PicoClawMigrationRule{}},
      {
        Version:       "0.2.7",
        ConfigVersion: 3,
        ConfigChanged: true,
        FromConfig:    2,
        ToConfig:      3,
        Actions: []PicoClawMigrationRule{
          {Op: "set", Path: "version", Value: 3},
          {Op: "delete", Path: "bindings"},
          {Op: "rename", From: "channels", To: "channel_list", Mode: "channels_to_nested"},
          {Op: "map", Path: "tools.mcp.servers", Field: "transport", To: "type", Value: "sse"},
        },
      },
      {Version: "0.2.8", ConfigVersion: 3, Actions: []PicoClawMigrationRule{}},
    },
  }
}

func testPicoClawMigrationService(t *testing.T, rules PicoClawMigrationRuleSet) *PicoClawMigrationService {
  t.Helper()
  return &PicoClawMigrationService{rules: rules}
}

func numericEqual(value interface{}, want int) bool {
  switch v := value.(type) {
  case int:
    return v == want
  case float64:
    return v == float64(want)
  default:
    return false
  }
}
