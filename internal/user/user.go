package user

import (
  "fmt"
  "os"
  "path/filepath"
  "regexp"
  "sort"
  "time"

  "github.com/picoaide/picoaide/internal/store"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 用户目录管理
// ============================================================

var validUsername = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`)

// ValidateUsername 验证用户名合法性
func ValidateUsername(username string) error {
  if username == "" {
    return fmt.Errorf("用户名不能为空")
  }
  if len(username) > 64 {
    return fmt.Errorf("用户名过长 (最多 64 字符)")
  }
  if !validUsername.MatchString(username) {
    return fmt.Errorf("用户名 '%s' 不合法，只允许字母、数字、点、短横线、下划线", username)
  }
  return nil
}

// LoadWhitelist 从数据库读取白名单，返回用户名集合
func LoadWhitelist() (map[string]bool, error) {
  engine, err := store.GetEngine()
  if err != nil {
    return nil, nil
  }

  var entries []store.WhitelistEntry
  if err := engine.Find(&entries); err != nil {
    return nil, nil
  }

  m := make(map[string]bool)
  for _, e := range entries {
    m[e.Username] = true
  }

  if len(m) == 0 {
    return nil, nil
  }
  return m, nil
}

// IsWhitelisted 检查用户是否在白名单内
func IsWhitelisted(whitelist map[string]bool, username string) bool {
  if whitelist == nil {
    return true
  }
  return whitelist[username]
}

func AllowedByWhitelist(cfg *config.GlobalConfig, provider, username string) bool {
  if cfg == nil || !cfg.WhitelistEnabledForProvider(provider) {
    return true
  }
  whitelist, _ := LoadWhitelist()
  return IsWhitelisted(whitelist, username)
}

// ResolveUsersRoot 解析用户根目录路径
func ResolveUsersRoot(cfg *config.GlobalConfig) string {
  if filepath.IsAbs(cfg.UsersRoot) {
    return cfg.UsersRoot
  }
  wd, _ := os.Getwd()
  return filepath.Join(wd, cfg.UsersRoot)
}

// UserDir 返回指定用户的目录路径
// 注意：调用方应先通过 ValidateUsername 验证用户名合法性
func UserDir(cfg *config.GlobalConfig, username string) string {
  base := filepath.Base(username) // 防御性截取：确保不会拼出子路径
  return filepath.Join(ResolveUsersRoot(cfg), base)
}

// EnsureUsersRoot 确保用户根目录和归档目录存在
func EnsureUsersRoot(cfg *config.GlobalConfig) error {
  root := ResolveUsersRoot(cfg)
  if err := os.MkdirAll(root, 0755); err != nil {
    return err
  }
  return os.MkdirAll(ResolveArchiveRoot(cfg), 0755)
}

// GetUserList 获取所有用户列表（从数据库读取）
func GetUserList(cfg *config.GlobalConfig) ([]string, error) {
  localUsers, err := store.GetAllLocalUsers()
  if err != nil {
    return nil, err
  }
  var users []string
  for _, u := range localUsers {
    users = append(users, u.Username)
  }
  sort.Strings(users)
  return users, nil
}

// InitUser 初始化单个用户：创建目录、workspace、生成 MCP token
func InitUser(cfg *config.GlobalConfig, username string) error {
  if err := ValidateUsername(username); err != nil {
    return err
  }
  if err := EnsureUsersRoot(cfg); err != nil {
    return fmt.Errorf("创建用户根目录失败: %w", err)
  }

  ud := UserDir(cfg, username)
  existing := false
  if _, err := os.Stat(ud); err == nil {
    existing = true
  } else {
    if err := os.MkdirAll(ud, 0755); err != nil {
      return fmt.Errorf("创建目录失败 %s: %w", username, err)
    }
  }

  // 将旧 .picoagent/workspace 目录内容迁移到用户根目录
  oldWs := filepath.Join(ud, ".picoagent", "workspace")
  if info, err := os.Stat(oldWs); err == nil && info.IsDir() {
    util.CopyDir(oldWs, ud)
    os.RemoveAll(filepath.Join(ud, ".picoagent"))
  }
  oldPicoclaw := filepath.Join(ud, ".picoclaw", "workspace")
  if info, err := os.Stat(oldPicoclaw); err == nil && info.IsDir() {
    util.CopyDir(oldPicoclaw, ud)
    os.RemoveAll(filepath.Join(ud, ".picoclaw"))
  }

  // 初始化 workspace（用户目录本身就是 workspace）
  if err := InitializeUser("", cfg.UsersRoot, username); err != nil {
    return fmt.Errorf("初始化 workspace 失败: %w", err)
  }

  // 确保 MCP token 存在（不覆盖已有 token）
  if existingToken, _ := store.GetMCPToken(username); existingToken == "" {
    if _, err := store.GenerateMCPToken(username); err != nil {
      fmt.Printf("  [警告] %s: 生成 MCP token 失败: %v\n", username, err)
    }
  }

  if existing {
    fmt.Printf("  [更新] %s\n", username)
  } else {
    fmt.Printf("  [初始化] %s 完成\n", username)
  }
  return nil
}

// ResolveArchiveRoot 解析归档目录路径
func ResolveArchiveRoot(cfg *config.GlobalConfig) string {
  if filepath.IsAbs(cfg.ArchiveRoot) {
    return cfg.ArchiveRoot
  }
  if cfg.ArchiveRoot == "" {
    cfg.ArchiveRoot = "./archive"
  }
  wd, _ := os.Getwd()
  return filepath.Join(wd, cfg.ArchiveRoot)
}

// ArchiveUser 将离职用户的目录从 users/ 移动到 archive/
func ArchiveUser(cfg *config.GlobalConfig, username string) error {
  archiveRoot := ResolveArchiveRoot(cfg)
  if err := os.MkdirAll(archiveRoot, 0755); err != nil {
    return fmt.Errorf("创建归档目录失败: %w", err)
  }

  srcDir := UserDir(cfg, username)
  if _, err := os.Stat(srcDir); err != nil {
    return nil
  }

  dirName := filepath.Base(srcDir)
  dstDir := filepath.Join(archiveRoot, dirName)

  if _, err := os.Stat(dstDir); err == nil {
    dstDir = filepath.Join(archiveRoot, dirName+"."+fmt.Sprintf("%d", time.Now().Unix()))
  }

  if err := os.Rename(srcDir, dstDir); err != nil {
    return fmt.Errorf("归档 %s 失败: %w", username, err)
  }
  return nil
}

// RemoveAllUserData removes ordinary user workspace and archive data. It is
// used when switching authentication providers to prevent identity data from
// one provider from leaking into the next provider's users.
func RemoveAllUserData(cfg *config.GlobalConfig) error {
  for _, dir := range []string{ResolveUsersRoot(cfg), ResolveArchiveRoot(cfg)} {
    if dir == "" || dir == "." || dir == string(filepath.Separator) {
      return fmt.Errorf("拒绝清空危险目录: %s", dir)
    }
    if err := os.RemoveAll(dir); err != nil {
      return err
    }
    if err := os.MkdirAll(dir, 0755); err != nil {
      return err
    }
  }
  return nil
}

// ============================================================
// Cookie 与安全配置
// ============================================================

// SyncCookies 将域名对应的 Cookie 字符串写入数据库（供 MCP API 使用）
func SyncCookies(cfg *config.GlobalConfig, username, domain, cookieStr string) error {
  if err := ValidateUsername(username); err != nil {
    return err
  }
  if err := store.SetCookie(username, domain, cookieStr); err != nil {
    return fmt.Errorf("同步 Cookie 到数据库失败: %w", err)
  }
  return nil
}

// ============================================================
// 用户初始化（从模板创建 workspace）
// ============================================================

// InitializeUser 复制 user-template 到用户的 workspace 目录
// templateDir: <WorkDir>/user-template/
// usersRoot:   <WorkDir>/users/
// username:    经过 ValidateUsername 校验的用户名
func InitializeUser(templateDir, usersRoot, username string) error {
  if err := util.SafePathSegment(username); err != nil {
    return fmt.Errorf("用户名不合法: %w", err)
  }
  workspace := filepath.Join(usersRoot, username)

  if err := os.MkdirAll(workspace, 0755); err != nil {
    return fmt.Errorf("创建工作目录失败: %w", err)
  }

  if templateDir != "" {
    cleanTemplate := filepath.Clean(templateDir)
    if info, err := os.Stat(cleanTemplate); err == nil && info.IsDir() {
      if err := util.CopyDir(cleanTemplate, workspace); err != nil {
        return fmt.Errorf("复制用户模板失败: %w", err)
      }
    }
  }

  for _, dir := range []string{"memory", "skills", "sessions"} {
    if err := os.MkdirAll(filepath.Join(workspace, dir), 0755); err != nil {
      return fmt.Errorf("创建 %s 目录失败: %w", dir, err)
    }
  }

  writeDefault := func(path, content string) {
    cleanPath := filepath.Clean(path)
    if _, err := os.Stat(cleanPath); err != nil {
      os.WriteFile(cleanPath, []byte(content), 0644)
    }
  }

  writeDefault(filepath.Join(workspace, "AGENT.md"),
    "---\nname: pico\ndescription: 默认通用助手\n---\n\n你是 PicoAgent，本工作区的默认助手。\n")

  writeDefault(filepath.Join(workspace, "SOUL.md"),
    "# 人格设定\n\n在这里描述 AI 助手的角色定位、语气风格和行为准则。\n")

  writeDefault(filepath.Join(workspace, "USER.md"),
    "# 用户信息\n\n在这里描述你的背景、偏好和常用工作流程，帮助 AI 更好地为你服务。\n")

  writeDefault(filepath.Join(workspace, "memory", "MEMORY.md"),
    "# 长期记忆\n\nAI 会在这里记录需要长期保留的重要信息。\n")

  return nil
}


