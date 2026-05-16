package user

import (
  "archive/zip"
  "bytes"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "net/http"
  "net/http/httptest"
  "os"
  "path/filepath"
  "strings"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
)

func TestParsePicoClawAdapterHashRejectsUnsafePath(t *testing.T) {
  _, err := ParsePicoClawAdapterHash([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  ../index.json\n"))
  if err == nil {
    t.Fatal("ParsePicoClawAdapterHash() error = nil, want unsafe path error")
  }
}

func TestLoadPicoClawAdapterPackageBundled(t *testing.T) {
  pkg, err := NewPicoClawAdapterPackage(t.TempDir())
  if err != nil {
    t.Fatalf("NewPicoClawAdapterPackage() error = %v", err)
  }
  if pkg.Index.LatestSupportedConfigVersion != 3 {
    t.Fatalf("LatestSupportedConfigVersion = %d, want 3", pkg.Index.LatestSupportedConfigVersion)
  }
  rules := pkg.ToMigrationRuleSet()
  if len(rules.Versions) != 5 {
    t.Fatalf("versions = %d, want 5", len(rules.Versions))
  }
  var found027 bool
  for _, version := range rules.Versions {
    if version.Version == "0.2.7" {
      found027 = true
      if !version.ConfigChanged || version.FromConfig != 2 || version.ToConfig != 3 {
        t.Fatalf("0.2.7 migration = %+v", version)
      }
    }
  }
  if !found027 {
    t.Fatal("0.2.7 should exist")
  }
}

func TestRefreshPicoClawAdapterFromRemoteVerifiesHashes(t *testing.T) {
  auth.ResetDB()
  if err := auth.InitDB(t.TempDir()); err != nil {
    t.Fatalf("InitDB() error = %v", err)
  }
  defer auth.ResetDB()

  files := map[string]string{
    "index.json": `{
  "adapter_schema_version": 1,
  "adapter_version": "test",
  "latest_supported_config_version": 1,
  "picoclaw_versions": [{"version":"0.2.4","config_version":1}],
  "config_schemas": {"1":"schemas/config-v1.json"},
  "ui_schemas": {"1":"ui/ui-v1.json"},
  "migrations": []
}`,
    "schemas/config-v1.json": `{
  "config_version": 1,
  "channels_path": "channels",
  "channel_settings_path": "channels.*",
  "models_path": "model_list",
  "default_model_path": "agents.defaults.model",
  "security": {"channels_path":"channels","channel_settings_path":"channels.*","models_path":"model_list"},
  "singleton_channels": [],
  "channel_types": ["dingtalk"]
}`,
    "ui/ui-v1.json": `{"config_version":1,"pages":[{"key":"channels","label":"Channels","sections":[{"key":"dingtalk","label":"钉钉","fields":[{"key":"enabled","label":"启用","type":"boolean","storage":"config","path":"channels.dingtalk.enabled"}]}]}]}`,
  }
  hash := ""
  for _, name := range []string{"index.json", "schemas/config-v1.json", "ui/ui-v1.json"} {
    hash += sha256String(files[name]) + "  " + name + "\n"
  }
  server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    rel := r.URL.Path[1:]
    if rel == "hash" {
      _, _ = w.Write([]byte(hash))
      return
    }
    if body, ok := files[rel]; ok {
      _, _ = w.Write([]byte(body))
      return
    }
    http.NotFound(w, r)
  }))
  defer server.Close()

  cacheDir := t.TempDir()
  pkg, err := RefreshPicoClawAdapterFromRemote(cacheDir, server.URL, server.Client())
  if err != nil {
    t.Fatalf("RefreshPicoClawAdapterFromRemote() error = %v", err)
  }
  if pkg.Index.AdapterVersion != "test" {
    t.Fatalf("AdapterVersion = %q", pkg.Index.AdapterVersion)
  }
  // 验证数据写入 DB
  engine, err := auth.GetEngine()
  if err != nil {
    t.Fatalf("GetEngine() error = %v", err)
  }
  record := &auth.PicoclawAdapterPackage{}
  has, err := engine.Desc("id").Get(record)
  if err != nil || !has {
    t.Fatalf("DB record not found: err=%v, has=%v", err, has)
  }
  if record.AdapterVersion != "test" {
    t.Fatalf("DB AdapterVersion = %q, want %q", record.AdapterVersion, "test")
  }
}

func TestSavePicoClawAdapterZipInstallsPackage(t *testing.T) {
  auth.ResetDB()
  if err := auth.InitDB(t.TempDir()); err != nil {
    t.Fatalf("InitDB() error = %v", err)
  }
  defer auth.ResetDB()

  files := testAdapterFiles()
  cacheDir := t.TempDir()
  pkg, err := SavePicoClawAdapterZip(cacheDir, buildAdapterZip(t, files))
  if err != nil {
    t.Fatalf("SavePicoClawAdapterZip() error = %v", err)
  }
  if pkg.Index.AdapterVersion != "test" {
    t.Fatalf("AdapterVersion = %q", pkg.Index.AdapterVersion)
  }
  // 验证数据写入 DB
  engine, err := auth.GetEngine()
  if err != nil {
    t.Fatalf("GetEngine() error = %v", err)
  }
  record := &auth.PicoclawAdapterPackage{}
  has, err := engine.Desc("id").Get(record)
  if err != nil || !has {
    t.Fatalf("DB record not found: err=%v, has=%v", err, has)
  }
  if record.AdapterVersion != "test" {
    t.Fatalf("DB AdapterVersion = %q, want %q", record.AdapterVersion, "test")
  }
}

func TestSavePicoClawAdapterZipRejectsTopLevelDirectory(t *testing.T) {
  files := map[string]string{}
  for name, body := range testAdapterFiles() {
    files["picoclaw/"+name] = body
  }
  _, err := SavePicoClawAdapterZip(t.TempDir(), buildAdapterZip(t, files))
  if err == nil {
    t.Fatal("SavePicoClawAdapterZip() error = nil, want top-level directory error")
  }
}

func TestForceReleasePicoClawAdapterCache(t *testing.T) {
  if err := ForceReleasePicoClawAdapterCache(t.TempDir()); err != nil {
    t.Errorf("ForceReleasePicoClawAdapterCache() error = %v", err)
  }
}

func TestLoadPicoClawAdapterPackage(t *testing.T) {
  tmpDir := t.TempDir()
  files := testAdapterFiles()
  for name, content := range files {
    dir := filepath.Dir(name)
    if dir != "." {
      if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
        t.Fatal(err)
      }
    }
    if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
      t.Fatal(err)
    }
  }

  pkg, err := LoadPicoClawAdapterPackage(tmpDir)
  if err != nil {
    t.Fatalf("LoadPicoClawAdapterPackage() error = %v", err)
  }
  if pkg.Index.AdapterVersion != "test" {
    t.Errorf("AdapterVersion = %q, want test", pkg.Index.AdapterVersion)
  }
}

func TestLoadPicoClawAdapterPackageMissingIndex(t *testing.T) {
  _, err := LoadPicoClawAdapterPackage(t.TempDir())
  if err == nil {
    t.Fatal("expected error for missing index.json")
  }
}

func TestLoadPicoClawAdapterPackageInvalidIndex(t *testing.T) {
  tmpDir := t.TempDir()
  if err := os.WriteFile(filepath.Join(tmpDir, "index.json"), []byte("{invalid"), 0644); err != nil {
    t.Fatal(err)
  }
  _, err := LoadPicoClawAdapterPackage(tmpDir)
  if err == nil {
    t.Fatal("expected error for invalid index.json")
  }
}

func TestVerifyPicoClawAdapterHash(t *testing.T) {
  tmpDir := t.TempDir()
  files := testAdapterFiles()
  for name, content := range files {
    dir := filepath.Dir(name)
    if dir != "." {
      if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
        t.Fatal(err)
      }
    }
    if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
      t.Fatal(err)
    }
  }

  if err := VerifyPicoClawAdapterHash(tmpDir); err != nil {
    t.Fatalf("VerifyPicoClawAdapterHash() error = %v", err)
  }
}

func TestVerifyPicoClawAdapterHashMissingHash(t *testing.T) {
  err := VerifyPicoClawAdapterHash(t.TempDir())
  if err == nil {
    t.Fatal("expected error for missing hash file")
  }
}

func TestVerifyPicoClawAdapterHashMismatch(t *testing.T) {
  tmpDir := t.TempDir()
  files := testAdapterFiles()
  files["index.json"] = `{"modified": "content"}`
  for name, content := range files {
    dir := filepath.Dir(name)
    if dir != "." {
      if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
        t.Fatal(err)
      }
    }
    if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
      t.Fatal(err)
    }
  }

  err := VerifyPicoClawAdapterHash(tmpDir)
  if err == nil {
    t.Fatal("expected hash mismatch error")
  }
}

func TestRefreshPicoClawAdapterFromRemoteIfChanged(t *testing.T) {
  auth.ResetDB()
  if err := auth.InitDB(t.TempDir()); err != nil {
    t.Fatalf("InitDB() error = %v", err)
  }
  defer auth.ResetDB()

  files := testAdapterFiles()
  hash := ""
  for _, name := range []string{"index.json", "schemas/config-v1.json", "ui/ui-v1.json"} {
    hash += sha256String(files[name]) + "  " + name + "\n"
  }

  server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    rel := r.URL.Path[1:]
    if rel == "hash" {
      _, _ = w.Write([]byte(hash))
      return
    }
    if body, ok := files[rel]; ok {
      _, _ = w.Write([]byte(body))
      return
    }
    http.NotFound(w, r)
  }))
  defer server.Close()

  cacheDir := t.TempDir()
  pkg, changed, err := RefreshPicoClawAdapterFromRemoteIfChanged(cacheDir, []string{server.URL}, server.Client())
  if err != nil {
    t.Fatalf("RefreshPicoClawAdapterFromRemoteIfChanged() error = %v", err)
  }
  if !changed {
    t.Fatal("should report changed on first fetch")
  }
  if pkg.Index.AdapterVersion != "test" {
    t.Fatalf("AdapterVersion = %q", pkg.Index.AdapterVersion)
  }

  _, changed, err = RefreshPicoClawAdapterFromRemoteIfChanged(cacheDir, []string{server.URL}, server.Client())
  if err != nil {
    t.Fatalf("second fetch error = %v", err)
  }
  if changed {
    t.Fatal("second fetch should report no change")
  }
}

func TestRefreshPicoClawAdapterFromRemoteIfChangedEmptyURLs(t *testing.T) {
  _, _, err := RefreshPicoClawAdapterFromRemoteIfChanged(t.TempDir(), nil, nil)
  if err == nil {
    t.Fatal("expected error for empty URLs")
  }
}

func TestPicoClawAdapterPackageValidate(t *testing.T) {
  t.Run("missing schema version", func(t *testing.T) {
    pkg := &PicoClawAdapterPackage{
      Index: PicoClawAdapterIndex{
        AdapterSchemaVersion: 0,
      },
    }
    if err := pkg.Validate(); err == nil {
      t.Fatal("expected error for missing schema version")
    }
  })

  t.Run("unsupported schema version", func(t *testing.T) {
    pkg := &PicoClawAdapterPackage{
      Index: PicoClawAdapterIndex{
        AdapterSchemaVersion: PicoAideSupportedAdapterSchemaVersion + 1,
      },
    }
    if err := pkg.Validate(); err == nil {
      t.Fatal("expected error for unsupported schema version")
    }
  })

  t.Run("missing config version", func(t *testing.T) {
    pkg := &PicoClawAdapterPackage{
      Index: PicoClawAdapterIndex{
        AdapterSchemaVersion:         1,
        LatestSupportedConfigVersion: 0,
      },
    }
    if err := pkg.Validate(); err == nil {
      t.Fatal("expected error for missing config version")
    }
  })

  t.Run("missing picoclaw versions", func(t *testing.T) {
    pkg := &PicoClawAdapterPackage{
      Index: PicoClawAdapterIndex{
        AdapterSchemaVersion:         1,
        LatestSupportedConfigVersion: 1,
        PicoClawVersions:             nil,
      },
    }
    if err := pkg.Validate(); err == nil {
      t.Fatal("expected error for missing versions")
    }
  })

  t.Run("invalid version format", func(t *testing.T) {
    pkg := &PicoClawAdapterPackage{
      Index: PicoClawAdapterIndex{
        AdapterSchemaVersion:         1,
        LatestSupportedConfigVersion: 1,
        PicoClawVersions: []PicoClawAdapterVersion{
          {Version: "invalid", ConfigVersion: 1},
        },
      },
      ConfigSchemas: map[int]PicoClawConfigSchema{1: {ConfigVersion: 1}},
      UISchemas:     map[int]PicoClawUISchema{1: {ConfigVersion: 1}},
    }
    if err := pkg.Validate(); err == nil {
      t.Fatal("expected error for invalid version format")
    }
  })

  t.Run("missing config schema", func(t *testing.T) {
    pkg := &PicoClawAdapterPackage{
      Index: PicoClawAdapterIndex{
        AdapterSchemaVersion:         1,
        LatestSupportedConfigVersion: 2,
        PicoClawVersions: []PicoClawAdapterVersion{
          {Version: "0.2.4", ConfigVersion: 2},
        },
      },
      ConfigSchemas: map[int]PicoClawConfigSchema{1: {ConfigVersion: 1}},
      UISchemas:     map[int]PicoClawUISchema{1: {ConfigVersion: 1}, 2: {ConfigVersion: 2}},
    }
    if err := pkg.Validate(); err == nil {
      t.Fatal("expected error for missing config schema")
    }
  })
}

func TestPicoClawAdapterChannelTypesFor(t *testing.T) {
  pkg := &PicoClawAdapterPackage{
    Index: PicoClawAdapterIndex{
      PicoClawVersions: []PicoClawAdapterVersion{
        {Version: "0.2.8", ConfigVersion: 3, ChannelTypes: []string{"dingtalk", "telegram"}},
      },
    },
    ConfigSchemas: map[int]PicoClawConfigSchema{3: {
      ChannelTypes: []string{"dingtalk", "telegram", "discord"},
    }},
  }

  // Nil receiver
  if got := (*PicoClawAdapterPackage)(nil).ChannelTypesFor(3, "v0.2.8"); got != nil {
    t.Errorf("nil receiver should return nil, got %v", got)
  }

  // Version-specific channels
  channels := pkg.ChannelTypesFor(3, "v0.2.8")
  if len(channels) != 2 || channels[0] != "dingtalk" {
    t.Errorf("expected [dingtalk telegram], got %v", channels)
  }

  // Unknown version falls back to schema channels
  channels = pkg.ChannelTypesFor(3, "v99.99")
  if len(channels) != 3 {
    t.Errorf("expected 3 schema channels, got %d", len(channels))
  }

  // Unknown config version
  channels = pkg.ChannelTypesFor(99, "")
  if channels != nil {
    t.Errorf("expected nil for unknown config version, got %v", channels)
  }
}

func TestChannelTypesFor(t *testing.T) {
  pkg, err := NewPicoClawAdapterPackage(t.TempDir())
  if err != nil {
    t.Fatalf("NewPicoClawAdapterPackage() error = %v", err)
  }
  channels := pkg.ChannelTypesFor(3, "v0.2.8")
  if len(channels) == 0 {
    t.Error("should return channels for v0.2.8")
  }
}

func TestParsePicoClawAdapterFilesMissingIndex(t *testing.T) {
  _, err := parsePicoClawAdapterFiles(map[string][]byte{})
  if err == nil {
    t.Fatal("expected error for missing index.json")
  }
}

func TestParsePicoClawAdapterFilesInvalidIndex(t *testing.T) {
  _, err := parsePicoClawAdapterFiles(map[string][]byte{
    "index.json": []byte("{invalid}"),
  })
  if err == nil {
    t.Fatal("expected error for invalid index.json")
  }
}

func TestParsePicoClawAdapterFilesMissingSchemaFile(t *testing.T) {
  _, err := parsePicoClawAdapterFiles(map[string][]byte{
    "index.json": []byte(`{
      "adapter_schema_version": 1,
      "adapter_version": "test",
      "latest_supported_config_version": 1,
      "picoclaw_versions": [{"version":"0.2.4","config_version":1}],
      "config_schemas": {"1":"missing.json"},
      "ui_schemas": {"1":"ui.json"},
      "migrations": []
    }`),
    "ui.json": []byte(`{"config_version":1,"pages":[]}`),
  })
  if err == nil {
    t.Fatal("expected error for missing schema file")
  }
}

func TestParsePicoClawAdapterFilesMissingUISchema(t *testing.T) {
  _, err := parsePicoClawAdapterFiles(map[string][]byte{
    "index.json": []byte(`{
      "adapter_schema_version": 1,
      "adapter_version": "test",
      "latest_supported_config_version": 1,
      "picoclaw_versions": [{"version":"0.2.4","config_version":1}],
      "config_schemas": {"1":"config.json"},
      "ui_schemas": {"1":"missing.json"},
      "migrations": []
    }`),
    "config.json": []byte(`{"config_version":1,"channel_types":["dingtalk"]}`),
  })
  if err == nil {
    t.Fatal("expected error for missing UI schema file")
  }
}

func TestPicoClawAdapterPackageLoadReferencedFiles(t *testing.T) {
  root := t.TempDir()
  if err := os.WriteFile(filepath.Join(root, "index.json"), []byte(`{
    "adapter_schema_version": 1,
    "adapter_version": "test",
    "latest_supported_config_version": 1,
    "picoclaw_versions": [{"version":"0.2.4","config_version":1}],
    "config_schemas": {"1":"nonexistent/config-v1.json"},
    "ui_schemas": {"1":"ui/ui-v1.json"},
    "migrations": []
  }`), 0644); err != nil {
    t.Fatal(err)
  }
  pkg := &PicoClawAdapterPackage{
    Root:  root,
    Index: PicoClawAdapterIndex{},
  }
  // Re-read the index
  indexData, err := os.ReadFile(filepath.Join(root, "index.json"))
  if err != nil {
    t.Fatal(err)
  }
  if err := json.Unmarshal(indexData, &pkg.Index); err != nil {
    t.Fatal(err)
  }
  pkg.ConfigSchemas = make(map[int]PicoClawConfigSchema)
  pkg.UISchemas = make(map[int]PicoClawUISchema)
  pkg.Migrations = make(map[string]PicoClawConfigMigration)
  err = pkg.loadReferencedFiles()
  if err == nil {
    t.Fatal("expected error for missing referenced file")
  }
}

func TestValidatePicoClawAdapterRelPath(t *testing.T) {
  cases := []struct {
    path    string
    wantErr bool
  }{
    {"schemas/config.json", false},
    {"../outside.json", true},
    {"/absolute.json", true},
    {"hash", true},
    {"", true},
    {".", true},
    {"..", true},
    {"dir/subdir/file.json", false},
  }
  for _, tt := range cases {
    _, err := validatePicoClawAdapterRelPath(tt.path)
    if (err != nil) != tt.wantErr {
      t.Errorf("validatePicoClawAdapterRelPath(%q) error = %v, wantErr = %v", tt.path, err, tt.wantErr)
    }
  }
}

func TestSavePicoClawAdapterZipRejectsHashMismatch(t *testing.T) {
  files := testAdapterFiles()
  files["hash"] = strings.Replace(files["hash"], "index.json", "missing.json", 1)
  _, err := SavePicoClawAdapterZip(t.TempDir(), buildAdapterZip(t, files))
  if err == nil {
    t.Fatal("SavePicoClawAdapterZip() error = nil, want hash mismatch error")
  }
}

func sha256String(value string) string {
  sum := sha256.Sum256([]byte(value))
  return hex.EncodeToString(sum[:])
}

func testAdapterFiles() map[string]string {
  files := map[string]string{
    "index.json": `{
  "adapter_schema_version": 1,
  "adapter_version": "test",
  "latest_supported_config_version": 1,
  "picoclaw_versions": [{"version":"0.2.4","config_version":1}],
  "config_schemas": {"1":"schemas/config-v1.json"},
  "ui_schemas": {"1":"ui/ui-v1.json"},
  "migrations": []
}`,
    "schemas/config-v1.json": `{
  "config_version": 1,
  "channels_path": "channels",
  "channel_settings_path": "channels.*",
  "models_path": "model_list",
  "default_model_path": "agents.defaults.model",
  "security": {"channels_path":"channels","channel_settings_path":"channels.*","models_path":"model_list"},
  "singleton_channels": [],
  "channel_types": ["dingtalk"]
}`,
    "ui/ui-v1.json": `{"config_version":1,"pages":[{"key":"channels","label":"Channels","sections":[{"key":"dingtalk","label":"钉钉","fields":[{"key":"enabled","label":"启用","type":"boolean","storage":"config","path":"channels.dingtalk.enabled"}]}]}]}`,
  }
  hash := ""
  for _, name := range []string{"index.json", "schemas/config-v1.json", "ui/ui-v1.json"} {
    hash += sha256String(files[name]) + "  " + name + "\n"
  }
  files["hash"] = hash
  return files
}

func buildAdapterZip(t *testing.T, files map[string]string) []byte {
  t.Helper()
  var buf bytes.Buffer
  zw := zip.NewWriter(&buf)
  for name, body := range files {
    w, err := zw.Create(name)
    if err != nil {
      t.Fatalf("zip create %s: %v", name, err)
    }
    if _, err := w.Write([]byte(body)); err != nil {
      t.Fatalf("zip write %s: %v", name, err)
    }
  }
  if err := zw.Close(); err != nil {
    t.Fatalf("zip close: %v", err)
  }
  return buf.Bytes()
}
