package auth

import (
  "testing"
)

func TestDeleteGroup(t *testing.T) {
  testInitDB(t)
  if err := CreateGroup("testgroup", "local", "desc", nil); err != nil {
    t.Fatalf("CreateGroup: %v", err)
  }
  if err := DeleteGroup("testgroup"); err != nil {
    t.Fatalf("DeleteGroup: %v", err)
  }
  // Group should not exist anymore
  _, err := GetGroupID("testgroup")
  if err == nil {
    t.Error("group should be deleted")
  }
}

func TestDeleteGroup_Nonexistent(t *testing.T) {
  testInitDB(t)
  err := DeleteGroup("ghost")
  if err == nil {
    t.Error("deleting nonexistent group should fail")
  }
}

func TestDeleteGroup_PromotesChildren(t *testing.T) {
  testInitDB(t)
  CreateGroup("parent", "local", "", nil)
  pid, _ := GetGroupID("parent")
  CreateGroup("child", "local", "", &pid)
  AddUsersToGroup("child", []string{"u1"})

  DeleteGroup("parent")

  // child should be promoted to top-level and still have members
  members, err := GetGroupMembers("child")
  if err != nil {
    t.Fatalf("GetGroupMembers child: %v", err)
  }
  if len(members) != 1 || members[0] != "u1" {
    t.Errorf("child members = %v, want [u1]", members)
  }
}

func TestListGroups_Empty(t *testing.T) {
  testInitDB(t)
  list, err := ListGroups()
  if err != nil {
    t.Fatalf("ListGroups: %v", err)
  }
  if len(list) != 0 {
    t.Errorf("len = %d, want 0", len(list))
  }
}

func TestListGroups_WithData(t *testing.T) {
  testInitDB(t)
  CreateGroup("alpha", "local", "first", nil)
  CreateGroup("beta", "local", "second", nil)
  CreateUser("u1", "pass", "user")
  AddUsersToGroup("alpha", []string{"u1"})

  list, err := ListGroups()
  if err != nil {
    t.Fatalf("ListGroups: %v", err)
  }
  if len(list) != 2 {
    t.Fatalf("len = %d, want 2", len(list))
  }
  if list[0].Name != "alpha" || list[1].Name != "beta" {
    t.Errorf("order: %v, want [alpha beta]", groupNames(list))
  }
  if list[0].MemberCount != 1 {
    t.Errorf("alpha member_count = %d, want 1", list[0].MemberCount)
  }
}

func groupNames(list []GroupInfo) []string {
  names := make([]string, len(list))
  for i, g := range list {
    names[i] = g.Name
  }
  return names
}

func TestGetGroupID_NotFound(t *testing.T) {
  testInitDB(t)
  _, err := GetGroupID("nonexistent")
  if err == nil {
    t.Error("should error for nonexistent group")
  }
}

func TestAddUsersToGroup_NonexistentGroup(t *testing.T) {
  testInitDB(t)
  err := AddUsersToGroup("ghost", []string{"u1"})
  if err == nil {
    t.Error("should fail for nonexistent group")
  }
}

func TestAddUsersToGroup_EmptyList(t *testing.T) {
  testInitDB(t)
  CreateGroup("g", "local", "", nil)
  if err := AddUsersToGroup("g", []string{}); err != nil {
    t.Fatalf("AddUsersToGroup empty: %v", err)
  }
  members, _ := GetGroupMembers("g")
  if len(members) != 0 {
    t.Errorf("members = %v, want empty", members)
  }
}

func TestGetGroupMembersWithSubGroups(t *testing.T) {
  testInitDB(t)
  CreateGroup("parent", "local", "", nil)
  pid, _ := GetGroupID("parent")
  CreateGroup("child", "local", "", &pid)
  CreateUser("u1", "pass", "user")
  CreateUser("u2", "pass", "user")
  AddUsersToGroup("parent", []string{"u1"})
  AddUsersToGroup("child", []string{"u2"})

  direct, inherited, err := GetGroupMembersWithSubGroups("parent")
  if err != nil {
    t.Fatalf("GetGroupMembersWithSubGroups: %v", err)
  }
  if len(direct) != 1 || direct[0] != "u1" {
    t.Errorf("direct = %v, want [u1]", direct)
  }
  if len(inherited) != 1 || inherited[0] != "u2" {
    t.Errorf("inherited = %v, want [u2]", inherited)
  }
}

func TestSetGroupParent(t *testing.T) {
  testInitDB(t)
  CreateGroup("child", "local", "", nil)
  CreateGroup("newparent", "local", "", nil)
  pid, _ := GetGroupID("newparent")

  if err := SetGroupParent("child", &pid); err != nil {
    t.Fatalf("SetGroupParent: %v", err)
  }

  var group Group
  has, _ := engine.Where("name = ?", "child").Get(&group)
  if !has {
    t.Fatal("child group should exist")
  }
  if group.ParentID == nil || *group.ParentID != pid {
    t.Errorf("child parent_id = %v, want %d", group.ParentID, pid)
  }
}

func TestGetSubGroupIDs_DeepRecursion(t *testing.T) {
  testInitDB(t)
  CreateGroup("l1", "local", "", nil)
  l1id, _ := GetGroupID("l1")
  CreateGroup("l2", "local", "", &l1id)
  l2id, _ := GetGroupID("l2")
  CreateGroup("l3", "local", "", &l2id)
  l3id, _ := GetGroupID("l3")

  ids, err := GetSubGroupIDs(l1id)
  if err != nil {
    t.Fatalf("GetSubGroupIDs: %v", err)
  }
  if len(ids) != 2 {
    t.Fatalf("len = %d, want 2 (l2, l3)", len(ids))
  }
  if ids[0] != l2id || ids[1] != l3id {
    t.Errorf("ids = %v, want [%d %d]", ids, l2id, l3id)
  }
}
