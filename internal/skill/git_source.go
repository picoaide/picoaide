package skill

import (
  "fmt"
  "os"
  "path/filepath"

  gogit "github.com/go-git/go-git/v5"
  "github.com/go-git/go-git/v5/plumbing"

  "github.com/go-git/go-git/v5/config"
)

// ============================================================
// Git 源操作（纯 Go 实现，无系统命令依赖）
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

  opts := &gogit.CloneOptions{
    URL: url,
  }
  if ref != "" {
    refName := "refs/heads/" + ref
    if refType == "tag" {
      refName = "refs/tags/" + ref
    }
    opts.ReferenceName = plumbing.ReferenceName(refName)
    opts.SingleBranch = true
    opts.Depth = 1
  }

  _, err := gogit.PlainClone(targetDir, false, opts)
  if err != nil {
    return fmt.Errorf("git clone 失败: %w", err)
  }
  return nil
}

// PullGitSource 拉取 Git 源更新并返回变更
func PullGitSource(name, ref, refType string) (*SyncResult, error) {
  repoDir := filepath.Join(SkillsRootDir(), name)
  if _, err := os.Stat(repoDir); err != nil {
    return nil, fmt.Errorf("源目录不存在: %s", repoDir)
  }

  r, err := gogit.PlainOpen(repoDir)
  if err != nil {
    return nil, fmt.Errorf("打开 Git 仓库失败: %w", err)
  }

  w, err := r.Worktree()
  if err != nil {
    return nil, fmt.Errorf("获取工作区失败: %w", err)
  }

  // 记录当前技能列表
  before, _ := RescanSource(name)
  beforeSet := make(map[string]bool)
  for _, s := range before {
    beforeSet[s] = true
  }

  if ref != "" {
    if refType == "tag" {
      err = r.Fetch(&gogit.FetchOptions{
        RefSpecs: []config.RefSpec{"+refs/tags/*:refs/tags/*"},
      })
    } else {
      err = r.Fetch(&gogit.FetchOptions{
        RefSpecs: []config.RefSpec{config.RefSpec("+refs/heads/" + ref + ":refs/remotes/origin/" + ref)},
      })
    }
    if err != nil && err != gogit.NoErrAlreadyUpToDate {
      return nil, fmt.Errorf("git fetch 失败: %w", err)
    }

    refName := plumbing.ReferenceName("refs/tags/" + ref)
    if refType != "tag" {
      refName = plumbing.ReferenceName("refs/remotes/origin/" + ref)
    }
    if err := w.Checkout(&gogit.CheckoutOptions{
      Branch: refName,
      Force:  true,
    }); err != nil {
      return nil, fmt.Errorf("git checkout 失败: %w", err)
    }
  } else {
    if err := w.Pull(&gogit.PullOptions{}); err != nil && err != gogit.NoErrAlreadyUpToDate {
      return nil, fmt.Errorf("git pull 失败: %w", err)
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
