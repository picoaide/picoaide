package auth

import (
  "os"
  "path/filepath"
  "strings"
  "testing"

  "golang.org/x/crypto/bcrypt"
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

func TestEnsureExternalUserStoresSourceAndRotatesLocalPassword(t *testing.T) {
  testInitDB(t)

  if err := EnsureExternalUser("ldapuser", "user", "ldap"); err != nil {
    t.Fatalf("EnsureExternalUser: %v", err)
  }
  if !UserExists("ldapuser") {
    t.Fatal("external user should exist locally")
  }
  if got := GetUserSource("ldapuser"); got != "ldap" {
    t.Fatalf("source = %q, want ldap", got)
  }
  if !IsExternalUser("ldapuser") {
    t.Fatal("ldapuser should be external")
  }
  if ok, _, err := AuthenticateLocal("ldapuser", "password123"); err != nil || ok {
    t.Fatalf("AuthenticateLocal external user ok=%v err=%v, want false nil", ok, err)
  }

  if err := EnsureExternalUser("ldapuser", "user", "ldap"); err != nil {
    t.Fatalf("EnsureExternalUser second call: %v", err)
  }
  users, err := GetAllLocalUsers()
  if err != nil {
    t.Fatalf("GetAllLocalUsers: %v", err)
  }
  if len(users) != 1 {
    t.Fatalf("local user count = %d, want 1", len(users))
  }
  if users[0].Source != "ldap" {
    t.Fatalf("listed source = %q, want ldap", users[0].Source)
  }
}

func TestAuthenticateLocalUpgradesBcryptHash(t *testing.T) {
  testInitDB(t)

  legacyHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
  if err != nil {
    t.Fatalf("GenerateFromPassword: %v", err)
  }
  _, err = engine.Insert(&LocalUser{
    Username:     "legacy",
    PasswordHash: string(legacyHash),
    Role:         "user",
  })
  if err != nil {
    t.Fatalf("insert legacy user: %v", err)
  }

  ok, role, err := AuthenticateLocal("legacy", "password123")
  if err != nil {
    t.Fatalf("AuthenticateLocal: %v", err)
  }
  if !ok {
    t.Fatal("AuthenticateLocal should succeed with legacy bcrypt hash")
  }
  if role != "user" {
    t.Errorf("role = %q, want user", role)
  }

  var user LocalUser
  has, err := engine.Where("username = ?", "legacy").Get(&user)
  if err != nil || !has {
    t.Fatalf("get legacy user: has=%v err=%v", has, err)
  }
  if !strings.HasPrefix(user.PasswordHash, argon2idHashPrefix) {
    t.Fatalf("PasswordHash was not upgraded to argon2id: %q", user.PasswordHash)
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

  err := SyncUserGroups("user1", []string{"group1", "group2"}, "ldap")
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
  SyncUserGroups("user1", []string{"group2", "group3"}, "ldap")
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

func TestReplaceGroupMembersBySourceRemovesStaleRelations(t *testing.T) {
  testInitDB(t)

  if err := CreateGroup("local-team", "local", "", nil); err != nil {
    t.Fatalf("CreateGroup local: %v", err)
  }
  if err := AddUsersToGroup("local-team", []string{"local-user"}); err != nil {
    t.Fatalf("AddUsersToGroup local: %v", err)
  }
  if err := ReplaceGroupMembersBySource("ldap", map[string][]string{
    "ldap-team": {"alice", "bob"},
  }); err != nil {
    t.Fatalf("ReplaceGroupMembersBySource first: %v", err)
  }
  if err := ReplaceGroupMembersBySource("ldap", map[string][]string{
    "ldap-team": {"bob"},
  }); err != nil {
    t.Fatalf("ReplaceGroupMembersBySource second: %v", err)
  }

  ldapMembers, err := GetGroupMembers("ldap-team")
  if err != nil {
    t.Fatalf("GetGroupMembers ldap-team: %v", err)
  }
  if len(ldapMembers) != 1 || ldapMembers[0] != "bob" {
    t.Fatalf("ldap-team members = %v, want [bob]", ldapMembers)
  }
  localMembers, err := GetGroupMembers("local-team")
  if err != nil {
    t.Fatalf("GetGroupMembers local-team: %v", err)
  }
  if len(localMembers) != 1 || localMembers[0] != "local-user" {
    t.Fatalf("local-team members = %v, want [local-user]", localMembers)
  }
}

func TestGetGroupMembersForDeployIncludesSubGroups(t *testing.T) {
  testInitDB(t)

  if err := CreateGroup("parent", "local", "", nil); err != nil {
    t.Fatalf("CreateGroup parent: %v", err)
  }
  parentID, err := GetGroupID("parent")
  if err != nil {
    t.Fatalf("GetGroupID parent: %v", err)
  }
  if err := CreateGroup("child", "local", "", &parentID); err != nil {
    t.Fatalf("CreateGroup child: %v", err)
  }
  if err := AddUsersToGroup("parent", []string{"direct"}); err != nil {
    t.Fatalf("AddUsersToGroup parent: %v", err)
  }
  if err := AddUsersToGroup("child", []string{"nested"}); err != nil {
    t.Fatalf("AddUsersToGroup child: %v", err)
  }

  members, err := GetGroupMembersForDeploy("parent")
  if err != nil {
    t.Fatalf("GetGroupMembersForDeploy: %v", err)
  }
  got := map[string]bool{}
  for _, member := range members {
    got[member] = true
  }
  if !got["direct"] || !got["nested"] {
    t.Fatalf("parent deploy members = %v, want direct and nested", members)
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
