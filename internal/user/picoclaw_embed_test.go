package user

import (
  "os"
  "path/filepath"
  "testing"
)

func TestFindBundledPicoClawAdapterRoot(t *testing.T) {
  root, err := findBundledPicoClawAdapterRoot()
  if err != nil {
    t.Fatalf("findBundledPicoClawAdapterRoot() error = %v", err)
  }
  if root == "" {
    t.Fatal("root should not be empty")
  }
  if _, err := os.Stat(filepath.Join(root, "index.json")); err != nil {
    t.Errorf("index.json not found at root %s: %v", root, err)
  }
}

func TestLoadFromBundledDir(t *testing.T) {
  pkg, err := loadFromBundledDir()
  if err != nil {
    t.Fatalf("loadFromBundledDir() error = %v", err)
  }
  if pkg == nil {
    t.Fatal("returned nil package")
  }
  if pkg.Index.AdapterVersion == "" {
    t.Error("AdapterVersion should not be empty")
  }
  if len(pkg.ConfigSchemas) != 3 {
    t.Errorf("expected 3 config schemas, got %d", len(pkg.ConfigSchemas))
  }
}

func TestNewPicoClawAdapterPackageFromEmbed(t *testing.T) {
  if !picoclawAdapterEmbedExists() {
    t.Skip("embedded adapter not available")
  }
  pkg, err := NewPicoClawAdapterPackageFromEmbed()
  if err != nil {
    t.Fatalf("NewPicoClawAdapterPackageFromEmbed() failed: %v", err)
  }
  if pkg == nil {
    t.Fatal("returned nil package")
  }
  if pkg.Index.AdapterVersion == "" {
    t.Error("AdapterVersion should not be empty")
  }
  if len(pkg.ConfigSchemas) != 3 {
    t.Errorf("expected 3 config schemas, got %d", len(pkg.ConfigSchemas))
  }
  if len(pkg.UISchemas) != 3 {
    t.Errorf("expected 3 UI schemas, got %d", len(pkg.UISchemas))
  }
  if len(pkg.Migrations) != 2 {
    t.Errorf("expected 2 migrations, got %d", len(pkg.Migrations))
  }
}
