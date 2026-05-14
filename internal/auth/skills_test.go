package auth

import (
  "os"
  "path/filepath"
  "testing"
)

func TestBindSkillToUser_CreatesRecord(t *testing.T) {
  testInitDB(t)
  if err := CreateUser("testuser", "pass", "user"); err != nil {
    t.Fatalf("CreateUser: %v", err)
  }

  err := BindSkillToUser("testuser", "test-skill", "skillhub")
  if err != nil {
    t.Fatalf("BindSkillToUser: %v", err)
  }

  var record UserSkill
  has, err := engine.Where("username = ? AND skill_name = ?", "testuser", "test-skill").Get(&record)
  if err != nil {
    t.Fatalf("query failed: %v", err)
  }
  if !has {
    t.Fatal("user_skills record should exist")
  }
  if record.Source != "skillhub" {
    t.Errorf("source = %q, want %q", record.Source, "skillhub")
  }
}

func TestBindSkillToUser_UpdateSource(t *testing.T) {
  testInitDB(t)
  if err := CreateUser("testuser", "pass", "user"); err != nil {
    t.Fatalf("CreateUser: %v", err)
  }

  BindSkillToUser("testuser", "test-skill", "old-source")
  BindSkillToUser("testuser", "test-skill", "new-source")

  var record UserSkill
  engine.Where("username = ? AND skill_name = ?", "testuser", "test-skill").Get(&record)
  if record.Source != "new-source" {
    t.Errorf("source = %q, want %q", record.Source, "new-source")
  }
}

func TestUnbindSkillFromUser_RemovesRecord(t *testing.T) {
  testInitDB(t)
  if err := CreateUser("testuser", "pass", "user"); err != nil {
    t.Fatalf("CreateUser: %v", err)
  }

  BindSkillToUser("testuser", "test-skill", "skillhub")
  if err := UnbindSkillFromUser("testuser", "test-skill"); err != nil {
    t.Fatalf("UnbindSkillFromUser: %v", err)
  }

  var record UserSkill
  has, _ := engine.Where("username = ? AND skill_name = ?", "testuser", "test-skill").Get(&record)
  if has {
    t.Fatal("user_skills record should be deleted")
  }
}

func TestGetUsersBySkill_ReturnsBoundUsers(t *testing.T) {
  testInitDB(t)
  users := []string{"user1", "user2", "user3"}
  for _, u := range users {
    if err := CreateUser(u, "pass", "user"); err != nil {
      t.Fatalf("CreateUser %s: %v", u, err)
    }
    BindSkillToUser(u, "my-skill", "skillhub")
  }
  CreateUser("other-user", "pass", "user")

  result, err := GetUsersBySkill("my-skill")
  if err != nil {
    t.Fatalf("GetUsersBySkill: %v", err)
  }
  if len(result) != 3 {
    t.Fatalf("len(result) = %d, want 3", len(result))
  }
}

func TestGetUsersForSkill_DirectOnly(t *testing.T) {
  testInitDB(t)
  CreateUser("directuser", "pass", "user")
  CreateUser("groupuser", "pass", "user")
  CreateGroup("test-group", "local", "", nil)
  AddUsersToGroup("test-group", []string{"groupuser"})

  BindSkillToUser("directuser", "my-skill", "skillhub")
  BindSkillToUser("groupuser", "my-skill", "group:test-group")

  result, err := GetUsersForSkill("my-skill")
  if err != nil {
    t.Fatalf("GetUsersForSkill: %v", err)
  }
  if len(result) != 2 {
    t.Fatalf("len(result) = %d, want 2 (both have direct bindings)", len(result))
  }
}

func TestUserHasSkillFromAnySource_DirectBinding(t *testing.T) {
  testInitDB(t)
  CreateUser("testuser", "pass", "user")

  has, err := UserHasSkillFromAnySource("testuser", "my-skill")
  if err != nil {
    t.Fatalf("UserHasSkillFromAnySource: %v", err)
  }
  if has {
    t.Error("should not have skill before binding")
  }

  BindSkillToUser("testuser", "my-skill", "skillhub")
  has, _ = UserHasSkillFromAnySource("testuser", "my-skill")
  if !has {
    t.Error("should have skill after binding")
  }
}

func TestGetUserSkillSource_ReturnsSource(t *testing.T) {
  testInitDB(t)
  CreateUser("testuser", "pass", "user")

  BindSkillToUser("testuser", "my-skill", "custom-source")
  src, err := GetUserSkillSource("testuser", "my-skill")
  if err != nil {
    t.Fatalf("GetUserSkillSource: %v", err)
  }
  if src != "custom-source" {
    t.Errorf("source = %q, want %q", src, "custom-source")
  }
}

func TestGetUserSkillSource_NoRecord(t *testing.T) {
  testInitDB(t)
  CreateUser("testuser", "pass", "user")

  src, err := GetUserSkillSource("testuser", "nonexistent")
  if err != nil {
    t.Fatalf("GetUserSkillSource: %v", err)
  }
  if src != "" {
    t.Errorf("source = %q, want empty", src)
  }
}

func TestGetUserAllSkillSources_ReturnsActualSource(t *testing.T) {
  testInitDB(t)
  CreateUser("testuser", "pass", "user")
  BindSkillToUser("testuser", "my-skill", "skillhub")

  sources, err := GetUserAllSkillSources("testuser", "my-skill")
  if err != nil {
    t.Fatalf("GetUserAllSkillSources: %v", err)
  }
  if len(sources) != 1 || sources[0] != "skillhub" {
    t.Fatalf("sources = %v, want [skillhub]", sources)
  }
}

func TestDeleteSkill_ClearsUserSkills(t *testing.T) {
  testInitDB(t)
  CreateUser("testuser", "pass", "user")

  BindSkillToUser("testuser", "my-skill", "skillhub")

  if err := DeleteSkill("my-skill"); err != nil {
    t.Fatalf("DeleteSkill: %v", err)
  }

  count1, _ := engine.Where("skill_name = ?", "my-skill").Count(&UserSkill{})
  if count1 > 0 {
    t.Error("user_skills should be empty after DeleteSkill")
  }
}

// ============================================================
// 默认技能
// ============================================================

func TestLoadDefaultSkills_EmptyWhenNoSetting(t *testing.T) {
  testInitDB(t)
  skills, err := LoadDefaultSkills()
  if err != nil {
    t.Fatalf("LoadDefaultSkills: %v", err)
  }
  if len(skills) != 0 {
    t.Errorf("skills = %v, want empty", skills)
  }
}

func TestToggleDefaultSkill_AddsToList(t *testing.T) {
  testInitDB(t)
  // 创建技能目录（LoadDefaultSkills 会过滤不存在的技能）
  os.MkdirAll(filepath.Join(dbDataDir, "skills", "test-source", "my-skill"), 0755)
  os.WriteFile(filepath.Join(dbDataDir, "skills", "test-source", "my-skill", "SKILL.md"), []byte("---\nname: my-skill\ndescription: Test\n---\n"), 0644)

  skills, err := ToggleDefaultSkill("my-skill")
  if err != nil {
    t.Fatalf("ToggleDefaultSkill: %v", err)
  }
  if len(skills) != 1 || skills[0] != "my-skill" {
    t.Errorf("skills = %v, want [my-skill]", skills)
  }

  // 再次读取确认持久化
  skills, _ = LoadDefaultSkills()
  if len(skills) != 1 || skills[0] != "my-skill" {
    t.Errorf("skills after reload = %v, want [my-skill]", skills)
  }
}

func TestToggleDefaultSkill_RemovesFromList(t *testing.T) {
  testInitDB(t)
  // 创建技能目录
  for _, name := range []string{"skill-a", "skill-b"} {
    os.MkdirAll(filepath.Join(dbDataDir, "skills", "test-source", name), 0755)
    os.WriteFile(filepath.Join(dbDataDir, "skills", "test-source", name, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Test\n---\n"), 0644)
  }
  ToggleDefaultSkill("skill-a")
  ToggleDefaultSkill("skill-b")

  skills, err := ToggleDefaultSkill("skill-a")
  if err != nil {
    t.Fatalf("ToggleDefaultSkill: %v", err)
  }
  if len(skills) != 1 || skills[0] != "skill-b" {
    t.Errorf("skills = %v, want [skill-b]", skills)
  }
}

func TestRemoveFromDefaultSkills_RemovesSkill(t *testing.T) {
  testInitDB(t)
  // 创建技能目录
  for _, name := range []string{"skill-a", "skill-b"} {
    os.MkdirAll(filepath.Join(dbDataDir, "skills", "test-source", name), 0755)
    os.WriteFile(filepath.Join(dbDataDir, "skills", "test-source", name, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Test\n---\n"), 0644)
  }
  ToggleDefaultSkill("skill-a")
  ToggleDefaultSkill("skill-b")

  if err := RemoveFromDefaultSkills("skill-a"); err != nil {
    t.Fatalf("RemoveFromDefaultSkills: %v", err)
  }

  skills, _ := LoadDefaultSkills()
  if len(skills) != 1 || skills[0] != "skill-b" {
    t.Errorf("skills = %v, want [skill-b]", skills)
  }
}

func TestLoadDefaultSkills_SkipsNonexistentSkills(t *testing.T) {
  testInitDB(t)
  ToggleDefaultSkill("exists")
  ToggleDefaultSkill("missing")

  // 创建 exists 技能的目录（InitDB 将 dbDataDir 设为工作目录）
  skillsRoot := filepath.Join(dbDataDir, "skills", "test-source")
  os.MkdirAll(filepath.Join(skillsRoot, "exists"), 0755)
  os.WriteFile(filepath.Join(skillsRoot, "exists", "SKILL.md"), []byte("---\nname: exists\ndescription: Exists\n---\n"), 0644)

  skills, err := LoadDefaultSkills()
  if err != nil {
    t.Fatalf("LoadDefaultSkills: %v", err)
  }
  if len(skills) != 1 || skills[0] != "exists" {
    t.Errorf("skills = %v, want [exists]", skills)
  }
}
