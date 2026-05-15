package authsource

import (
  "fmt"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
)

type mockDirProvider struct{}

func (mockDirProvider) FetchUsers(cfg *config.GlobalConfig) ([]string, error) {
  return []string{"alice", "bob"}, nil
}

func (mockDirProvider) FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error) {
  switch username {
  case "alice":
    return []string{"dev", "ops"}, nil
  case "bob":
    return []string{"dev"}, nil
  default:
    return nil, nil
  }
}

func (mockDirProvider) FetchGroups(cfg *config.GlobalConfig) (GroupHierarchy, error) {
  return GroupHierarchy{
    "dev": {Members: []string{"alice", "bob"}},
    "ops": {Members: []string{"alice"}},
    "empty": {Members: nil},
  }, nil
}

type mockDirEmptyProvider struct{}

func (mockDirEmptyProvider) FetchUsers(cfg *config.GlobalConfig) ([]string, error) {
  return nil, nil
}

func (mockDirEmptyProvider) FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error) {
  return nil, nil
}

func (mockDirEmptyProvider) FetchGroups(cfg *config.GlobalConfig) (GroupHierarchy, error) {
  return GroupHierarchy{}, nil
}

type mockDirInvalidUserProvider struct{}

func (mockDirInvalidUserProvider) FetchUsers(cfg *config.GlobalConfig) ([]string, error) {
  return []string{"valid-user", "invalid/user!"}, nil
}

func (mockDirInvalidUserProvider) FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error) {
  return nil, nil
}

func (mockDirInvalidUserProvider) FetchGroups(cfg *config.GlobalConfig) (GroupHierarchy, error) {
  return GroupHierarchy{}, nil
}

type mockDirErrorProvider struct{}

func (mockDirErrorProvider) FetchUsers(cfg *config.GlobalConfig) ([]string, error) {
  return nil, fmt.Errorf("fetch users error")
}

func (mockDirErrorProvider) FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error) {
  return nil, nil
}

func (mockDirErrorProvider) FetchGroups(cfg *config.GlobalConfig) (GroupHierarchy, error) {
  return nil, fmt.Errorf("fetch groups error")
}

func testSyncSetup(t *testing.T) {
  t.Helper()
  auth.ResetDB()
  if err := auth.InitDB(t.TempDir()); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
}

func TestSyncUserDirectory_noDirectoryProvider(t *testing.T) {
  _, err := SyncUserDirectory("local", testConfig("local"))
  if err == nil {
    t.Fatal("error expected for local provider")
  }
}

func TestSyncUserDirectory_unregisteredProvider(t *testing.T) {
  _, err := SyncUserDirectory("nonexistent", testConfig("local"))
  if err == nil {
    t.Fatal("error expected for unregistered provider")
  }
}

func TestSyncUserDirectory_withMockProvider(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  Register("test-sync", mockDirProvider{})
  cfg := testConfig("test-sync")

  result, err := SyncUserDirectory("test-sync", cfg)
  if err != nil {
    t.Fatalf("SyncUserDirectory: %v", err)
  }

  if result.ProviderUserCount != 2 {
    t.Fatalf("ProviderUserCount = %d, want 2", result.ProviderUserCount)
  }
  if result.AllowedUserCount != 2 {
    t.Fatalf("AllowedUserCount = %d, want 2", result.AllowedUserCount)
  }
  if result.LocalUserSynced != 2 {
    t.Fatalf("LocalUserSynced = %d, want 2", result.LocalUserSynced)
  }
  if result.GroupMemberCount != 3 {
    t.Fatalf("GroupMemberCount = %d, want 3", result.GroupMemberCount)
  }
}

func TestSyncUserDirectory_cleansLocalUsers(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  if err := auth.CreateUser("localuser", "password", "user"); err != nil {
    t.Fatalf("CreateUser: %v", err)
  }

  Register("test-sync-clean", mockDirProvider{})
  cfg := testConfig("test-sync-clean")

  result, err := SyncUserDirectory("test-sync-clean", cfg)
  if err != nil {
    t.Fatalf("SyncUserDirectory: %v", err)
  }

  if result.DeletedLocalAuth != 1 {
    t.Fatalf("DeletedLocalAuth = %d, want 1", result.DeletedLocalAuth)
  }
  if result.LocalUserSynced != 2 {
    t.Fatalf("LocalUserSynced = %d, want 2", result.LocalUserSynced)
  }
  if auth.UserExists("localuser") {
    t.Error("localuser should have been deleted")
  }
}

func TestSyncUserDirectory_skipsSuperadmin(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  if err := auth.CreateUser("superadmin", "password", "superadmin"); err != nil {
    t.Fatalf("CreateUser: %v", err)
  }

  Register("test-sync-sa", mockDirProvider{})
  cfg := testConfig("test-sync-sa")

  result, err := SyncUserDirectory("test-sync-sa", cfg)
  if err != nil {
    t.Fatalf("SyncUserDirectory: %v", err)
  }

  if result.DeletedLocalAuth != 0 {
    t.Fatalf("DeletedLocalAuth = %d, want 0", result.DeletedLocalAuth)
  }
  if !auth.UserExists("superadmin") {
    t.Error("superadmin should not have been deleted")
  }
}

func TestSyncUserDirectory_emptyProviderUsers(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  Register("test-sync-empty", mockDirEmptyProvider{})
  cfg := testConfig("test-sync-empty")

  result, err := SyncUserDirectory("test-sync-empty", cfg)
  if err != nil {
    t.Fatalf("SyncUserDirectory: %v", err)
  }

  if result.ProviderUserCount != 0 {
    t.Fatalf("ProviderUserCount = %d, want 0", result.ProviderUserCount)
  }
}

func TestSyncUserDirectory_invalidUsername(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  Register("test-sync-inv", mockDirInvalidUserProvider{})
  cfg := testConfig("test-sync-inv")

  result, err := SyncUserDirectory("test-sync-inv", cfg)
  if err != nil {
    t.Fatalf("SyncUserDirectory: %v", err)
  }

  if result.InvalidUsernameCount != 1 {
    t.Fatalf("InvalidUsernameCount = %d, want 1", result.InvalidUsernameCount)
  }
  if result.LocalUserSynced != 1 {
    t.Fatalf("LocalUserSynced = %d, want 1 (only valid-user)", result.LocalUserSynced)
  }
}

func TestSyncGroups_noDirectoryProvider(t *testing.T) {
  _, err := SyncGroups("local", testConfig("local"), nil)
  if err == nil {
    t.Fatal("error expected for local provider")
  }
}

func TestSyncGroups_unregisteredProvider(t *testing.T) {
  _, err := SyncGroups("nonexistent", testConfig("local"), nil)
  if err == nil {
    t.Fatal("error expected for unregistered provider")
  }
}

func TestSyncGroups_withMockProvider(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  Register("test-groups", mockDirProvider{})
  cfg := testConfig("test-groups")

  ensureUserCalled := 0
  ensureUser := func(username string) error {
    ensureUserCalled++
    return nil
  }

  result, err := SyncGroups("test-groups", cfg, ensureUser)
  if err != nil {
    t.Fatalf("SyncGroups: %v", err)
  }

  if result.GroupCount != 3 {
    t.Fatalf("GroupCount = %d, want 3", result.GroupCount)
  }
  if result.MemberCount != 3 {
    t.Fatalf("MemberCount = %d, want 3", result.MemberCount)
  }
  if ensureUserCalled != 3 {
    t.Fatalf("ensureUser called %d times, want 3", ensureUserCalled)
  }
}

func TestSyncGroups_emptyGroups(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  Register("test-groups-empty", mockDirEmptyProvider{})
  cfg := testConfig("test-groups-empty")

  result, err := SyncGroups("test-groups-empty", cfg, nil)
  if err != nil {
    t.Fatalf("SyncGroups with empty groups: %v", err)
  }

  if result.GroupCount != 0 {
    t.Fatalf("GroupCount = %d, want 0", result.GroupCount)
  }
}

func TestSyncGroups_ensureUserError(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  Register("test-groups-err", mockDirProvider{})
  cfg := testConfig("test-groups-err")

  ensureUser := func(username string) error {
    if username == "bob" {
      return fmt.Errorf("rejected")
    }
    return nil
  }

  result, err := SyncGroups("test-groups-err", cfg, ensureUser)
  if err != nil {
    t.Fatalf("SyncGroups: %v", err)
  }

  // alice: dev+ops, bob: dev (rejected)
  if result.MemberCount != 2 {
    t.Fatalf("MemberCount = %d, want 2 (bob rejected)", result.MemberCount)
  }
}

func TestSyncUserDirectory_ensureExternalUserError(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  // Pre-create alice as superadmin - EnsureExternalUser will fail
  if err := auth.CreateUser("alice", "password", "superadmin"); err != nil {
    t.Fatalf("CreateUser: %v", err)
  }

  Register("test-sync-eue", mockDirProvider{})
  cfg := testConfig("test-sync-eue")

  _, err := SyncUserDirectory("test-sync-eue", cfg)
  if err == nil {
    t.Fatal("expected error from EnsureExternalUser for superadmin")
  }
}

func TestSyncUserDirectory_fetchUsersError(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  Register("test-sync-err", mockDirErrorProvider{})
  cfg := testConfig("test-sync-err")

  _, err := SyncUserDirectory("test-sync-err", cfg)
  if err == nil {
    t.Fatal("expected error from FetchUsers")
  }
}

func TestSyncGroups_fetchGroupsError(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  Register("test-groups-err2", mockDirErrorProvider{})
  cfg := testConfig("test-groups-err2")

  _, err := SyncGroups("test-groups-err2", cfg, nil)
  if err == nil {
    t.Fatal("expected error from FetchGroups")
  }
}

func TestSyncGroups_ensureExternalUserError(t *testing.T) {
  testSyncSetup(t)
  defer auth.ResetDB()

  // Pre-create alice as superadmin - EnsureExternalUser will fail for her
  if err := auth.CreateUser("alice", "password", "superadmin"); err != nil {
    t.Fatalf("CreateUser: %v", err)
  }

  Register("test-groups-eue", mockDirProvider{})
  cfg := testConfig("test-groups-eue")

  result, err := SyncGroups("test-groups-eue", cfg, nil)
  if err != nil {
    t.Fatalf("SyncGroups: %v", err)
  }

  // "alice" should be skipped (superadmin can't be external user), "bob" should be included
  // dev: alice(skipped) + bob(included), ops: alice(skipped), empty: no members
  if result.MemberCount != 1 {
    t.Fatalf("MemberCount = %d, want 1 (only bob)", result.MemberCount)
  }
}
