package config

import (
  "bytes"
  "encoding/json"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "strconv"
  "strings"
  "text/template"
  "time"

  "github.com/picoaide/picoaide/internal/auth"
)

// ============================================================
// 常量和模板
// ============================================================

const AppName = "picoaide"

// DefaultWorkDir 默认工作目录
var DefaultWorkDir = "/data/picoaide"

// WorkDir 返回工作目录
func WorkDir() string {
  return DefaultWorkDir
}

var Version = "dev"

const SessionMaxAge = 86400 // 24 hours

// ============================================================
// 配置结构体
// ============================================================

type LDAPConfig struct {
  Host                 string
  BindDN               string
  BindPassword         string
  BaseDN               string
  Filter               string
  UsernameAttribute    string
  GroupSearchMode      string // "member_of" | "group_search"
  GroupBaseDN          string
  GroupFilter          string
  GroupMemberAttribute string
  WhitelistEnabled     bool
  SyncInterval         string // "0" 禁用, "1h", "24h", "30m" 等
}

type OIDCConfig struct {
  IssuerURL        string
  ClientID         string
  ClientSecret     string
  RedirectURL      string
  Scopes           string
  UsernameClaim    string
  GroupsClaim      string
  WhitelistEnabled bool
  SyncInterval     string
}

type ImageConfig struct {
  Name     string
  Tag      string
  Timezone string
  Registry string // "github" | "tencent"
}

// IsTencent 是否使用腾讯云镜像仓库
func (i ImageConfig) IsTencent() bool {
  return i.Registry == "tencent"
}

// IsDev 是否为开发模式（通过环境变量 PICOAIDE_DEV=1 启用）
func (i ImageConfig) IsDev() bool {
  return os.Getenv("PICOAIDE_DEV") == "1"
}

// RepoName 返回镜像仓库名
func (i ImageConfig) RepoName() string {
  if i.IsDev() {
    return "picoaide/picoaide-dev"
  }
  return "picoaide/picoaide"
}

// PullRef 根据配置返回实际拉取地址
func (i ImageConfig) PullRef(tag string) string {
  repo := i.RepoName()
  if i.IsTencent() {
    return "hkccr.ccs.tencentyun.com/" + repo + ":" + tag
  }
  return "ghcr.io/" + repo + ":" + tag
}

// UnifiedRef 返回统一名称
func (i ImageConfig) UnifiedRef(tag string) string {
  return "ghcr.io/" + i.RepoName() + ":" + tag
}

type TLSConfig struct {
  Enabled bool
  CertPEM string // PEM 编码的证书内容
  KeyPEM  string // PEM 编码的私钥内容
}

type WebConfig struct {
  Listen           string
  ContainerBaseURL string
  LDAPEnabled      *bool
  AuthMode         string    // "ldap" | "oidc" | "local"
  LogRetention     string    // "1m","3m","6m","1y","3y","5y","forever"
  LogLevel         string    // "debug","info","warn","error"
  TLS              TLSConfig
}

type SkillRepoCredential struct {
  Name     string `json:"name"`
  Provider string `json:"provider"`
  Mode     string `json:"mode"` // "ssh" | "http" | "https"
  Username string `json:"username"`
  Secret   string `json:"secret"`
}

type SkillRepo struct {
  Name        string                `json:"name"`
  URL         string                `json:"url"`
  Ref         string                `json:"ref"`
  RefType     string                `json:"ref_type"` // "branch" | "tag"
  Public      bool                  `json:"public"`
  Credentials []SkillRepoCredential `json:"credentials"`
  LastPull    string                `json:"last_pull"`
}

// RegistrySource 注册源（如 SkillHub）
type RegistrySource struct {
  Name                string `json:"name"`
  DisplayName         string `json:"display_name"`
  IndexURL            string `json:"index_url"`
  SearchURL           string `json:"search_url,omitempty"`
  DownloadURLTemplate string `json:"download_url_template,omitempty"`
  PrimaryDownloadURL  string `json:"primary_download_url,omitempty"`
  AuthHeader          string `json:"-"`
  Enabled             bool   `json:"enabled"`
  LastRefresh         string `json:"last_refresh"`
}

// GitSource 以 Git 仓库为后端的技能源
type GitSource struct {
  Name        string                `json:"name"`
  URL         string                `json:"url"`
  Ref         string                `json:"ref,omitempty"`
  RefType     string                `json:"ref_type,omitempty"` // "branch" | "tag"
  Credentials []SkillRepoCredential `json:"credentials,omitempty"`
  Enabled     bool                  `json:"enabled"`
  LastPull    string                `json:"last_pull"`
}

// SkillsSourceWrapper 用于 JSON 序列化分派
type SkillsSourceWrapper struct {
  Type string          `json:"type"`
  Name string          `json:"name"`
  Git  *GitSource      `json:",inline"`
  Reg  *RegistrySource `json:",inline"`
}

type SkillsConfig struct {
  Repos   []SkillRepo          `json:"-"`
  Sources []SkillsSourceWrapper `json:"sources"`
}

type GlobalConfig struct {
  LDAP                         LDAPConfig
  OIDC                         OIDCConfig
  Image                        ImageConfig
  UsersRoot                    string
  ArchiveRoot                  string
  PicoClawAdapterRemoteBaseURL string
  Web                          WebConfig
  PicoClaw                     interface{}
  Security                     interface{}
  Skills                       SkillsConfig
}

func (cfg *GlobalConfig) GetPicoConfig() interface{} {
  return cfg.PicoClaw
}

func (cfg *GlobalConfig) GetSecurityConfig() interface{} {
  return cfg.Security
}

// LDAPEnabled 返回是否启用 LDAP（默认启用，只有明确设为 false 才禁用）
func (cfg *GlobalConfig) LDAPEnabled() bool {
  if cfg.Web.LDAPEnabled == nil {
    return true
  }
  return *cfg.Web.LDAPEnabled
}

// UnifiedAuthEnabled 返回是否启用了统一认证（LDAP/OIDC/其他外部认证）
// 统一认证模式下：用户来自外部系统，禁止手动创建和修改密码
// 本地模式下：用户由管理员创建，可修改密码，白名单无意义
func (cfg *GlobalConfig) UnifiedAuthEnabled() bool {
  if cfg.Web.AuthMode != "" {
    return cfg.Web.AuthMode != "local"
  }
  // 向后兼容：未设置 auth_mode 时，根据 ldap_enabled 推断
  return cfg.LDAPEnabled()
}

// AuthMode 返回当前认证模式字符串
func (cfg *GlobalConfig) AuthMode() string {
  if cfg.Web.AuthMode != "" {
    return cfg.Web.AuthMode
  }
  if cfg.LDAPEnabled() {
    return "ldap"
  }
  return "local"
}

func (cfg *GlobalConfig) ActiveAuthProvider() string {
  return cfg.AuthMode()
}

func (cfg *GlobalConfig) WhitelistEnabledForProvider(provider string) bool {
  switch provider {
  case "ldap":
    return cfg.LDAP.WhitelistEnabled
  case "oidc":
    return cfg.OIDC.WhitelistEnabled
  default:
    return false
  }
}

func (cfg *GlobalConfig) WhitelistEnabled() bool {
  return cfg.WhitelistEnabledForProvider(cfg.AuthMode())
}

// SyncIntervalDuration 解析同步间隔配置，返回 time.Duration，0 表示禁用
func (cfg *GlobalConfig) SyncIntervalDuration() time.Duration {
  if cfg.AuthMode() == "oidc" {
    if cfg.OIDC.SyncInterval == "" || cfg.OIDC.SyncInterval == "0" {
      return 0
    }
    if d, err := strconv.ParseInt(cfg.OIDC.SyncInterval, 10, 64); err == nil {
      return time.Duration(d) * time.Hour
    }
    d, err := time.ParseDuration(cfg.OIDC.SyncInterval)
    if err != nil {
      return 0
    }
    return d
  }
  if cfg.LDAP.SyncInterval == "" || cfg.LDAP.SyncInterval == "0" {
    return 0
  }
  // 纯数字默认为小时
  if d, err := strconv.ParseInt(cfg.LDAP.SyncInterval, 10, 64); err == nil {
    return time.Duration(d) * time.Hour
  }
  d, err := time.ParseDuration(cfg.LDAP.SyncInterval)
  if err != nil {
    return 0
  }
  return d
}

// SkillsDirPath 返回技能目录路径
func SkillsDirPath() string {
  return filepath.Join(WorkDir(), "skills")
}

func RuleCacheDir() string {
  if dir := strings.TrimSpace(os.Getenv("PICOAIDE_RULE_CACHE_DIR")); dir != "" {
    return dir
  }
  return filepath.Join(WorkDir(), "rules")
}

func PicoClawAdapterRemoteBaseURL() string {
  urls := PicoClawAdapterRemoteBaseURLs()
  if len(urls) > 0 {
    return urls[0]
  }
  return ""
}

// PicoClawAdapterRemoteBaseURLs 返回多个适配器远程 URL（支持逗号分隔的多个回退地址）
// 优先级：数据库配置 > PICOAIDE_PICOCLAW_ADAPTER_URLS 环境变量 > PICOAIDE_PICOCLAW_ADAPTER_URL 环境变量 > 默认值
func PicoClawAdapterRemoteBaseURLs() []string {
  // 1. 数据库配置（逗号分隔）
  if cfg, err := LoadFromDB(); err == nil && cfg != nil && cfg.PicoClawAdapterRemoteBaseURL != "" {
    if urls := parseAdapterURLs(cfg.PicoClawAdapterRemoteBaseURL); len(urls) > 0 {
      return urls
    }
  }
  // 2. 环境变量（逗号分隔）
  if value := os.Getenv("PICOAIDE_PICOCLAW_ADAPTER_URLS"); value != "" {
    if urls := parseAdapterURLs(value); len(urls) > 0 {
      return urls
    }
  }
  if value := os.Getenv("PICOAIDE_PICOCLAW_ADAPTER_URL"); value != "" {
    if urls := parseAdapterURLs(value); len(urls) > 0 {
      return urls
    }
  }
  // 3. 默认
  return []string{"https://www.picoaide.com/rules/picoclaw"}
}

func parseAdapterURLs(s string) []string {
  parts := strings.Split(s, ",")
  var urls []string
  for _, p := range parts {
    p = strings.TrimSpace(p)
    if p != "" {
      urls = append(urls, strings.TrimRight(p, "/"))
    }
  }
  return urls
}

// ============================================================
// systemd 服务文件管理
// ============================================================

// SystemServiceTemplate systemd 服务文件模板
const SystemServiceTemplate = `[Unit]
Description=PicoAide Management API Server
After=network.target docker.service

[Service]
Type=simple
User=root
ExecStart=/usr/sbin/picoaide serve
WorkingDirectory={{.WorkingDir}}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

// ServiceTemplateData 服务模板数据
type ServiceTemplateData struct {
  WorkingDir string
}

const serviceFilePath = "/etc/systemd/system/picoaide.service"

// InstallService 生成并安装 systemd 服务文件
func InstallService(cfg *GlobalConfig) error {
  workDir, _ := os.Getwd()
  if workDir == "" {
    workDir = "/data/picoaide"
  }

  data := ServiceTemplateData{
    WorkingDir: workDir,
  }

  tmpl, err := template.New("service").Parse(SystemServiceTemplate)
  if err != nil {
    return fmt.Errorf("解析服务模板失败: %w", err)
  }

  var buf bytes.Buffer
  if err := tmpl.Execute(&buf, data); err != nil {
    return fmt.Errorf("生成服务文件失败: %w", err)
  }
  newContent := buf.Bytes()

  if existing, err := os.ReadFile(serviceFilePath); err == nil && bytes.Equal(existing, newContent) {
    fmt.Println("服务文件已存在且一致，跳过")
    return nil
  }

  if err := os.WriteFile(serviceFilePath, newContent, 0644); err != nil {
    return fmt.Errorf("写入服务文件失败: %w", err)
  }
  fmt.Println("服务文件已写入:", serviceFilePath)

  // daemon-reload
  if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
    return fmt.Errorf("daemon-reload 失败: %s: %w", strings.TrimSpace(string(out)), err)
  }

  // enable
  if out, err := exec.Command("systemctl", "enable", AppName).CombinedOutput(); err != nil {
    return fmt.Errorf("enable 失败: %s: %w", strings.TrimSpace(string(out)), err)
  }
  fmt.Println("已设置开机自启")

  // start or restart
  action := "start"
  if _, err := exec.Command("systemctl", "is-active", "--quiet", AppName).CombinedOutput(); err == nil {
    action = "restart"
  }
  if out, err := exec.Command("systemctl", action, AppName).CombinedOutput(); err != nil {
    return fmt.Errorf("%s 失败: %s: %w", action, strings.TrimSpace(string(out)), err)
  }
  actionLabel := "启动"
  if action == "restart" {
    actionLabel = "重启"
  }
  fmt.Printf("已%s服务\n", actionLabel)

  return nil
}

// ============================================================
// 数据库配置管理
// ============================================================

// SettingsCount 返回 settings 表中的配置项数量
func SettingsCount() (int, error) {
  engine, err := auth.GetEngine()
  if err != nil {
    return 0, fmt.Errorf("获取数据库引擎失败: %w", err)
  }
  count, err := engine.Count(&auth.Setting{})
  if err != nil {
    return 0, fmt.Errorf("查询配置数量失败: %w", err)
  }
  return int(count), nil
}

// LoadFromDB 从数据库加载全局配置
func LoadFromDB() (*GlobalConfig, error) {
  engine, err := auth.GetEngine()
  if err != nil {
    return nil, fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  var settings []auth.Setting
  if err := engine.Find(&settings); err != nil {
    return nil, fmt.Errorf("查询配置失败: %w", err)
  }

  // 读取所有键值对
  kv := make(map[string]string)
  for _, s := range settings {
    if isInternalSettingKey(s.Key) || s.Key == "web.password" {
      continue
    }
    kv[s.Key] = s.Value
  }

  cfg := &GlobalConfig{}

  // 简单字段直接赋值
  cfg.LDAP.Host = kv["ldap.host"]
  cfg.LDAP.BindDN = kv["ldap.bind_dn"]
  cfg.LDAP.BindPassword = kv["ldap.bind_password"]
  cfg.LDAP.BaseDN = kv["ldap.base_dn"]
  cfg.LDAP.Filter = kv["ldap.filter"]
  cfg.LDAP.UsernameAttribute = kv["ldap.username_attribute"]
  cfg.LDAP.GroupSearchMode = kv["ldap.group_search_mode"]
  cfg.LDAP.GroupBaseDN = kv["ldap.group_base_dn"]
  cfg.LDAP.GroupFilter = kv["ldap.group_filter"]
  cfg.LDAP.GroupMemberAttribute = kv["ldap.group_member_attribute"]
  cfg.LDAP.SyncInterval = kv["ldap.sync_interval"]
  cfg.OIDC.IssuerURL = kv["oidc.issuer_url"]
  cfg.OIDC.ClientID = kv["oidc.client_id"]
  cfg.OIDC.ClientSecret = kv["oidc.client_secret"]
  cfg.OIDC.RedirectURL = kv["oidc.redirect_url"]
  cfg.OIDC.Scopes = kv["oidc.scopes"]
  cfg.OIDC.UsernameClaim = kv["oidc.username_claim"]
  cfg.OIDC.GroupsClaim = kv["oidc.groups_claim"]
  cfg.OIDC.SyncInterval = kv["oidc.sync_interval"]
  cfg.Image.Name = kv["image.name"]
  cfg.Image.Tag = kv["image.tag"]
  cfg.Image.Timezone = kv["image.timezone"]
  cfg.Image.Registry = kv["image.registry"]
  cfg.UsersRoot = kv["users_root"]
  cfg.ArchiveRoot = kv["archive_root"]
  cfg.PicoClawAdapterRemoteBaseURL = kv["picoclaw_adapter_remote_base_url"]
  cfg.Web.Listen = kv["web.listen"]
  cfg.Web.AuthMode = kv["web.auth_mode"]
  cfg.Web.LogRetention = kv["web.log_retention"]
  cfg.Web.LogLevel = kv["web.log_level"]

  // web.ldap_enabled 需要解析为 bool 指针
  if v, ok := kv["web.ldap_enabled"]; ok && v != "" {
    b, err := strconv.ParseBool(v)
    if err == nil {
      cfg.Web.LDAPEnabled = &b
    }
  }
  cfg.LDAP.WhitelistEnabled, _ = strconv.ParseBool(kv["ldap.whitelist_enabled"])
  cfg.OIDC.WhitelistEnabled, _ = strconv.ParseBool(kv["oidc.whitelist_enabled"])

  // TLS 配置
  cfg.Web.TLS.Enabled, _ = strconv.ParseBool(kv["web.tls.enabled"])
  cfg.Web.TLS.CertPEM = kv["web.tls.cert_pem"]
  cfg.Web.TLS.KeyPEM = kv["web.tls.key_pem"]

  // 结构化字段从 JSON 反序列化
  if v, ok := kv["picoclaw"]; ok && v != "" {
    var picoclaw interface{}
    if err := json.Unmarshal([]byte(v), &picoclaw); err == nil {
      cfg.PicoClaw = picoclaw
    }
  }
  if v, ok := kv["security"]; ok && v != "" {
    var security interface{}
    if err := json.Unmarshal([]byte(v), &security); err == nil {
      cfg.Security = security
    }
  }
  if v, ok := kv["skills"]; ok && v != "" {
    var skills SkillsConfig
    if err := json.Unmarshal([]byte(v), &skills); err == nil {
      cfg.Skills = skills
    }
  }
  if len(cfg.Skills.Sources) == 0 {
    cfg.Skills.Sources = DefaultGlobalConfig().Skills.Sources
  }

  return cfg, nil
}

func configToKV(cfg *GlobalConfig) (map[string]string, error) {
  kv := make(map[string]string)
  kv["ldap.host"] = cfg.LDAP.Host
  kv["ldap.bind_dn"] = cfg.LDAP.BindDN
  kv["ldap.bind_password"] = cfg.LDAP.BindPassword
  kv["ldap.base_dn"] = cfg.LDAP.BaseDN
  kv["ldap.filter"] = cfg.LDAP.Filter
  kv["ldap.username_attribute"] = cfg.LDAP.UsernameAttribute
  kv["ldap.group_search_mode"] = cfg.LDAP.GroupSearchMode
  kv["ldap.group_base_dn"] = cfg.LDAP.GroupBaseDN
  kv["ldap.group_filter"] = cfg.LDAP.GroupFilter
  kv["ldap.group_member_attribute"] = cfg.LDAP.GroupMemberAttribute
  kv["ldap.whitelist_enabled"] = strconv.FormatBool(cfg.LDAP.WhitelistEnabled)
  kv["ldap.sync_interval"] = cfg.LDAP.SyncInterval
  kv["oidc.issuer_url"] = cfg.OIDC.IssuerURL
  kv["oidc.client_id"] = cfg.OIDC.ClientID
  kv["oidc.client_secret"] = cfg.OIDC.ClientSecret
  kv["oidc.redirect_url"] = cfg.OIDC.RedirectURL
  kv["oidc.scopes"] = cfg.OIDC.Scopes
  kv["oidc.username_claim"] = cfg.OIDC.UsernameClaim
  kv["oidc.groups_claim"] = cfg.OIDC.GroupsClaim
  kv["oidc.whitelist_enabled"] = strconv.FormatBool(cfg.OIDC.WhitelistEnabled)
  kv["oidc.sync_interval"] = cfg.OIDC.SyncInterval
  kv["image.name"] = cfg.Image.Name
  kv["image.tag"] = cfg.Image.Tag
  kv["image.timezone"] = cfg.Image.Timezone
  kv["image.registry"] = cfg.Image.Registry
  kv["users_root"] = cfg.UsersRoot
  kv["archive_root"] = cfg.ArchiveRoot
  kv["web.listen"] = cfg.Web.Listen
  kv["web.auth_mode"] = cfg.Web.AuthMode
  kv["web.log_retention"] = cfg.Web.LogRetention
  kv["web.log_level"] = cfg.Web.LogLevel

  if cfg.Web.LDAPEnabled != nil {
    kv["web.ldap_enabled"] = strconv.FormatBool(*cfg.Web.LDAPEnabled)
  }

  // TLS 配置
  kv["web.tls.enabled"] = strconv.FormatBool(cfg.Web.TLS.Enabled)
  kv["web.tls.cert_pem"] = cfg.Web.TLS.CertPEM
  kv["web.tls.key_pem"] = cfg.Web.TLS.KeyPEM

  // 结构化字段序列化为 JSON
  if cfg.PicoClaw != nil {
    b, err := json.Marshal(cfg.PicoClaw)
    if err != nil {
      return nil, fmt.Errorf("序列化 picoclaw 配置失败: %w", err)
    }
    kv["picoclaw"] = string(b)
  }
  if cfg.Security != nil {
    b, err := json.Marshal(cfg.Security)
    if err != nil {
      return nil, fmt.Errorf("序列化 security 配置失败: %w", err)
    }
    kv["security"] = string(b)
  }
  // skills 即使为空值也需要保存（保留默认结构）
  {
    b, err := json.Marshal(cfg.Skills)
    if err != nil {
      return nil, fmt.Errorf("序列化 skills 配置失败: %w", err)
    }
    kv["skills"] = string(b)
  }
  return kv, nil
}

// SaveToDB 将全局配置保存到数据库
func SaveToDB(cfg *GlobalConfig, changedBy string) error {
  engine, err := auth.GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  kv, err := configToKV(cfg)
  if err != nil {
    return err
  }

  // 事务写入
  session := engine.NewSession()
  defer session.Close()

  if err := session.Begin(); err != nil {
    return fmt.Errorf("开启事务失败: %w", err)
  }

  for key, newValue := range kv {
    // 查询当前值
    var existing auth.Setting
    has, err := session.Where("key = ?", key).Get(&existing)
    if err != nil {
      return fmt.Errorf("查询配置失败: %w", err)
    }

    // 值相同则跳过
    if has && existing.Value == newValue {
      continue
    }

    // 记录变更历史
    if has {
      history := &auth.SettingsHistory{
        Key:       key,
        OldValue:  existing.Value,
        NewValue:  newValue,
        ChangedBy: changedBy,
      }
      if _, err := session.Insert(history); err != nil {
        return fmt.Errorf("写入配置历史失败: %w", err)
      }
    }

    // 写入新值（INSERT OR REPLACE）
    if _, err := session.Exec(
      "INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now','localtime'))",
      key, newValue,
    ); err != nil {
      return fmt.Errorf("写入配置失败: %w", err)
    }
  }

  if _, err := session.Exec("DELETE FROM settings WHERE key = ?", "web.password"); err != nil {
    return fmt.Errorf("删除废弃配置失败: %w", err)
  }

  return session.Commit()
}

// LoadRawFromDB 从数据库加载配置并返回嵌套 JSON 结构（与 LoadRaw 返回格式一致）
func LoadRawFromDB() (map[string]interface{}, error) {
  engine, err := auth.GetEngine()
  if err != nil {
    return nil, fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  var settings []auth.Setting
  if err := engine.Find(&settings); err != nil {
    return nil, fmt.Errorf("查询配置失败: %w", err)
  }

  kv := make(map[string]string)
  for _, s := range settings {
    if isInternalSettingKey(s.Key) || s.Key == "web.password" {
      continue
    }
    kv[s.Key] = s.Value
  }

  return buildNested(kv), nil
}

// SaveRawToDB 将嵌套 JSON 配置保存到数据库
func SaveRawToDB(data map[string]interface{}, changedBy string) error {
  removeFixedConfigFields(data)
  flat := flattenConfig(data)

  engine, err := auth.GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  session := engine.NewSession()
  defer session.Close()

  if err := session.Begin(); err != nil {
    return fmt.Errorf("开启事务失败: %w", err)
  }

  for key, newValue := range flat {
    if isInternalSettingKey(key) || key == "web.password" {
      continue
    }
    // 查询当前值
    var existing auth.Setting
    has, err := session.Where("key = ?", key).Get(&existing)
    if err != nil {
      return fmt.Errorf("查询配置失败: %w", err)
    }

    // 值相同则跳过
    if has && existing.Value == newValue {
      continue
    }

    // 记录变更历史
    if has {
      history := &auth.SettingsHistory{
        Key:       key,
        OldValue:  existing.Value,
        NewValue:  newValue,
        ChangedBy: changedBy,
      }
      if _, err := session.Insert(history); err != nil {
        return fmt.Errorf("写入配置历史失败: %w", err)
      }
    }

    // 写入新值（INSERT OR REPLACE）
    if _, err := session.Exec(
      "INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now','localtime'))",
      key, newValue,
    ); err != nil {
      return fmt.Errorf("写入配置失败: %w", err)
    }
  }

  if _, err := session.Exec("DELETE FROM settings WHERE key = ?", "web.password"); err != nil {
    return fmt.Errorf("删除废弃配置失败: %w", err)
  }

  return session.Commit()
}

func removeFixedConfigFields(data map[string]interface{}) {
  delete(data, "internal")
  web, ok := data["web"].(map[string]interface{})
  if !ok {
    return
  }
  delete(web, "container_base_url")
  delete(web, "password")
}

func isInternalSettingKey(key string) bool {
  return key == "internal" || strings.HasPrefix(key, "internal.")
}

func DefaultGlobalConfig() *GlobalConfig {
  return &GlobalConfig{
    LDAP: LDAPConfig{
      Host:              "ldap://ldap.example.com:389",
      BindDN:            "cn=admin,dc=example,dc=com",
      BindPassword:      "your-password-here",
      BaseDN:            "ou=users,dc=example,dc=com",
      Filter:            "(objectClass=inetOrgPerson)",
      UsernameAttribute: "uid",
      SyncInterval:      "24h",
    },
    OIDC: OIDCConfig{
      Scopes:        "openid profile email",
      UsernameClaim: "preferred_username",
      GroupsClaim:   "groups",
      SyncInterval:  "0",
    },
    Image: ImageConfig{
      Name:     "ghcr.io/picoaide/picoaide",
      Timezone: "Asia/Shanghai",
      Registry: "github",
    },
    UsersRoot:   "./users",
    ArchiveRoot: "./archive",
    Web: WebConfig{
      Listen:       ":80",
      LogRetention: "6m",
    },
    PicoClaw: map[string]interface{}{
      "agents": map[string]interface{}{
        "defaults": map[string]interface{}{
          "model_name":          "gpt-5.4",
          "max_tokens":          32768,
          "max_tool_iterations": 50,
        },
      },
      "model_list": []interface{}{
        map[string]interface{}{
          "model_name":      "gpt-5.4",
          "model":           "openai/gpt-5.4",
          "api_base":        "https://api.openai.com/v1",
          "request_timeout": 6000,
        },
      },
      "channel_list": map[string]interface{}{
        "dingtalk": map[string]interface{}{
          "enabled": false,
          "type":    "dingtalk",
        },
        "feishu": map[string]interface{}{
          "enabled": false,
          "type":    "feishu",
        },
      },
      "tools": map[string]interface{}{
        "web": map[string]interface{}{
          "duckduckgo": map[string]interface{}{
            "enabled": true,
          },
        },
        "mcp": map[string]interface{}{
          "enabled":               true,
          "max_inline_text_chars": 8192,
          "servers": map[string]interface{}{
            "browser": map[string]interface{}{
              "enabled": false,
            },
          },
        },
      },
      "gateway": map[string]interface{}{
        "host": "0.0.0.0",
        "port": 18790,
      },
    },
    Security: map[string]interface{}{
      "model_list": map[string]interface{}{
        "gpt-5.4:0": map[string]interface{}{
          "api_keys": []interface{}{"sk-openai-replace-me"},
        },
      },
    },
    Skills: SkillsConfig{
      Repos: []SkillRepo{},
      Sources: []SkillsSourceWrapper{
        {
          Type: "registry",
          Name: "skillhub.cn",
          Reg: &RegistrySource{
            Name:                "skillhub.cn",
            DisplayName:         "SkillHub 中文技能市场",
            IndexURL:            "https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/skills.json",
            SearchURL:           "https://lightmake.site/api/v1/search",
            PrimaryDownloadURL:  "https://lightmake.site/api/v1/download?slug={slug}",
            DownloadURLTemplate: "https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/skills/{slug}.zip",
            Enabled:             true,
          },
        },
      },
    },
  }
}

// InitDBDefaults 将默认配置写入数据库（不覆盖已有值）
func InitDBDefaults() error {
  cfg := DefaultGlobalConfig()

  engine, err := auth.GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  session := engine.NewSession()
  defer session.Close()

  if err := session.Begin(); err != nil {
    return fmt.Errorf("开启事务失败: %w", err)
  }

  kv, err := configToKV(cfg)
  if err != nil {
    return err
  }
  for key, value := range kv {
    if _, err := session.Exec(
      "INSERT OR IGNORE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now','localtime'))",
      key, value,
    ); err != nil {
      return fmt.Errorf("写入默认配置失败: %w", err)
    }
  }

  return session.Commit()
}

// flattenConfig 将嵌套配置展平为点分隔的键值映射
// 规则：
//   - 字符串/数字/布尔值 → 直接存储为字符串
//   - 嵌套 map → 递归展平，键用点连接
//   - 切片/数组 → 序列化为 JSON 字符串，存储在父键下
//   - 特殊键 picoclaw、security、skills → 整体序列化为 JSON
func flattenConfig(data map[string]interface{}) map[string]string {
  result := make(map[string]string)
  flattenRecursive(data, "", result)
  return result
}

// flattenConfig 内部递归实现
func flattenRecursive(data map[string]interface{}, prefix string, result map[string]string) {
  // 需要整体存储为 JSON 的顶层键
  jsonBlobKeys := map[string]bool{
    "picoclaw": true,
    "security": true,
    "skills":   true,
  }

  for key, val := range data {
    fullKey := key
    if prefix != "" {
      fullKey = prefix + "." + key
    }

    // 顶层特殊键：整体序列化为 JSON
    if prefix == "" && jsonBlobKeys[key] {
      b, err := json.Marshal(val)
      if err == nil {
        result[key] = string(b)
      }
      continue
    }

    switch v := val.(type) {
    case map[string]interface{}:
      // 嵌套 map → 递归展平
      flattenRecursive(v, fullKey, result)
    case []interface{}:
      // 切片 → 序列化为 JSON
      b, err := json.Marshal(v)
      if err == nil {
        result[fullKey] = string(b)
      }
    case nil:
      result[fullKey] = ""
    default:
      // 字符串、数字、布尔值等 → 转为字符串
      result[fullKey] = fmt.Sprintf("%v", v)
    }
  }
}

// buildNested 将展平的键值映射重建为嵌套 JSON 结构
func buildNested(flat map[string]string) map[string]interface{} {
  // 需要从 JSON 反序列化的顶层键
  jsonBlobKeys := map[string]bool{
    "picoclaw": true,
    "security": true,
    "skills":   true,
  }

  // 需要作为 bool 返回的键
  boolKeys := map[string]bool{
    "web.ldap_enabled":       true,
    "web.tls.enabled":        true,
    "ldap.whitelist_enabled": true,
    "oidc.whitelist_enabled": true,
  }

  result := make(map[string]interface{})

  for key, value := range flat {
    // 特殊键直接从 JSON 解析
    if jsonBlobKeys[key] {
      var parsed interface{}
      if err := json.Unmarshal([]byte(value), &parsed); err == nil {
        result[key] = parsed
      }
      continue
    }

    // 类型转换
    var typedVal interface{} = value
    if boolKeys[key] {
      if b, err := strconv.ParseBool(value); err == nil {
        typedVal = b
      }
    } else if iv, err := strconv.ParseInt(value, 10, 64); err == nil && strconv.FormatInt(iv, 10) == value {
      typedVal = iv
    }

    // 按点分隔逐层构建嵌套 map
    parts := strings.Split(key, ".")
    current := result
    for i := 0; i < len(parts)-1; i++ {
      part := parts[i]
      if _, ok := current[part]; !ok {
        current[part] = make(map[string]interface{})
      }
      if m, ok := current[part].(map[string]interface{}); ok {
        current = m
      }
    }
    current[parts[len(parts)-1]] = typedVal
  }

  return result
}
