package user

import (
  "encoding/json"
  "fmt"
  "log/slog"
  "os"
  "path/filepath"
  "regexp"
  "sort"
  "time"

  "gopkg.in/yaml.v3"

  "github.com/picoaide/picoaide/internal/auth"
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
  engine, err := auth.GetEngine()
  if err != nil {
    return nil, nil
  }

  var entries []auth.WhitelistEntry
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

// DeployGroupSkillsToUser 部署用户所属组绑定的技能到用户目录
func DeployGroupSkillsToUser(cfg *config.GlobalConfig, username string) {
  groups, err := auth.GetGroupsForUser(username)
  if err != nil {
    return
  }
  skillsDir := config.SkillsDirPath()
  targetSkillsDir := filepath.Join(UserDir(cfg, username), ".picoclaw", "workspace", "skills")

  for _, groupName := range groups {
    skills, err := auth.GetGroupSkills(groupName)
    if err != nil {
      continue
    }
    for _, skillName := range skills {
      if err := util.SafePathSegment(skillName); err != nil {
        slog.Warn("跳过不合法技能名", "skill", skillName, "error", err)
        continue
      }
      srcPath := filepath.Join(skillsDir, skillName)
      dstPath := filepath.Join(targetSkillsDir, skillName)
      os.RemoveAll(dstPath)
      if err := util.CopyDir(srcPath, dstPath); err != nil {
        slog.Warn("部署技能到用户失败", "skill", skillName, "username", username, "error", err)
      }
    }
  }
}

// SyncCookies 将域名对应的 Cookie 字符串写入用户的 .security.yml
// 格式：cookies: { domain.com: "name1=val1; name2=val2" }
func SyncCookies(cfg *config.GlobalConfig, username, domain, cookieStr string) error {
  if err := ValidateUsername(username); err != nil {
    return err
  }
  picoclawDir := filepath.Join(UserDir(cfg, username), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    return fmt.Errorf("创建目录失败: %w", err)
  }

  securityPath := filepath.Join(picoclawDir, ".security.yml")

  secMap := make(map[string]interface{})
  if data, err := os.ReadFile(securityPath); err == nil {
    if err := yaml.Unmarshal(data, &secMap); err != nil {
      return fmt.Errorf(".security.yml 格式错误，拒绝覆盖: %w", err)
    }
  }

  cookiesMap, _ := secMap["cookies"].(map[string]interface{})
  if cookiesMap == nil {
    cookiesMap = make(map[string]interface{})
  }

  cookiesMap[domain] = cookieStr
  secMap["cookies"] = cookiesMap

  data, err := yaml.Marshal(secMap)
  if err != nil {
    return fmt.Errorf("序列化失败: %w", err)
  }

  return os.WriteFile(securityPath, data, 0600)
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

// ============================================================
// 钉钉配置
// ============================================================

// GetDingTalkConfig 获取用户的钉钉配置（clientID 和 clientSecret）
func GetDingTalkConfig(cfg *config.GlobalConfig, username string) (clientID, clientSecret string) {
  if err := ValidateUsername(username); err != nil {
    return "", ""
  }
  if values, err := GetPicoClawConfigFields(cfg, username, 0, "dingtalk"); err == nil && len(values) > 0 {
    for _, value := range values {
      switch value.Field.Key {
      case "client_id":
        if v, ok := value.Value.(string); ok {
          clientID = v
        }
      case "client_secret":
        if v, ok := value.Value.(string); ok {
          clientSecret = v
        }
      }
    }
    if clientID != "" || clientSecret != "" {
      return
    }
  }
  picoclawDir := filepath.Join(UserDir(cfg, username), ".picoclaw")

  configPath := filepath.Join(picoclawDir, "config.json")
  if data, err := os.ReadFile(configPath); err == nil {
    var m map[string]interface{}
    if json.Unmarshal(data, &m) == nil {
      if v, ok := getDingTalkField(m, "client_id"); ok {
        clientID = v
      }
    }
  }

  securityPath := filepath.Join(picoclawDir, ".security.yml")
  if data, err := os.ReadFile(securityPath); err == nil {
    var m map[string]interface{}
    if yaml.Unmarshal(data, &m) == nil {
      if v, ok := getDingTalkField(m, "client_secret"); ok {
        clientSecret = v
      }
    }
  }

  return
}

// SaveDingTalkConfig 保存用户的钉钉配置
func SaveDingTalkConfig(cfg *config.GlobalConfig, username, clientID, clientSecret string) error {
  if err := ValidateUsername(username); err != nil {
    return err
  }
  if err := SavePicoClawConfigFields(cfg, username, 0, map[string]interface{}{
    "enabled":       true,
    "client_id":     clientID,
    "client_secret": clientSecret,
  }); err == nil {
    return nil
  }
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
  if err := ensureSupportedConfigFileVersion(configMap); err != nil {
    return err
  }
  setDingTalkField(configMap, "client_id", clientID)

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
  setDingTalkFieldInBase(secMap, dingTalkBaseKey(configMap), "client_secret", clientSecret)

  securityYAML, err := yaml.Marshal(secMap)
  if err != nil {
    return fmt.Errorf("序列化 .security.yml 失败: %w", err)
  }
  if err := os.WriteFile(securityPath, securityYAML, 0600); err != nil {
    return fmt.Errorf("写入 .security.yml 失败: %w", err)
  }

  return nil
}

func ensureSupportedConfigFileVersion(configMap map[string]interface{}) error {
  version := configVersionFromMap(configMap)
  if version > PicoAideSupportedPicoClawConfigVersion {
    return fmt.Errorf("config.json 使用配置版本 %d，但当前 PicoAide 只支持到 %d，请先适配迁移规则", version, PicoAideSupportedPicoClawConfigVersion)
  }
  return nil
}

func configVersionFromMap(configMap map[string]interface{}) int {
  switch v := configMap["version"].(type) {
  case int:
    return v
  case int64:
    return int(v)
  case float64:
    return int(v)
  case json.Number:
    n, _ := v.Int64()
    return int(n)
  default:
    return 3
  }
}

func getDingTalkField(root map[string]interface{}, field string) (string, bool) {
  for _, baseKey := range []string{"channel_list", "channels"} {
    channels, ok := root[baseKey].(map[string]interface{})
    if !ok {
      continue
    }
    dingtalk, ok := channels["dingtalk"].(map[string]interface{})
    if !ok {
      continue
    }
    if settings, ok := dingtalk["settings"].(map[string]interface{}); ok {
      if v, ok := settings[field].(string); ok {
        return v, true
      }
    }
    if v, ok := dingtalk[field].(string); ok {
      return v, true
    }
  }
  return "", false
}

func setDingTalkField(root map[string]interface{}, field string, value string) {
  setDingTalkFieldInBase(root, dingTalkBaseKey(root), field, value)
}

func setDingTalkFieldInBase(root map[string]interface{}, baseKey string, field string, value string) {
  channels, _ := root[baseKey].(map[string]interface{})
  if channels == nil {
    channels = make(map[string]interface{})
    root[baseKey] = channels
  }
  dingtalk, _ := channels["dingtalk"].(map[string]interface{})
  if dingtalk == nil {
    dingtalk = make(map[string]interface{})
    channels["dingtalk"] = dingtalk
  }
  dingtalk["enabled"] = true
  if baseKey == "channel_list" {
    dingtalk["type"] = "dingtalk"
    settings, _ := dingtalk["settings"].(map[string]interface{})
    if settings == nil {
      settings = make(map[string]interface{})
      dingtalk["settings"] = settings
    }
    settings[field] = value
    return
  }
  dingtalk[field] = value
}

func dingTalkBaseKey(root map[string]interface{}) string {
  if _, ok := root["channel_list"].(map[string]interface{}); ok {
    return "channel_list"
  }
  if _, ok := root["channels"].(map[string]interface{}); ok {
    return "channels"
  }
  if configVersionFromMap(root) >= 3 {
    return "channel_list"
  }
  return "channels"
}
