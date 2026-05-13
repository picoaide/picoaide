package skill

import (
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
)

// ============================================================
// Git 源操作
// ============================================================

// SyncResult Git 源拉取更新结果
type SyncResult struct {
  Added   []string `json:"added"`
  Updated []string `json:"updated"`
  Removed []string `json:"removed"`
}

// CloneGitSource clone Git 仓库到 skills/<name>/
func CloneGitSource(name, url, ref, refType string) error {
  targetDir := filepath.Join(SkillsRootDir(), name)
  if _, err := os.Stat(targetDir); err == nil {
    return fmt.Errorf("源目录已存在: %s", targetDir)
  }

  os.MkdirAll(filepath.Dir(targetDir), 0755)

  args := []string{"clone", "--depth", "1"}
  if ref != "" {
    args = append(args, "--branch", ref, "--single-branch")
  }
  args = append(args, url, targetDir)

  cmd := exec.Command("git", args...)
  output, err := cmd.CombinedOutput()
  if err != nil {
    return fmt.Errorf("git clone 失败: %s: %w", strings.TrimSpace(string(output)), err)
  }
  return nil
}

// PullGitSource 拉取 Git 源更新并返回变更
func PullGitSource(name, ref, refType string) (*SyncResult, error) {
  repoDir := filepath.Join(SkillsRootDir(), name)
  if _, err := os.Stat(repoDir); err != nil {
    return nil, fmt.Errorf("源目录不存在: %s", repoDir)
  }
  if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
    return nil, fmt.Errorf("不是 Git 仓库: %s", repoDir)
  }

  // 记录当前技能列表
  before, _ := RescanSource(name)
  beforeSet := make(map[string]bool)
  for _, s := range before {
    beforeSet[s] = true
  }

  // 执行 git pull
  var pullCmd *exec.Cmd
  if ref != "" {
    if refType == "tag" {
      pullCmd = exec.Command("git", "fetch", "--tags", "origin")
    } else {
      pullCmd = exec.Command("git", "fetch", "origin", ref)
    }
  } else {
    pullCmd = exec.Command("git", "pull", "--ff-only")
  }
  pullCmd.Dir = repoDir
  output, err := pullCmd.CombinedOutput()
  if err != nil {
    return nil, fmt.Errorf("git pull 失败: %s: %w", strings.TrimSpace(string(output)), err)
  }

  if ref != "" {
    resetCmd := exec.Command("git", "checkout", "-f", ref)
    resetCmd.Dir = repoDir
    if out, err := resetCmd.CombinedOutput(); err != nil {
      return nil, fmt.Errorf("git checkout 失败: %s: %w", strings.TrimSpace(string(out)), err)
    }
  }

  // 重新扫描
  after, _ := RescanSource(name)
  afterSet := make(map[string]bool)
  for _, s := range after {
    afterSet[s] = true
  }

  result := &SyncResult{}
  for _, s := range after {
    if !beforeSet[s] {
      result.Added = append(result.Added, s)
    } else {
      result.Updated = append(result.Updated, s)
    }
  }
  for _, s := range before {
    if !afterSet[s] {
      result.Removed = append(result.Removed, s)
    }
  }

  return result, nil
}
