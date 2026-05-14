package skill

import (
  "fmt"
  "os"
  "path/filepath"
  "regexp"
  "strings"

  "gopkg.in/yaml.v3"
)

// ============================================================
// SKILL.md 解析与校验
// ============================================================

var validSkillNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$|^[a-z]$`)

// Metadata SKILL.md 的 frontmatter 元数据
type Metadata struct {
  Name        string `yaml:"name"`
  Description string `yaml:"description"`
}

// ParseMetadata 读取 SKILL.md 并解析 YAML frontmatter
func ParseMetadata(skillDir string) (*Metadata, error) {
  path := filepath.Join(skillDir, "SKILL.md")
  data, err := os.ReadFile(path)
  if err != nil {
    return nil, fmt.Errorf("SKILL.md 不存在: %w", err)
  }

  content := string(data)
  // 统一换行为 \n，支持 Windows 编辑器的 \r\n
  content = strings.ReplaceAll(content, "\r\n", "\n")
  if !strings.HasPrefix(content, "---\n") {
    return nil, fmt.Errorf("SKILL.md 缺少 YAML frontmatter（必须以 --- 开头）")
  }

  end := strings.Index(content[4:], "\n---\n")
  if end < 0 {
    return nil, fmt.Errorf("SKILL.md frontmatter 缺少结束 ---")
  }
  yamlBlock := content[4 : 4+end]

  var meta Metadata
  if err := yaml.Unmarshal([]byte(yamlBlock), &meta); err != nil {
    return nil, fmt.Errorf("SKILL.md frontmatter 解析失败: %w", err)
  }

  if meta.Name == "" {
    return nil, fmt.Errorf("SKILL.md frontmatter 缺少必填字段 name")
  }
  if meta.Description == "" {
    return nil, fmt.Errorf("SKILL.md frontmatter 缺少必填字段 description")
  }

  return &meta, nil
}

// ValidateName 校验技能名格式
func ValidateName(name string) error {
  if name == "" {
    return fmt.Errorf("技能名不能为空")
  }
  if len(name) > 64 {
    return fmt.Errorf("技能名过长（最多 64 字符）")
  }
  if !validSkillNameRe.MatchString(name) {
    return fmt.Errorf("技能名 '%s' 不符合规范：仅允许小写字母、数字和连字符，不能以连字符开头或结尾", name)
  }
  return nil
}

// ValidateMetadata 校验元数据与目录名是否一致
func ValidateMetadata(meta *Metadata, dirName string) error {
  if err := ValidateName(meta.Name); err != nil {
    return fmt.Errorf("name 字段不合法: %w", err)
  }
  if len(meta.Description) > 1024 {
    return fmt.Errorf("description 过长（最多 1024 字符）")
  }
  if meta.Name != dirName {
    return fmt.Errorf("SKILL.md 中 name '%s' 与目录名 '%s' 不一致", meta.Name, dirName)
  }
  return nil
}

// ParseAndValidate 解析并校验 SKILL.md，一次完成
func ParseAndValidate(skillDir string) (*Metadata, error) {
  meta, err := ParseMetadata(skillDir)
  if err != nil {
    return nil, err
  }
  dirName := filepath.Base(skillDir)
  if err := ValidateMetadata(meta, dirName); err != nil {
    return nil, err
  }
  return meta, nil
}
