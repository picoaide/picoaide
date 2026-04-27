package main

import (
  "fmt"
  "os"
  "path/filepath"
  "strings"

  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
)

func SkillsDeploy(cfg *config.GlobalConfig, targetUser string) error {
  skillDir := config.SkillsDirPath()

  entries, err := os.ReadDir(skillDir)
  if err != nil {
    return fmt.Errorf("读取 skill 目录失败: %w", err)
  }

  var skillNames []string
  for _, e := range entries {
    if e.IsDir() {
      skillNames = append(skillNames, e.Name())
    }
  }

  if len(skillNames) == 0 {
    return fmt.Errorf("skill 目录下没有找到任何技能文件夹")
  }

  fmt.Printf("发现 %d 个技能: %s\n", len(skillNames), strings.Join(skillNames, ", "))

  deployFn := func(username string) error {
    targetSkillsDir := filepath.Join(user.UserDir(cfg, username), ".picoclaw", "workspace", "skills")

    for _, skillName := range skillNames {
      srcPath := filepath.Join(skillDir, skillName)
      dstPath := filepath.Join(targetSkillsDir, skillName)
      if err := util.CopyDir(srcPath, dstPath); err != nil {
        return fmt.Errorf("复制技能 %s 失败: %w", skillName, err)
      }
    }

    fmt.Printf("  [技能] %s: 已部署 %d 个技能\n", username, len(skillNames))
    return nil
  }

  if targetUser != "" {
    return deployFn(targetUser)
  }

  return user.ForEachUser(cfg, deployFn)
}
