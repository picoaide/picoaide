package user

import (
  "os"
  "path/filepath"
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

  svc := testPicoClawMigrationService(t, `{
    "versions": [
      {
        "version": "0.2.8",
        "config_changed": true,
        "actions": [
          { "op": "set", "path": "version", "value": 3 },
          { "op": "rename", "from": "channels", "to": "channel_list", "mode": "channels_to_nested" },
          { "op": "map", "path": "tools.mcp.servers", "field": "transport", "to": "type", "value": "sse" }
        ]
      }
    ]
  }`)
  if err := svc.Migrate(cfg, "v0.2.7", "v0.2.8"); err != nil {
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

  svc := testPicoClawMigrationService(t, `{
    "versions": [
      {
        "version": "0.2.8",
        "config_changed": true,
        "actions": [
          { "op": "set", "path": "version", "value": 3 },
          { "op": "rename", "from": "channels", "to": "channel_list", "mode": "channels_to_nested" },
          { "op": "map", "path": "tools.mcp.servers", "field": "transport", "to": "type", "value": "sse" }
        ]
      }
    ]
  }`)
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
    "versions": [
      { "version": "0.2.8", "config_changed": false, "actions": [] }
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
  svc := testPicoClawMigrationService(t, `{
    "versions": [
      { "version": "0.2.8", "config_changed": false, "actions": [] }
    ]
  }`)
  if err := svc.Migrate(cfg, "v0.2.7", "v0.2.8"); err != nil {
    t.Fatalf("Migrate() error = %v", err)
  }
  if cfg["version"] != float64(3) {
    t.Fatalf("version = %v, want 3", cfg["version"])
  }
  if _, ok := cfg["channel_list"]; ok {
    t.Fatal("channel_list should not be added when config_changed=false")
  }
  if _, ok := cfg["channels"]; !ok {
    t.Fatal("channels should remain when config_changed=false")
  }
}

func TestReleasePicoClawMigrationRulesCache(t *testing.T) {
  cacheDir := t.TempDir()
  if err := ReleasePicoClawMigrationRulesCache(cacheDir); err != nil {
    t.Fatalf("ReleasePicoClawMigrationRulesCache() error = %v", err)
  }
  data, err := os.ReadFile(filepath.Join(cacheDir, picoClawMigrationCacheFile))
  if err != nil {
    t.Fatalf("read cache error = %v", err)
  }
  rules, err := parsePicoClawMigrationRules(data)
  if err != nil {
    t.Fatalf("parse cached rules error = %v", err)
  }
  if len(rules.Versions) == 0 {
    t.Fatal("cached rules should not be empty")
  }
}

func TestPicoClawMigrationSupportsCrossVersionChain(t *testing.T) {
  cfg := map[string]interface{}{}
  svc := testPicoClawMigrationService(t, `{
    "versions": [
      { "version": "0.2.8", "config_changed": false, "actions": [] },
      { "version": "0.2.9", "config_changed": true, "actions": [
        { "op": "set", "path": "version", "value": 4 }
      ] }
    ]
  }`)
  if err := svc.EnsureUpgradeable("v0.2.7", "v0.2.9"); err != nil {
    t.Fatalf("EnsureUpgradeable() error = %v", err)
  }
  if err := svc.Migrate(cfg, "v0.2.7", "v0.2.9"); err != nil {
    t.Fatalf("Migrate() error = %v", err)
  }
  if cfg["version"] != float64(4) {
    t.Fatalf("version = %v, want 4", cfg["version"])
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
