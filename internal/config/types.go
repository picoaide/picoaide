package config

import (
  "os"
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
  AuthMode         string // "ldap" | "oidc" | "local"
  LogRetention     string // "1m","3m","6m","1y","3y","5y","forever"
  LogLevel         string // "debug","info","warn","error"
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
  Repos   []SkillRepo           `json:"-"`
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
