package auth

import (
  "strings"
  "testing"

  "golang.org/x/crypto/bcrypt"
)

func TestGetExternalUsers_NoSource(t *testing.T) {
  testInitDB(t)
  CreateUser("localuser", "pass", "user")
  EnsureExternalUser("ldapuser", "user", "ldap")

  users, err := GetExternalUsers("")
  if err != nil {
    t.Fatalf("GetExternalUsers: %v", err)
  }
  if len(users) != 1 {
    t.Fatalf("len = %d, want 1", len(users))
  }
  if users[0].Username != "ldapuser" || users[0].Source != "ldap" {
    t.Errorf("got %+v, want ldapuser/ldap", users[0])
  }
}

func TestGetExternalUsers_WithSource(t *testing.T) {
  testInitDB(t)
  EnsureExternalUser("ldapuser", "user", "ldap")
  EnsureExternalUser("oidcuser", "user", "oidc")

  ldapUsers, err := GetExternalUsers("ldap")
  if err != nil {
    t.Fatalf("GetExternalUsers ldap: %v", err)
  }
  if len(ldapUsers) != 1 || ldapUsers[0].Username != "ldapuser" {
    t.Errorf("ldap users = %v, want [ldapuser]", ldapUsers)
  }
}

func TestGetExternalUsers_None(t *testing.T) {
  testInitDB(t)
  users, err := GetExternalUsers("")
  if err != nil {
    t.Fatalf("GetExternalUsers: %v", err)
  }
  if len(users) != 0 {
    t.Errorf("len = %d, want 0", len(users))
  }
}

func TestGetSuperadmins(t *testing.T) {
  testInitDB(t)
  CreateUser("admin1", "pass", "superadmin")
  CreateUser("admin2", "pass", "superadmin")
  CreateUser("user1", "pass", "user")

  admins, err := GetSuperadmins()
  if err != nil {
    t.Fatalf("GetSuperadmins: %v", err)
  }
  if len(admins) != 2 {
    t.Fatalf("len = %d, want 2", len(admins))
  }
  if admins[0] != "admin1" || admins[1] != "admin2" {
    t.Errorf("admins = %v, want [admin1 admin2]", admins)
  }
}

func TestGetSuperadmins_None(t *testing.T) {
  testInitDB(t)
  admins, err := GetSuperadmins()
  if err != nil {
    t.Fatalf("GetSuperadmins: %v", err)
  }
  if len(admins) != 0 {
    t.Errorf("len = %d, want 0", len(admins))
  }
}

func TestGetUserRole(t *testing.T) {
  testInitDB(t)
  CreateUser("admin", "pass", "superadmin")
  CreateUser("user", "pass", "user")

  if role := GetUserRole("admin"); role != "superadmin" {
    t.Errorf("admin role = %q, want superadmin", role)
  }
  if role := GetUserRole("user"); role != "user" {
    t.Errorf("user role = %q, want user", role)
  }
  if role := GetUserRole("ghost"); role != "" {
    t.Errorf("ghost role = %q, want empty", role)
  }
}

func TestDeleteAllRegularUsers(t *testing.T) {
  testInitDB(t)
  CreateUser("super", "pass", "superadmin")
  CreateUser("regular", "pass", "user")
  CreateUser("regular2", "pass", "user")

  // Add groups, channels, skills to verify cascade
  CreateGroup("g1", "local", "", nil)
  AddUsersToGroup("g1", []string{"regular", "regular2"})
  UpsertUserChannelStatus("regular", "web", true, true, false, 1)
  BindSkillToUser("regular", "skill1", "self")

  count, err := DeleteAllRegularUsers()
  if err != nil {
    t.Fatalf("DeleteAllRegularUsers: %v", err)
  }
  if count != 2 {
    t.Errorf("deleted count = %d, want 2", count)
  }

  // Superadmin should still exist
  if !UserExists("super") {
    t.Error("superadmin should still exist")
  }
  if UserExists("regular") {
    t.Error("regular user should be deleted")
  }

  // Group should still exist (only members removed)
  _, err = GetGroupID("g1")
  if err != nil {
    t.Error("groups should still exist after regular user deletion")
  }
}

func TestClearAllGroups(t *testing.T) {
  testInitDB(t)
  CreateGroup("g1", "local", "", nil)
  CreateGroup("g2", "ldap", "", nil)
  CreateUser("u1", "pass", "user")
  AddUsersToGroup("g1", []string{"u1"})

  if err := ClearAllGroups(); err != nil {
    t.Fatalf("ClearAllGroups: %v", err)
  }

  list, _ := ListGroups()
  if len(list) != 0 {
    t.Errorf("groups after clear = %d, want 0", len(list))
  }
}

func TestClearAllContainers(t *testing.T) {
  testInitDB(t)
  UpsertContainer(&ContainerRecord{Username: "u1", Image: "img", Status: "running"})
  UpsertContainer(&ContainerRecord{Username: "u2", Image: "img", Status: "stopped"})

  count, err := ClearAllContainers()
  if err != nil {
    t.Fatalf("ClearAllContainers: %v", err)
  }
  if count != 2 {
    t.Errorf("deleted = %d, want 2", count)
  }

  list, _ := GetAllContainers()
  if len(list) != 0 {
    t.Error("containers should be empty after clear")
  }
}

func TestGetUserSource_NotInitialized(t *testing.T) {
  ResetDB()
  source := GetUserSource("nobody")
  if source != "" {
    t.Errorf("source = %q, want empty", source)
  }
}

func TestIsSuperadmin_NotInitialized(t *testing.T) {
  ResetDB()
  if IsSuperadmin("admin") {
    t.Error("IsSuperadmin should return false when DB not initialized")
  }
}

func TestUserExists_NotInitialized(t *testing.T) {
  ResetDB()
  if UserExists("nobody") {
    t.Error("UserExists should return false when DB not initialized")
  }
}

func TestGetUserRole_NotInitialized(t *testing.T) {
  ResetDB()
  role := GetUserRole("nobody")
  if role != "" {
    t.Errorf("role = %q, want empty", role)
  }
}

func TestVerifyArgon2idPassword_InvalidHash(t *testing.T) {
  // Hash with wrong number of parts
  ok, err := verifyArgon2idPassword("$argon2id$v=19$toofew", "pass")
  if ok || err != nil {
    t.Errorf("ok=%v err=%v, want false,nil", ok, err)
  }

  // Hash with wrong prefix (not argon2id)
  ok, err = verifyArgon2idPassword("$bcrypt$foo$bar$baz$qux", "pass")
  if ok || err != nil {
    t.Errorf("ok=%v err=%v, want false,nil", ok, err)
  }
}

func TestVerifyPassword_BcryptFallback(t *testing.T) {
  testInitDB(t)
  // Generate a proper bcrypt hash for "testpass"
  bcryptHash, err := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
  if err != nil {
    t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
  }
  _, err = engine.Exec("INSERT INTO local_users (username, password_hash, role) VALUES (?, ?, ?)",
    "bcryptuser", string(bcryptHash), "user")
  if err != nil {
    t.Fatalf("insert bcrypt user: %v", err)
  }

  ok, needsUpgrade, err := verifyPassword(string(bcryptHash), "testpass")
  if err != nil {
    t.Fatalf("verifyPassword: %v", err)
  }
  if !ok {
    t.Error("bcrypt password should verify")
  }
  if !needsUpgrade {
    t.Error("bcrypt should need upgrade")
  }
}

func TestVerifyPassword_WrongBcrypt(t *testing.T) {
  bcryptHash, err := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
  if err != nil {
    t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
  }

  ok, needsUpgrade, err := verifyPassword(string(bcryptHash), "wrongpassword")
  if err != nil {
    t.Fatalf("verifyPassword: %v", err)
  }
  if ok {
    t.Error("wrong password should not verify")
  }
  if needsUpgrade {
    t.Error("should not need upgrade on failed verification")
  }
}

func TestEnsureExternalUser_SuperadminConflict(t *testing.T) {
  testInitDB(t)
  CreateUser("super", "pass", "superadmin")
  err := EnsureExternalUser("super", "user", "ldap")
  if err == nil {
    t.Error("should fail when converting superadmin to external user")
  }
  if !strings.Contains(err.Error(), "本地超管") {
    t.Errorf("unexpected error: %v", err)
  }
}

func TestEnsureExternalUser_EmptySourceDefaults(t *testing.T) {
  testInitDB(t)
  if err := EnsureExternalUser("externaluser", "", ""); err != nil {
    t.Fatalf("EnsureExternalUser: %v", err)
  }
  source := GetUserSource("externaluser")
  if source != "external" {
    t.Errorf("source = %q, want external", source)
  }
}

func TestAuthenticateLocal_QueryError(t *testing.T) {
  testInitDB(t)
  ok, role, err := AuthenticateLocal("", "")
  if err != nil {
    // Should not error for empty username, just return false
    t.Fatalf("unexpected error: %v", err)
  }
  if ok {
    t.Error("should not authenticate with empty username")
  }
  if role != "" {
    t.Errorf("role = %q, want empty", role)
  }
}

func TestGetAllLocalUsers_Empty(t *testing.T) {
  testInitDB(t)
  users, err := GetAllLocalUsers()
  if err != nil {
    t.Fatalf("GetAllLocalUsers: %v", err)
  }
  if len(users) != 0 {
    t.Errorf("len = %d, want 0", len(users))
  }
}
