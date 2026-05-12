package auth

import (
  "database/sql"
  "fmt"
  "sort"
  "strings"
)

// ============================================================
// 用户组管理
// ============================================================

// CreateGroup 创建组，parentID 为 nil 表示顶级组。
func CreateGroup(name, source, description string, parentID *int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  group := &Group{
    Name:        name,
    ParentID:    parentID,
    Source:      source,
    Description: description,
  }
  _, err := engine.Insert(group)
  if err != nil {
    return fmt.Errorf("创建组失败: %w", err)
  }
  return nil
}

// DeleteGroup 删除组及其成员、技能绑定，并将子组提升为顶级组。
func DeleteGroup(name string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  gid, err := GetGroupID(name)
  if err != nil {
    return err
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    return err
  }
  if _, err := session.Where("group_id = ?", gid).Delete(&UserGroup{}); err != nil {
    _ = session.Rollback()
    return err
  }
  if _, err := session.Where("group_id = ?", gid).Delete(&GroupSkill{}); err != nil {
    _ = session.Rollback()
    return err
  }
  if _, err := session.Exec("UPDATE groups SET parent_id = NULL WHERE parent_id = ?", gid); err != nil {
    _ = session.Rollback()
    return err
  }
  affected, err := session.Where("id = ?", gid).Delete(&Group{})
  if err != nil {
    _ = session.Rollback()
    return err
  }
  if affected == 0 {
    _ = session.Rollback()
    return fmt.Errorf("组 %s 不存在", name)
  }
  return session.Commit()
}

// ListGroups 列出所有组及其统计
func ListGroups() ([]GroupInfo, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  rows, err := engine.DB().Query(`SELECT g.id, g.name, g.parent_id, g.source, g.description,
    (SELECT COUNT(*) FROM user_groups ug WHERE ug.group_id = g.id) AS member_count,
    (SELECT COUNT(*) FROM group_skills gs WHERE gs.group_id = g.id) AS skill_count
    FROM groups g ORDER BY g.name`)
  if err != nil {
    return nil, err
  }
  defer rows.Close()

  var list []GroupInfo
  for rows.Next() {
    var group GroupInfo
    var parentID sql.NullInt64
    if err := rows.Scan(
      &group.ID,
      &group.Name,
      &parentID,
      &group.Source,
      &group.Description,
      &group.MemberCount,
      &group.SkillCount,
    ); err != nil {
      return nil, err
    }
    if parentID.Valid {
      group.ParentID = &parentID.Int64
    }
    list = append(list, group)
  }
  if err := rows.Err(); err != nil {
    return nil, err
  }
  return list, nil
}

// GetGroupID 根据组名获取组 ID
func GetGroupID(name string) (int64, error) {
  if err := ensureDB(); err != nil {
    return 0, err
  }
  var group Group
  has, err := engine.Where("name = ?", name).Get(&group)
  if err != nil {
    return 0, err
  }
  if !has {
    return 0, fmt.Errorf("组 %s 不存在", name)
  }
  return group.ID, nil
}

// AddUsersToGroup 批量添加用户到组
func AddUsersToGroup(groupName string, usernames []string) error {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return err
  }
  for _, u := range usernames {
    // INSERT OR IGNORE 避免重复插入
    engine.Exec("INSERT OR IGNORE INTO user_groups (username, group_id) VALUES (?, ?)", u, gid)
  }
  return nil
}

// RemoveUserFromGroup 从组中移除用户。
func RemoveUserFromGroup(groupName, username string) error {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return err
  }
  if err := ensureDB(); err != nil {
    return err
  }
  _, err = engine.Where("username = ? AND group_id = ?", username, gid).Delete(&UserGroup{})
  return err
}

// GetGroupMembers 获取组成员列表
func GetGroupMembers(groupName string) ([]string, error) {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return nil, err
  }
  var userGroups []UserGroup
  err = engine.Where("group_id = ?", gid).OrderBy("username").Find(&userGroups)
  if err != nil {
    return nil, err
  }
  list := make([]string, 0, len(userGroups))
  for _, ug := range userGroups {
    list = append(list, ug.Username)
  }
  return list, nil
}

// GetGroupMembersWithSubGroups 获取组的直接成员和子组成员。
func GetGroupMembersWithSubGroups(groupName string) ([]string, []string, error) {
  directMembers, err := GetGroupMembers(groupName)
  if err != nil {
    return nil, nil, err
  }
  allMembers, err := GetGroupMembersForDeploy(groupName)
  if err != nil {
    return nil, nil, err
  }
  direct := make(map[string]bool, len(directMembers))
  for _, member := range directMembers {
    direct[member] = true
  }
  var inherited []string
  for _, member := range allMembers {
    if !direct[member] {
      inherited = append(inherited, member)
    }
  }
  sort.Strings(inherited)
  return directMembers, inherited, nil
}

// GetGroupsForUser 获取用户所属的组名列表
func GetGroupsForUser(username string) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var results []struct {
    Name string `xorm:"name"`
  }
  err := engine.SQL(`SELECT g.name FROM groups g JOIN user_groups ug ON g.id = ug.group_id WHERE ug.username = ? ORDER BY g.name`, username).Find(&results)
  if err != nil {
    return nil, err
  }
  list := make([]string, 0, len(results))
  for _, r := range results {
    list = append(list, r.Name)
  }
  return list, nil
}

// SyncUserGroups 差量更新用户的组关系（传入用户应属于的组名列表）
func SyncUserGroups(username string, groupNames []string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  session := engine.NewSession()
  defer session.Close()

  if err := session.Begin(); err != nil {
    return err
  }

  // 确保所有组存在
  for _, name := range groupNames {
    session.Exec("INSERT OR IGNORE INTO groups (name, source) VALUES (?, 'ldap')", name)
  }

  // 删除用户当前所有组关系
  session.Where("username = ?", username).Delete(&UserGroup{})

  // 添加新的组关系
  for _, name := range groupNames {
    var group Group
    has, err := session.Where("name = ?", name).Get(&group)
    if err != nil || !has {
      continue
    }
    session.Exec("INSERT OR IGNORE INTO user_groups (username, group_id) VALUES (?, ?)", username, group.ID)
  }

  return session.Commit()
}

func ReplaceGroupMembersBySource(source string, groupMembers map[string][]string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  session := engine.NewSession()
  defer session.Close()

  if err := session.Begin(); err != nil {
    return err
  }

  for groupName := range groupMembers {
    if _, err := session.Exec("INSERT OR IGNORE INTO groups (name, source) VALUES (?, ?)", groupName, source); err != nil {
      _ = session.Rollback()
      return err
    }
  }

  if _, err := session.Exec("DELETE FROM user_groups WHERE group_id IN (SELECT id FROM groups WHERE source = ?)", source); err != nil {
    _ = session.Rollback()
    return err
  }

  for groupName, members := range groupMembers {
    var group Group
    has, err := session.Where("name = ?", groupName).Get(&group)
    if err != nil {
      _ = session.Rollback()
      return err
    }
    if !has {
      continue
    }
    for _, username := range members {
      if _, err := session.Exec("INSERT OR IGNORE INTO user_groups (username, group_id) VALUES (?, ?)", username, group.ID); err != nil {
        _ = session.Rollback()
        return err
      }
    }
  }

  return session.Commit()
}

// ReplaceLDAPGroupMembers 用 LDAP 当前结果整体替换 LDAP 组成员关系。
func ReplaceLDAPGroupMembers(groupMembers map[string][]string) error {
  return ReplaceGroupMembersBySource("ldap", groupMembers)
}

// BindSkillToGroup 绑定技能到组
func BindSkillToGroup(groupName, skillName string) error {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return err
  }
  _, err = engine.Exec("INSERT OR IGNORE INTO group_skills (group_id, skill_name) VALUES (?, ?)", gid, skillName)
  return err
}

// UnbindSkillFromGroup 解绑技能
func UnbindSkillFromGroup(groupName, skillName string) error {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return err
  }
  _, err = engine.Where("group_id = ? AND skill_name = ?", gid, skillName).Delete(&GroupSkill{})
  return err
}

// GetGroupSkills 获取组绑定的技能列表
func GetGroupSkills(groupName string) ([]string, error) {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return nil, err
  }
  var skills []GroupSkill
  err = engine.Where("group_id = ?", gid).OrderBy("skill_name").Find(&skills)
  if err != nil {
    return nil, err
  }
  list := make([]string, 0, len(skills))
  for _, s := range skills {
    list = append(list, s.SkillName)
  }
  return list, nil
}

// GetGroupMembersForDeploy 获取组成员的用户名列表（包含子组成员）。
func GetGroupMembersForDeploy(groupName string) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }

  var group Group
  has, err := engine.Where("name = ?", groupName).Get(&group)
  if err != nil || !has {
    return nil, fmt.Errorf("组 %s 不存在", groupName)
  }

  ids := []int64{group.ID}
  subIDs, err := GetSubGroupIDs(group.ID)
  if err != nil {
    return nil, err
  }
  ids = append(ids, subIDs...)

  seen := make(map[string]bool)
  var members []string
  for _, gid := range ids {
    var userGroups []UserGroup
    if err := engine.Where("group_id = ?", gid).OrderBy("username").Find(&userGroups); err != nil {
      return nil, err
    }
    for _, ug := range userGroups {
      if !seen[ug.Username] {
        seen[ug.Username] = true
        members = append(members, ug.Username)
      }
    }
  }
  return members, nil
}

// GetSubGroupIDs 递归获取所有子组 ID。
func GetSubGroupIDs(groupID int64) ([]int64, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var result []int64
  var walk func(pid int64) error
  walk = func(pid int64) error {
    var children []Group
    if err := engine.Where("parent_id = ?", pid).OrderBy("name").Find(&children); err != nil {
      return err
    }
    for _, child := range children {
      result = append(result, child.ID)
      if err := walk(child.ID); err != nil {
        return err
      }
    }
    return nil
  }
  if err := walk(groupID); err != nil {
    return nil, err
  }
  return result, nil
}

// SetGroupParent 设置组的父组。
func SetGroupParent(groupName string, parentID *int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("name = ?", groupName).
    Cols("parent_id").
    Update(&Group{ParentID: parentID})
  return err
}

// ensure interface compatibility: strings is imported but only used by SyncUserGroups indirectly
var _ = strings.TrimSpace
