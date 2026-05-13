package auth

import (
  "fmt"
  "sort"
  "time"
)

// ============================================================
// 用户技能绑定
// ============================================================

// BindSkillToUser 绑定技能到用户（记录来源）
func BindSkillToUser(username, skillName, source string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  now := time.Now().Format("2006-01-02 15:04:05")
  _, err := engine.Exec(
    `INSERT INTO user_skills (username, skill_name, source, updated_at) VALUES (?, ?, ?, ?)
     ON CONFLICT(username, skill_name) DO UPDATE SET source = ?, updated_at = ?`,
    username, skillName, source, now, source, now,
  )
  return err
}

// UnbindSkillFromUser 解绑用户技能
func UnbindSkillFromUser(username, skillName string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("username = ? AND skill_name = ?", username, skillName).Delete(&UserSkill{})
  return err
}

// GetUserSkillSource 获取用户技能绑定的来源（"self" 或实际仓库名）
func GetUserSkillSource(username, skillName string) (string, error) {
  if err := ensureDB(); err != nil {
    return "", err
  }
  var skill UserSkill
  has, err := engine.Where("username = ? AND skill_name = ?", username, skillName).Get(&skill)
  if err != nil {
    return "", err
  }
  if !has {
    return "", nil
  }
  return skill.Source, nil
}

// GetUserSkills 获取用户直接绑定的技能列表
func GetUserSkills(username string) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var skills []UserSkill
  if err := engine.Where("username = ?", username).OrderBy("skill_name").Find(&skills); err != nil {
    return nil, err
  }
  list := make([]string, 0, len(skills))
  for _, s := range skills {
    list = append(list, s.SkillName)
  }
  return list, nil
}

// GetUsersBySkill 获取直接绑定某技能的所有用户
func GetUsersBySkill(skillName string) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var skills []UserSkill
  if err := engine.Where("skill_name = ?", skillName).OrderBy("username").Find(&skills); err != nil {
    return nil, err
  }
  list := make([]string, 0, len(skills))
  for _, s := range skills {
    list = append(list, s.Username)
  }
  return list, nil
}

// BindSkillByGroupID 通过 group_id 绑定技能到组
func BindSkillByGroupID(groupID int64, skillName, source string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Exec(
    `INSERT INTO group_skills (group_id, skill_name, source) VALUES (?, ?, ?)
     ON CONFLICT(group_id, skill_name) DO UPDATE SET source = ?`,
    groupID, skillName, source, source,
  )
  return err
}

// UnbindSkillByGroupID 通过 group_id 解绑组技能
func UnbindSkillByGroupID(groupID int64, skillName string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("group_id = ? AND skill_name = ?", groupID, skillName).Delete(&GroupSkill{})
  return err
}

// ============================================================
// 智能查询（含子组继承）
// ============================================================

// groupIDsWithSubs 获取组 ID 及其所有子组 ID（递归去重）
func groupIDsWithSubs(groupID int64) []int64 {
  seen := map[int64]bool{groupID: true}
  var walk func(pid int64)
  walk = func(pid int64) {
    subs, err := GetSubGroupIDs(pid)
    if err != nil {
      return
    }
    for _, id := range subs {
      if !seen[id] {
        seen[id] = true
        walk(id)
      }
    }
  }
  walk(groupID)
  result := make([]int64, 0, len(seen))
  for id := range seen {
    result = append(result, id)
  }
  return result
}

// GetUsersForSkill 返回所有应该拥有此技能的用户（直接绑定 + 组成员，含子组继承）
func GetUsersForSkill(skillName string) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }

  userSet := make(map[string]bool)

  // 直接绑定
  direct, err := GetUsersBySkill(skillName)
  if err != nil {
    return nil, err
  }
  for _, u := range direct {
    userSet[u] = true
  }

  // 组绑定（含子组继承）
  var groupSkills []GroupSkill
  if err := engine.Where("skill_name = ?", skillName).Find(&groupSkills); err != nil {
    return nil, err
  }
  for _, gs := range groupSkills {
    allIDs := groupIDsWithSubs(gs.GroupID)
    for _, gid := range allIDs {
      var members []UserGroup
      if err := engine.Where("group_id = ?", gid).Find(&members); err != nil {
        continue
      }
      for _, m := range members {
        userSet[m.Username] = true
      }
    }
  }

  result := make([]string, 0, len(userSet))
  for u := range userSet {
    result = append(result, u)
  }
  sort.Strings(result)
  return result, nil
}

// UserHasSkillFromAnySource 判断用户是否应拥有此技能（直接绑定或组成员，含子组继承）
func UserHasSkillFromAnySource(username, skillName string) (bool, error) {
  if err := ensureDB(); err != nil {
    return false, err
  }

  // 1. 直接绑定
  count, err := engine.Where("username = ? AND skill_name = ?", username, skillName).Count(&UserSkill{})
  if err != nil {
    return false, err
  }
  if count > 0 {
    return true, nil
  }

  // 2. 组绑定（含子组继承）
  var groupSkills []GroupSkill
  if err := engine.Where("skill_name = ?", skillName).Find(&groupSkills); err != nil {
    return false, err
  }
  for _, gs := range groupSkills {
    allIDs := groupIDsWithSubs(gs.GroupID)
    for _, gid := range allIDs {
      count, err := engine.Where("username = ? AND group_id = ?", username, gid).Count(&UserGroup{})
      if err != nil {
        continue
      }
      if count > 0 {
        return true, nil
      }
    }
  }

  return false, nil
}

// GetUserAllSkillSources 返回用户拥有此技能的所有来源（含子组继承）
func GetUserAllSkillSources(username, skillName string) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var sources []string

  count, err := engine.Where("username = ? AND skill_name = ?", username, skillName).Count(&UserSkill{})
  if err != nil {
    return nil, fmt.Errorf("查询直接绑定失败: %w", err)
  }
  if count > 0 {
    sources = append(sources, "direct")
  }

  // 通过组（含子组）继承
  var groupSkills []GroupSkill
  if err := engine.Where("skill_name = ?", skillName).Find(&groupSkills); err != nil {
    return nil, err
  }
  for _, gs := range groupSkills {
    allIDs := groupIDsWithSubs(gs.GroupID)
    for _, gid := range allIDs {
      var group Group
      has, err := engine.ID(gid).Get(&group)
      if err != nil || !has {
        continue
      }
      count, err := engine.Where("username = ? AND group_id = ?", username, gid).Count(&UserGroup{})
      if err != nil {
        continue
      }
      if count > 0 {
        sources = append(sources, "group:"+group.Name)
        break
      }
    }
  }

  if sources == nil {
    sources = []string{}
  }
  return sources, nil
}

// ============================================================
// 技能表操作（已废弃，保留空函数兼容编译）
// ============================================================

// UpsertSkill 已废弃，保留空函数兼容编译
func UpsertSkill(name, description string) error {
  return nil
}

// GetAllSkills 已废弃，返回空列表
func GetAllSkills() ([]SkillRecord, error) {
  return []SkillRecord{}, nil
}

// GetSkill 已废弃
func GetSkill(name string) (*SkillRecord, error) {
  return nil, fmt.Errorf("已废弃: %s", name)
}

// DeleteSkill 删除技能所有绑定关系（不删 skills 表，因为已废弃）
func DeleteSkill(name string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    return err
  }
  if _, err := session.Where("skill_name = ?", name).Delete(&UserSkill{}); err != nil {
    _ = session.Rollback()
    return err
  }
  if _, err := session.Where("skill_name = ?", name).Delete(&GroupSkill{}); err != nil {
    _ = session.Rollback()
    return err
  }
  return session.Commit()
}
