package web

import (
  "testing"

  "github.com/picoaide/picoaide/internal/store"
)

func TestSharedFolders_ListEmpty(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/shared-folders", "testadmin")
  assertStatus(t, resp, 200)
}

func TestSharedFolders_CreateSuccess(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postMap(t, "/api/admin/shared-folders/create", "testadmin", map[string]interface{}{
    "name":        "项目文档",
    "description": "项目相关共享文档",
  })
  assertStatus(t, resp, 200)
  resp = env.get(t, "/api/admin/shared-folders", "testadmin")
  assertStatus(t, resp, 200)
}

func TestSharedFolders_CreateDuplicateName(t *testing.T) {
  env := setupTestServer(t)
  body := map[string]interface{}{"name": "test"}
  resp := env.postMap(t, "/api/admin/shared-folders/create", "testadmin", body)
  assertStatus(t, resp, 200)
  resp = env.postMap(t, "/api/admin/shared-folders/create", "testadmin", body)
  assertStatus(t, resp, 400)
}

func TestSharedFolders_CreateInvalidName(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postMap(t, "/api/admin/shared-folders/create", "testadmin", map[string]interface{}{"name": "../evil"})
  assertStatus(t, resp, 400)
}

func TestSharedFolders_CreateEmptyName(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postMap(t, "/api/admin/shared-folders/create", "testadmin", map[string]interface{}{"name": ""})
  assertStatus(t, resp, 400)
}

func TestSharedFolders_UpdateSuccess(t *testing.T) {
  env := setupTestServer(t)
  env.postMap(t, "/api/admin/shared-folders/create", "testadmin", map[string]interface{}{
    "name": "old-name", "description": "old",
  })
  resp := env.get(t, "/api/admin/shared-folders", "testadmin")
  var list struct {
    Success bool `json:"success"`
    Folders []struct {
      ID   int    `json:"id"`
      Name string `json:"name"`
    } `json:"folders"`
  }
  parseJSON(t, resp, &list)
  if !list.Success || len(list.Folders) == 0 {
    t.Fatalf("no folders found")
  }
  id := list.Folders[0].ID

  resp = env.postMap(t, "/api/admin/shared-folders/update", "testadmin", map[string]interface{}{
    "id": id, "name": "new-name", "description": "new desc", "is_public": true,
  })
  assertStatus(t, resp, 200)
}

func TestSharedFolders_DeleteSuccess(t *testing.T) {
  env := setupTestServer(t)
  env.postMap(t, "/api/admin/shared-folders/create", "testadmin", map[string]interface{}{"name": "todelete"})
  resp := env.get(t, "/api/admin/shared-folders", "testadmin")
  var list struct {
    Success bool `json:"success"`
    Folders []struct {
      ID int `json:"id"`
    } `json:"folders"`
  }
  parseJSON(t, resp, &list)
  if len(list.Folders) == 0 {
    t.Fatalf("no folders")
  }
  resp = env.postMap(t, "/api/admin/shared-folders/delete", "testadmin", map[string]interface{}{
    "id": list.Folders[0].ID,
  })
  assertStatus(t, resp, 200)
}

func TestSharedFolders_DeleteNonexistent(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postMap(t, "/api/admin/shared-folders/delete", "testadmin", map[string]interface{}{"id": 999})
  assertStatus(t, resp, 400)
}

func TestSharedFolders_AdminOnly(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/shared-folders", "testuser")
  assertStatus(t, resp, 403)

  resp = env.postMap(t, "/api/admin/shared-folders/create", "testuser", map[string]interface{}{"name": "x"})
  assertStatus(t, resp, 403)
}

func TestSharedFolders_UserCanViewAccessible(t *testing.T) {
  env := setupTestServer(t)
  env.postMap(t, "/api/admin/shared-folders/create", "testadmin", map[string]interface{}{
    "name": "pub", "is_public": true,
  })
  resp := env.get(t, "/api/shared-folders", "testuser")
  assertStatus(t, resp, 200)
}

func TestSharedFolders_SetGroups(t *testing.T) {
  env := setupTestServer(t)
  store.CreateGroup("team-a", "local", "", nil)
  store.CreateGroup("team-b", "local", "", nil)
  gidA, _ := store.GetGroupID("team-a")
  gidB, _ := store.GetGroupID("team-b")

  env.postMap(t, "/api/admin/shared-folders/create", "testadmin", map[string]interface{}{"name": "test"})
  resp := env.get(t, "/api/admin/shared-folders", "testadmin")
  var list struct {
    Success bool `json:"success"`
    Folders []struct {
      ID int `json:"id"`
    } `json:"folders"`
  }
  parseJSON(t, resp, &list)
  id := list.Folders[0].ID

  resp = env.postMap(t, "/api/admin/shared-folders/groups/set", "testadmin", map[string]interface{}{
    "folder_id": id,
    "group_ids": []int64{gidA, gidB},
  })
  assertStatus(t, resp, 200)

  resp = env.postMap(t, "/api/admin/shared-folders/groups/set", "testadmin", map[string]interface{}{
    "folder_id": id,
    "group_ids": []int64{},
  })
  assertStatus(t, resp, 200)
}

func TestSharedFolders_SetGroups_NonexistentFolder(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postMap(t, "/api/admin/shared-folders/groups/set", "testadmin", map[string]interface{}{
    "folder_id": 999,
    "group_ids": []int64{1},
  })
  assertStatus(t, resp, 400)
}

func TestSharedFolders_TestMount(t *testing.T) {
  env := setupTestServer(t)
  store.CreateSharedFolder("test", "", false, "testadmin")
  sf, _ := store.GetSharedFolderByName("test")

  resp := env.postMap(t, "/api/admin/shared-folders/test", "testadmin", map[string]interface{}{
    "folder_id": sf.ID,
    "username":  "testuser",
  })
  assertStatus(t, resp, 200)
}

func TestSharedFolders_TestMount_NonexistentUser(t *testing.T) {
  env := setupTestServer(t)
  store.CreateSharedFolder("test", "", false, "testadmin")
  sf, _ := store.GetSharedFolderByName("test")

  resp := env.postMap(t, "/api/admin/shared-folders/test", "testadmin", map[string]interface{}{
    "folder_id": sf.ID,
    "username":  "ghost",
  })
  assertStatus(t, resp, 200)
}

func TestSharedFolders_MountAll(t *testing.T) {
  env := setupTestServer(t)
  store.CreateSharedFolder("test", "", true, "testadmin")
  sf, _ := store.GetSharedFolderByName("test")

  resp := env.postMap(t, "/api/admin/shared-folders/mount", "testadmin", map[string]interface{}{
    "folder_id": sf.ID,
  })
  assertStatus(t, resp, 200)
}

func TestSharedFolders_MountAll_NonexistentFolder(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postMap(t, "/api/admin/shared-folders/mount", "testadmin", map[string]interface{}{
    "folder_id": 999,
  })
  assertStatus(t, resp, 400)
}

func TestSharedFolders_GroupDeleteCascades(t *testing.T) {
  env := setupTestServer(t)
  store.CreateGroup("to-delete", "local", "", nil)
  gid, _ := store.GetGroupID("to-delete")
  store.AddUsersToGroup("to-delete", []string{"testuser"})

  store.CreateSharedFolder("shared-with-group", "", false, "testadmin")
  sf, _ := store.GetSharedFolderByName("shared-with-group")
  store.SetSharedFolderGroups(sf.ID, []int64{gid})

  resp := env.postMap(t, "/api/admin/groups/delete", "testadmin", map[string]interface{}{"name": "to-delete"})
  assertStatus(t, resp, 200)

  groups, _ := store.GetSharedFolderGroupIDs(sf.ID)
  if len(groups) != 0 {
    t.Errorf("shared folder still has %d group associations after group delete", len(groups))
  }
}

func TestSharedFolders_UserDeleteCleansMountRecords(t *testing.T) {
  env := setupTestServer(t)
  store.CreateUser("delete-me", "pass", "user")

  store.CreateSharedFolder("test", "", false, "testadmin")
  sf, _ := store.GetSharedFolderByName("test")
  store.RecordMountTest(sf.ID, "delete-me", true)

  resp := env.postMap(t, "/api/admin/users/delete", "testadmin", map[string]interface{}{"username": "delete-me"})
  assertStatus(t, resp, 200)

  mountStatuses, err := store.GetMountStatusesForFolder(sf.ID)
  if err != nil {
    t.Fatal(err)
  }
  if _, exists := mountStatuses["delete-me"]; exists {
    t.Error("mount record should be deleted after user deletion")
  }
}
