package skill

import (
  "os"
  "path/filepath"
  "strings"
)

// ============================================================
// 技能目录扫描（文件系统扫描，无 DB 依赖）
// ============================================================

// CheckUnknownDirs 检查所有源中含不规范子目录
func CheckUnknownDirs() []string {
  root := SkillsRootDir()
  entries, err := os.ReadDir(root)
  if err != nil {
    return nil
  }
  var unknown []string
  for _, e := range entries {
    if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
      continue
    }
    sourceDir := filepath.Join(root, e.Name())
    subs, err := os.ReadDir(sourceDir)
    if err != nil {
      continue
    }
    for _, sub := range subs {
      if !sub.IsDir() || strings.HasPrefix(sub.Name(), ".") {
        continue
      }
      skmdPath := filepath.Join(sourceDir, sub.Name(), "SKILL.md")
      if _, err := os.Stat(skmdPath); os.IsNotExist(err) {
        unknown = append(unknown, e.Name()+"/"+sub.Name())
      }
    }
  }
  return unknown
}
