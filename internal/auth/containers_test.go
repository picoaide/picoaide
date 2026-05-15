package auth

import (
  "testing"
)

func TestGetContainerByUsername_Existing(t *testing.T) {
  testInitDB(t)
  upsertRec := &ContainerRecord{Username: "user1", Image: "img:1", Status: "running", IP: "100.64.0.2"}
  if err := UpsertContainer(upsertRec); err != nil {
    t.Fatalf("UpsertContainer: %v", err)
  }
  got, err := GetContainerByUsername("user1")
  if err != nil {
    t.Fatalf("GetContainerByUsername: %v", err)
  }
  if got == nil {
    t.Fatal("expected non-nil record")
  }
  if got.Username != "user1" || got.Image != "img:1" || got.Status != "running" {
    t.Errorf("got %+v, want username=user1 image=img:1 status=running", got)
  }
}

func TestGetContainerByUsername_NotFound(t *testing.T) {
  testInitDB(t)
  got, err := GetContainerByUsername("nonexistent")
  if err != nil {
    t.Fatalf("GetContainerByUsername: %v", err)
  }
  if got != nil {
    t.Fatal("expected nil for nonexistent user")
  }
}

func TestGetAllContainers_WithData(t *testing.T) {
  testInitDB(t)
  UpsertContainer(&ContainerRecord{Username: "u1", Image: "img1", Status: "running"})
  UpsertContainer(&ContainerRecord{Username: "u2", Image: "img2", Status: "stopped"})
  list, err := GetAllContainers()
  if err != nil {
    t.Fatalf("GetAllContainers: %v", err)
  }
  if len(list) != 2 {
    t.Fatalf("len = %d, want 2", len(list))
  }
}

func TestGetAllContainers_Empty(t *testing.T) {
  testInitDB(t)
  list, err := GetAllContainers()
  if err != nil {
    t.Fatalf("GetAllContainers: %v", err)
  }
  if len(list) != 0 {
    t.Errorf("len = %d, want 0", len(list))
  }
}

func TestDeleteContainer(t *testing.T) {
  testInitDB(t)
  UpsertContainer(&ContainerRecord{Username: "todelete", Image: "img", Status: "stopped"})
  if err := DeleteContainer("todelete"); err != nil {
    t.Fatalf("DeleteContainer: %v", err)
  }
  got, _ := GetContainerByUsername("todelete")
  if got != nil {
    t.Error("container should be deleted")
  }
}

func TestDeleteContainer_NotFound(t *testing.T) {
  testInitDB(t)
  if err := DeleteContainer("ghost"); err != nil {
    t.Errorf("DeleteContainer on nonexistent should succeed: %v", err)
  }
}

func TestUpdateContainerStatus(t *testing.T) {
  testInitDB(t)
  UpsertContainer(&ContainerRecord{Username: "u1", Image: "img", Status: "stopped"})
  if err := UpdateContainerStatus("u1", "running"); err != nil {
    t.Fatalf("UpdateContainerStatus: %v", err)
  }
  got, _ := GetContainerByUsername("u1")
  if got.Status != "running" {
    t.Errorf("status = %q, want running", got.Status)
  }
}

func TestUpdateContainerID(t *testing.T) {
  testInitDB(t)
  UpsertContainer(&ContainerRecord{Username: "u1", Image: "img", Status: "stopped"})
  if err := UpdateContainerID("u1", "abc123"); err != nil {
    t.Fatalf("UpdateContainerID: %v", err)
  }
  got, _ := GetContainerByUsername("u1")
  if got.ContainerID != "abc123" {
    t.Errorf("container_id = %q, want abc123", got.ContainerID)
  }
}

func TestUpdateContainerImage(t *testing.T) {
  testInitDB(t)
  UpsertContainer(&ContainerRecord{Username: "u1", Image: "oldimg", Status: "stopped"})
  if err := UpdateContainerImage("u1", "newimg"); err != nil {
    t.Fatalf("UpdateContainerImage: %v", err)
  }
  got, _ := GetContainerByUsername("u1")
  if got.Image != "newimg" {
    t.Errorf("image = %q, want newimg", got.Image)
  }
}

func TestUpsertUserChannelStatus_Create(t *testing.T) {
  testInitDB(t)
  if err := UpsertUserChannelStatus("u1", "web", true, true, false, 1); err != nil {
    t.Fatalf("UpsertUserChannelStatus: %v", err)
  }
  rec, err := GetUserChannelStatus("u1", "web")
  if err != nil {
    t.Fatalf("GetUserChannelStatus: %v", err)
  }
  if rec == nil {
    t.Fatal("expected non-nil record")
  }
  if !rec.Allowed || !rec.Enabled || rec.Configured || rec.ConfigVersion != 1 {
    t.Errorf("unexpected record: %+v", rec)
  }
}

func TestUpsertUserChannelStatus_Update(t *testing.T) {
  testInitDB(t)
  UpsertUserChannelStatus("u1", "web", true, false, false, 1)
  if err := UpsertUserChannelStatus("u1", "web", true, true, false, 2); err != nil {
    t.Fatalf("UpsertUserChannelStatus update: %v", err)
  }
  rec, _ := GetUserChannelStatus("u1", "web")
  if !rec.Enabled || rec.ConfigVersion != 2 {
    t.Errorf("after update: enabled=%v version=%d", rec.Enabled, rec.ConfigVersion)
  }
}

func TestGetUserChannelStatus_NotFound(t *testing.T) {
  testInitDB(t)
  rec, err := GetUserChannelStatus("nobody", "web")
  if err != nil {
    t.Fatalf("GetUserChannelStatus: %v", err)
  }
  if rec != nil {
    t.Fatal("expected nil for nonexistent channel")
  }
}

func TestBoolInt(t *testing.T) {
  if boolInt(true) != 1 {
    t.Error("boolInt(true) should be 1")
  }
  if boolInt(false) != 0 {
    t.Error("boolInt(false) should be 0")
  }
}

func TestAllocateNextIP_InvalidIP(t *testing.T) {
  testInitDB(t)
  // Insert a record with invalid IP
  UpsertContainer(&ContainerRecord{Username: "badip", Image: "img", Status: "stopped", IP: "not.an.ip"})
  ip, err := AllocateNextIP()
  if err != nil {
    t.Fatalf("AllocateNextIP: %v", err)
  }
  if ip != "100.64.0.2" {
    t.Errorf("ip = %q, want 100.64.0.2", ip)
  }
}

func TestGenerateMCPToken_NewRecord(t *testing.T) {
  testInitDB(t)
  token, err := GenerateMCPToken("newuser")
  if err != nil {
    t.Fatalf("GenerateMCPToken: %v", err)
  }
  if token == "" {
    t.Fatal("token should not be empty")
  }
  // Verify it created a container record
  rec, _ := GetContainerByUsername("newuser")
  if rec == nil {
    t.Fatal("expected container record to be created")
  }
  if rec.MCPToken != token {
    t.Errorf("stored token = %q, want %q", rec.MCPToken, token)
  }
}

func TestGetMCPToken_NotFound(t *testing.T) {
  testInitDB(t)
  token, err := GetMCPToken("nobody")
  if err != nil {
    t.Fatalf("GetMCPToken: %v", err)
  }
  if token != "" {
    t.Errorf("token = %q, want empty", token)
  }
}
