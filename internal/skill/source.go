package skill

import (
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "strings"

  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 技能源管理 — 统一的文件系统扫描器
// ============================================================

// SkillsRootDir 返回 skills/ 根目录
func SkillsRootDir() string {
  return filepath.Join(config.WorkDir(), "skills")
}

// SkillInfo 技能信息
type SkillInfo struct {
  Name        string `json:"name"`
  Description string `json:"description"`
  Source      string `json:"source"`
  Version     string `json:"version,omitempty"`
  FileCount   int    `json:"file_count"`
  Size        int64  `json:"size"`
  SizeStr     string `json:"size_str"`
  ModTime     string `json:"mod_time"`
}

// ListAllSkills 扫描 skills/<source>/*/SKILL.md 返回所有技能
func ListAllSkills() ([]SkillInfo, error) {
  root := SkillsRootDir()
  entries, err := os.ReadDir(root)
  if err != nil {
    if os.IsNotExist(err) {
      return []SkillInfo{}, nil
    }
    return nil, err
  }
  var skills []SkillInfo
  for _, e := range entries {
    if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
      continue
    }
    sourceSkills, err := ListSourceSkills(e.Name())
    if err != nil {
      continue
    }
    skills = append(skills, sourceSkills...)
  }
  if skills == nil {
    skills = []SkillInfo{}
  }
  sort.Slice(skills, func(i, j int) bool {
    return skills[i].Name < skills[j].Name
  })
  return skills, nil
}

// ListSourceSkills 扫描 skills/<source>/ 下所有含 SKILL.md 的子目录
func ListSourceSkills(source string) ([]SkillInfo, error) {
  if err := util.SafePathSegment(source); err != nil {
    return nil, fmt.Errorf("源名称不合法: %w", err)
  }
  root := filepath.Join(SkillsRootDir(), source)
  entries, err := os.ReadDir(root)
  if err != nil {
    if os.IsNotExist(err) {
      return []SkillInfo{}, nil
    }
    return nil, err
  }
  var skills []SkillInfo
  for _, e := range entries {
    if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
      continue
    }
    skillDir := filepath.Join(root, e.Name())
    skmdPath := filepath.Join(skillDir, "SKILL.md")
    if _, err := os.Stat(skmdPath); os.IsNotExist(err) {
      continue
    }
    meta, pErr := ParseMetadata(skillDir)
    if pErr != nil {
      continue
    }
    info, iErr := e.Info()
    if iErr != nil {
      continue
    }
    var fileCount int
    var totalSize int64
    filepath.WalkDir(skillDir, func(path string, d os.DirEntry, err error) error {
      if err != nil || d.IsDir() {
        return nil
      }
      fileCount++
      if fi, fe := d.Info(); fe == nil {
        totalSize += fi.Size()
      }
      return nil
    })
    skills = append(skills, SkillInfo{
      Name:        meta.Name,
      Description: meta.Description,
      Source:      source,
      FileCount:   fileCount,
      Size:        totalSize,
      SizeStr:     formatSize(totalSize),
      ModTime:     info.ModTime().Format("2006-01-02 15:04"),
    })
  }
  return skills, nil
}

// RescanSource 重新扫描源下的所有技能目录，返回技能名列表
func RescanSource(source string) ([]string, error) {
  if err := util.SafePathSegment(source); err != nil {
    return nil, fmt.Errorf("源名称不合法: %w", err)
  }
  root := filepath.Join(SkillsRootDir(), source)
  entries, err := os.ReadDir(root)
  if err != nil {
    if os.IsNotExist(err) {
      return nil, nil
    }
    return nil, err
  }
  var names []string
  for _, e := range entries {
    if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
      continue
    }
    skmdPath := filepath.Join(root, e.Name(), "SKILL.md")
    if _, err := os.Stat(skmdPath); os.IsNotExist(err) {
      continue
    }
    meta, pErr := ParseMetadata(filepath.Join(root, e.Name()))
    if pErr != nil {
      continue
    }
    if err := util.SafePathSegment(meta.Name); err != nil {
      continue
    }
    names = append(names, meta.Name)
  }
  return names, nil
}

func formatSize(size int64) string {
  if size < 1024 {
    return fmt.Sprintf("%d B", size)
  }
  if size < 1024*1024 {
    return fmt.Sprintf("%.1f KB", float64(size)/1024)
  }
  return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}
