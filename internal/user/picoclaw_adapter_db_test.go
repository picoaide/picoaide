package user

import (
  "encoding/json"
  "testing"
)

func TestPicoClawAdapterPackageSerialize(t *testing.T) {
  pkg, err := NewPicoClawAdapterPackage("")
  if err != nil {
    t.Fatalf("NewPicoClawAdapterPackage() failed: %v", err)
  }
  content, err := pkg.Serialize()
  if err != nil {
    t.Fatalf("Serialize() failed: %v", err)
  }
  var parsed SerializableAdapterContent
  if err := json.Unmarshal([]byte(content), &parsed); err != nil {
    t.Fatalf("Unmarshal failed: %v", err)
  }
  if parsed.Index.AdapterVersion != pkg.Index.AdapterVersion {
    t.Errorf("AdapterVersion mismatch: got %s, want %s", parsed.Index.AdapterVersion, pkg.Index.AdapterVersion)
  }
  if len(parsed.ConfigSchemas) != len(pkg.ConfigSchemas) {
    t.Errorf("ConfigSchemas count mismatch: got %d, want %d", len(parsed.ConfigSchemas), len(pkg.ConfigSchemas))
  }
  if len(parsed.UISchemas) != len(pkg.UISchemas) {
    t.Errorf("UISchemas count mismatch: got %d, want %d", len(parsed.UISchemas), len(pkg.UISchemas))
  }
  if len(parsed.Migrations) != len(pkg.Migrations) {
    t.Errorf("Migrations count mismatch: got %d, want %d", len(parsed.Migrations), len(pkg.Migrations))
  }
}

func TestPicoClawAdapterPackageDBRoundtrip(t *testing.T) {
  t.Skip("需要 SQLite 数据库环境")
}
