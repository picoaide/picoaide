package user

import (
  "os"
  "path/filepath"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
)

func TestPicoclawImageTagFromRef(t *testing.T) {
  cases := []struct {
    name string
    ref  string
    want string
  }{
    {"simple tag", "picoaide/picoaide:v0.2.8", "v0.2.8"},
    {"full registry", "ghcr.io/picoaide/picoaide:v0.2.8", "v0.2.8"},
    {"digest only", "ghcr.io/picoaide/picoaide@sha256:abc123", ""},
    {"latest tag", "picoaide/picoaide:latest", "latest"},
    {"no tag", "picoaide/picoaide", ""},
    {"tag only no slash", "v0.2.8", ""},
    {"empty string", "", ""},
    {"digest stripped", "picoaide/picoaide:v0.2.8@sha256:abc", "v0.2.8"},
    {"only digest", "picoaide/picoaide@sha256:abc", ""},
    {"with port", "my.reg:5000/picoaide/picoaide:v1.0", "v1.0"},
    {"no slash no colon", "imagename", ""},
  }
  for _, tt := range cases {
    t.Run(tt.name, func(t *testing.T) {
      if got := picoclawImageTagFromRef(tt.ref); got != tt.want {
        t.Errorf("picoclawImageTagFromRef(%q) = %q, want %q", tt.ref, got, tt.want)
      }
    })
  }
}

func TestValueConfigured(t *testing.T) {
  cases := []struct {
    name  string
    value interface{}
    want  bool
  }{
    {"non-empty string", "hello", true},
    {"empty string", "", false},
    {"whitespace string", "  ", false},
    {"nil", nil, false},
    {"integer", 42, true},
    {"bool true", true, true},
    {"zero int", 0, true},
    {"float", 3.14, true},
    {"slice", []string{"a"}, true},
    {"map", map[string]interface{}{}, true},
  }
  for _, tt := range cases {
    t.Run(tt.name, func(t *testing.T) {
      if got := valueConfigured(tt.value); got != tt.want {
        t.Errorf("valueConfigured(%#v) = %v, want %v", tt.value, got, tt.want)
      }
    })
  }
}

func TestCoercePicoClawFieldValue(t *testing.T) {
  t.Run("boolean", func(t *testing.T) {
    field := PicoClawConfigField{Key: "enabled", Type: "boolean"}
    v, err := coercePicoClawFieldValue(field, true)
    if err != nil || v != true {
      t.Fatalf("bool(true) = %v, %v", v, err)
    }
    v, err = coercePicoClawFieldValue(field, "true")
    if err != nil || v != true {
      t.Fatalf("bool('true') = %v, %v", v, err)
    }
    v, err = coercePicoClawFieldValue(field, "")
    if err != nil || v != false {
      t.Fatalf("bool('') = %v, %v", v, err)
    }
    _, err = coercePicoClawFieldValue(field, 123)
    if err == nil {
      t.Fatal("bool(123) should error")
    }
    _, err = coercePicoClawFieldValue(field, "notbool")
    if err == nil {
      t.Fatal("bool('notbool') should error")
    }
  })

  t.Run("number", func(t *testing.T) {
    field := PicoClawConfigField{Key: "port", Type: "number"}
    v, err := coercePicoClawFieldValue(field, float64(8080))
    if err != nil || v != float64(8080) {
      t.Fatalf("number(float64) = %v, %v", v, err)
    }
    v, err = coercePicoClawFieldValue(field, 8080)
    if err != nil || v != 8080 {
      t.Fatalf("number(int) = %v, %v", v, err)
    }
    v, err = coercePicoClawFieldValue(field, "8080")
    if err != nil || v != int64(8080) {
      t.Fatalf("number('8080') = %v, %v", v, err)
    }
    v, err = coercePicoClawFieldValue(field, "80.5")
    if err != nil || v != float64(80.5) {
      t.Fatalf("number('80.5') = %v, %v", v, err)
    }
    v, err = coercePicoClawFieldValue(field, "")
    if err != nil || v != nil {
      t.Fatalf("number('') = %v, %v", v, err)
    }
    _, err = coercePicoClawFieldValue(field, true)
    if err == nil {
      t.Fatal("number(true) should error")
    }
  })

  t.Run("integer", func(t *testing.T) {
    field := PicoClawConfigField{Key: "count", Type: "int"}
    v, err := coercePicoClawFieldValue(field, float64(5))
    if err != nil || v != int64(5) {
      t.Fatalf("int(float64) = %v, %v", v, err)
    }
    v, err = coercePicoClawFieldValue(field, int64(5))
    if err != nil || v != int64(5) {
      t.Fatalf("int(int64) = %v, %v", v, err)
    }
    v, err = coercePicoClawFieldValue(field, int32(3))
    if err != nil || v != int32(3) {
      t.Fatalf("int(int32) = %v, %v", v, err)
    }
    v, err = coercePicoClawFieldValue(field, "5")
    if err != nil || v != int64(5) {
      t.Fatalf("int('5') = %v, %v", v, err)
    }
    _, err = coercePicoClawFieldValue(field, "abc")
    if err == nil {
      t.Fatal("int('abc') should error")
    }
    _, err = coercePicoClawFieldValue(field, true)
    if err == nil {
      t.Fatal("int(true) should error")
    }
  })

  t.Run("string_list", func(t *testing.T) {
    field := PicoClawConfigField{Key: "items", Type: "list"}
    v, err := coercePicoClawFieldValue(field, []interface{}{"a", "b"})
    if err != nil {
      t.Fatalf("list([]interface{}) error = %v", err)
    }
    if arr, ok := v.([]interface{}); !ok || len(arr) != 2 {
      t.Fatalf("list([]interface{}) = %#v", v)
    }

    v, err = coercePicoClawFieldValue(field, []string{"a", "b"})
    if err != nil {
      t.Fatalf("list([]string) error = %v", err)
    }
    if strs, ok := v.([]string); !ok || len(strs) != 2 {
      t.Fatalf("list([]string) = %#v", v)
    }

    v, err = coercePicoClawFieldValue(field, "")
    if err != nil {
      t.Fatalf("list('') error = %v", err)
    }
    if strs, ok := v.([]string); !ok || len(strs) != 0 {
      t.Fatalf("list('') = %#v", v)
    }

    v, err = coercePicoClawFieldValue(field, `["a","b"]`)
    if err != nil {
      t.Fatalf("list(json) error = %v", err)
    }
    if arr, ok := v.([]interface{}); !ok || len(arr) != 2 {
      t.Fatalf("list(json) = %#v", v)
    }

    v, err = coercePicoClawFieldValue(field, "a,b")
    if err != nil || len(v.([]string)) != 2 {
      t.Fatalf("list('a,b') = %#v, %v", v, err)
    }

    v, err = coercePicoClawFieldValue(field, "a\nb\nc")
    if err != nil || len(v.([]string)) != 3 {
      t.Fatalf("list('a\\nb\\nc') = %#v, %v", v, err)
    }

    _, err = coercePicoClawFieldValue(field, 123)
    if err == nil {
      t.Fatal("list(123) should error")
    }
  })

  t.Run("json", func(t *testing.T) {
    field := PicoClawConfigField{Key: "data", Type: "json"}
    v, err := coercePicoClawFieldValue(field, map[string]interface{}{"a": float64(1)})
    if err != nil {
      t.Fatalf("json(map) error = %v", err)
    }
    if m, ok := v.(map[string]interface{}); !ok || m["a"] != float64(1) {
      t.Fatalf("json(map) = %#v", v)
    }

    _, err = coercePicoClawFieldValue(field, `{"a":1}`)
    if err != nil {
      t.Fatalf("json(string) error = %v", err)
    }

    v, err = coercePicoClawFieldValue(field, "")
    if err != nil {
      t.Fatalf("json('') error = %v", err)
    }
    if _, ok := v.(map[string]interface{}); !ok {
      t.Fatalf("json('') type = %T", v)
    }

    _, err = coercePicoClawFieldValue(field, 123)
    if err == nil {
      t.Fatal("json(123) should error")
    }
  })

  t.Run("default string field", func(t *testing.T) {
    field := PicoClawConfigField{Key: "name", Type: "string"}
    v, err := coercePicoClawFieldValue(field, nil)
    if err != nil || v != "" {
      t.Fatalf("default(nil) = %v, %v", v, err)
    }
    v, err = coercePicoClawFieldValue(field, "hello")
    if err != nil || v != "hello" {
      t.Fatalf("default('hello') = %v, %v", v, err)
    }
    v, err = coercePicoClawFieldValue(field, 42)
    if err != nil || v != 42 {
      t.Fatalf("default(42) = %v, %v", v, err)
    }
  })
}

func TestUserChannelState(t *testing.T) {
  configMap := map[string]interface{}{
    "channel_list": map[string]interface{}{
      "dingtalk": map[string]interface{}{
        "enabled": true,
        "type":    "dingtalk",
        "settings": map[string]interface{}{
          "client_id": "abc",
        },
      },
    },
  }
  securityMap := map[string]interface{}{
    "channel_list": map[string]interface{}{
      "dingtalk": map[string]interface{}{
        "client_secret": "secret-val",
      },
    },
  }
  fields := []PicoClawConfigField{
    {Key: "enabled", Type: "boolean", Storage: "config", Path: "channel_list.dingtalk.enabled"},
    {Key: "client_id", Type: "string", Storage: "config", Path: "channel_list.dingtalk.settings.client_id"},
    {Key: "client_secret", Type: "string", Storage: "security", Path: "channel_list.dingtalk.client_secret"},
  }

  enabled, configured := userChannelState(configMap, securityMap, "dingtalk", fields)
  if !enabled {
    t.Errorf("enabled = false, want true")
  }
  if !configured {
    t.Errorf("configured = false, want true")
  }

  enabled, configured = userChannelState(configMap, securityMap, "", fields)
  if enabled || configured {
    t.Errorf("empty section key should return false, false")
  }

  fields2 := []PicoClawConfigField{
    {Key: "enabled", Type: "boolean", Storage: "config", Path: "channel_list.dingtalk.enabled"},
    {Key: "client_secret", Type: "string", Storage: "security", Path: "channel_list.dingtalk.client_secret", Secret: true},
  }
  _, configured = userChannelState(configMap, securityMap, "dingtalk", fields2)
  if configured {
    t.Errorf("configured should be false when only secret field is set")
  }

  notEnabledMap := map[string]interface{}{
    "channel_list": map[string]interface{}{
      "dingtalk": map[string]interface{}{
        "enabled": false,
      },
    },
  }
  enabled, _ = userChannelState(notEnabledMap, nil, "dingtalk", fields)
  if enabled {
    t.Errorf("enabled should be false")
  }

  noValueMap := map[string]interface{}{}
  _, configured = userChannelState(noValueMap, nil, "dingtalk", fields)
  if configured {
    t.Errorf("configured should be false when no values set")
  }
}

func TestListPicoClawAdminChannels(t *testing.T) {
  channels, err := ListPicoClawAdminChannels(t.TempDir(), 3)
  if err != nil {
    t.Fatalf("ListPicoClawAdminChannels() error = %v", err)
  }
  if len(channels) == 0 {
    t.Error("should return at least one channel")
  }
  found := false
  for _, ch := range channels {
    if ch.Key == "dingtalk" {
      found = true
      if !ch.Allowed {
        t.Error("dingtalk should be allowed")
      }
    }
  }
  if !found {
    t.Error("dingtalk channel should be in the list")
  }
}

func TestListPicoClawAdminChannelsUnsupportedVersion(t *testing.T) {
  _, err := ListPicoClawAdminChannels(t.TempDir(), 99)
  if err == nil {
    t.Fatal("expected error for unsupported config version")
  }
}

func TestReadPicoClawJSONMap(t *testing.T) {
  t.Run("non-existent file", func(t *testing.T) {
    tmpDir := t.TempDir()
    root, err := os.OpenRoot(tmpDir)
    if err != nil {
      t.Fatal(err)
    }
    defer root.Close()
    m, err := readPicoClawJSONMap(root)
    if err != nil {
      t.Fatalf("readPicoClawJSONMap() error = %v", err)
    }
    if len(m) != 0 {
      t.Errorf("map should be empty, got %v", m)
    }
  })

  t.Run("empty file", func(t *testing.T) {
    tmpDir := t.TempDir()
    if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte{}, 0644); err != nil {
      t.Fatal(err)
    }
    root, err := os.OpenRoot(tmpDir)
    if err != nil {
      t.Fatal(err)
    }
    defer root.Close()
    m, err := readPicoClawJSONMap(root)
    if err != nil {
      t.Fatalf("readPicoClawJSONMap() error = %v", err)
    }
    if len(m) != 0 {
      t.Errorf("map should be empty, got %v", m)
    }
  })

  t.Run("invalid JSON", func(t *testing.T) {
    tmpDir := t.TempDir()
    if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte("{invalid"), 0644); err != nil {
      t.Fatal(err)
    }
    root, err := os.OpenRoot(tmpDir)
    if err != nil {
      t.Fatal(err)
    }
    defer root.Close()
    _, err = readPicoClawJSONMap(root)
    if err == nil {
      t.Fatal("expected error for invalid JSON")
    }
  })
}

func TestReadPicoClawYAMLMap(t *testing.T) {
  t.Run("non-existent file", func(t *testing.T) {
    tmpDir := t.TempDir()
    root, err := os.OpenRoot(tmpDir)
    if err != nil {
      t.Fatal(err)
    }
    defer root.Close()
    m, err := readPicoClawYAMLMap(root)
    if err != nil {
      t.Fatalf("readPicoClawYAMLMap() error = %v", err)
    }
    if len(m) != 0 {
      t.Errorf("map should be empty, got %v", m)
    }
  })

  t.Run("empty file", func(t *testing.T) {
    tmpDir := t.TempDir()
    if err := os.WriteFile(filepath.Join(tmpDir, ".security.yml"), []byte{}, 0644); err != nil {
      t.Fatal(err)
    }
    root, err := os.OpenRoot(tmpDir)
    if err != nil {
      t.Fatal(err)
    }
    defer root.Close()
    m, err := readPicoClawYAMLMap(root)
    if err != nil {
      t.Fatalf("readPicoClawYAMLMap() error = %v", err)
    }
    if len(m) != 0 {
      t.Errorf("map should be empty, got %v", m)
    }
  })

  t.Run("invalid YAML", func(t *testing.T) {
    tmpDir := t.TempDir()
    if err := os.WriteFile(filepath.Join(tmpDir, ".security.yml"), []byte(": bad yaml :"), 0644); err != nil {
      t.Fatal(err)
    }
    root, err := os.OpenRoot(tmpDir)
    if err != nil {
      t.Fatal(err)
    }
    defer root.Close()
    _, err = readPicoClawYAMLMap(root)
    if err == nil {
      t.Fatal("expected error for invalid YAML")
    }
  })
}

func TestListPicoClawUserChannels(t *testing.T) {
  testInitAuthDB(t)
  tmpDir := t.TempDir()
  config.DefaultWorkDir = tmpDir
  cfg := &config.GlobalConfig{
    UsersRoot: filepath.Join(tmpDir, "users"),
    PicoClaw: map[string]interface{}{
      "channel_list": map[string]interface{}{
        "dingtalk": map[string]interface{}{"enabled": true, "type": "dingtalk"},
        "discord":  map[string]interface{}{"enabled": true, "type": "discord"},
      },
    },
  }

  picoclawDir := filepath.Join(UserDir(cfg, "testuser"), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(filepath.Join(picoclawDir, "config.json"), []byte(`{"version":3}`), 0644); err != nil {
    t.Fatal(err)
  }

  // Need container record for supportedPicoClawChannelsForUser
  if err := auth.UpsertContainer(&auth.ContainerRecord{Username: "testuser", Status: "stopped", IP: "100.64.0.2"}); err != nil {
    t.Fatalf("UpsertContainer: %v", err)
  }

  channels, err := ListPicoClawUserChannels(cfg, "testuser", 3)
  if err != nil {
    t.Fatalf("ListPicoClawUserChannels() error = %v", err)
  }
  if len(channels) == 0 {
    t.Error("should return at least one channel")
  }
  found := false
  for _, ch := range channels {
    if ch.Key == "dingtalk" {
      found = true
      if !ch.Allowed {
        t.Error("dingtalk should be allowed")
      }
    }
  }
  if !found {
    t.Error("dingtalk should be in the list")
  }
}

func TestListPicoClawUserChannelsInvalidUsername(t *testing.T) {
  _, err := ListPicoClawUserChannels(nil, "", 0)
  if err == nil {
    t.Fatal("expected error for empty username")
  }
}

func TestAllowedPicoClawChannelsFromConfig(t *testing.T) {
  t.Run("nil config", func(t *testing.T) {
    m := allowedPicoClawChannelsFromConfig(nil)
    if len(m) != 0 {
      t.Error("nil config should return empty map")
    }
  })

  t.Run("enabled channels", func(t *testing.T) {
    cfg := &config.GlobalConfig{
      PicoClaw: map[string]interface{}{
        "channel_list": map[string]interface{}{
          "dingtalk": map[string]interface{}{"enabled": true, "type": "dingtalk"},
          "discord":  map[string]interface{}{"enabled": false, "type": "discord"},
        },
      },
    }
    m := allowedPicoClawChannelsFromConfig(cfg)
    if !m["dingtalk"] {
      t.Error("dingtalk should be allowed")
    }
    if m["discord"] {
      t.Error("discord should not be allowed")
    }
  })
}

func TestNormalizePicoClawFieldStorage(t *testing.T) {
  if got := normalizePicoClawFieldStorage("security"); got != "security" {
    t.Errorf("normalizePicoClawFieldStorage('security') = %q, want 'security'", got)
  }
  if got := normalizePicoClawFieldStorage("SECURITY"); got != "security" {
    t.Errorf("normalizePicoClawFieldStorage('SECURITY') = %q, want 'security'", got)
  }
  if got := normalizePicoClawFieldStorage("config"); got != "config" {
    t.Errorf("normalizePicoClawFieldStorage('config') = %q, want 'config'", got)
  }
  if got := normalizePicoClawFieldStorage(""); got != "config" {
    t.Errorf("normalizePicoClawFieldStorage('') = %q, want 'config'", got)
  }
  if got := normalizePicoClawFieldStorage("anything"); got != "config" {
    t.Errorf("normalizePicoClawFieldStorage('anything') = %q, want 'config'", got)
  }
}
