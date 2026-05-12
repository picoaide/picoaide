package auth

import (
  "testing"
)

func testInitDBForSF(t *testing.T) {
  t.Helper()
  engine = nil
  dbDataDir = ""
  if err := InitDB(t.TempDir()); err != nil {
    t.Fatalf("InitDB failed: %v", err)
  }
}

// ============================================================
// 共享文件夹 CRUD
// ============================================================

func TestCreateAndGetSharedFolder(t *testing.T) {
  testInitDBForSF(t)
  err := CreateSharedFolder("项目文档", "项目相关", false, "admin")
  if err != nil {
    t.Fatalf("CreateSharedFolder: %v", err)
  }
  sf, err := GetSharedFolderByName("项目文档")
  if err != nil {
    t.Fatalf("GetSharedFolderByName: %v", err)
  }
  if sf.Name != "项目文档" {
    t.Errorf("name = %q, want %q", sf.Name, "项目文档")
  }
  if sf.Description != "项目相关" {
    t.Errorf("description = %q, want %q", sf.Description, "项目相关")
  }
  if sf.IsPublic {
    t.Error("IsPublic should be false")
  }
  if sf.CreatedBy != "admin" {
    t.Errorf("CreatedBy = %q, want %q", sf.CreatedBy, "admin")
  }
  if sf.ID == 0 {
    t.Error("ID should not be 0")
  }
}

func TestCreateSharedFolder_NameUnique(t *testing.T) {
  testInitDBForSF(t)
  if err := CreateSharedFolder("same-name", "", false, "admin"); err != nil {
    t.Fatalf("first CreateSharedFolder: %v", err)
  }
  err := CreateSharedFolder("same-name", "", false, "admin")
  if err == nil {
    t.Fatal("duplicate name should fail")
  }
}

func TestCreateSharedFolder_InvalidName(t *testing.T) {
  testInitDBForSF(t)
  err := CreateSharedFolder("../evil", "", false, "admin")
  if err == nil {
    t.Fatal("name with path traversal should fail")
  }
  err = CreateSharedFolder("", "", false, "admin")
  if err == nil {
    t.Fatal("empty name should fail")
  }
}

func TestGetSharedFolder_ByID(t *testing.T) {
  testInitDBForSF(t)
  if err := CreateSharedFolder("test", "desc", true, "admin"); err != nil {
    t.Fatalf("CreateSharedFolder: %v", err)
  }
  byName, err := GetSharedFolderByName("test")
  if err != nil {
    t.Fatalf("GetSharedFolderByName: %v", err)
  }
  byID, err := GetSharedFolder(byName.ID)
  if err != nil {
    t.Fatalf("GetSharedFolder: %v", err)
  }
  if byID.Name != "test" {
    t.Errorf("name = %q, want %q", byID.Name, "test")
  }
}

func TestGetSharedFolder_NotFound(t *testing.T) {
  testInitDBForSF(t)
  _, err := GetSharedFolder(999)
  if err == nil {
    t.Fatal("should error for nonexistent ID")
  }
  _, err = GetSharedFolderByName("nonexistent")
  if err == nil {
    t.Fatal("should error for nonexistent name")
  }
}

func TestListSharedFolders(t *testing.T) {
  testInitDBForSF(t)
  list, err := ListSharedFolders()
  if err != nil {
    t.Fatalf("ListSharedFolders empty: %v", err)
  }
  if len(list) != 0 {
    t.Errorf("expected empty list, got %d", len(list))
  }
  CreateSharedFolder("a", "", false, "admin")
  CreateSharedFolder("b", "", true, "admin")
  list, err = ListSharedFolders()
  if err != nil {
    t.Fatalf("ListSharedFolders: %v", err)
  }
  if len(list) != 2 {
    t.Fatalf("expected 2 folders, got %d", len(list))
  }
  // 按名称排序
  if list[0].Name != "a" || list[1].Name != "b" {
    t.Errorf("order: %v, want [a b]", []string{list[0].Name, list[1].Name})
  }
}

func TestUpdateSharedFolder(t *testing.T) {
  testInitDBForSF(t)
  CreateSharedFolder("old", "old desc", false, "admin")
  sf, _ := GetSharedFolderByName("old")

  err := UpdateSharedFolder(sf.ID, "new", "new desc", true)
  if err != nil {
    t.Fatalf("UpdateSharedFolder: %v", err)
  }
  updated, _ := GetSharedFolder(sf.ID)
  if updated.Name != "new" {
    t.Errorf("name = %q, want %q", updated.Name, "new")
  }
  if updated.Description != "new desc" {
    t.Errorf("description = %q, want %q", updated.Description, "new desc")
  }
  if !updated.IsPublic {
    t.Error("IsPublic should be true")
  }
}

func TestUpdateSharedFolder_DuplicateName(t *testing.T) {
  testInitDBForSF(t)
  CreateSharedFolder("a", "", false, "admin")
  CreateSharedFolder("b", "", false, "admin")
  sfA, _ := GetSharedFolderByName("a")
  err := UpdateSharedFolder(sfA.ID, "b", "", false)
  if err == nil {
    t.Fatal("renaming to an existing name should fail")
  }
}

func TestDeleteSharedFolder(t *testing.T) {
  testInitDBForSF(t)
  CreateSharedFolder("todelete", "", false, "admin")
  sf, _ := GetSharedFolderByName("todelete")
  if err := DeleteSharedFolder(sf.ID); err != nil {
    t.Fatalf("DeleteSharedFolder: %v", err)
  }
  _, err := GetSharedFolder(sf.ID)
  if err == nil {
    t.Fatal("folder should be deleted")
  }
}

func TestDeleteSharedFolder_Nonexistent(t *testing.T) {
  testInitDBForSF(t)
  err := DeleteSharedFolder(999)
  if err == nil {
    t.Fatal("deleting nonexistent folder should fail")
  }
}

// ============================================================
// 共享文件夹-组关联
// ============================================================

func TestSetAndGetSharedFolderGroups(t *testing.T) {
  testInitDBForSF(t)
  CreateSharedFolder("test", "", false, "admin")
  sf, _ := GetSharedFolderByName("test")
  CreateGroup("g1", "local", "", nil)
  CreateGroup("g2", "local", "", nil)
  g1ID, _ := GetGroupID("g1")
  g2ID, _ := GetGroupID("g2")

  if err := SetSharedFolderGroups(sf.ID, []int64{g1ID, g2ID}); err != nil {
    t.Fatalf("SetSharedFolderGroups: %v", err)
  }
  groups, err := GetSharedFolderGroupIDs(sf.ID)
  if err != nil {
    t.Fatalf("GetSharedFolderGroupIDs: %v", err)
  }
  if len(groups) != 2 {
    t.Fatalf("expected 2 groups, got %d", len(groups))
  }
  // 替换为 g1 仅
  if err := SetSharedFolderGroups(sf.ID, []int64{g1ID}); err != nil {
    t.Fatalf("SetSharedFolderGroups replace: %v", err)
  }
  groups, _ = GetSharedFolderGroupIDs(sf.ID)
  if len(groups) != 1 || groups[0] != g1ID {
    t.Fatalf("groups = %v, want [%d]", groups, g1ID)
  }
}

func TestSetSharedFolderGroups_ClearsAll(t *testing.T) {
  testInitDBForSF(t)
  CreateSharedFolder("test", "", false, "admin")
  sf, _ := GetSharedFolderByName("test")
  CreateGroup("g1", "local", "", nil)
  g1ID, _ := GetGroupID("g1")
  SetSharedFolderGroups(sf.ID, []int64{g1ID})

  // 清空
  if err := SetSharedFolderGroups(sf.ID, []int64{}); err != nil {
    t.Fatalf("SetSharedFolderGroups clear: %v", err)
  }
  groups, _ := GetSharedFolderGroupIDs(sf.ID)
  if len(groups) != 0 {
    t.Fatalf("expected 0 groups, got %d", len(groups))
  }
}

// ============================================================
// 成员计算
// ============================================================

func TestGetSharedFolderMembers_ByGroup(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("member1", "pass", "user")
  CreateUser("member2", "pass", "user")
  CreateSharedFolder("test", "", false, "admin")
  sf, _ := GetSharedFolderByName("test")
  CreateGroup("team", "local", "", nil)
  gid, _ := GetGroupID("team")
  AddUsersToGroup("team", []string{"member1", "member2"})
  SetSharedFolderGroups(sf.ID, []int64{gid})

  members, err := GetSharedFolderMembers(sf.ID)
  if err != nil {
    t.Fatalf("GetSharedFolderMembers: %v", err)
  }
  if len(members) != 2 {
    t.Fatalf("expected 2 members, got %d: %v", len(members), members)
  }
  got := make(map[string]bool)
  for _, m := range members {
    got[m] = true
  }
  if !got["member1"] || !got["member2"] {
    t.Errorf("members = %v, want [member1 member2]", members)
  }
}

func TestGetSharedFolderMembers_Public(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("u1", "pass", "user")
  CreateUser("u2", "pass", "user")
  CreateUser("admin", "pass", "superadmin")
  CreateSharedFolder("pub", "", true, "admin")
  sf, _ := GetSharedFolderByName("pub")

  members, err := GetSharedFolderMembers(sf.ID)
  if err != nil {
    t.Fatalf("GetSharedFolderMembers: %v", err)
  }
  // 超管不应在成员列表中
  if len(members) != 2 {
    t.Fatalf("expected 2 members (no superadmin), got %d: %v", len(members), members)
  }
  for _, m := range members {
    if m == "admin" {
      t.Error("superadmin should not be in members")
    }
  }
}

func TestGetSharedFolderMembers_InheritsSubGroups(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("direct", "pass", "user")
  CreateUser("nested", "pass", "user")
  CreateGroup("parent", "local", "", nil)
  parentID, _ := GetGroupID("parent")
  CreateGroup("child", "local", "", &parentID)
  AddUsersToGroup("parent", []string{"direct"})
  AddUsersToGroup("child", []string{"nested"})

  CreateSharedFolder("test", "", false, "admin")
  sf, _ := GetSharedFolderByName("test")
  gid, _ := GetGroupID("parent")
  SetSharedFolderGroups(sf.ID, []int64{gid})

  members, err := GetSharedFolderMembers(sf.ID)
  if err != nil {
    t.Fatalf("GetSharedFolderMembers: %v", err)
  }
  got := make(map[string]bool)
  for _, m := range members {
    got[m] = true
  }
  if !got["direct"] || !got["nested"] {
    t.Errorf("members = %v, want [direct nested]", members)
  }
}

func TestGetSharedFolderMembers_MultipleGroups(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("a", "pass", "user")
  CreateUser("b", "pass", "user")
  CreateUser("c", "pass", "user")
  CreateGroup("g1", "local", "", nil)
  CreateGroup("g2", "local", "", nil)
  AddUsersToGroup("g1", []string{"a", "b"})
  AddUsersToGroup("g2", []string{"b", "c"})

  CreateSharedFolder("multi", "", false, "admin")
  sf, _ := GetSharedFolderByName("multi")
  g1id, _ := GetGroupID("g1")
  g2id, _ := GetGroupID("g2")
  SetSharedFolderGroups(sf.ID, []int64{g1id, g2id})

  members, err := GetSharedFolderMembers(sf.ID)
  if err != nil {
    t.Fatalf("GetSharedFolderMembers: %v", err)
  }
  if len(members) != 3 {
    t.Fatalf("expected 3 unique members, got %d: %v", len(members), members)
  }
}

func TestIsUserInSharedFolder(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("u1", "pass", "user")
  CreateUser("u2", "pass", "user")
  CreateSharedFolder("test", "", false, "admin")
  sf, _ := GetSharedFolderByName("test")
  CreateGroup("team", "local", "", nil)
  AddUsersToGroup("team", []string{"u1"})
  gid, _ := GetGroupID("team")
  SetSharedFolderGroups(sf.ID, []int64{gid})

  ok, err := IsUserInSharedFolder(sf.ID, "u1")
  if err != nil {
    t.Fatalf("IsUserInSharedFolder u1: %v", err)
  }
  if !ok {
    t.Error("u1 should be in shared folder")
  }
  ok, err = IsUserInSharedFolder(sf.ID, "u2")
  if err != nil {
    t.Fatalf("IsUserInSharedFolder u2: %v", err)
  }
  if ok {
    t.Error("u2 should NOT be in shared folder")
  }
}

func TestGetAccessibleSharedFolders(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("u1", "pass", "user")
  CreateUser("u2", "pass", "user")
  CreateGroup("team", "local", "", nil)
  AddUsersToGroup("team", []string{"u1"})
  gid, _ := GetGroupID("team")

  CreateSharedFolder("pub", "", true, "admin")
  GetSharedFolderByName("pub")
  CreateSharedFolder("grp", "", false, "admin")
  sfGrp, _ := GetSharedFolderByName("grp")
  SetSharedFolderGroups(sfGrp.ID, []int64{gid})

  // u1 能访问 pub + grp
  list, err := GetAccessibleSharedFolders("u1")
  if err != nil {
    t.Fatalf("GetAccessibleSharedFolders u1: %v", err)
  }
  if len(list) != 2 {
    t.Fatalf("u1: expected 2 folders, got %d", len(list))
  }
  // u2 只能访问 pub
  list, err = GetAccessibleSharedFolders("u2")
  if err != nil {
    t.Fatalf("GetAccessibleSharedFolders u2: %v", err)
  }
  if len(list) != 1 || list[0].Name != "pub" {
    t.Fatalf("u2: expected [pub], got %v", list)
  }
}

// ============================================================
// 挂载记录管理
// ============================================================

func TestRecordAndGetMountStatus(t *testing.T) {
  testInitDBForSF(t)
  CreateSharedFolder("test", "", false, "admin")
  sf, _ := GetSharedFolderByName("test")
  CreateUser("u1", "pass", "user")

  if err := RecordMountTest(sf.ID, "u1", true); err != nil {
    t.Fatalf("RecordMountTest: %v", err)
  }
  status, err := GetMountStatus(sf.ID, "u1")
  if err != nil {
    t.Fatalf("GetMountStatus: %v", err)
  }
  if !status.Mounted {
    t.Error("status.Mounted should be true")
  }
  if status.CheckedAt == "" {
    t.Error("CheckedAt should be set")
  }
}

func TestDeleteSharedFolderMountsByUser(t *testing.T) {
  testInitDBForSF(t)
  CreateSharedFolder("sf1", "", false, "admin")
  CreateSharedFolder("sf2", "", false, "admin")
  sf1, _ := GetSharedFolderByName("sf1")
  sf2, _ := GetSharedFolderByName("sf2")
  CreateUser("u1", "pass", "user")

  RecordMountTest(sf1.ID, "u1", true)
  RecordMountTest(sf2.ID, "u1", true)

  if err := DeleteSharedFolderMountsByUser("u1"); err != nil {
    t.Fatalf("DeleteSharedFolderMountsByUser: %v", err)
  }
  _, err := GetMountStatus(sf1.ID, "u1")
  if err == nil {
    t.Error("mount record should be deleted")
  }
}

// ============================================================
// 集成钩子：组删除
// ============================================================

func TestRemoveGroupFromAllSharedFolders(t *testing.T) {
  testInitDBForSF(t)
  CreateSharedFolder("sf1", "", false, "admin")
  CreateSharedFolder("sf2", "", false, "admin")
  sf1, _ := GetSharedFolderByName("sf1")
  sf2, _ := GetSharedFolderByName("sf2")
  CreateGroup("g1", "local", "", nil)
  CreateGroup("g2", "local", "", nil)
  g1id, _ := GetGroupID("g1")
  g2id, _ := GetGroupID("g2")

  SetSharedFolderGroups(sf1.ID, []int64{g1id, g2id})
  SetSharedFolderGroups(sf2.ID, []int64{g1id})

  affected, err := RemoveGroupFromAllSharedFolders(g1id)
  if err != nil {
    t.Fatalf("RemoveGroupFromAllSharedFolders: %v", err)
  }
  if len(affected) != 2 {
    t.Fatalf("expected 2 affected folders, got %d", len(affected))
  }
  // sf1 仍关联 g2
  groups, _ := GetSharedFolderGroupIDs(sf1.ID)
  if len(groups) != 1 || groups[0] != g2id {
    t.Fatalf("sf1 groups = %v, want [%d]", groups, g2id)
  }
  // sf2 无组了（orphaned）
  groups, _ = GetSharedFolderGroupIDs(sf2.ID)
  if len(groups) != 0 {
    t.Fatalf("sf2 groups = %v, want []", groups)
  }
}

func TestGetOrphanedSharedFolders(t *testing.T) {
  testInitDBForSF(t)
  CreateSharedFolder("normal", "", false, "admin")
  CreateSharedFolder("orphan", "", false, "admin")
  CreateSharedFolder("public", "", true, "admin")
  normal, _ := GetSharedFolderByName("normal")
  orphan, _ := GetSharedFolderByName("orphan")
  CreateGroup("g1", "local", "", nil)
  g1id, _ := GetGroupID("g1")

  SetSharedFolderGroups(normal.ID, []int64{g1id})
  // orphan 不关联任何组且非公共

  orphans, err := GetOrphanedSharedFolders()
  if err != nil {
    t.Fatalf("GetOrphanedSharedFolders: %v", err)
  }
  if len(orphans) != 1 || orphans[0].ID != orphan.ID {
    t.Fatalf("orphans = %v, want [%d]", orphans, orphan.ID)
  }
}

// ============================================================
// 成员变更影响分析
// ============================================================

func TestOnGroupMembersAdded(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("u1", "pass", "user")
  CreateUser("u2", "pass", "user")
  CreateGroup("team", "local", "", nil)
  gid, _ := GetGroupID("team")
  CreateSharedFolder("test", "", false, "admin")
  sf, _ := GetSharedFolderByName("test")
  SetSharedFolderGroups(sf.ID, []int64{gid})

  // 只有 u1 在组里，添加 u2 不影响 sf 成员（u2 已经是其他组的成员？）
  // 实际上 onGroupMembersAdded 只返回受影响的用户
  affected, err := OnGroupMembersAdded(gid, []string{"u1", "u2"})
  if err != nil {
    t.Fatalf("OnGroupMembersAdded: %v", err)
  }
  // u1 和 u2 都应该在 affected 中（他们现在可以访问了，如果没加错的话）
  if len(affected) == 0 {
    t.Error("expected affected users")
  }
}

func TestOnGroupMembersRemoved(t *testing.T) {
  testInitDBForSF(t)
  CreateUser("u1", "pass", "user")
  CreateUser("u2", "pass", "user")
  CreateGroup("team", "local", "", nil)
  CreateGroup("backup", "local", "", nil)
  gid, _ := GetGroupID("team")
  backupID, _ := GetGroupID("backup")
  AddUsersToGroup("team", []string{"u1", "u2"})

  CreateSharedFolder("test", "", false, "admin")
  sf, _ := GetSharedFolderByName("test")
  SetSharedFolderGroups(sf.ID, []int64{gid})

  // 模拟：从组中移除 u1（hook 是在移除之后调用）
  RemoveUserFromGroup("team", "u1")
  affected, err := OnGroupMembersRemoved(gid, "u1")
  if err != nil {
    t.Fatalf("OnGroupMembersRemoved: %v", err)
  }
  if len(affected) != 1 || affected[0] != "u1" {
    t.Fatalf("affected = %v, want [u1]", affected)
  }

  // 先把 u2 加入 backup 组，再把 backup 也关联到共享文件夹
  AddUsersToGroup("backup", []string{"u2"})
  SetSharedFolderGroups(sf.ID, []int64{gid, backupID})

  // 从 team 移除 u2 — u2 仍可通过 backup 访问，不应受影响
  RemoveUserFromGroup("team", "u2")
  affected2, err := OnGroupMembersRemoved(gid, "u2")
  if err != nil {
    t.Fatalf("OnGroupMembersRemoved u2: %v", err)
  }
  if len(affected2) != 0 {
    t.Fatalf("u2 affected = %v, want [] (still has backup access)", affected2)
  }
}

// ============================================================
// 清理钩子：认证源切换
// ============================================================

func TestPurgeSharedFolderMountsBySource(t *testing.T) {
  testInitDBForSF(t)
  CreateSharedFolder("test", "", false, "admin")
  sf, _ := GetSharedFolderByName("test")
  CreateUser("u1", "pass", "user")
  CreateGroup("g1", "ldap", "", nil)
  CreateGroup("g2", "local", "", nil)
  g1id, _ := GetGroupID("g1")
  g2id, _ := GetGroupID("g2")
  SetSharedFolderGroups(sf.ID, []int64{g1id, g2id})
  RecordMountTest(sf.ID, "u1", true)

  // 清理 ldap 来源的组关联
  if err := RemoveGroupSourceFromSharedFolders("ldap"); err != nil {
    t.Fatalf("RemoveGroupSourceFromSharedFolders: %v", err)
  }
  groups, _ := GetSharedFolderGroupIDs(sf.ID)
  if len(groups) != 1 || groups[0] != g2id {
    t.Fatalf("groups after ldap purge = %v, want [%d]", groups, g2id)
  }
}
