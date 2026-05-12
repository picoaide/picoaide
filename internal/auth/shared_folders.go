package auth

import (
  "fmt"
  "path/filepath"
  "time"

  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 共享文件夹 CRUD
// ============================================================

// CreateSharedFolder 创建共享文件夹。name 全局唯一，需 SafePathSegment 校验。
func CreateSharedFolder(name, description string, isPublic bool, createdBy string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  if name == "" {
    return fmt.Errorf("名称不能为空")
  }
  if err := util.SafePathSegment(name); err != nil {
    return fmt.Errorf("名称不合法: %w", err)
  }
  now := time.Now().Format("2006-01-02 15:04:05")
  sf := &SharedFolder{
    Name:        name,
    Description: description,
    IsPublic:    isPublic,
    CreatedBy:   createdBy,
    CreatedAt:   now,
    UpdatedAt:   now,
  }
  _, err := engine.Insert(sf)
  if err != nil {
    return fmt.Errorf("创建共享文件夹失败: %w", err)
  }
  return nil
}

// GetSharedFolder 按 ID 获取共享文件夹
func GetSharedFolder(id int64) (*SharedFolder, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var sf SharedFolder
  has, err := engine.ID(id).Get(&sf)
  if err != nil {
    return nil, err
  }
  if !has {
    return nil, fmt.Errorf("共享文件夹不存在")
  }
  return &sf, nil
}

// GetSharedFolderByName 按名称获取共享文件夹
func GetSharedFolderByName(name string) (*SharedFolder, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var sf SharedFolder
  has, err := engine.Where("name = ?", name).Get(&sf)
  if err != nil {
    return nil, err
  }
  if !has {
    return nil, fmt.Errorf("共享文件夹 %s 不存在", name)
  }
  return &sf, nil
}

// ListSharedFolders 返回全部共享文件夹列表（按名称排序）
func ListSharedFolders() ([]SharedFolder, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var list []SharedFolder
  err := engine.OrderBy("name").Find(&list)
  if err != nil {
    return nil, err
  }
  if list == nil {
    list = []SharedFolder{}
  }
  return list, nil
}

// UpdateSharedFolder 更新共享文件夹。改名时调用方需处理主机目录 mv 和容器重启。
func UpdateSharedFolder(id int64, name, description string, isPublic bool) error {
  if err := ensureDB(); err != nil {
    return err
  }
  if name == "" {
    return fmt.Errorf("名称不能为空")
  }
  if err := util.SafePathSegment(name); err != nil {
    return fmt.Errorf("名称不合法: %w", err)
  }
  // 检查新名称是否已被其他记录占用
  existing, err := GetSharedFolderByName(name)
  if err == nil && existing.ID != id {
    return fmt.Errorf("名称已被占用")
  }
  now := time.Now().Format("2006-01-02 15:04:05")
  sf := &SharedFolder{
    Name:        name,
    Description: description,
    IsPublic:    isPublic,
    UpdatedAt:   now,
  }
  affected, err := engine.ID(id).Cols("name", "description", "is_public", "updated_at").Update(sf)
  if err != nil {
    return fmt.Errorf("更新共享文件夹失败: %w", err)
  }
  if affected == 0 {
    return fmt.Errorf("共享文件夹不存在")
  }
  return nil
}

// DeleteSharedFolder 删除共享文件夹记录（及关联的组关联和挂载记录）
func DeleteSharedFolder(id int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    return err
  }
  // 删除组关联
  if _, err := session.Where("folder_id = ?", id).Delete(&SharedFolderGroup{}); err != nil {
    _ = session.Rollback()
    return err
  }
  // 删除挂载记录
  if _, err := session.Where("folder_id = ?", id).Delete(&SharedFolderMount{}); err != nil {
    _ = session.Rollback()
    return err
  }
  // 删除文件夹
  affected, err := session.ID(id).Delete(&SharedFolder{})
  if err != nil {
    _ = session.Rollback()
    return err
  }
  if affected == 0 {
    _ = session.Rollback()
    return fmt.Errorf("共享文件夹不存在")
  }
  return session.Commit()
}

// ============================================================
// 共享文件夹-组关联
// ============================================================

// SetSharedFolderGroups 全量替换共享文件夹的关联组
func SetSharedFolderGroups(folderID int64, groupIDs []int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  if _, err := GetSharedFolder(folderID); err != nil {
    return err
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    return err
  }
  if _, err := session.Where("folder_id = ?", folderID).Delete(&SharedFolderGroup{}); err != nil {
    _ = session.Rollback()
    return err
  }
  for _, gid := range groupIDs {
    if _, err := session.Insert(&SharedFolderGroup{FolderID: folderID, GroupID: gid}); err != nil {
      _ = session.Rollback()
      return err
    }
  }
  return session.Commit()
}

// GetSharedFolderGroupIDs 获取共享文件夹关联的组 ID 列表
func GetSharedFolderGroupIDs(folderID int64) ([]int64, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var sfg []SharedFolderGroup
  err := engine.Where("folder_id = ?", folderID).Find(&sfg)
  if err != nil {
    return nil, err
  }
  ids := make([]int64, 0, len(sfg))
  for _, s := range sfg {
    ids = append(ids, s.GroupID)
  }
  return ids, nil
}

// ============================================================
// 成员计算
// ============================================================

// GetSharedFolderMembers 获取共享文件夹的所有可访问用户（去重，排除超管）
func GetSharedFolderMembers(folderID int64) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  sf, err := GetSharedFolder(folderID)
  if err != nil {
    return nil, err
  }
  memberSet := make(map[string]bool)

  if sf.IsPublic {
    // 公共共享：所有非超管用户
    users, err := GetAllNonSuperadminUsers()
    if err != nil {
      return nil, err
    }
    for _, u := range users {
      memberSet[u] = true
    }
  }

  // 通过关联组获取成员（递归含子组）
  groupIDs, err := GetSharedFolderGroupIDs(folderID)
  if err != nil {
    return nil, err
  }
  for _, gid := range groupIDs {
    group, err := getGroupByID(gid)
    if err != nil {
      continue
    }
    members, err := GetGroupMembersForDeploy(group.Name)
    if err != nil {
      continue
    }
    for _, m := range members {
      if !IsSuperadmin(m) {
        memberSet[m] = true
      }
    }
  }

  result := make([]string, 0, len(memberSet))
  for m := range memberSet {
    result = append(result, m)
  }
  return result, nil
}

// IsUserInSharedFolder 判断用户是否有权访问指定共享文件夹
func IsUserInSharedFolder(folderID int64, username string) (bool, error) {
  if IsSuperadmin(username) {
    return false, nil
  }
  sf, err := GetSharedFolder(folderID)
  if err != nil {
    return false, err
  }
  if sf.IsPublic {
    return true, nil
  }
  // 检查用户是否在任意关联组的递归成员中
  groupIDs, err := GetSharedFolderGroupIDs(folderID)
  if err != nil {
    return false, err
  }
  for _, gid := range groupIDs {
    group, err := getGroupByID(gid)
    if err != nil {
      continue
    }
    members, err := GetGroupMembersForDeploy(group.Name)
    if err != nil {
      continue
    }
    for _, m := range members {
      if m == username {
        return true, nil
      }
    }
  }
  return false, nil
}

// GetAccessibleSharedFolders 获取用户所有可访问的共享文件夹
func GetAccessibleSharedFolders(username string) ([]SharedFolder, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  if IsSuperadmin(username) {
    return []SharedFolder{}, nil
  }
  all, err := ListSharedFolders()
  if err != nil {
    return nil, err
  }
  accessible := make([]SharedFolder, 0, len(all))
  for _, sf := range all {
    ok, err := IsUserInSharedFolder(sf.ID, username)
    if err != nil {
      continue
    }
    if ok {
      accessible = append(accessible, sf)
    }
  }
  return accessible, nil
}

// GetSharedFolderMountsForUser 获取用户应挂载的共享文件夹列表（供 Docker 容器创建使用）
// workDir 为工作目录绝对路径
func GetSharedFolderMountsForUser(workDir, username string) ([]ShareMount, error) {
  folders, err := GetAccessibleSharedFolders(username)
  if err != nil {
    return nil, err
  }
  mounts := make([]ShareMount, 0, len(folders))
  for _, sf := range folders {
    mounts = append(mounts, ShareMount{
      Source: filepath.Join(workDir, "shared", sf.Name),
      Target: "/root/.picoclaw/workspace/share/" + sf.Name,
    })
  }
  return mounts, nil
}

// ============================================================
// 挂载记录管理
// ============================================================

// RecordMountTest 记录挂载测试结果
func RecordMountTest(folderID int64, username string, mounted bool) error {
  if err := ensureDB(); err != nil {
    return err
  }
  now := time.Now().Format("2006-01-02 15:04:05")
  // UPSERT: 更新或插入
  _, err := engine.Exec(`INSERT INTO shared_folder_mounts (folder_id, username, mounted, checked_at)
    VALUES (?, ?, ?, ?)
    ON CONFLICT(folder_id, username) DO UPDATE SET
      mounted = excluded.mounted,
      checked_at = excluded.checked_at`,
    folderID, username, mounted, now)
  return err
}

// GetMountStatus 获取用户的挂载状态
func GetMountStatus(folderID int64, username string) (*SharedFolderMount, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var m SharedFolderMount
  has, err := engine.Where("folder_id = ? AND username = ?", folderID, username).Get(&m)
  if err != nil {
    return nil, err
  }
  if !has {
    return nil, fmt.Errorf("未找到挂载记录")
  }
  return &m, nil
}

// GetMountStatusesForFolder 获取共享文件夹所有用户的挂载状态
func GetMountStatusesForFolder(folderID int64) (map[string]bool, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var mounts []SharedFolderMount
  err := engine.Where("folder_id = ?", folderID).Find(&mounts)
  if err != nil {
    return nil, err
  }
  result := make(map[string]bool, len(mounts))
  for _, m := range mounts {
    result[m.Username] = m.Mounted
  }
  return result, nil
}

// DeleteSharedFolderMountsByUser 删除指定用户的所有挂载记录
func DeleteSharedFolderMountsByUser(username string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("username = ?", username).Delete(&SharedFolderMount{})
  return err
}

// ============================================================
// 集成钩子：组删除
// ============================================================

// RemoveGroupFromAllSharedFolders 从所有共享文件夹中移除指定组的关联。
// 返回受影响的共享文件夹 ID 列表。
func RemoveGroupFromAllSharedFolders(groupID int64) ([]int64, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var sfg []SharedFolderGroup
  err := engine.Where("group_id = ?", groupID).Find(&sfg)
  if err != nil {
    return nil, err
  }
  affected := make([]int64, 0, len(sfg))
  for _, s := range sfg {
    affected = append(affected, s.FolderID)
  }
  _, err = engine.Where("group_id = ?", groupID).Delete(&SharedFolderGroup{})
  if err != nil {
    return nil, err
  }
  return affected, nil
}

// GetOrphanedSharedFolders 获取没有关联组且非公共的共享文件夹（孤立状态）
func GetOrphanedSharedFolders() ([]SharedFolder, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  all, err := ListSharedFolders()
  if err != nil {
    return nil, err
  }
  orphaned := make([]SharedFolder, 0)
  for _, sf := range all {
    if sf.IsPublic {
      continue
    }
    gids, err := GetSharedFolderGroupIDs(sf.ID)
    if err != nil {
      continue
    }
    if len(gids) == 0 {
      orphaned = append(orphaned, sf)
    }
  }
  return orphaned, nil
}

// ============================================================
// 集成钩子：认证源切换
// ============================================================

// RemoveGroupSourceFromSharedFolders 删除指定来源的所有组的共享文件夹关联
func RemoveGroupSourceFromSharedFolders(source string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  // 找到所有指定来源的组 ID
  var groups []Group
  err := engine.Where("source = ?", source).Find(&groups)
  if err != nil {
    return err
  }
  if len(groups) == 0 {
    return nil
  }
  gids := make([]interface{}, len(groups))
  for i, g := range groups {
    gids[i] = g.ID
  }
  _, err = engine.In("group_id", gids...).Delete(&SharedFolderGroup{})
  return err
}

// ============================================================
// 集成钩子：组成员变更 → 影响分析
// ============================================================

// OnGroupMembersAdded 组成员被添加时，返回所有因该变更需要重启容器的用户
func OnGroupMembersAdded(groupID int64, usernames []string) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  // 找到关联该组的所有共享文件夹
  var sfg []SharedFolderGroup
  err := engine.Where("group_id = ?", groupID).Find(&sfg)
  if err != nil {
    return nil, err
  }
  if len(sfg) == 0 {
    return nil, nil
  }
  // 新加的用户都应重启（如果他们有运行容器）
  return usernames, nil
}

// OnGroupMembersRemoved 组成员被移除时，返回因失去访问权需要重启容器的用户。
// 如果用户还通过其他组或公共方式访问，则不需要重启。
func OnGroupMembersRemoved(groupID int64, username string) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  // 找到关联该组的所有共享文件夹
  var sfg []SharedFolderGroup
  err := engine.Where("group_id = ?", groupID).Find(&sfg)
  if err != nil {
    return nil, err
  }
  if len(sfg) == 0 {
    return nil, nil
  }
  // 检查该用户是否通过其他方式仍能访问每个共享文件夹
  needsRestart := false
  for _, s := range sfg {
    ok, err := IsUserInSharedFolder(s.FolderID, username)
    if err != nil {
      continue
    }
    if !ok {
      needsRestart = true
      break
    }
  }
  if needsRestart {
    return []string{username}, nil
  }
  return nil, nil
}

// ============================================================
// 内部辅助
// ============================================================

// getGroupByID 按 ID 获取组
func getGroupByID(id int64) (*Group, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var g Group
  has, err := engine.ID(id).Get(&g)
  if err != nil {
    return nil, err
  }
  if !has {
    return nil, fmt.Errorf("组不存在")
  }
  return &g, nil
}

// GetAllNonSuperadminUsers 获取所有非超管用户
func GetAllNonSuperadminUsers() ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var users []LocalUser
  err := engine.Where("role = ?", "user").Find(&users)
  if err != nil {
    return nil, err
  }
  result := make([]string, 0, len(users))
  for _, u := range users {
    result = append(result, u.Username)
  }
  return result, nil
}

// GetSharedFolderInfo 返回带统计信息的共享文件夹视图
type SharedFolderInfo struct {
  SharedFolder
  Groups       []GroupInfo `json:"groups,omitempty"`
  MemberCount  int         `json:"member_count"`
  MountedCount int         `json:"mounted_count"`
  Orphaned     bool        `json:"orphaned"`
  Members      []struct {
    Username  string `json:"username"`
    Mounted   bool   `json:"mounted"`
    CheckedAt string `json:"checked_at"`
  } `json:"members,omitempty"`
}

// BuildSharedFolderInfo 构建共享文件夹详细信息（含关联组、成员、挂载状态）
func BuildSharedFolderInfo(sf *SharedFolder) (*SharedFolderInfo, error) {
  info := &SharedFolderInfo{SharedFolder: *sf}

  // 关联组
  gids, err := GetSharedFolderGroupIDs(sf.ID)
  if err != nil {
    return nil, err
  }
  for _, gid := range gids {
    g, err := getGroupByID(gid)
    if err != nil {
      continue
    }
    info.Groups = append(info.Groups, GroupInfo{
      ID:   g.ID,
      Name: g.Name,
    })
  }

  // 成员
  members, err := GetSharedFolderMembers(sf.ID)
  if err != nil {
    return nil, err
  }
  info.MemberCount = len(members)

  // 挂载状态
  mountStatuses, err := GetMountStatusesForFolder(sf.ID)
  if err != nil {
    return nil, err
  }
  mountedCount := 0
  for _, m := range members {
    entry := struct {
      Username  string `json:"username"`
      Mounted   bool   `json:"mounted"`
      CheckedAt string `json:"checked_at"`
    }{Username: m}
    if status, ok := mountStatuses[m]; ok {
      entry.Mounted = status
      if status {
        mountedCount++
      }
    }
    info.Members = append(info.Members, entry)
  }
  info.MountedCount = mountedCount

  // 孤立状态
  info.Orphaned = !sf.IsPublic && len(gids) == 0

  return info, nil
}
