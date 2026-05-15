package user

import (
  "encoding/json"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
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
  engine, err := auth.GetEngine()
  if err != nil {
    t.Skip("数据库不可用:", err)
  }

  // 种子数据到 DB
  if err := SeedPicoClawAdapterToDB(engine); err != nil {
    t.Fatalf("SeedPicoClawAdapterToDB() 失败: %v", err)
  }

  // 从 DB 加载
  loaded, err := PicoClawAdapterPackageFromDB(engine)
  if err != nil {
    t.Fatalf("PicoClawAdapterPackageFromDB() 失败: %v", err)
  }
  if loaded == nil {
    t.Fatal("PicoClawAdapterPackageFromDB() 返回 nil")
  }

  // 通过 NewPicoClawAdapterPackage 验证（走 DB 路径）
  pkg, err := NewPicoClawAdapterPackage("")
  if err != nil {
    t.Fatalf("NewPicoClawAdapterPackage() 失败: %v", err)
  }
  if pkg.Index.AdapterVersion == "" {
    t.Error("AdapterVersion 不应为空")
  }
  if len(pkg.ConfigSchemas) != 3 {
    t.Errorf("期望 3 个 config schemas，实际 %d", len(pkg.ConfigSchemas))
  }
}
