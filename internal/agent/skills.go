package agent

import (
  "fmt"
  "os"
  "path/filepath"
  "strings"

  "gopkg.in/yaml.v3"
)

// ============================================================
// Skill — 从文件加载的代理技能
// ============================================================

type Skill struct {
  Name        string
  Description string
  License     string
  Content     string
}

// SkillFrontmatter SKILL.md 的 YAML 前置元数据
type SkillFrontmatter struct {
  Name        string `yaml:"name"`
  Description string `yaml:"description"`
  License     string `yaml:"license"`
}

// LoadSkills 从 workspace/skills/<name>/SKILL.md 加载所有技能
func LoadSkills(workspace string) ([]*Skill, error) {
  skillsDir := filepath.Join(workspace, "skills")
  entries, err := os.ReadDir(skillsDir)
  if err != nil {
    if os.IsNotExist(err) {
      return nil, nil
    }
    return nil, fmt.Errorf("读取 skills 目录失败: %w", err)
  }

  var skills []*Skill
  for _, entry := range entries {
    if !entry.IsDir() {
      continue
    }
    skill, err := loadSkill(filepath.Join(skillsDir, entry.Name()))
    if err != nil || skill == nil {
      continue
    }
    skills = append(skills, skill)
  }
  return skills, nil
}

// BuildSkillsPrompt 将技能列表转换为 system prompt 追加内容
func BuildSkillsPrompt(skills []*Skill) string {
  if len(skills) == 0 {
    return ""
  }

  var parts []string
  parts = append(parts, "## 可用技能\n")
  for _, s := range skills {
    parts = append(parts, fmt.Sprintf("### %s\n\n%s\n(%s)\n", s.Name, s.Content, s.Description))
  }
  return strings.Join(parts, "\n")
}

func loadSkill(dir string) (*Skill, error) {
  data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
  if err != nil {
    return nil, err
  }

  content := string(data)
  fm, body, ok := parseFrontmatter(content)
  if !ok || fm.Name == "" {
    return nil, fmt.Errorf("无效的 SKILL.md: 缺少 name 或 frontmatter")
  }

  return &Skill{
    Name:        fm.Name,
    Description: fm.Description,
    License:     fm.License,
    Content:     body,
  }, nil
}

// parseFrontmatter 解析 YAML frontmatter（--- 分隔）
// 返回 frontmatter, body, 是否成功
func parseFrontmatter(content string) (SkillFrontmatter, string, bool) {
  content = strings.TrimSpace(content)
  if !strings.HasPrefix(content, "---") {
    return SkillFrontmatter{}, content, false
  }

  // 查找第二个 ---
  rest := content[3:]
  endIdx := strings.Index(rest, "\n---")
  if endIdx < 0 {
    return SkillFrontmatter{}, content, false
  }

  yamlStr := strings.TrimSpace(rest[:endIdx])
  body := strings.TrimSpace(rest[endIdx+4:])

  var fm SkillFrontmatter
  if err := yaml.Unmarshal([]byte(yamlStr), &fm); err != nil {
    return SkillFrontmatter{}, content, false
  }

  return fm, body, true
}
