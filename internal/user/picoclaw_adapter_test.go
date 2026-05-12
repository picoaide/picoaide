package user

import (
  "archive/zip"
  "bytes"
  "crypto/sha256"
  "encoding/hex"
  "net/http"
  "net/http/httptest"
  "os"
  "path/filepath"
  "strings"
  "testing"
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
  if _, err := os.Stat(filepath.Join(cacheDir, picoclawAdapterDir, picoclawAdapterIndexFile)); err != nil {
    t.Fatalf("active adapter index missing: %v", err)
  }
}

func TestSavePicoClawAdapterZipInstallsPackage(t *testing.T) {
  files := testAdapterFiles()
  cacheDir := t.TempDir()
  pkg, err := SavePicoClawAdapterZip(cacheDir, buildAdapterZip(t, files))
  if err != nil {
    t.Fatalf("SavePicoClawAdapterZip() error = %v", err)
  }
  if pkg.Index.AdapterVersion != "test" {
    t.Fatalf("AdapterVersion = %q", pkg.Index.AdapterVersion)
  }
  if _, err := os.Stat(filepath.Join(cacheDir, picoclawAdapterDir, picoclawAdapterIndexFile)); err != nil {
    t.Fatalf("active adapter index missing: %v", err)
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
