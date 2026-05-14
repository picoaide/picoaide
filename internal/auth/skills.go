package auth

import (
  "encoding/json"
  "fmt"
  "os"
  "path/filepath"
  "strings"
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

// ============================================================
// 智能查询（仅查 user_skills，组展开为直接绑定后不再需要 group_skills 继承）
// ============================================================

// GetUsersForSkill 返回 user_skills 中直接绑定了此技能的所有用户
func GetUsersForSkill(skillName string) ([]string, error) {
  return GetUsersBySkill(skillName)
}

// UserHasSkillFromAnySource 判断用户是否有此技能（仅查 user_skills）
func UserHasSkillFromAnySource(username, skillName string) (bool, error) {
  if err := ensureDB(); err != nil {
    return false, err
  }

  count, err := engine.Where("username = ? AND skill_name = ?", username, skillName).Count(&UserSkill{})
  if err != nil {
    return false, err
  }
  return count > 0, nil
}

// GetUserAllSkillSources 返回用户拥有此技能的所有来源（仅查 user_skills）
func GetUserAllSkillSources(username, skillName string) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }

  var skill UserSkill
  has, err := engine.Where("username = ? AND skill_name = ?", username, skillName).Get(&skill)
  if err != nil {
    return nil, fmt.Errorf("查询失败: %w", err)
  }
  if has {
    return []string{skill.Source}, nil
  }
  return []string{}, nil
}

// ============================================================
// 默认技能
// ============================================================

const defaultSkillsKey = "internal.default_skills"

func loadDefaultSkillsRaw() ([]string, error) {
  var setting Setting
  has, err := engine.Where("key = ?", defaultSkillsKey).Get(&setting)
  if err != nil {
    return nil, err
  }
  if !has || setting.Value == "" {
    return []string{}, nil
  }
  var skills []string
  if err := json.Unmarshal([]byte(setting.Value), &skills); err != nil {
    return []string{}, nil
  }
  return skills, nil
}

func saveDefaultSkills(skills []string) error {
  data, _ := json.Marshal(skills)
  _, err := engine.Exec(
    "INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now','localtime'))",
    defaultSkillsKey, string(data),
  )
  return err
}

func skillExistsOnDisk(name string) bool {
  entries, err := os.ReadDir(SkillsRootDir)
  if err != nil {
    return false
  }
  for _, e := range entries {
    if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
      continue
    }
    if _, err := os.Stat(filepath.Join(SkillsRootDir, e.Name(), name, "SKILL.md")); err == nil {
      return true
    }
  }
  return false
}

// LoadDefaultSkills 读取默认技能列表，自动剔除不存在的技能
func LoadDefaultSkills() ([]string, error) {
  skills, err := loadDefaultSkillsRaw()
  if err != nil {
    return nil, err
  }
  var valid []string
  for _, name := range skills {
    if skillExistsOnDisk(name) {
      valid = append(valid, name)
    }
  }
  if len(valid) != len(skills) {
    saveDefaultSkills(valid)
  }
  return valid, nil
}

// ToggleDefaultSkill 切换技能是否在默认列表中
func ToggleDefaultSkill(name string) ([]string, error) {
  skills, err := loadDefaultSkillsRaw()
  if err != nil {
    return nil, err
  }
  found := false
  for i, s := range skills {
    if s == name {
      skills = append(skills[:i], skills[i+1:]...)
      found = true
      break
    }
  }
  if !found {
    skills = append(skills, name)
  }
  if err := saveDefaultSkills(skills); err != nil {
    return nil, err
  }
  return skills, nil
}

// RemoveFromDefaultSkills 从默认列表中移除指定技能
func RemoveFromDefaultSkills(name string) error {
  skills, err := loadDefaultSkillsRaw()
  if err != nil {
    return err
  }
  var filtered []string
  for _, s := range skills {
    if s != name {
      filtered = append(filtered, s)
    }
  }
  return saveDefaultSkills(filtered)
}

// DeleteSkill 删除技能所有绑定关系
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
  return session.Commit()
}
