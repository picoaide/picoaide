package skill

import (
  "errors"
  "fmt"
  "os"
  "path/filepath"
  "strings"

  gogit "github.com/go-git/go-git/v5"
  "github.com/go-git/go-git/v5/plumbing"

  "github.com/go-git/go-git/v5/config"
  "github.com/go-git/go-git/v5/plumbing/transport"
  githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
  "github.com/picoaide/picoaide/internal/util"
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

// IsAuthError 判断错误是否为 Git 鉴权失败
func IsAuthError(err error) bool {
  if err == nil {
    return false
  }
  if errors.Is(err, transport.ErrAuthenticationRequired) ||
    errors.Is(err, transport.ErrAuthorizationFailed) {
    return true
  }
  msg := err.Error()
  return strings.Contains(msg, "authentication required") ||
    strings.Contains(msg, "authorization failed") ||
    strings.Contains(msg, "access denied") ||
    strings.Contains(msg, "HTTP Basic: Access denied") ||
    strings.Contains(msg, "Invalid credentials")
}

// setGitAuth 设置 go-git 鉴权信息（username/password 都为空时不设置）
func setGitAuth(opts interface {
  SetAuth(transport.AuthMethod)
}, username, password string) {
  if username != "" && password != "" {
    opts.SetAuth(&githttp.BasicAuth{Username: username, Password: password})
  }
}

// cloneAuthOptions CloneOptions 包装，支持 SetAuth
type cloneAuthOptions struct{ *gogit.CloneOptions }

func (o cloneAuthOptions) SetAuth(a transport.AuthMethod) { o.Auth = a }

// fetchAuthOptions FetchOptions 包装，支持 SetAuth
type fetchAuthOptions struct{ *gogit.FetchOptions }

func (o fetchAuthOptions) SetAuth(a transport.AuthMethod) { o.Auth = a }

// pullAuthOptions PullOptions 包装，支持 SetAuth
type pullAuthOptions struct{ *gogit.PullOptions }

func (o pullAuthOptions) SetAuth(a transport.AuthMethod) { o.Auth = a }

// CloneGitSource clone Git 仓库到 skills/<name>/
// username/password 用于 HTTP Basic 鉴权，都为空时不设置鉴权
func CloneGitSource(name, url, ref, refType, username, password string) error {
  if err := util.SafePathSegment(name); err != nil {
    return fmt.Errorf("源名称不合法: %w", err)
  }
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

  setGitAuth(cloneAuthOptions{opts}, username, password)

  _, err := gogit.PlainClone(targetDir, false, opts)
  if err != nil {
    return fmt.Errorf("git clone 失败: %w", err)
  }
  return nil
}

// PullGitSource 拉取 Git 源更新并返回变更
// username/password 用于 HTTP Basic 鉴权，都为空时不设置鉴权
func PullGitSource(name, ref, refType, username, password string) (*SyncResult, error) {
  if err := util.SafePathSegment(name); err != nil {
    return nil, fmt.Errorf("源名称不合法: %w", err)
  }
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
    fetchOpts := &gogit.FetchOptions{}
    if refType == "tag" {
      fetchOpts.RefSpecs = []config.RefSpec{"+refs/tags/*:refs/tags/*"}
    } else {
      fetchOpts.RefSpecs = []config.RefSpec{config.RefSpec("+refs/heads/" + ref + ":refs/remotes/origin/" + ref)}
    }
    setGitAuth(fetchAuthOptions{fetchOpts}, username, password)

    err = r.Fetch(fetchOpts)
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
    pullOpts := &gogit.PullOptions{}
    setGitAuth(pullAuthOptions{pullOpts}, username, password)
    if err := w.Pull(pullOpts); err != nil && err != gogit.NoErrAlreadyUpToDate {
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
