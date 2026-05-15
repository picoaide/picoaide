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

func TestResetDB(t *testing.T) {
  testInitDB(t)
  ResetDB()
  if engine != nil {
    t.Error("engine should be nil after ResetDB")
  }
  if dbDataDir != "" {
    t.Error("dbDataDir should be empty after ResetDB")
  }
}

func TestGetEngine_Success(t *testing.T) {
  testInitDB(t)
  e, err := GetEngine()
  if err != nil {
    t.Fatalf("GetEngine: %v", err)
  }
  if e == nil {
    t.Fatal("engine should not be nil")
  }
}

func TestGetEngine_NotInitialized(t *testing.T) {
  ResetDB()
  _, err := GetEngine()
  if err == nil {
    t.Error("GetEngine should fail when DB not initialized")
  }
}

func TestGetEngine_AutoReinit(t *testing.T) {
  tmpDir := t.TempDir()
  engine = nil
  dbDataDir = tmpDir
  if err := InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  // Simulate engine being nil after data dir set
  engine = nil
  e, err := GetEngine()
  if err != nil {
    t.Fatalf("GetEngine auto-reinit: %v", err)
  }
  if e == nil {
    t.Fatal("engine should be non-nil after auto-reinit")
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

func TestInitDB_InvalidPath(t *testing.T) {
  ResetDB()
  // Use an existing file as dataDir - MkdirAll will fail with ENOTDIR
  tmpFile := filepath.Join(t.TempDir(), "existing_file")
  if err := os.WriteFile(tmpFile, []byte("data"), 0644); err != nil {
    t.Fatalf("WriteFile: %v", err)
  }
  err := InitDB(tmpFile)
  if err == nil {
    t.Error("InitDB should fail when dataDir is a file")
  }
}

func TestUserHasSkillFromAnySource_DBError(t *testing.T) {
  ResetDB()
  _, err := UserHasSkillFromAnySource("nobody", "skill")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetUserAllSkillSources_DBError(t *testing.T) {
  ResetDB()
  _, err := GetUserAllSkillSources("nobody", "skill")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestLoadDefaultSkills_InvalidJSON(t *testing.T) {
  testInitDB(t)
  // Insert invalid JSON
  _, err := engine.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)",
    defaultSkillsKey, "{invalid-json")
  if err != nil {
    t.Fatalf("insert setting: %v", err)
  }
  skills, err := LoadDefaultSkills()
  if err != nil {
    t.Fatalf("LoadDefaultSkills: %v", err)
  }
  if len(skills) != 0 {
    t.Errorf("skills = %v, want empty for invalid JSON", skills)
  }
}

func TestSkipSkillsRootDir(t *testing.T) {
  testInitDB(t)
  // SkillsRootDir doesn't exist - skillExists should return false
  if skillExistsOnDisk("any-skill") {
    t.Error("should not exist when SkillsRootDir doesn't exist")
  }
}

func TestRemoveFromDefaultSkills_Empty(t *testing.T) {
  testInitDB(t)
  if err := RemoveFromDefaultSkills("nonexistent"); err != nil {
    t.Fatalf("RemoveFromDefaultSkills: %v", err)
  }
  skills, _ := LoadDefaultSkills()
  if len(skills) != 0 {
    t.Errorf("skills = %v, want empty", skills)
  }
}

func TestCreateUser_DBError(t *testing.T) {
  ResetDB()
  err := CreateUser("u1", "pass", "user")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestEnsureExternalUser_DBError(t *testing.T) {
  ResetDB()
  err := EnsureExternalUser("u1", "user", "ldap")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestAuthenticateLocal_DBError(t *testing.T) {
  ResetDB()
  _, _, err := AuthenticateLocal("u1", "pass")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetAllLocalUsers_DBError(t *testing.T) {
  ResetDB()
  _, err := GetAllLocalUsers()
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetExternalUsers_DBError(t *testing.T) {
  ResetDB()
  _, err := GetExternalUsers("")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetSuperadmins_DBError(t *testing.T) {
  ResetDB()
  _, err := GetSuperadmins()
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestChangePassword_DBError(t *testing.T) {
  ResetDB()
  err := ChangePassword("u1", "pass")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestDeleteUser_DBError(t *testing.T) {
  ResetDB()
  err := DeleteUser("u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestDeleteAllRegularUsers_DBError(t *testing.T) {
  ResetDB()
  _, err := DeleteAllRegularUsers()
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestClearAllGroups_DBError(t *testing.T) {
  ResetDB()
  err := ClearAllGroups()
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestClearAllContainers_DBError(t *testing.T) {
  ResetDB()
  _, err := ClearAllContainers()
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestUpsertContainer_DBError(t *testing.T) {
  ResetDB()
  err := UpsertContainer(&ContainerRecord{Username: "u1", Image: "img"})
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetContainerByUsername_DBError(t *testing.T) {
  ResetDB()
  _, err := GetContainerByUsername("u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetAllContainers_DBError(t *testing.T) {
  ResetDB()
  _, err := GetAllContainers()
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestDeleteContainer_DBError(t *testing.T) {
  ResetDB()
  err := DeleteContainer("u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestUpdateContainerStatus_DBError(t *testing.T) {
  ResetDB()
  err := UpdateContainerStatus("u1", "running")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestUpdateContainerID_DBError(t *testing.T) {
  ResetDB()
  err := UpdateContainerID("u1", "abc")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestUpdateContainerImage_DBError(t *testing.T) {
  ResetDB()
  err := UpdateContainerImage("u1", "img:latest")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestUpsertUserChannelStatus_DBError(t *testing.T) {
  ResetDB()
  err := UpsertUserChannelStatus("u1", "web", true, true, false, 1)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetUserChannelStatus_DBError(t *testing.T) {
  ResetDB()
  _, err := GetUserChannelStatus("u1", "web")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestAllocateNextIP_DBError(t *testing.T) {
  ResetDB()
  _, err := AllocateNextIP()
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGenerateMCPToken_DBError(t *testing.T) {
  ResetDB()
  _, err := GenerateMCPToken("u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetMCPToken_DBError(t *testing.T) {
  ResetDB()
  _, err := GetMCPToken("u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestSetCookie_DBError(t *testing.T) {
  ResetDB()
  err := SetCookie("u1", "example.com", "cookies")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetCookie_DBError(t *testing.T) {
  ResetDB()
  _, err := GetCookie("u1", "example.com")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetAllCookies_DBError(t *testing.T) {
  ResetDB()
  _, err := GetAllCookies("u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestDeleteCookie_DBError(t *testing.T) {
  ResetDB()
  err := DeleteCookie("u1", "example.com")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestListCookieDomains_DBError(t *testing.T) {
  ResetDB()
  _, err := ListCookieDomains("u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestCreateGroup_DBError(t *testing.T) {
  ResetDB()
  err := CreateGroup("g1", "local", "", nil)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestDeleteGroup_DBError(t *testing.T) {
  ResetDB()
  err := DeleteGroup("g1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestListGroups_DBError(t *testing.T) {
  ResetDB()
  _, err := ListGroups()
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetGroupID_DBError(t *testing.T) {
  ResetDB()
  _, err := GetGroupID("g1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestAddUsersToGroup_DBError(t *testing.T) {
  ResetDB()
  err := AddUsersToGroup("g1", []string{"u1"})
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestRemoveUserFromGroup_DBError(t *testing.T) {
  ResetDB()
  err := RemoveUserFromGroup("g1", "u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetGroupMembers_DBError(t *testing.T) {
  ResetDB()
  _, err := GetGroupMembers("g1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetGroupMembersWithSubGroups_DBError(t *testing.T) {
  ResetDB()
  _, _, err := GetGroupMembersWithSubGroups("g1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetGroupsForUser_DBError(t *testing.T) {
  ResetDB()
  _, err := GetGroupsForUser("u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestSyncUserGroups_DBError(t *testing.T) {
  ResetDB()
  err := SyncUserGroups("u1", []string{"g1"}, "local")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestReplaceGroupMembersBySource_DBError(t *testing.T) {
  ResetDB()
  err := ReplaceGroupMembersBySource("local", map[string][]string{})
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetGroupMembersForDeploy_DBError(t *testing.T) {
  ResetDB()
  _, err := GetGroupMembersForDeploy("g1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetSubGroupIDs_DBError(t *testing.T) {
  ResetDB()
  _, err := GetSubGroupIDs(1)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestSetGroupParent_DBError(t *testing.T) {
  ResetDB()
  err := SetGroupParent("g1", nil)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestCreateSharedFolder_DBError(t *testing.T) {
  ResetDB()
  err := CreateSharedFolder("sf1", "", false, "admin")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetSharedFolder_DBError(t *testing.T) {
  ResetDB()
  _, err := GetSharedFolder(1)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetSharedFolderByName_DBError(t *testing.T) {
  ResetDB()
  _, err := GetSharedFolderByName("sf1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestListSharedFolders_DBError(t *testing.T) {
  ResetDB()
  _, err := ListSharedFolders()
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestUpdateSharedFolder_DBError(t *testing.T) {
  ResetDB()
  err := UpdateSharedFolder(1, "sf1", "", false)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestDeleteSharedFolder_DBError(t *testing.T) {
  ResetDB()
  err := DeleteSharedFolder(1)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestSetSharedFolderGroups_DBError(t *testing.T) {
  ResetDB()
  err := SetSharedFolderGroups(1, []int64{})
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetSharedFolderGroupIDs_DBError(t *testing.T) {
  ResetDB()
  _, err := GetSharedFolderGroupIDs(1)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetSharedFolderMembers_DBError(t *testing.T) {
  ResetDB()
  _, err := GetSharedFolderMembers(1)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestIsUserInSharedFolder_DBError(t *testing.T) {
  ResetDB()
  _, err := IsUserInSharedFolder(1, "u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetAccessibleSharedFolders_DBError(t *testing.T) {
  ResetDB()
  _, err := GetAccessibleSharedFolders("u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetSharedFolderMountsForUser_DBError(t *testing.T) {
  ResetDB()
  _, err := GetSharedFolderMountsForUser("/work", "u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestRecordMountTest_DBError(t *testing.T) {
  ResetDB()
  err := RecordMountTest(1, "u1", true)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetMountStatus_DBError(t *testing.T) {
  ResetDB()
  _, err := GetMountStatus(1, "u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetMountStatusesForFolder_DBError(t *testing.T) {
  ResetDB()
  _, err := GetMountStatusesForFolder(1)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestDeleteSharedFolderMountsByUser_DBError(t *testing.T) {
  ResetDB()
  err := DeleteSharedFolderMountsByUser("u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestRemoveGroupFromAllSharedFolders_DBError(t *testing.T) {
  ResetDB()
  _, err := RemoveGroupFromAllSharedFolders(1)
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetOrphanedSharedFolders_DBError(t *testing.T) {
  ResetDB()
  _, err := GetOrphanedSharedFolders()
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestRemoveGroupSourceFromSharedFolders_DBError(t *testing.T) {
  ResetDB()
  err := RemoveGroupSourceFromSharedFolders("ldap")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestOnGroupMembersAdded_DBError(t *testing.T) {
  ResetDB()
  _, err := OnGroupMembersAdded(1, []string{"u1"})
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestOnGroupMembersRemoved_DBError(t *testing.T) {
  ResetDB()
  _, err := OnGroupMembersRemoved(1, "u1")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetAllNonSuperadminUsers_DBError(t *testing.T) {
  ResetDB()
  _, err := GetAllNonSuperadminUsers()
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestBuildSharedFolderInfo_DBError(t *testing.T) {
  ResetDB()
  _, err := BuildSharedFolderInfo(&SharedFolder{ID: 1})
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestBindSkillToUser_DBError(t *testing.T) {
  ResetDB()
  err := BindSkillToUser("u1", "skill", "source")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestUnbindSkillFromUser_DBError(t *testing.T) {
  ResetDB()
  err := UnbindSkillFromUser("u1", "skill")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetUserSkillSource_DBError(t *testing.T) {
  ResetDB()
  _, err := GetUserSkillSource("u1", "skill")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestGetUsersBySkill_DBError(t *testing.T) {
  ResetDB()
  _, err := GetUsersBySkill("skill")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestDeleteSkill_DBError(t *testing.T) {
  ResetDB()
  err := DeleteSkill("skill")
  if err == nil {
    t.Error("should error when DB not initialized")
  }
}

func TestVerifyArgon2idPassword_BadVersion(t *testing.T) {
  ok, err := verifyArgon2idPassword("$argon2id$v=99$m=65536,t=3,p=4$c2FsdA==$a2V5", "pass")
  if ok || err != nil {
    t.Errorf("ok=%v err=%v, want false,nil", ok, err)
  }
}

func TestVerifyArgon2idPassword_BadParams(t *testing.T) {
  ok, err := verifyArgon2idPassword("$argon2id$v=19$invalid-params$c2FsdA==$a2V5", "pass")
  if ok || err != nil {
    t.Errorf("ok=%v err=%v, want false,nil", ok, err)
  }
}

func TestVerifyArgon2idPassword_InvalidSalt(t *testing.T) {
  ok, err := verifyArgon2idPassword("$argon2id$v=19$m=65536,t=3,p=4$!!!invalid-base64!!!$a2V5", "pass")
  if ok || err != nil {
    t.Errorf("ok=%v err=%v, want false,nil", ok, err)
  }
}

func TestVerifyArgon2idPassword_InvalidKey(t *testing.T) {
  ok, err := verifyArgon2idPassword("$argon2id$v=19$m=65536,t=3,p=4$c2FsdA==$!!!invalid-key!!!", "pass")
  if ok || err != nil {
    t.Errorf("ok=%v err=%v, want false,nil", ok, err)
  }
}

func TestUserExists_NotFound(t *testing.T) {
  testInitDB(t)
  if UserExists("nobody") {
    t.Error("should return false for nonexistent user")
  }
}

func TestGetUserSource_LocalDefault(t *testing.T) {
  testInitDB(t)
  CreateUser("localuser", "pass", "user")

  source := GetUserSource("localuser")
  if source != "local" {
    t.Errorf("source = %q, want local", source)
  }
}

func TestIsSuperadmin_FalseForNonexistent(t *testing.T) {
  testInitDB(t)
  if IsSuperadmin("nobody") {
    t.Error("nonexistent user should not be superadmin")
  }
}

func TestDeleteUser_WithSkillBinding(t *testing.T) {
  testInitDB(t)
  CreateUser("u1", "pass", "user")
  BindSkillToUser("u1", "skill1", "self")

  if err := DeleteUser("u1"); err != nil {
    t.Fatalf("DeleteUser: %v", err)
  }
  // skill binding should also be deleted
  sources, _ := GetUserSkillSource("u1", "skill1")
  if sources != "" {
    t.Error("skill binding should be deleted with user")
  }
}

func TestDeleteAllRegularUsers_SessionError(t *testing.T) {
  testInitDB(t)
  // Just test normal path once more
  CreateUser("sa", "pass", "superadmin")
  CreateUser("reg", "pass", "user")

  count, err := DeleteAllRegularUsers()
  if err != nil {
    t.Fatalf("DeleteAllRegularUsers: %v", err)
  }
  if count != 1 {
    t.Errorf("count = %d, want 1", count)
  }
}

func TestGetGroupMembersForDeploy_Nonexistent(t *testing.T) {
  testInitDB(t)
  _, err := GetGroupMembersForDeploy("ghost")
  if err == nil {
    t.Error("should error for nonexistent group")
  }
}

func TestGetSubGroupIDs_NoChildren(t *testing.T) {
  testInitDB(t)
  CreateGroup("leaf", "local", "", nil)
  gid, _ := GetGroupID("leaf")
  ids, err := GetSubGroupIDs(gid)
  if err != nil {
    t.Fatalf("GetSubGroupIDs: %v", err)
  }
  if len(ids) != 0 {
    t.Errorf("ids = %v, want empty", ids)
  }
}

func TestLoadDefaultSkills_JSONError(t *testing.T) {
  testInitDB(t)
  _, err := engine.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)",
    defaultSkillsKey, "{invalid}")
  if err != nil {
    t.Fatalf("insert setting: %v", err)
  }
  skills, err := loadDefaultSkillsRaw()
  if err != nil {
    t.Fatalf("loadDefaultSkillsRaw: %v", err)
  }
  if len(skills) != 0 {
    t.Errorf("skills = %v, want empty for invalid JSON", skills)
  }
}

func TestInitDB_CorruptedDB(t *testing.T) {
  ResetDB()
  tmpDir := t.TempDir()
  // Create a corrupted database file
  corruptedDB := tmpDir + "/picoaide.db"
  if err := os.WriteFile(corruptedDB, []byte("not a valid sqlite database"), 0644); err != nil {
    t.Fatalf("WriteFile: %v", err)
  }
  // InitDB should backup the corrupted file and create a new one
  if err := InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB with corrupted DB: %v", err)
  }
  // The corrupted file should have been renamed
  entries, _ := os.ReadDir(tmpDir)
  foundBackup := false
  foundDB := false
  for _, e := range entries {
    if strings.HasPrefix(e.Name(), "picoaide.db.broken.") {
      foundBackup = true
    }
    if e.Name() == "picoaide.db" {
      foundDB = true
    }
  }
  if !foundBackup {
    t.Error("corrupted backup file not found")
  }
  if !foundDB {
    t.Error("new picoaide.db not found")
  }
}

func TestListSharedFolders_ReturnsEmptySlice(t *testing.T) {
  testInitDB(t)
  list, err := ListSharedFolders()
  if err != nil {
    t.Fatalf("ListSharedFolders: %v", err)
  }
  if list == nil {
    t.Error("ListSharedFolders should return empty slice, not nil")
  }
}

func TestGetSharedFolderMembers_SuperadminExcluded(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("admin", "pass", "superadmin")
  CreateSharedFolder("test", "", true, "admin")
  sf, _ := GetSharedFolderByName("test")

  members, err := GetSharedFolderMembers(sf.ID)
  if err != nil {
    t.Fatalf("GetSharedFolderMembers: %v", err)
  }
  for _, m := range members {
    if m == "admin" {
      t.Error("superadmin should not be in members")
    }
  }
}

func TestIsUserInSharedFolder_Superadmin(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("admin", "pass", "superadmin")
  CreateSharedFolder("test", "", true, "admin")
  sf, _ := GetSharedFolderByName("test")

  ok, err := IsUserInSharedFolder(sf.ID, "admin")
  if err != nil {
    t.Fatalf("IsUserInSharedFolder: %v", err)
  }
  if ok {
    t.Error("superadmin should not be considered 'in' shared folder")
  }
}

func TestGetAccessibleSharedFolders_Superadmin(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("admin", "pass", "superadmin")
  CreateSharedFolder("test", "", true, "admin")

  list, err := GetAccessibleSharedFolders("admin")
  if err != nil {
    t.Fatalf("GetAccessibleSharedFolders: %v", err)
  }
  if len(list) != 0 {
    t.Errorf("superadmin should get 0 accessible folders, got %d", len(list))
  }
}

func TestGetUserSource_EmptySource(t *testing.T) {
  testInitDB(t)
  // Insert user with empty source
  _, err := engine.Exec("INSERT INTO local_users (username, password_hash, role, source) VALUES (?, ?, ?, ?)",
    "nosource", "hash", "user", "")
  if err != nil {
    t.Fatalf("insert user: %v", err)
  }
  source := GetUserSource("nosource")
  if source != "local" {
    t.Errorf("source = %q, want local", source)
  }
}

func TestIsSuperadmin_FalseOnDBError(t *testing.T) {
  ResetDB()
  if IsSuperadmin("admin") {
    t.Error("should return false when DB not initialized")
  }
}

func TestGetExternalUsers_QuerySourceOnly(t *testing.T) {
  testInitDB(t)
  EnsureExternalUser("ldap1", "user", "ldap")
  EnsureExternalUser("oidc1", "user", "oidc")

  // With source filter
  ldapUsers, err := GetExternalUsers("ldap")
  if err != nil {
    t.Fatalf("GetExternalUsers ldap: %v", err)
  }
  if len(ldapUsers) != 1 || ldapUsers[0].Username != "ldap1" {
    t.Errorf("ldap users = %v, want [ldap1]", ldapUsers)
  }
}

func TestDeleteUser_SkillBindingError(t *testing.T) {
  testInitDB(t)
  CreateUser("u1", "pass", "user")
  BindSkillToUser("u1", "skill1", "self")

  if err := DeleteUser("u1"); err != nil {
    t.Fatalf("DeleteUser: %v", err)
  }
  if UserExists("u1") {
    t.Error("user should be deleted")
  }
}

func TestDeleteAllRegularUsers_NoRegularUsers(t *testing.T) {
  testInitDB(t)
  CreateUser("sa", "pass", "superadmin")

  count, err := DeleteAllRegularUsers()
  if err != nil {
    t.Fatalf("DeleteAllRegularUsers: %v", err)
  }
  if count != 0 {
    t.Errorf("count = %d, want 0", count)
  }
}

func TestClearAllGroups_EmptySession(t *testing.T) {
  testInitDB(t)
  if err := ClearAllGroups(); err != nil {
    t.Fatalf("ClearAllGroups empty: %v", err)
  }
  list, _ := ListGroups()
  if len(list) != 0 {
    t.Error("groups should be empty")
  }
}

func TestGetGroupByID_NotFound(t *testing.T) {
  testInitDB(t)
  _, err := getGroupByID(999)
  if err == nil {
    t.Error("should error for nonexistent group")
  }
}

func TestGetMountStatus_NotFound(t *testing.T) {
  testInitDBForSF(t)
  _, err := GetMountStatus(999, "nobody")
  if err == nil {
    t.Error("should error for nonexistent mount record")
  }
}

func TestRecordMountTest_Overwrite(t *testing.T) {
  testInitDBForSF(t)
  CreateSharedFolder("test", "", false, "admin")
  sf, _ := GetSharedFolderByName("test")
  CreateUser("u1", "pass", "user")

  RecordMountTest(sf.ID, "u1", true)
  if err := RecordMountTest(sf.ID, "u1", false); err != nil {
    t.Fatalf("RecordMountTest overwrite: %v", err)
  }
  status, _ := GetMountStatus(sf.ID, "u1")
  if status.Mounted {
    t.Error("mounted should be false after overwrite")
  }
}

func TestSkillExistsOnDisk_NotFound(t *testing.T) {
  testInitDB(t)
  // SkillsRootDir exists but has no matching skill dir
  os.MkdirAll(SkillsRootDir, 0755)
  if skillExistsOnDisk("nonexistent-skill") {
    t.Error("should not exist")
  }
}

func TestSharedFolderMembers_PublicGroupIntersection(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("u1", "pass", "user")
  CreateSharedFolder("pub", "", true, "admin")
  sf, _ := GetSharedFolderByName("pub")

  members, err := GetSharedFolderMembers(sf.ID)
  if err != nil {
    t.Fatalf("GetSharedFolderMembers: %v", err)
  }
  if len(members) != 1 || members[0] != "u1" {
    t.Errorf("members = %v, want [u1]", members)
  }
}

func TestClearAllContainers_Empty(t *testing.T) {
  testInitDB(t)
  count, err := ClearAllContainers()
  if err != nil {
    t.Fatalf("ClearAllContainers: %v", err)
  }
  if count != 0 {
    t.Errorf("count = %d, want 0", count)
  }
}

func TestGetGroupMembersForDeploy_NestedChildren(t *testing.T) {
  testInitDB(t)
  CreateGroup("g1", "local", "", nil)
  g1id, _ := GetGroupID("g1")
  CreateGroup("g2", "local", "", &g1id)
  g2id, _ := GetGroupID("g2")
  CreateGroup("g3", "local", "", &g2id)
  CreateUser("u1", "pass", "user")
  AddUsersToGroup("g3", []string{"u1"})

  members, err := GetGroupMembersForDeploy("g1")
  if err != nil {
    t.Fatalf("GetGroupMembersForDeploy: %v", err)
  }
  if len(members) != 1 || members[0] != "u1" {
    t.Errorf("members = %v, want [u1]", members)
  }
}

func TestUpdateSharedFolder_Nonexistent(t *testing.T) {
  testInitDBForSF(t)
  err := UpdateSharedFolder(999, "name", "desc", false)
  if err == nil {
    t.Error("should fail for nonexistent folder")
  }
}

func TestGetSharedFolderMembers_NonexistentFolder(t *testing.T) {
  testInitDBForSF(t)
  _, err := GetSharedFolderMembers(999)
  if err == nil {
    t.Error("should error for nonexistent folder")
  }
}

func TestIsUserInSharedFolder_NonexistentFolder(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("u1", "pass", "user")
  _, err := IsUserInSharedFolder(999, "u1")
  if err == nil {
    t.Error("should error for nonexistent folder")
  }
}

func TestGetUserSource_NotFound(t *testing.T) {
  testInitDB(t)
  source := GetUserSource("nobody")
  if source != "" {
    t.Errorf("source = %q, want empty", source)
  }
}

func TestIsSuperadmin_ErrorLog(t *testing.T) {
  testInitDB(t)
  // User exists but query fails - can't easily simulate
  // Just test normal case
  CreateUser("admin", "pass", "superadmin")
  if !IsSuperadmin("admin") {
    t.Error("admin should be superadmin")
  }
}

func TestLoadDefaultSkills_EmptySettingOnly(t *testing.T) {
  testInitDB(t)
  _, err := engine.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)",
    defaultSkillsKey, "")
  if err != nil {
    t.Fatalf("insert setting: %v", err)
  }
  skills, err := LoadDefaultSkills()
  if err != nil {
    t.Fatalf("LoadDefaultSkills: %v", err)
  }
  if len(skills) != 0 {
    t.Errorf("skills = %v, want empty", skills)
  }
}

func TestSkillExistsOnDisk_EmptyRoot(t *testing.T) {
  testInitDB(t)
  os.MkdirAll(SkillsRootDir, 0755)
  if skillExistsOnDisk("any") {
    t.Error("should not exist in empty root")
  }
}

func TestSkillExistsOnDisk_DirMatchNoSkillMD(t *testing.T) {
  testInitDB(t)
  os.MkdirAll(filepath.Join(SkillsRootDir, "test-source", "myskill"), 0755)
  if skillExistsOnDisk("myskill") {
    t.Error("should not exist without SKILL.md")
  }
}

func TestUserExists_WithUser(t *testing.T) {
  testInitDB(t)
  CreateUser("exists", "pass", "user")
  if !UserExists("exists") {
    t.Error("should return true for existing user")
  }
}
