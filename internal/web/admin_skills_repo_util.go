package web

import (
  "fmt"
  "io"
  "os"
  "os/exec"
  "path/filepath"
  "regexp"
  "strings"
  "sync"
  "time"

  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// Git 仓库操作 — 凭据与命令构建
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
// Git clone / pull 入口
// ============================================================

func cloneGitRepoWithCredentials(reposDir, repoName, repoURL string, repo config.SkillRepo) (string, error) {
  destBase := filepath.Join(reposDir, repoName)
  if err := os.RemoveAll(destBase); err != nil {
    return "", err
  }
  cleanups := []func(){}
  defer func() {
    for _, fn := range cleanups {
      fn()
    }
  }()

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
    out, err := gitCloneCmdWithCredential(reposDir, cred, repoURL, tempDest, repo.Ref)
    if err == nil {
      if tempDest != destBase {
        if err := os.RemoveAll(destBase); err != nil {
          return "", err
        }
        if err := os.Rename(tempDest, destBase); err != nil {
          return "", err
        }
      }
      return out, nil
    }
    lastErr = err
    if tempDest != destBase {
      cleanups = append(cleanups, func() { _ = os.RemoveAll(tempDest) })
    }
  }
  return "", lastErr
}

func pullGitRepoWithCredentials(repoDir string, repo config.SkillRepo) (string, error) {
  attempts := repo.Credentials
  if repo.Public || len(attempts) == 0 {
    attempts = []config.SkillRepoCredential{{Name: "public", Mode: "https"}}
  }

  var lastErr error
  for _, cred := range attempts {
    if repo.RefType == "tag" && repo.Ref != "" {
      if _, err := gitFetchTagsCmdWithCredential(repoDir, cred); err != nil {
        lastErr = err
        continue
      }
      if _, err := gitCheckoutTagCmdWithCredential(repoDir, cred, repo.Ref); err != nil {
        lastErr = err
        continue
      }
      if _, err := gitResetHardCmdWithCredential(repoDir, cred, repo.Ref); err != nil {
        lastErr = err
        continue
      }
      return "tag refreshed", nil
    }
    if repo.Ref != "" {
      if _, err := gitFetchRefCmdWithCredential(repoDir, cred, repo.Ref); err != nil {
        lastErr = err
        continue
      }
      if _, err := gitCheckoutBranchCmdWithCredential(repoDir, cred, repo.Ref); err != nil {
        lastErr = err
        continue
      }
      if _, err := gitResetOriginBranchCmdWithCredential(repoDir, cred, repo.Ref); err != nil {
        lastErr = err
        continue
      }
      return "branch refreshed", nil
    }
    out, err := gitPullCmdWithCredential(repoDir, cred)
    if err == nil {
      return out, nil
    }
    lastErr = err
  }
  return "", lastErr
}

// ============================================================
// Git 子命令构建器
// ============================================================

func gitCloneCmdWithCredential(dir string, cred config.SkillRepoCredential, repoURL, dest, ref string) (string, error) {
  if err := validateSkillRepoRef(ref); err != nil {
    return "", err
  }
  return gitCmdWithCredential(dir, cred, gitCloneScript, "PICOAIDE_GIT_URL="+repoURL, "PICOAIDE_GIT_DEST="+dest, "PICOAIDE_GIT_REF="+ref)
}

func gitFetchTagsCmdWithCredential(dir string, cred config.SkillRepoCredential) (string, error) {
  return gitCmdWithCredential(dir, cred, gitFetchTagsScript)
}

func gitFetchRefCmdWithCredential(dir string, cred config.SkillRepoCredential, ref string) (string, error) {
  if err := validateSkillRepoRef(ref); err != nil {
    return "", err
  }
  return gitCmdWithCredential(dir, cred, gitFetchRefScript, "PICOAIDE_GIT_REF="+ref)
}

func gitCheckoutTagCmdWithCredential(dir string, cred config.SkillRepoCredential, ref string) (string, error) {
  if err := validateSkillRepoRef(ref); err != nil {
    return "", err
  }
  return gitCmdWithCredential(dir, cred, gitCheckoutTagScript, "PICOAIDE_GIT_REF="+ref)
}

func gitCheckoutBranchCmdWithCredential(dir string, cred config.SkillRepoCredential, ref string) (string, error) {
  if err := validateSkillRepoRef(ref); err != nil {
    return "", err
  }
  return gitCmdWithCredential(dir, cred, gitCheckoutBranchScript, "PICOAIDE_GIT_REF="+ref)
}

func gitResetHardCmdWithCredential(dir string, cred config.SkillRepoCredential, ref string) (string, error) {
  if err := validateSkillRepoRef(ref); err != nil {
    return "", err
  }
  return gitCmdWithCredential(dir, cred, gitResetHardScript, "PICOAIDE_GIT_REF="+ref)
}

func gitResetOriginBranchCmdWithCredential(dir string, cred config.SkillRepoCredential, ref string) (string, error) {
  if err := validateSkillRepoRef(ref); err != nil {
    return "", err
  }
  return gitCmdWithCredential(dir, cred, gitResetOriginBranchScript, "PICOAIDE_GIT_REF="+ref)
}

func gitPullCmdWithCredential(dir string, cred config.SkillRepoCredential) (string, error) {
  return gitCmdWithCredential(dir, cred, gitPullScript)
}

// ============================================================
// Git 脚本常量
// ============================================================

const (
  gitCloneScript = `#!/bin/sh
if [ -n "${PICOAIDE_GIT_REF:-}" ]; then
  exec git clone --branch "$PICOAIDE_GIT_REF" --single-branch -- "$PICOAIDE_GIT_URL" "$PICOAIDE_GIT_DEST"
fi
exec git clone -- "$PICOAIDE_GIT_URL" "$PICOAIDE_GIT_DEST"
`
  gitFetchTagsScript = `#!/bin/sh
exec git fetch --tags origin
`
  gitFetchRefScript = `#!/bin/sh
exec git fetch origin "$PICOAIDE_GIT_REF"
`
  gitCheckoutTagScript = `#!/bin/sh
exec git checkout -f "$PICOAIDE_GIT_REF"
`
  gitCheckoutBranchScript = `#!/bin/sh
exec git checkout -B "$PICOAIDE_GIT_REF" "origin/$PICOAIDE_GIT_REF"
`
  gitResetHardScript = `#!/bin/sh
exec git reset --hard "$PICOAIDE_GIT_REF"
`
  gitResetOriginBranchScript = `#!/bin/sh
exec git reset --hard "origin/$PICOAIDE_GIT_REF"
`
  gitPullScript = `#!/bin/sh
exec git pull --ff-only
`
)

// ============================================================
// gitCmdWithCredential — 统一 Git 命令执行
// ============================================================

// gitCmdWithCredential — 统一 Git 命令执行（返回输出）
func gitCmdWithCredential(dir string, cred config.SkillRepoCredential, gitScript string, extraEnv ...string) (string, error) {
  env := os.Environ()
  env = append(env, "GIT_TERMINAL_PROMPT=0")
  env = append(env, extraEnv...)

  cleanups := []func(){}
  switch cred.Mode {
  case "ssh":
    keyFile, err := os.CreateTemp("", "picoaide-git-key-*")
    if err != nil {
      return "", err
    }
    if _, err := keyFile.WriteString(cred.Secret); err != nil {
      keyFile.Close()
      os.Remove(keyFile.Name())
      return "", err
    }
    if err := keyFile.Chmod(0600); err != nil {
      keyFile.Close()
      os.Remove(keyFile.Name())
      return "", err
    }
    if err := keyFile.Close(); err != nil {
      os.Remove(keyFile.Name())
      return "", err
    }
    sshWrapper, err := os.CreateTemp("", "picoaide-git-ssh-*")
    if err != nil {
      os.Remove(keyFile.Name())
      return "", err
    }
    wrapperContent := "#!/bin/sh\nexec ssh -i \"" + keyFile.Name() + "\" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new \"$@\"\n"
    if _, err := sshWrapper.WriteString(wrapperContent); err != nil {
      sshWrapper.Close()
      os.Remove(sshWrapper.Name())
      os.Remove(keyFile.Name())
      return "", err
    }
    if err := sshWrapper.Chmod(0700); err != nil {
      sshWrapper.Close()
      os.Remove(sshWrapper.Name())
      os.Remove(keyFile.Name())
      return "", err
    }
    if err := sshWrapper.Close(); err != nil {
      os.Remove(sshWrapper.Name())
      os.Remove(keyFile.Name())
      return "", err
    }
    env = append(env, "GIT_SSH_COMMAND="+sshWrapper.Name())
    cleanups = append(cleanups, func() {
      _ = os.Remove(keyFile.Name())
      _ = os.Remove(sshWrapper.Name())
    })
  default:
    script, err := os.CreateTemp("", "picoaide-git-askpass-*")
    if err != nil {
      return "", err
    }
    content := "#!/bin/sh\ncase \"$1\" in\n*Username*) printf '%s' \"${PICOAIDE_GIT_USERNAME:-" + skillRepoDefaultUsername(cred.Provider) + "}\" ;;\n*) printf '%s' \"${PICOAIDE_GIT_PASSWORD:-}\" ;;\nesac\n"
    if _, err := script.WriteString(content); err != nil {
      script.Close()
      os.Remove(script.Name())
      return "", err
    }
    if err := script.Chmod(0700); err != nil {
      script.Close()
      os.Remove(script.Name())
      return "", err
    }
    if err := script.Close(); err != nil {
      os.Remove(script.Name())
      return "", err
    }
    env = append(env, "GIT_ASKPASS="+script.Name())
    env = append(env, "PICOAIDE_GIT_USERNAME="+cred.Username)
    env = append(env, "PICOAIDE_GIT_PASSWORD="+cred.Secret)
    cleanups = append(cleanups, func() { _ = os.Remove(script.Name()) })
  }
  gitWrapper, err := os.CreateTemp("", "picoaide-git-op-*")
  if err != nil {
    for _, cleanup := range cleanups {
      cleanup()
    }
    return "", err
  }
  if _, err := gitWrapper.WriteString(gitScript); err != nil {
    gitWrapper.Close()
    os.Remove(gitWrapper.Name())
    for _, cleanup := range cleanups {
      cleanup()
    }
    return "", err
  }
  if err := gitWrapper.Chmod(0700); err != nil {
    gitWrapper.Close()
    os.Remove(gitWrapper.Name())
    for _, cleanup := range cleanups {
      cleanup()
    }
    return "", err
  }
  if err := gitWrapper.Close(); err != nil {
    os.Remove(gitWrapper.Name())
    for _, cleanup := range cleanups {
      cleanup()
    }
    return "", err
  }
  cleanups = append(cleanups, func() { _ = os.Remove(gitWrapper.Name()) })
  defer func() {
    for _, cleanup := range cleanups {
      cleanup()
    }
  }()
  cmd := exec.Command(gitWrapper.Name())
  cmd.Dir = dir
  cmd.Env = env
  var stdout, stderr strings.Builder
  cmd.Stdout = &stdout
  cmd.Stderr = &stderr
  if err := cmd.Run(); err != nil {
    return "", fmt.Errorf("%w\n%s", err, stderr.String())
  }
  return stdout.String(), nil
}

// writeSSE 写一行 SSE data 事件
func writeSSE(w io.Writer, flush func(), data string) {
  fmt.Fprintf(w, "data: %s\n\n", data)
  flush()
}

// gitCmdWithStream 执行 git 命令并流式输出 stdout/stderr 到 writer
func gitCmdWithStream(dir string, cred config.SkillRepoCredential, gitScript string, w io.Writer, flush func(), extraEnv ...string) error {
  env := os.Environ()
  env = append(env, "GIT_TERMINAL_PROMPT=0")
  env = append(env, extraEnv...)

  var cleanups []func()

  switch cred.Mode {
  case "ssh":
    keyFile, err := os.CreateTemp("", "picoaide-git-key-*")
    if err != nil {
      return err
    }
    if _, err := keyFile.WriteString(cred.Secret); err != nil {
      keyFile.Close()
      os.Remove(keyFile.Name())
      return err
    }
    if err := keyFile.Chmod(0600); err != nil {
      keyFile.Close()
      os.Remove(keyFile.Name())
      return err
    }
    if err := keyFile.Close(); err != nil {
      os.Remove(keyFile.Name())
      return err
    }
    sshWrapper, err := os.CreateTemp("", "picoaide-git-ssh-*")
    if err != nil {
      os.Remove(keyFile.Name())
      return err
    }
    wrapperContent := "#!/bin/sh\nexec ssh -i \"" + keyFile.Name() + "\" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new \"$@\"\n"
    if _, err := sshWrapper.WriteString(wrapperContent); err != nil {
      sshWrapper.Close()
      os.Remove(sshWrapper.Name())
      os.Remove(keyFile.Name())
      return err
    }
    if err := sshWrapper.Chmod(0700); err != nil {
      sshWrapper.Close()
      os.Remove(sshWrapper.Name())
      os.Remove(keyFile.Name())
      return err
    }
    if err := sshWrapper.Close(); err != nil {
      os.Remove(sshWrapper.Name())
      os.Remove(keyFile.Name())
      return err
    }
    env = append(env, "GIT_SSH_COMMAND="+sshWrapper.Name())
    cleanups = append(cleanups, func() {
      _ = os.Remove(keyFile.Name())
      _ = os.Remove(sshWrapper.Name())
    })
  default:
    script, err := os.CreateTemp("", "picoaide-git-askpass-*")
    if err != nil {
      return err
    }
    content := "#!/bin/sh\ncase \"$1\" in\n*Username*) printf '%s' \"${PICOAIDE_GIT_USERNAME:-" + skillRepoDefaultUsername(cred.Provider) + "}\" ;;\n*) printf '%s' \"${PICOAIDE_GIT_PASSWORD:-}\" ;;\nesac\n"
    if _, err := script.WriteString(content); err != nil {
      script.Close()
      os.Remove(script.Name())
      return err
    }
    if err := script.Chmod(0700); err != nil {
      script.Close()
      os.Remove(script.Name())
      return err
    }
    if err := script.Close(); err != nil {
      os.Remove(script.Name())
      return err
    }
    env = append(env, "GIT_ASKPASS="+script.Name())
    env = append(env, "PICOAIDE_GIT_USERNAME="+cred.Username)
    env = append(env, "PICOAIDE_GIT_PASSWORD="+cred.Secret)
    cleanups = append(cleanups, func() { _ = os.Remove(script.Name()) })
  }

  gitWrapper, err := os.CreateTemp("", "picoaide-git-op-*")
  if err != nil {
    for _, cleanup := range cleanups {
      cleanup()
    }
    return err
  }
  if _, err := gitWrapper.WriteString(gitScript); err != nil {
    gitWrapper.Close()
    os.Remove(gitWrapper.Name())
    for _, cleanup := range cleanups {
      cleanup()
    }
    return err
  }
  if err := gitWrapper.Chmod(0700); err != nil {
    gitWrapper.Close()
    os.Remove(gitWrapper.Name())
    for _, cleanup := range cleanups {
      cleanup()
    }
    return err
  }
  if err := gitWrapper.Close(); err != nil {
    os.Remove(gitWrapper.Name())
    for _, cleanup := range cleanups {
      cleanup()
    }
    return err
  }
  cleanups = append(cleanups, func() { _ = os.Remove(gitWrapper.Name()) })
  defer func() {
    for _, cleanup := range cleanups {
      cleanup()
    }
  }()

  cmd := exec.Command(gitWrapper.Name())
  cmd.Dir = dir
  cmd.Env = env

  stdoutPipe, _ := cmd.StdoutPipe()
  stderrPipe, _ := cmd.StderrPipe()

  if err := cmd.Start(); err != nil {
    return err
  }

  // 流式读取 stdout
  stdoutDone := make(chan struct{})
  go func() {
    defer close(stdoutDone)
    buf := make([]byte, 4096)
    for {
      n, err := stdoutPipe.Read(buf)
      if n > 0 {
        writeSSE(w, flush, strings.TrimSpace(string(buf[:n])))
      }
      if err != nil {
        return
      }
    }
  }()

  // 流式读取 stderr（git 进度信息通常在 stderr）
  stderrDone := make(chan struct{})
  go func() {
    defer close(stderrDone)
    buf := make([]byte, 4096)
    for {
      n, err := stderrPipe.Read(buf)
      if n > 0 {
        writeSSE(w, flush, strings.TrimSpace(string(buf[:n])))
      }
      if err != nil {
        return
      }
    }
  }()

  <-stdoutDone
  <-stderrDone
  return cmd.Wait()
}
