package user

import (
  "encoding/json"
  "fmt"
  "os"
  "path/filepath"
  "regexp"
  "sort"
  "strings"
  "time"

  "gopkg.in/yaml.v3"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/ldap"
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
  d, err := auth.GetDB()
  if err != nil {
    return nil, nil
  }

  rows, err := d.Query("SELECT username FROM whitelist")
  if err != nil {
    return nil, nil
  }
  defer rows.Close()

  m := make(map[string]bool)
  for rows.Next() {
    var username string
    if err := rows.Scan(&username); err != nil {
      continue
    }
    m[username] = true
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

// ResolveUsersRoot 解析用户根目录路径
func ResolveUsersRoot(cfg *config.GlobalConfig) string {
  if filepath.IsAbs(cfg.UsersRoot) {
    return cfg.UsersRoot
  }
  wd, _ := os.Getwd()
  return filepath.Join(wd, cfg.UsersRoot)
}

// UserDir 返回指定用户的目录路径
func UserDir(cfg *config.GlobalConfig, username string) string {
  return filepath.Join(ResolveUsersRoot(cfg), username)
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
  containers, err := auth.GetAllContainers()
  if err != nil {
    return nil, err
  }
  var users []string
  for _, c := range containers {
    users = append(users, c.Username)
  }
  sort.Strings(users)
  return users, nil
}

// ForEachUser 遍历所有用户并执行回调函数
func ForEachUser(cfg *config.GlobalConfig, fn func(string) error) error {
  users, err := GetUserList(cfg)
  if err != nil {
    return err
  }
  for _, u := range users {
    if err := fn(u); err != nil {
      fmt.Fprintf(os.Stderr, "处理用户 %s 失败: %v\n", u, err)
    }
  }
  return nil
}

// InitUser 初始化单个用户：创建目录、分配 IP、写入数据库
func InitUser(cfg *config.GlobalConfig, username string, imageTag string) error {
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
    migrateLegacyRootDir(ud)
  } else {
    if err := os.MkdirAll(ud, 0755); err != nil {
      return fmt.Errorf("创建目录失败 %s: %w", username, err)
    }
  }

  // 检查是否已有 DB 记录
  rec, _ := auth.GetContainerByUsername(username)
  if rec == nil {
    ip, err := auth.AllocateNextIP()
    if err != nil {
      return fmt.Errorf("分配 IP 失败 %s: %w", username, err)
    }

    imageRef := ""
      if imageTag != "" {
        imageRef = cfg.Image.Name + ":" + imageTag
      }
    rec = &auth.ContainerRecord{
      Username: username,
      Image:    imageRef,
      Status:   "stopped",
      IP:       ip,
    }
    if err := auth.UpsertContainer(rec); err != nil {
      return fmt.Errorf("写入数据库失败 %s: %w", username, err)
    }
    // 生成 MCP token
    if _, err := auth.GenerateMCPToken(username); err != nil {
      fmt.Printf("  [警告] %s: 生成 MCP token 失败: %v\n", username, err)
    }
  }

  if existing {
    fmt.Printf("  [更新] %s (IP: %s)\n", username, rec.IP)
  } else {
    fmt.Printf("  [初始化] %s 完成 (IP: %s)\n", username, rec.IP)
  }
  return nil
}

// InitAll 从 LDAP 获取用户列表并初始化所有白名单内的用户
func InitAll(cfg *config.GlobalConfig, imageTag string) error {

  whitelist, err := LoadWhitelist()
  if err != nil {
    return fmt.Errorf("加载白名单失败: %w", err)
  }

  users, err := ldap.FetchUsers(cfg)
  if err != nil {
    return err
  }

  fmt.Printf("从 LDAP 获取到 %d 个用户\n", len(users))
  if whitelist != nil {
    fmt.Printf("白名单已启用，允许 %d 个用户\n", len(whitelist))
  }

  for _, u := range users {
    if !IsWhitelisted(whitelist, u) {
      fmt.Printf("  [跳过] %s 不在白名单中\n", u)
      continue
    }
    if err := InitUser(cfg, u, imageTag); err != nil {
      fmt.Fprintf(os.Stderr, "初始化用户 %s 失败: %v\n", u, err)
    }
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
  fmt.Printf("  [归档] %s: %s -> %s\n", username, srcDir, dstDir)
  return nil
}

// RestoreUser 将归档用户从 archive/ 恢复到 users/
func RestoreUser(cfg *config.GlobalConfig, username string) error {
  archiveRoot := ResolveArchiveRoot(cfg)
  archiveDir := filepath.Join(archiveRoot, username)
  if _, err := os.Stat(archiveDir); err != nil {
    return nil
  }

  dstDir := UserDir(cfg, username)
  if err := os.Rename(archiveDir, dstDir); err != nil {
    return fmt.Errorf("恢复 %s 失败: %w", username, err)
  }
  fmt.Printf("  [恢复] %s: archive -> users\n", username)
  return nil
}

// GetArchivedUsers 获取归档目录中的用户列表
func GetArchivedUsers(cfg *config.GlobalConfig) ([]string, error) {
  archiveRoot := ResolveArchiveRoot(cfg)
  entries, err := os.ReadDir(archiveRoot)
  if err != nil {
    if os.IsNotExist(err) {
      return nil, nil
    }
    return nil, fmt.Errorf("读取归档目录失败: %w", err)
  }

  var users []string
  for _, e := range entries {
    if e.IsDir() {
      users = append(users, e.Name())
    }
  }
  return users, nil
}

// ============================================================
// 用户配置文件操作
// ============================================================

// ApplyConfigToJSON 将全局 picoclaw 配置合并到用户的 config.json，并注入 MCP 配置
func ApplyConfigToJSON(cfg *config.GlobalConfig, picoclawDir string, username string) error {
  configPath := filepath.Join(picoclawDir, "config.json")

  existing := make(map[string]interface{})
  if data, err := os.ReadFile(configPath); err == nil {
    if err := json.Unmarshal(data, &existing); err != nil {
      return fmt.Errorf("config.json 格式错误，拒绝覆盖: %w", err)
    }
  }

  var globalPico map[string]interface{}
  if m, ok := cfg.GetPicoConfig().(map[string]interface{}); ok {
    globalPico = util.DeepCopyMap(m)
  } else {
    globalPico = make(map[string]interface{})
  }

  merged := util.MergeMap(existing, globalPico)

  // 注入 browser MCP 配置
  mcpToken, _ := auth.GetMCPToken(username)
  if mcpToken != "" {
    injectMCPConfig(merged, mcpToken, cfg)
  }

  jsonData, err := json.MarshalIndent(merged, "", "  ")
  if err != nil {
    return fmt.Errorf("格式化 config.json 失败: %w", err)
  }

  return os.WriteFile(configPath, jsonData, 0644)
}

// injectMCPConfig 向 config.json 注入 browser MCP server 配置（SSE 直连 Go relay）
func injectMCPConfig(config map[string]interface{}, mcpToken string, cfg *config.GlobalConfig) {
  tools, _ := config["tools"].(map[string]interface{})
  if tools == nil {
    tools = make(map[string]interface{})
    config["tools"] = tools
  }
  mcp, _ := tools["mcp"].(map[string]interface{})
  if mcp == nil {
    mcp = make(map[string]interface{})
    tools["mcp"] = mcp
  }
  servers, _ := mcp["servers"].(map[string]interface{})
  if servers == nil {
    servers = make(map[string]interface{})
    mcp["servers"] = servers
  }

  listenAddr := cfg.Web.Listen
  host := "100.64.0.1"
  port := "80"
  if parts := strings.SplitN(listenAddr, ":", 2); len(parts) == 2 {
    if parts[0] != "" && parts[0] != ":" {
      host = parts[0]
    }
    if parts[1] != "" {
      port = parts[1]
    }
  }

  servers["browser"] = map[string]interface{}{
    "enabled":   true,
    "url":       fmt.Sprintf("http://%s:%s/api/browser/mcp/sse?token=%s", host, port, mcpToken),
    "transport": "sse",
  }

  // 清理旧配置
  delete(servers, "chrome-devtools")
}

// ApplySecurityToYAML 将全局安全配置合并到用户的 .security.yml
func ApplySecurityToYAML(cfg *config.GlobalConfig, picoclawDir string) error {
  securityPath := filepath.Join(picoclawDir, ".security.yml")

  existing := make(map[string]interface{})
  if data, err := os.ReadFile(securityPath); err == nil {
    if err := yaml.Unmarshal(data, &existing); err != nil {
      return fmt.Errorf(".security.yml 格式错误，拒绝覆盖: %w", err)
    }
  }

  var globalSec map[string]interface{}
  if m, ok := cfg.GetSecurityConfig().(map[string]interface{}); ok {
    globalSec = util.DeepCopyMap(m)
  } else {
    globalSec = make(map[string]interface{})
  }

  merged := util.MergeMap(existing, globalSec)

  data, err := yaml.Marshal(merged)
  if err != nil {
    return fmt.Errorf("序列化 .security.yml 失败: %w", err)
  }

  return os.WriteFile(securityPath, data, 0600)
}

// GetDingTalkConfig 获取用户的钉钉配置（clientID 和 clientSecret）
func GetDingTalkConfig(cfg *config.GlobalConfig, username string) (clientID, clientSecret string) {
  picoclawDir := filepath.Join(UserDir(cfg, username), ".picoclaw")

  configPath := filepath.Join(picoclawDir, "config.json")
  if data, err := os.ReadFile(configPath); err == nil {
    var m map[string]interface{}
    if json.Unmarshal(data, &m) == nil {
      if ch, ok := m["channel_list"].(map[string]interface{}); ok {
        if dt, ok := ch["dingtalk"].(map[string]interface{}); ok {
          if settings, ok := dt["settings"].(map[string]interface{}); ok {
            if v, ok := settings["client_id"].(string); ok {
              clientID = v
            }
          }
        }
      }
    }
  }

  securityPath := filepath.Join(picoclawDir, ".security.yml")
  if data, err := os.ReadFile(securityPath); err == nil {
    var m map[string]interface{}
    if yaml.Unmarshal(data, &m) == nil {
      if ch, ok := m["channel_list"].(map[string]interface{}); ok {
        if dt, ok := ch["dingtalk"].(map[string]interface{}); ok {
          if settings, ok := dt["settings"].(map[string]interface{}); ok {
            if v, ok := settings["client_secret"].(string); ok {
              clientSecret = v
            }
          }
        }
      }
    }
  }

  return
}

// SaveDingTalkConfig 保存用户的钉钉配置
func SaveDingTalkConfig(cfg *config.GlobalConfig, username, clientID, clientSecret string) error {
  picoclawDir := filepath.Join(UserDir(cfg, username), ".picoclaw")
  os.MkdirAll(picoclawDir, 0755)

  // config.json — 不存在则创建空结构
  configPath := filepath.Join(picoclawDir, "config.json")
  configData, err := os.ReadFile(configPath)
  if err != nil {
    configData = []byte("{}")
  }
  var configMap map[string]interface{}
  if err := json.Unmarshal(configData, &configMap); err != nil {
    configMap = make(map[string]interface{})
  }

  channelList, _ := configMap["channel_list"].(map[string]interface{})
  if channelList == nil {
    channelList = make(map[string]interface{})
    configMap["channel_list"] = channelList
  }
  dingtalk, _ := channelList["dingtalk"].(map[string]interface{})
  if dingtalk == nil {
    dingtalk = make(map[string]interface{})
    channelList["dingtalk"] = dingtalk
  }
  settings, _ := dingtalk["settings"].(map[string]interface{})
  if settings == nil {
    settings = make(map[string]interface{})
    dingtalk["settings"] = settings
  }
  settings["client_id"] = clientID
  dingtalk["enabled"] = true

  configJSON, err := json.MarshalIndent(configMap, "", "  ")
  if err != nil {
    return fmt.Errorf("序列化 config.json 失败: %w", err)
  }
  if err := os.WriteFile(configPath, configJSON, 0644); err != nil {
    return fmt.Errorf("写入 config.json 失败: %w", err)
  }

  // .security.yml — 不存在则创建空结构
  securityPath := filepath.Join(picoclawDir, ".security.yml")
  securityData, err := os.ReadFile(securityPath)
  if err != nil {
    securityData = []byte("{}")
  }
  var secMap map[string]interface{}
  if err := yaml.Unmarshal(securityData, &secMap); err != nil {
    secMap = make(map[string]interface{})
  }

  secChannelList, _ := secMap["channel_list"].(map[string]interface{})
  if secChannelList == nil {
    secChannelList = make(map[string]interface{})
    secMap["channel_list"] = secChannelList
  }
  secDingtalk, _ := secChannelList["dingtalk"].(map[string]interface{})
  if secDingtalk == nil {
    secDingtalk = make(map[string]interface{})
    secChannelList["dingtalk"] = secDingtalk
  }
  secSettings, _ := secDingtalk["settings"].(map[string]interface{})
  if secSettings == nil {
    secSettings = make(map[string]interface{})
    secDingtalk["settings"] = secSettings
  }
  secSettings["client_secret"] = clientSecret

  securityYAML, err := yaml.Marshal(secMap)
  if err != nil {
    return fmt.Errorf("序列化 .security.yml 失败: %w", err)
  }
  if err := os.WriteFile(securityPath, securityYAML, 0600); err != nil {
    return fmt.Errorf("写入 .security.yml 失败: %w", err)
  }

  return nil
}

// ============================================================
// 旧版目录迁移
// ============================================================

// migrateLegacyRootDir 将旧版 users/xxx/root/ 的内容提升到 users/xxx/
func migrateLegacyRootDir(userDir string) {
  rootDir := filepath.Join(userDir, "root")
  info, err := os.Stat(rootDir)
  if err != nil || !info.IsDir() {
    return
  }

  entries, err := os.ReadDir(rootDir)
  if err != nil {
    return
  }

  for _, e := range entries {
    src := filepath.Join(rootDir, e.Name())
    dst := filepath.Join(userDir, e.Name())
    if _, err := os.Stat(dst); err == nil {
      continue
    }
    os.Rename(src, dst)
  }

  if remaining, _ := os.ReadDir(rootDir); len(remaining) == 0 {
    os.Remove(rootDir)
  }
}
