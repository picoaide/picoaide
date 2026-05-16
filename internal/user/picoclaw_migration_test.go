package user

import (
  "encoding/json"
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

func TestForceReleasePicoClawMigrationRulesCache(t *testing.T) {
  if err := ForceReleasePicoClawMigrationRulesCache(t.TempDir()); err != nil {
    t.Errorf("ForceReleasePicoClawMigrationRulesCache() error = %v", err)
  }
}

func TestReleasePicoClawMigrationRulesCacheIfValid(t *testing.T) {
  if err := ReleasePicoClawMigrationRulesCacheIfValid(t.TempDir()); err != nil {
    t.Errorf("ReleasePicoClawMigrationRulesCacheIfValid() error = %v", err)
  }
}

func TestRefreshPicoClawMigrationRulesFromURLs(t *testing.T) {
  err := RefreshPicoClawMigrationRulesFromURLs(t.TempDir(), nil)
  if err == nil {
    t.Fatal("expected error with nil URLs")
  }
}

func TestRefreshPicoClawMigrationRulesFromURLsCheck(t *testing.T) {
  changed, err := RefreshPicoClawMigrationRulesFromURLsCheck(t.TempDir(), nil)
  if err == nil {
    t.Fatal("expected error with nil URLs")
  }
  if changed {
    t.Fatal("changed should be false on error")
  }
}

func TestRefreshPicoClawMigrationRulesFromAdapter(t *testing.T) {
  err := RefreshPicoClawMigrationRulesFromAdapter(t.TempDir(), "")
  if err == nil {
    t.Fatal("expected error with empty URL")
  }
}

func TestLoadPicoClawMigrationRulesInfo(t *testing.T) {
  info, err := LoadPicoClawMigrationRulesInfo(t.TempDir())
  if err != nil {
    t.Fatalf("LoadPicoClawMigrationRulesInfo() error = %v", err)
  }
  if info.LatestSupportedConfigVersion != 3 {
    t.Errorf("LatestSupportedConfigVersion = %d, want 3", info.LatestSupportedConfigVersion)
  }
  if len(info.Versions) == 0 {
    t.Error("Versions should not be empty")
  }
}

func TestMoveByPatternNoWildcard(t *testing.T) {
  cfg := map[string]interface{}{
    "old": map[string]interface{}{
      "nested": "value",
    },
  }
  if err := moveByPattern(cfg, "old", "new"); err != nil {
    t.Fatalf("moveByPattern() error = %v", err)
  }
  if _, ok := cfg["old"]; ok {
    t.Error("old key should be removed")
  }
  v, ok := deepGet(cfg, "new.nested")
  if !ok || v != "value" {
    t.Errorf("new.nested = %v, want 'value'", v)
  }
}

func TestMoveByPatternWildcard(t *testing.T) {
  cfg := map[string]interface{}{
    "channels": map[string]interface{}{
      "telegram": map[string]interface{}{
        "setting": "val1",
      },
      "discord": map[string]interface{}{
        "setting": "val2",
      },
    },
  }
  if err := moveByPattern(cfg, "channels.*.setting", "channels.*.field"); err != nil {
    t.Fatalf("moveByPattern() error = %v", err)
  }
  if _, ok := deepGet(cfg, "channels.telegram.setting"); ok {
    t.Error("setting should be removed from telegram")
  }
  v, ok := deepGet(cfg, "channels.telegram.field")
  if !ok || v != "val1" {
    t.Errorf("telegram field = %v, want val1", v)
  }
  v, ok = deepGet(cfg, "channels.discord.field")
  if !ok || v != "val2" {
    t.Errorf("discord field = %v, want val2", v)
  }
}

func TestMoveByPatternInvalid(t *testing.T) {
  cfg := map[string]interface{}{"key": "value"}
  err := moveByPattern(cfg, "a.b", "c")
  if err == nil {
    t.Fatal("expected error for mismatched pattern lengths")
  }
}

func TestMoveByPatternNonExistent(t *testing.T) {
  cfg := map[string]interface{}{}
  if err := moveByPattern(cfg, "nonexistent", "target"); err != nil {
    t.Fatalf("moveByPattern() non-existent should be no-op: %v", err)
  }
}

func TestInferModelEnabled(t *testing.T) {
  t.Run("enables model with api_key", func(t *testing.T) {
    cfg := map[string]interface{}{
      "model_list": []interface{}{
        map[string]interface{}{
          "model_name": "gpt-4",
          "api_key":    "sk-test",
        },
      },
    }
    inferModelEnabled(cfg)
    models := cfg["model_list"].([]interface{})
    model := models[0].(map[string]interface{})
    if model["enabled"] != true {
      t.Errorf("enabled = %v, want true", model["enabled"])
    }
    if _, ok := model["api_key"]; ok {
      t.Error("api_key should be removed")
    }
    if keys, ok := model["api_keys"].([]interface{}); !ok || len(keys) != 1 || keys[0] != "sk-test" {
      t.Errorf("api_keys = %v, want [sk-test]", model["api_keys"])
    }
  })

  t.Run("enables local model", func(t *testing.T) {
    cfg := map[string]interface{}{
      "model_list": []interface{}{
        map[string]interface{}{
          "model_name": "local-model",
        },
      },
    }
    inferModelEnabled(cfg)
    models := cfg["model_list"].([]interface{})
    model := models[0].(map[string]interface{})
    if model["enabled"] != true {
      t.Errorf("local model enabled = %v, want true", model["enabled"])
    }
  })

  t.Run("preserves explicit enabled false", func(t *testing.T) {
    cfg := map[string]interface{}{
      "model_list": []interface{}{
        map[string]interface{}{
          "model_name": "gpt-4",
          "enabled":    false,
          "api_key":    "sk-test",
        },
      },
    }
    inferModelEnabled(cfg)
    models := cfg["model_list"].([]interface{})
    model := models[0].(map[string]interface{})
    if model["enabled"] != false {
      t.Errorf("enabled should preserve false, got %v", model["enabled"])
    }
  })

  t.Run("no model_list is noop", func(t *testing.T) {
    cfg := map[string]interface{}{}
    inferModelEnabled(cfg)
  })

  t.Run("non-map models are skipped", func(t *testing.T) {
    cfg := map[string]interface{}{
      "model_list": []interface{}{"string-model"},
    }
    inferModelEnabled(cfg)
  })
}

func TestHasNonEmptyAPIKeys(t *testing.T) {
  if !hasNonEmptyAPIKeys([]interface{}{"key1"}) {
    t.Error("[]interface{}{key1} should be true")
  }
  if hasNonEmptyAPIKeys([]interface{}{}) {
    t.Error("empty []interface{} should be false")
  }
  if !hasNonEmptyAPIKeys([]string{"key1"}) {
    t.Error("[]string{key1} should be true")
  }
  if hasNonEmptyAPIKeys([]string{}) {
    t.Error("empty []string should be false")
  }
  if hasNonEmptyAPIKeys(nil) {
    t.Error("nil should be false")
  }
  if hasNonEmptyAPIKeys("string") {
    t.Error("string should be false")
  }
}

func TestPicoclawConfigVersionAtLeast(t *testing.T) {
  cases := []struct {
    name       string
    cfg        map[string]interface{}
    minVersion int
    want       bool
  }{
    {"no version key", map[string]interface{}{}, 3, false},
    {"int v3 >= 3", map[string]interface{}{"version": 3}, 3, true},
    {"int v2 >= 3", map[string]interface{}{"version": 2}, 3, false},
    {"float64 v3 >= 3", map[string]interface{}{"version": float64(3)}, 3, true},
    {"float64 v2 >= 3", map[string]interface{}{"version": float64(2)}, 3, false},
    {"int64 v3 >= 3", map[string]interface{}{"version": int64(3)}, 3, true},
    {"int64 v2 >= 3", map[string]interface{}{"version": int64(2)}, 3, false},
    {"json.Number v3", map[string]interface{}{"version": json.Number("3")}, 3, true},
    {"json.Number v2", map[string]interface{}{"version": json.Number("2")}, 3, false},
    {"invalid version type", map[string]interface{}{"version": "abc"}, 3, false},
  }
  for _, tt := range cases {
    t.Run(tt.name, func(t *testing.T) {
      if got := picoclawConfigVersionAtLeast(tt.cfg, tt.minVersion); got != tt.want {
        t.Errorf("picoclawConfigVersionAtLeast() = %v, want %v", got, tt.want)
      }
    })
  }
}

func TestAutoRefreshPicoClawMigrationRules(t *testing.T) {
  // With no URLs, this returns immediately after seeding
  AutoRefreshPicoClawMigrationRules(t.TempDir(), []string{})
}

func TestMapNestedField(t *testing.T) {
  cfg := map[string]interface{}{
    "tools": map[string]interface{}{
      "mcp": map[string]interface{}{
        "servers": map[string]interface{}{
          "browser": map[string]interface{}{
            "transport": "sse",
            "enabled":   true,
          },
        },
      },
    },
  }
  if err := mapNestedField(cfg, "tools.mcp.servers", "transport", "type", "sse"); err != nil {
    t.Fatalf("mapNestedField() error = %v", err)
  }
  browser := cfg["tools"].(map[string]interface{})["mcp"].(map[string]interface{})["servers"].(map[string]interface{})["browser"].(map[string]interface{})
  if browser["type"] != "sse" {
    t.Errorf("type = %v, want sse", browser["type"])
  }
  if _, ok := browser["transport"]; ok {
    t.Error("transport should be removed")
  }
}

func TestMapNestedFieldNoTarget(t *testing.T) {
  cfg := map[string]interface{}{
    "servers": map[string]interface{}{
      "browser": map[string]interface{}{
        "transport": "sse",
      },
    },
  }
  if err := mapNestedField(cfg, "servers", "transport", "type", nil); err != nil {
    t.Fatalf("mapNestedField() error = %v", err)
  }
  browser := cfg["servers"].(map[string]interface{})["browser"].(map[string]interface{})
  if browser["type"] != "sse" {
    t.Errorf("type = %v, want sse", browser["type"])
  }
}

func TestMapNestedFieldNonMapNode(t *testing.T) {
  cfg := map[string]interface{}{
    "servers": map[string]interface{}{
      "browser": "string",
    },
  }
  if err := mapNestedField(cfg, "servers", "transport", "type", nil); err != nil {
    t.Fatalf("mapNestedField() non-map node error = %v", err)
  }
}

func TestRenameByModeChannelsToNested(t *testing.T) {
  cfg := map[string]interface{}{
    "channels": map[string]interface{}{
      "telegram": map[string]interface{}{
        "enabled": true,
        "token":   "secret",
      },
    },
  }
  if err := renameByMode(cfg, "channels", "channel_list", "channels_to_nested"); err != nil {
    t.Fatalf("renameByMode() error = %v", err)
  }
  channelList := cfg["channel_list"].(map[string]interface{})
  telegram := channelList["telegram"].(map[string]interface{})
  if telegram["type"] != "telegram" {
    t.Errorf("type = %v, want telegram", telegram["type"])
  }
  settings := telegram["settings"].(map[string]interface{})
  if settings["token"] != "secret" {
    t.Errorf("settings.token = %v, want secret", settings["token"])
  }
}

func TestRenameByModeNonMapValue(t *testing.T) {
  cfg := map[string]interface{}{
    "old_key": "string_value",
  }
  if err := renameByMode(cfg, "old_key", "new_key", "channels_to_nested"); err != nil {
    t.Fatalf("renameByMode() string value error = %v", err)
  }
  if cfg["new_key"] != "string_value" {
    t.Errorf("new_key = %v, want string_value", cfg["new_key"])
  }
}

func TestApplyPicoClawMigrationRuleSetOp(t *testing.T) {
  cfg := map[string]interface{}{}
  if err := applyPicoClawMigrationRule(cfg, PicoClawMigrationRule{Op: "set", Path: "version", Value: 3}); err != nil {
    t.Fatalf("set op error = %v", err)
  }
  if !numericEqual(cfg["version"], 3) {
    t.Errorf("version = %v, want 3", cfg["version"])
  }
}

func TestApplyPicoClawMigrationRuleDeleteOp(t *testing.T) {
  cfg := map[string]interface{}{"old": "value", "keep": "value"}
  if err := applyPicoClawMigrationRule(cfg, PicoClawMigrationRule{Op: "delete", Path: "old"}); err != nil {
    t.Fatalf("delete op error = %v", err)
  }
  if _, ok := cfg["old"]; ok {
    t.Error("old should be deleted")
  }
  if cfg["keep"] != "value" {
    t.Error("keep should be preserved")
  }
}

func TestApplyPicoClawMigrationRuleUnsupportedOp(t *testing.T) {
  err := applyPicoClawMigrationRule(nil, PicoClawMigrationRule{Op: "unsupported"})
  if err == nil {
    t.Fatal("expected error for unsupported op")
  }
}

func TestApplyPicoClawMigrationRuleMapOp(t *testing.T) {
  cfg := map[string]interface{}{
    "models": map[string]interface{}{
      "gpt4": map[string]interface{}{
        "old_field": "val1",
      },
    },
  }
  if err := applyPicoClawMigrationRule(cfg, PicoClawMigrationRule{
    Op: "map", Path: "models", Field: "old_field", To: "new_field",
  }); err != nil {
    t.Fatalf("map op error = %v", err)
  }
  gpt4 := cfg["models"].(map[string]interface{})["gpt4"].(map[string]interface{})
  if gpt4["new_field"] != "val1" {
    t.Errorf("new_field = %v, want val1", gpt4["new_field"])
  }
  if _, ok := gpt4["old_field"]; ok {
    t.Error("old_field should be removed")
  }
}

func TestApplyPicoClawMigrationRuleInferModelEnabledOp(t *testing.T) {
  cfg := map[string]interface{}{
    "model_list": []interface{}{
      map[string]interface{}{"model_name": "local-model"},
    },
  }
  if err := applyPicoClawMigrationRule(cfg, PicoClawMigrationRule{Op: "infer_model_enabled"}); err != nil {
    t.Fatalf("infer_model_enabled op error = %v", err)
  }
  models := cfg["model_list"].([]interface{})
  model := models[0].(map[string]interface{})
  if model["enabled"] != true {
    t.Error("local model should be enabled")
  }
}

func TestDeepGetEdgeCases(t *testing.T) {
  cfg := map[string]interface{}{
    "a": map[string]interface{}{
      "b": map[string]interface{}{
        "c": "value",
      },
    },
  }
  v, ok := deepGet(cfg, "a.b.c")
  if !ok || v != "value" {
    t.Errorf("deepGet(a.b.c) = %v, %v", v, ok)
  }

  // Empty path
  _, ok = deepGet(cfg, "")
  if ok {
    t.Error("empty path should return false")
  }

  // Non-existent path
  _, ok = deepGet(cfg, "x.y.z")
  if ok {
    t.Error("non-existent path should return false")
  }

  // Path with non-map intermediate
  cfg2 := map[string]interface{}{
    "a": map[string]interface{}{
      "b": "not-a-map",
    },
  }
  _, ok = deepGet(cfg2, "a.b.c")
  if ok {
    t.Error("path through non-map should return false")
  }
}

func TestSetByPathEdgeCases(t *testing.T) {
  cfg := map[string]interface{}{}

  // Empty path is no-op
  setByPath(cfg, "", "value")
  if len(cfg) != 0 {
    t.Error("empty path should be no-op")
  }

  // Deep path creates intermediate maps
  setByPath(cfg, "a.b.c", "deep-value")
  v, ok := deepGet(cfg, "a.b.c")
  if !ok || v != "deep-value" {
    t.Errorf("deepGet(a.b.c) = %v, %v", v, ok)
  }
}

func TestDeleteByPathEdgeCases(t *testing.T) {
  cfg := map[string]interface{}{
    "a": map[string]interface{}{
      "b": "value",
      "c": "keep",
    },
  }

  // Empty path is no-op
  deleteByPath(cfg, "")

  // Delete top-level key
  deleteByPath(cfg, "a.b")
  if _, ok := deepGet(cfg, "a.b"); ok {
    t.Error("a.b should be deleted")
  }
  if _, ok := deepGet(cfg, "a.c"); !ok {
    t.Error("a.c should be preserved")
  }

  // Non-existent path is no-op
  deleteByPath(cfg, "x.y.z")
}

func TestStringSetEmptyValues(t *testing.T) {
  m := stringSet([]string{"a", "", "b", " ", "c"})
  if !m["a"] || !m["b"] || !m["c"] {
    t.Error("expected a, b, c to be in set")
  }
  if len(m) != 3 {
    t.Errorf("expected 3 entries, got %d", len(m))
  }
}

func TestEnsurePicoClawChannelType(t *testing.T) {
  t.Run("sets type for channel", func(t *testing.T) {
    cfg := map[string]interface{}{
      "channel_list": map[string]interface{}{
        "dingtalk": map[string]interface{}{
          "enabled": true,
        },
      },
    }
    ensurePicoClawChannelType(cfg, "channel_list.dingtalk.settings.client_id")
    v, _ := deepGet(cfg, "channel_list.dingtalk.type")
    if v != "dingtalk" {
      t.Errorf("type = %v, want dingtalk", v)
    }
  })

  t.Run("preserves existing type", func(t *testing.T) {
    cfg := map[string]interface{}{
      "channel_list": map[string]interface{}{
        "dingtalk": map[string]interface{}{
          "enabled": true,
          "type":    "existing-type",
        },
      },
    }
    ensurePicoClawChannelType(cfg, "channel_list.dingtalk.settings.client_id")
    v, _ := deepGet(cfg, "channel_list.dingtalk.type")
    if v != "existing-type" {
      t.Errorf("type = %v, want existing-type", v)
    }
  })

  t.Run("noop for short path", func(t *testing.T) {
    cfg := map[string]interface{}{}
    ensurePicoClawChannelType(cfg, "short.path")
  })
}

func TestApplyPicoClawMigrationVersionError(t *testing.T) {
  cfg := map[string]interface{}{}
  version := PicoClawMigrationVersionRule{
    Version: "0.0.1",
    Actions: []PicoClawMigrationRule{
      {Op: "unsupported"},
    },
  }
  err := applyPicoClawMigrationVersion(cfg, version)
  if err == nil {
    t.Fatal("expected error for unsupported op inside version")
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
