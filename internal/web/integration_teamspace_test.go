package web

import (
  "net/url"
  "testing"

  "github.com/picoaide/picoaide/internal/auth"
)

// ============================================================
// 基础 CRUD
// ============================================================

func TestSharedFolders_ListEmpty(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/shared-folders", "testadmin")
  assertStatus(t, resp, 200)
}

func TestSharedFolders_CreateSuccess(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "name":        {"项目文档"},
    "description": {"项目相关共享文档"},
  }
  resp := env.postForm(t, "/api/admin/shared-folders/create", "testadmin", form)
  assertStatus(t, resp, 200)
  // 验证能读到
  resp = env.get(t, "/api/admin/shared-folders", "testadmin")
  assertStatus(t, resp, 200)
}

func TestSharedFolders_CreateDuplicateName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"name": {"test"}}
  resp := env.postForm(t, "/api/admin/shared-folders/create", "testadmin", form)
  assertStatus(t, resp, 200)
  resp = env.postForm(t, "/api/admin/shared-folders/create", "testadmin", form)
  assertStatus(t, resp, 400)
}

func TestSharedFolders_CreateInvalidName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"name": {"../evil"}}
  resp := env.postForm(t, "/api/admin/shared-folders/create", "testadmin", form)
  assertStatus(t, resp, 400)
}

func TestSharedFolders_CreateEmptyName(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postForm(t, "/api/admin/shared-folders/create", "testadmin", url.Values{"name": {""}})
  assertStatus(t, resp, 400)
}

func TestSharedFolders_UpdateSuccess(t *testing.T) {
  env := setupTestServer(t)
  // 先创建
  env.postForm(t, "/api/admin/shared-folders/create", "testadmin", url.Values{
    "name": {"old-name"},
    "description": {"old"},
  })
  // 获取列表得到 ID
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

  // 更新
  form := url.Values{
    "id":          {itoa(id)},
    "name":        {"new-name"},
    "description": {"new desc"},
    "is_public":   {"1"},
  }
  resp = env.postForm(t, "/api/admin/shared-folders/update", "testadmin", form)
  assertStatus(t, resp, 200)
}

func TestSharedFolders_DeleteSuccess(t *testing.T) {
  env := setupTestServer(t)
  env.postForm(t, "/api/admin/shared-folders/create", "testadmin", url.Values{"name": {"todelete"}})
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
  resp = env.postForm(t, "/api/admin/shared-folders/delete", "testadmin", url.Values{
    "id": {itoa(list.Folders[0].ID)},
  })
  assertStatus(t, resp, 200)
}

func TestSharedFolders_DeleteNonexistent(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postForm(t, "/api/admin/shared-folders/delete", "testadmin", url.Values{"id": {"999"}})
  assertStatus(t, resp, 400)
}

// ============================================================
// 权限检查
// ============================================================

func TestSharedFolders_AdminOnly(t *testing.T) {
  env := setupTestServer(t)
  // 普通用户不能访问 admin 端点
  resp := env.get(t, "/api/admin/shared-folders", "testuser")
  assertStatus(t, resp, 403)

  resp = env.postForm(t, "/api/admin/shared-folders/create", "testuser", url.Values{"name": {"x"}})
  assertStatus(t, resp, 403)
}

func TestSharedFolders_UserCanViewAccessible(t *testing.T) {
  env := setupTestServer(t)
  // 创建公共共享文件夹
  env.postForm(t, "/api/admin/shared-folders/create", "testadmin", url.Values{
    "name": {"pub"},
    "is_public": {"1"},
  })
  // 普通用户可访问
  resp := env.get(t, "/api/shared-folders", "testuser")
  assertStatus(t, resp, 200)
}

// ============================================================
// 组关联管理
// ============================================================

func TestSharedFolders_SetGroups(t *testing.T) {
  env := setupTestServer(t)
  // 创建组
  auth.CreateGroup("team-a", "local", "", nil)
  auth.CreateGroup("team-b", "local", "", nil)
  gidA, _ := auth.GetGroupID("team-a")
  gidB, _ := auth.GetGroupID("team-b")

  // 创建共享文件夹
  env.postForm(t, "/api/admin/shared-folders/create", "testadmin", url.Values{"name": {"test"}})
  resp := env.get(t, "/api/admin/shared-folders", "testadmin")
  var list struct {
    Success bool `json:"success"`
    Folders []struct {
      ID int `json:"id"`
    } `json:"folders"`
  }
  parseJSON(t, resp, &list)
  id := list.Folders[0].ID

  // 设置关联组
  resp = env.postForm(t, "/api/admin/shared-folders/groups/set", "testadmin", url.Values{
    "folder_id": {itoa(id)},
    "group_ids": {itoa(int(gidA)) + "," + itoa(int(gidB))},
  })
  assertStatus(t, resp, 200)

  // 清除关联组
  resp = env.postForm(t, "/api/admin/shared-folders/groups/set", "testadmin", url.Values{
    "folder_id": {itoa(id)},
    "group_ids": {""},
  })
  assertStatus(t, resp, 200)
}

func TestSharedFolders_SetGroups_NonexistentFolder(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postForm(t, "/api/admin/shared-folders/groups/set", "testadmin", url.Values{
    "folder_id": {"999"},
    "group_ids": {"1"},
  })
  assertStatus(t, resp, 400)
}

// ============================================================
// 测试挂载
// ============================================================

func TestSharedFolders_TestMount(t *testing.T) {
  env := setupTestServer(t)
  // 创建共享文件夹
  auth.CreateSharedFolder("test", "", false, "testadmin")
  sf, _ := auth.GetSharedFolderByName("test")

  // 测试挂载（Docker 不可用，预期返回错误信息但请求本身成功）
  resp := env.postForm(t, "/api/admin/shared-folders/test", "testadmin", url.Values{
    "folder_id": {itoa(int(sf.ID))},
    "username":  {"testuser"},
  })
  // 请求应该成功返回（即使 Docker 不可用，也会返回正确的结果）
  assertStatus(t, resp, 200)
}

func TestSharedFolders_TestMount_NonexistentUser(t *testing.T) {
  env := setupTestServer(t)
  auth.CreateSharedFolder("test", "", false, "testadmin")
  sf, _ := auth.GetSharedFolderByName("test")

  resp := env.postForm(t, "/api/admin/shared-folders/test", "testadmin", url.Values{
    "folder_id": {itoa(int(sf.ID))},
    "username":  {"ghost"},
  })
  assertStatus(t, resp, 200)
}

// ============================================================
// 一键挂载
// ============================================================

func TestSharedFolders_MountAll(t *testing.T) {
  env := setupTestServer(t)
  auth.CreateSharedFolder("test", "", true, "testadmin")
  sf, _ := auth.GetSharedFolderByName("test")

  resp := env.postForm(t, "/api/admin/shared-folders/mount", "testadmin", url.Values{
    "folder_id": {itoa(int(sf.ID))},
  })
  assertStatus(t, resp, 200)
}

func TestSharedFolders_MountAll_NonexistentFolder(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postForm(t, "/api/admin/shared-folders/mount", "testadmin", url.Values{
    "folder_id": {"999"},
  })
  assertStatus(t, resp, 400)
}

// ============================================================
// 集成：组删除级联
// ============================================================

func TestSharedFolders_GroupDeleteCascades(t *testing.T) {
  env := setupTestServer(t)
  // 创建组、用户
  auth.CreateGroup("to-delete", "local", "", nil)
  gid, _ := auth.GetGroupID("to-delete")
  auth.AddUsersToGroup("to-delete", []string{"testuser"})

  // 创建共享文件夹关联该组
  auth.CreateSharedFolder("shared-with-group", "", false, "testadmin")
  sf, _ := auth.GetSharedFolderByName("shared-with-group")
  auth.SetSharedFolderGroups(sf.ID, []int64{gid})

  // 删除组
  resp := env.postForm(t, "/api/admin/groups/delete", "testadmin", url.Values{"name": {"to-delete"}})
  assertStatus(t, resp, 200)

  // 验证共享文件夹的组关联已被清除
  groups, _ := auth.GetSharedFolderGroupIDs(sf.ID)
  if len(groups) != 0 {
    t.Errorf("shared folder still has %d group associations after group delete", len(groups))
  }
}

// ============================================================
// 集成：用户删除清理
// ============================================================

func TestSharedFolders_UserDeleteCleansMountRecords(t *testing.T) {
  env := setupTestServer(t)
  // 创建额外用户
  auth.CreateUser("delete-me", "pass", "user")

  // 创建共享文件夹并记录挂载
  auth.CreateSharedFolder("test", "", false, "testadmin")
  sf, _ := auth.GetSharedFolderByName("test")
  auth.RecordMountTest(sf.ID, "delete-me", true)

  // 删除用户
  resp := env.postForm(t, "/api/admin/users/delete", "testadmin", url.Values{"username": {"delete-me"}})
  assertStatus(t, resp, 200)

  // 验证挂载记录已清理
  _, err := auth.GetMountStatus(sf.ID, "delete-me")
  if err == nil {
    t.Error("mount record should be deleted after user deletion")
  }
}

// ============================================================
// 工具函数
// ============================================================

// itoa 将 int 转为 string
func itoa(n int) string {
  if n == 0 {
    return "0"
  }
  s := ""
  for n > 0 {
    s = string(rune('0'+n%10)) + s
    n /= 10
  }
  return s
}
