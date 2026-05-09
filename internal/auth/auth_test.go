package auth

import (
  "os"
  "path/filepath"
  "testing"
)

func testInitDB(t *testing.T) {
  t.Helper()
  tmpDir := t.TempDir()
  // 重置全局 engine 状态
  engine = nil
  dbDataDir = ""
  if err := InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB failed: %v", err)
  }
}

func TestCreateAndAuthenticate(t *testing.T) {
  testInitDB(t)

  err := CreateUser("testuser", "password123", "user")
  if err != nil {
    t.Fatalf("CreateUser: %v", err)
  }

  // 正确密码
  ok, role, err := AuthenticateLocal("testuser", "password123")
  if err != nil {
    t.Fatalf("AuthenticateLocal: %v", err)
  }
  if !ok {
    t.Error("AuthenticateLocal should succeed with correct password")
  }
  if role != "user" {
    t.Errorf("role = %q, want %q", role, "user")
  }

  // 错误密码
  ok, _, _ = AuthenticateLocal("testuser", "wrongpassword")
  if ok {
    t.Error("AuthenticateLocal should fail with wrong password")
  }

  // 不存在的用户
  ok, _, _ = AuthenticateLocal("nonexistent", "password123")
  if ok {
    t.Error("AuthenticateLocal should fail for nonexistent user")
  }
}

func TestChangePassword(t *testing.T) {
  testInitDB(t)

  CreateUser("user1", "oldpass", "user")

  // 修改密码
  if err := ChangePassword("user1", "newpass"); err != nil {
    t.Fatalf("ChangePassword: %v", err)
  }

  // 旧密码应失败
  ok, _, _ := AuthenticateLocal("user1", "oldpass")
  if ok {
    t.Error("old password should not work after change")
  }

  // 新密码应成功
  ok, _, _ = AuthenticateLocal("user1", "newpass")
  if !ok {
    t.Error("new password should work after change")
  }

  // 不存在的用户
  err := ChangePassword("ghost", "pass")
  if err == nil {
    t.Error("ChangePassword should fail for nonexistent user")
  }
}

func TestAllocateNextIP(t *testing.T) {
  testInitDB(t)

  ip1, err := AllocateNextIP()
  if err != nil {
    t.Fatalf("AllocateNextIP: %v", err)
  }
  if ip1 != "100.64.0.2" {
    t.Errorf("first IP = %q, want %q", ip1, "100.64.0.2")
  }

  // 插入一条容器记录占用 IP
  UpsertContainer(&ContainerRecord{
    Username: "user1",
    Image:    "test:latest",
    Status:   "stopped",
    IP:       ip1,
  })

  ip2, err := AllocateNextIP()
  if err != nil {
    t.Fatalf("AllocateNextIP 2: %v", err)
  }
  if ip2 != "100.64.0.3" {
    t.Errorf("second IP = %q, want %q", ip2, "100.64.0.3")
  }
}

func TestMCPToken(t *testing.T) {
  testInitDB(t)

  // 先创建容器记录
  UpsertContainer(&ContainerRecord{
    Username: "mcpuser",
    Image:    "test:latest",
    Status:   "stopped",
    IP:       "100.64.0.10",
  })

  token, err := GenerateMCPToken("mcpuser")
  if err != nil {
    t.Fatalf("GenerateMCPToken: %v", err)
  }
  if token == "" {
    t.Fatal("token should not be empty")
  }

  // 验证 token
  username, ok := ValidateMCPToken(token)
  if !ok {
    t.Error("ValidateMCPToken should succeed")
  }
  if username != "mcpuser" {
    t.Errorf("username = %q, want %q", username, "mcpuser")
  }

  // 无效 token
  _, ok = ValidateMCPToken("invalid")
  if ok {
    t.Error("ValidateMCPToken should fail for invalid token")
  }

  // 空 token
  _, ok = ValidateMCPToken("")
  if ok {
    t.Error("ValidateMCPToken should fail for empty token")
  }

  // 获取存储的 token
  stored, err := GetMCPToken("mcpuser")
  if err != nil {
    t.Fatalf("GetMCPToken: %v", err)
  }
  if stored != token {
    t.Errorf("stored token = %q, want %q", stored, token)
  }
}

func TestSyncUserGroups(t *testing.T) {
  testInitDB(t)

  err := SyncUserGroups("user1", []string{"group1", "group2"})
  if err != nil {
    t.Fatalf("SyncUserGroups: %v", err)
  }

  groups, err := GetGroupsForUser("user1")
  if err != nil {
    t.Fatalf("GetGroupsForUser: %v", err)
  }
  if len(groups) != 2 {
    t.Fatalf("expected 2 groups, got %d", len(groups))
  }

  // 更新组（移除 group1，添加 group3）
  SyncUserGroups("user1", []string{"group2", "group3"})
  groups, _ = GetGroupsForUser("user1")
  if len(groups) != 2 {
    t.Fatalf("expected 2 groups after sync, got %d: %v", len(groups), groups)
  }

  // 验证 group1 被移除
  for _, g := range groups {
    if g == "group1" {
      t.Error("group1 should have been removed")
    }
  }
}

func TestDeleteUser(t *testing.T) {
  testInitDB(t)

  CreateUser("todelete", "pass", "user")
  if !UserExists("todelete") {
    t.Error("user should exist after creation")
  }

  if err := DeleteUser("todelete"); err != nil {
    t.Fatalf("DeleteUser: %v", err)
  }
  if UserExists("todelete") {
    t.Error("user should not exist after deletion")
  }

  // 删除不存在的用户
  err := DeleteUser("ghost")
  if err == nil {
    t.Error("DeleteUser should fail for nonexistent user")
  }
}

func TestIsSuperadmin(t *testing.T) {
  testInitDB(t)

  CreateUser("admin", "pass", "superadmin")
  CreateUser("normal", "pass", "user")

  if !IsSuperadmin("admin") {
    t.Error("admin should be superadmin")
  }
  if IsSuperadmin("normal") {
    t.Error("normal should not be superadmin")
  }
  if IsSuperadmin("nonexistent") {
    t.Error("nonexistent should not be superadmin")
  }
}

func TestDBFilePath(t *testing.T) {
  tmpDir := t.TempDir()
  engine = nil
  dbDataDir = ""
  InitDB(tmpDir)

  dbPath := filepath.Join(tmpDir, "picoaide.db")
  if _, err := os.Stat(dbPath); os.IsNotExist(err) {
    t.Errorf("database file should exist at %q", dbPath)
  }
}
