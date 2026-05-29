package config

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

type TLSConfig struct {
  Enabled bool
  CertPEM string // PEM 编码的证书内容
  KeyPEM  string // PEM 编码的私钥内容
}

type WebConfig struct {
  Listen       string
  LDAPEnabled  *bool
  AuthMode     string // "ldap" | "oidc" | "local"
  LogRetention string // "1m","3m","6m","1y","3y","5y","forever"
  LogLevel     string // "debug","info","warn","error"
  DebugMode    bool   // 调试模式：记录所有操作到 debug.log
  TLS          TLSConfig
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
  Repos   []SkillRepo           `json:"-"`
  Sources []SkillsSourceWrapper `json:"sources"`
}

type TimeoutConfig struct {
  CronJob string // cron 任务超时，如 "60m", "30m"，空值则用默认 60 分钟
}

type GlobalConfig struct {
  LDAP        LDAPConfig
  OIDC        OIDCConfig
  UsersRoot   string
  ArchiveRoot string
  Web         WebConfig
  Security    interface{}
  Skills      SkillsConfig
  Timeout     TimeoutConfig
}
