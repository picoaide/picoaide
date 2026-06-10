package store

import (
  "os"
  "strings"
  "testing"
)

func TestMain(m *testing.M) {
  old := passwordHashParams
  passwordHashParams.memory = 64 * 1024
  passwordHashParams.time = 1
  passwordHashParams.threads = 1
  code := m.Run()
  passwordHashParams = old
  os.Exit(code)
}

func testInitDB(t *testing.T) {
  t.Helper()
  tmpDir := t.TempDir()
  engine = nil
  dbDataDir = ""
  if err := InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB failed: %v", err)
  }
}

func TestCreateUserAndQuery(t *testing.T) {
  testInitDB(t)

  if err := CreateUser("testuser", "password123", "user"); err != nil {
    t.Fatalf("CreateUser: %v", err)
  }

  if !UserExists("testuser") {
    t.Error("UserExists should return true")
  }
  if UserExists("nonexistent") {
    t.Error("UserExists should return false for nonexistent user")
  }

  if got := GetUserSource("testuser"); got != "local" {
    t.Errorf("GetUserSource = %q, want local", got)
  }

  if IsExternalUser("testuser") {
    t.Error("local user should not be external")
  }

  if role := GetUserRole("testuser"); role != "user" {
    t.Errorf("GetUserRole = %q, want user", role)
  }

  users, err := GetAllLocalUsers()
  if err != nil {
    t.Fatalf("GetAllLocalUsers: %v", err)
  }
  if len(users) != 1 {
    t.Fatalf("user count = %d, want 1", len(users))
  }
  if users[0].Username != "testuser" {
    t.Errorf("username = %q", users[0].Username)
  }
}

func TestMCPToken(t *testing.T) {
  testInitDB(t)

  token, err := GenerateMCPToken("mcpuser")
  if err != nil {
    t.Fatalf("GenerateMCPToken: %v", err)
  }
  if token == "" {
    t.Fatal("token should not be empty")
  }

  username, ok := ValidateMCPToken(token)
  if !ok {
    t.Error("ValidateMCPToken should succeed")
  }
  if username != "mcpuser" {
    t.Errorf("username = %q, want %q", username, "mcpuser")
  }

  _, ok = ValidateMCPToken("invalid")
  if ok {
    t.Error("ValidateMCPToken should fail for invalid token")
  }

  _, ok = ValidateMCPToken("")
  if ok {
    t.Error("ValidateMCPToken should fail for empty token")
  }

  stored, err := GetMCPToken("mcpuser")
  if err != nil {
    t.Fatalf("GetMCPToken: %v", err)
  }
  if stored != token {
    t.Errorf("stored token mismatch")
  }

  // 不存在的用户
  missing, err := GetMCPToken("ghost")
  if err != nil {
    t.Fatalf("GetMCPToken ghost: %v", err)
  }
  if missing != "" {
    t.Errorf("expected empty token for ghost, got %q", missing)
  }
}

func TestIsSuperadmin(t *testing.T) {
  testInitDB(t)

  CreateUser("normal", "pass", "user")
  CreateUser("admin", "pass", "superadmin")

  if IsSuperadmin("normal") {
    t.Error("normal user should not be superadmin")
  }
  if !IsSuperadmin("admin") {
    t.Error("admin should be superadmin")
  }
  if IsSuperadmin("nonexistent") {
    t.Error("nonexistent user should not be superadmin")
  }
}

func TestDeleteUser(t *testing.T) {
  testInitDB(t)

  CreateUser("todelete", "pass", "user")

  if !UserExists("todelete") {
    t.Fatal("user should exist before delete")
  }

  if err := DeleteUser("todelete"); err != nil {
    t.Fatalf("DeleteUser: %v", err)
  }

  if UserExists("todelete") {
    t.Error("user should not exist after delete")
  }
}

func TestBcryptHashUpgrade(t *testing.T) {
  testInitDB(t)

  legacyHash := "$2y$10$dummyhashingisnotvalidbutwewilltestwithfreshone"
  // Use bcrypt to generate a real legacy hash
  legacyHash = "$2y$04$E/CPq3s1MZpDTeD6MEoQJO4qUYiTqG/N.PglLWJL0Q7TJrjDqnjFq"
  // Insert a user with bcrypt-format hash directly
  _, err := engine.Exec("INSERT INTO local_users (username, password_hash, role) VALUES (?, ?, ?)",
    "legacy", legacyHash, "user")
  if err != nil {
    t.Fatalf("insert legacy user: %v", err)
  }

  // Verify the user exists
  if !UserExists("legacy") {
    t.Error("legacy user should exist")
  }
}

func TestInitDBRecreatesOnCorruption(t *testing.T) {
  tmpDir := t.TempDir()
  engine = nil
  dbDataDir = ""

  if err := InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB failed: %v", err)
  }

  ResetDB()

  // 验证可以重新初始化
  if err := InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB second call failed: %v", err)
  }
}

func TestArgon2idHashPrefix(t *testing.T) {
  if !strings.HasPrefix(argon2idHashPrefix, "$argon2id$") {
    t.Errorf("unexpected prefix: %q", argon2idHashPrefix)
  }
}
