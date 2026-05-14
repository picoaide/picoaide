package web

import (
  "fmt"
  "io"
  "os"
  "path/filepath"
  "regexp"
  "strings"
  "sync"
  "time"

  gogit "github.com/go-git/go-git/v5"
  gogitconfig "github.com/go-git/go-git/v5/config"
  "github.com/go-git/go-git/v5/plumbing"
  "github.com/go-git/go-git/v5/plumbing/transport"
  githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
  gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
  "golang.org/x/crypto/ssh"

  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// Git 仓库操作 — 使用 go-git（纯 Go 实现，无系统命令依赖）
// ============================================================

var gitMutex sync.Mutex

var safeSkillRepoRefRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

type skillRepoCredentialInput struct {
  Name     string `json:"name"`
  Provider string `json:"provider"`
  Mode     string `json:"mode"`
  Username string `json:"username"`
  Secret   string `json:"secret"`
}

type skillRepoInput struct {
  Name        string                     `json:"name"`
  URL         string                     `json:"url"`
  Ref         string                     `json:"ref"`
  RefType     string                     `json:"ref_type"`
  Public      bool                       `json:"public"`
  Credentials []skillRepoCredentialInput `json:"credentials"`
}

func normalizeSkillRepoCredentialInput(input skillRepoCredentialInput, fallbackName string) config.SkillRepoCredential {
  name := strings.TrimSpace(input.Name)
  if name == "" {
    name = fallbackName
  }
  return config.SkillRepoCredential{
    Name:     name,
    Provider: strings.TrimSpace(input.Provider),
    Mode:     strings.ToLower(strings.TrimSpace(input.Mode)),
    Username: strings.TrimSpace(input.Username),
    Secret:   strings.TrimSpace(input.Secret),
  }
}

func normalizeSkillRepoRef(ref, refType string) (string, string) {
  ref = strings.TrimSpace(ref)
  refType = strings.ToLower(strings.TrimSpace(refType))
  if ref == "" {
    return "", ""
  }
  if refType != "tag" {
    refType = "branch"
  }
  return ref, refType
}

func validateSkillRepoRef(ref string) error {
  if ref == "" {
    return nil
  }
  if len(ref) > 255 || strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") || strings.Contains(ref, "//") {
    return fmt.Errorf("Git ref 不合法")
  }
  if !safeSkillRepoRefRe.MatchString(ref) || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") {
    return fmt.Errorf("Git ref 不合法")
  }
  return nil
}

func inferSkillRepoCredentialMode(repoURL string) string {
  lower := strings.ToLower(strings.TrimSpace(repoURL))
  if strings.HasPrefix(lower, "git@") || strings.HasPrefix(lower, "ssh://") {
    return "ssh"
  }
  if strings.HasPrefix(lower, "http://") {
    return "http"
  }
  return "https"
}

func skillRepoDefaultUsername(provider string) string {
  switch strings.ToLower(strings.TrimSpace(provider)) {
  case "gitlab":
    return "oauth2"
  case "gitee":
    return "git"
  default:
    return "x-access-token"
  }
}

func skillRepoFromInput(input skillRepoInput) (config.SkillRepo, error) {
  name := strings.TrimSpace(input.Name)
  if name == "" {
    return config.SkillRepo{}, fmt.Errorf("仓库名称不能为空")
  }
  if err := util.SafePathSegment(name); err != nil {
    return config.SkillRepo{}, fmt.Errorf("仓库名称不合法: %w", err)
  }
  url := strings.TrimSpace(input.URL)
  if url == "" {
    return config.SkillRepo{}, fmt.Errorf("仓库地址不能为空")
  }
  if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "git@") && !strings.HasPrefix(url, "ssh://") {
    return config.SkillRepo{}, fmt.Errorf("仓库地址必须是 http://、https://、git@ 或 ssh:// 开头的 Git 地址")
  }
  ref, refType := normalizeSkillRepoRef(input.Ref, input.RefType)
  if err := validateSkillRepoRef(ref); err != nil {
    return config.SkillRepo{}, err
  }
  var creds []config.SkillRepoCredential
  for i, raw := range input.Credentials {
    cred := normalizeSkillRepoCredentialInput(raw, fmt.Sprintf("%s-%d", name, i+1))
    if cred.Name == "" {
      continue
    }
    cred.Mode = inferSkillRepoCredentialMode(url)
    if cred.Mode != "ssh" && cred.Mode != "http" && cred.Mode != "https" {
      return config.SkillRepo{}, fmt.Errorf("凭据 %s 的方式必须是 ssh/http/https", cred.Name)
    }
    if cred.Mode == "http" || cred.Mode == "https" {
      if cred.Username == "" {
        cred.Username = skillRepoDefaultUsername(cred.Provider)
      }
    }
    creds = append(creds, cred)
  }
  if !input.Public && len(creds) == 0 {
    return config.SkillRepo{}, fmt.Errorf("私有仓库至少需要配置一个凭据")
  }
  return config.SkillRepo{
    Name:        name,
    URL:         url,
    Ref:         ref,
    RefType:     refType,
    Public:      input.Public,
    Credentials: creds,
  }, nil
}

func skillRepoByName(repos []config.SkillRepo, name string) (config.SkillRepo, int, bool) {
  for i, repo := range repos {
    if repo.Name == name {
      return repo, i, true
    }
  }
  return config.SkillRepo{}, -1, false
}

// ============================================================
// go-git 认证方法
// ============================================================

func goGitAuth(cred config.SkillRepoCredential) (transport.AuthMethod, error) {
  switch cred.Mode {
  case "ssh":
    signer, err := ssh.ParsePrivateKey([]byte(cred.Secret))
    if err != nil {
      return nil, fmt.Errorf("解析 SSH 私钥失败: %w", err)
    }
    return &gitssh.PublicKeys{
      User:   "git",
      Signer: signer,
      HostKeyCallbackHelper: gitssh.HostKeyCallbackHelper{
        HostKeyCallback: ssh.InsecureIgnoreHostKey(),
      },
    }, nil
  default:
    return &githttp.BasicAuth{
      Username: cred.Username,
      Password: cred.Secret,
    }, nil
  }
}

// gitRefName 根据 ref 和 refType 返回完整的 git 引用名
func gitRefName(ref, refType string) string {
  if refType == "tag" {
    return "refs/tags/" + ref
  }
  return "refs/heads/" + ref
}

// ============================================================
// Git clone / pull 入口
// ============================================================

func cloneGitRepoWithCredentials(reposDir, repoName, repoURL string, repo config.SkillRepo) (string, error) {
  destBase := filepath.Join(reposDir, repoName)
  if err := os.RemoveAll(destBase); err != nil {
    return "", err
  }

  attempts := repo.Credentials
  if repo.Public || len(attempts) == 0 {
    attempts = []config.SkillRepoCredential{{Name: "public", Mode: "https"}}
  }

  var lastErr error
  for idx, cred := range attempts {
    tempDest := destBase
    if len(attempts) > 1 {
      tempDest = filepath.Join(reposDir, fmt.Sprintf("%s.tmp-%d-%d", repoName, time.Now().UnixNano(), idx))
      if err := os.RemoveAll(tempDest); err != nil {
        return "", err
      }
    }

    auth, authErr := goGitAuth(cred)
    if authErr != nil {
      lastErr = authErr
      if tempDest != destBase {
        os.RemoveAll(tempDest)
      }
      continue
    }

    opts := &gogit.CloneOptions{
      URL:  repoURL,
      Auth: auth,
    }
    if repo.Ref != "" {
      opts.ReferenceName = plumbing.ReferenceName(gitRefName(repo.Ref, repo.RefType))
      opts.SingleBranch = true
      opts.Depth = 1
    }

    _, err := gogit.PlainClone(tempDest, false, opts)
    if err == nil {
      if tempDest != destBase {
        if err := os.RemoveAll(destBase); err != nil {
          return "", err
        }
        if err := os.Rename(tempDest, destBase); err != nil {
          return "", err
        }
      }
      return "克隆成功", nil
    }
    lastErr = err
    if tempDest != destBase {
      os.RemoveAll(tempDest)
    }
  }
  return "", lastErr
}

func pullGitRepoWithCredentials(repoDir string, repo config.SkillRepo) (string, error) {
  r, err := gogit.PlainOpen(repoDir)
  if err != nil {
    return "", fmt.Errorf("打开 Git 仓库失败: %w", err)
  }

  w, err := r.Worktree()
  if err != nil {
    return "", fmt.Errorf("获取工作区失败: %w", err)
  }

  attempts := repo.Credentials
  if repo.Public || len(attempts) == 0 {
    attempts = []config.SkillRepoCredential{{Name: "public", Mode: "https"}}
  }

  for _, cred := range attempts {
    auth, authErr := goGitAuth(cred)
    if authErr != nil {
      continue
    }

    err = pullWithRepo(r, w, repo, auth)
    if err == nil {
      return "拉取成功", nil
    }
    if err == gogit.NoErrAlreadyUpToDate {
      return "Already up to date.", nil
    }
  }
  return "", err
}

func pullWithRepo(r *gogit.Repository, w *gogit.Worktree, repo config.SkillRepo, auth transport.AuthMethod) error {
  if repo.RefType == "tag" && repo.Ref != "" {
    if err := r.Fetch(&gogit.FetchOptions{
      RefSpecs: []gogitconfig.RefSpec{"+refs/tags/*:refs/tags/*"},
      Auth:     auth,
    }); err != nil && err != gogit.NoErrAlreadyUpToDate {
      return err
    }
    if err := w.Checkout(&gogit.CheckoutOptions{
      Branch: plumbing.ReferenceName("refs/tags/" + repo.Ref),
      Force:  true,
    }); err != nil {
      return err
    }
    tagRef, err := r.Reference(plumbing.ReferenceName("refs/tags/"+repo.Ref), true)
    if err != nil {
      return err
    }
    commit, err := r.CommitObject(tagRef.Hash())
    if err != nil {
      return err
    }
    return w.Reset(&gogit.ResetOptions{
      Mode:   gogit.HardReset,
      Commit: commit.Hash,
    })
  }

  if repo.Ref != "" {
    refSpec := gogitconfig.RefSpec("+refs/heads/" + repo.Ref + ":refs/remotes/origin/" + repo.Ref)
    if err := r.Fetch(&gogit.FetchOptions{
      RefSpecs: []gogitconfig.RefSpec{refSpec},
      Auth:     auth,
    }); err != nil && err != gogit.NoErrAlreadyUpToDate {
      return err
    }
    if err := w.Checkout(&gogit.CheckoutOptions{
      Branch: plumbing.ReferenceName("refs/heads/" + repo.Ref),
      Create: true,
      Force:  true,
    }); err != nil {
      return err
    }
    originRef, err := r.Reference(plumbing.ReferenceName("refs/remotes/origin/"+repo.Ref), true)
    if err != nil {
      return err
    }
    commit, err := r.CommitObject(originRef.Hash())
    if err != nil {
      return err
    }
    return w.Reset(&gogit.ResetOptions{
      Mode:   gogit.HardReset,
      Commit: commit.Hash,
    })
  }

  if err := w.Pull(&gogit.PullOptions{
    Auth: auth,
  }); err != nil && err != gogit.NoErrAlreadyUpToDate {
    return err
  }
  return nil
}

// ============================================================
// 辅助
// ============================================================

func writeSSE(w io.Writer, flush func(), data string) {
  fmt.Fprintf(w, "data: %s\n\n", data)
  flush()
}
