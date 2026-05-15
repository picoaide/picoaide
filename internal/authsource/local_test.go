package authsource

import (
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
)

func TestLocalProviderDisplayName(t *testing.T) {
  p := LocalProvider{}
  if got := p.DisplayName(); got != "本地用户" {
    t.Fatalf("DisplayName = %q, want 本地用户", got)
  }
}

func TestLocalProviderConfigFields(t *testing.T) {
  p := LocalProvider{}
  if got := p.ConfigFields(); got != nil {
    t.Fatalf("ConfigFields = %v, want nil", got)
  }
}

func TestLocalProviderAuthenticate(t *testing.T) {
  auth.ResetDB()
  tmpDir := t.TempDir()
  if err := auth.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  defer auth.ResetDB()

  if err := auth.CreateUser("testuser", "password123", "user"); err != nil {
    t.Fatalf("CreateUser: %v", err)
  }

  p := LocalProvider{}
  cfg := testConfig("local")

  if !p.Authenticate(cfg, "testuser", "password123") {
    t.Error("Authenticate should succeed with correct password")
  }
  if p.Authenticate(cfg, "testuser", "wrongpassword") {
    t.Error("Authenticate should fail with wrong password")
  }
  if p.Authenticate(cfg, "nonexistent", "password123") {
    t.Error("Authenticate should fail for nonexistent user")
  }
}
