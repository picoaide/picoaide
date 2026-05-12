package authsource

import (
  "context"
  "fmt"
  "sort"
  "sync"

  "github.com/picoaide/picoaide/internal/config"
)

// Identity 表示认证源返回的已认证用户身份
type Identity struct {
  Username string
  Groups   []string
}

// GroupNode 表示目录提供者返回的组节点信息
type GroupNode struct {
  Members   []string
  SubGroups []string
}

// GroupHierarchy 是组名到组信息的映射
type GroupHierarchy map[string]GroupNode

// ProviderMeta 描述一个认证源的能力，用于前端动态渲染登录页面
type ProviderMeta struct {
  Name         string `json:"name"`          // "local", "ldap", "oidc", 等
  DisplayName  string `json:"display_name"`  // 显示名，如 "LDAP", "企业微信"
  HasPassword  bool   `json:"has_password"`  // true → 显示用户名密码输入框
  HasBrowser   bool   `json:"has_browser"`   // true → 显示浏览器 SSO 按钮
  HasDirectory bool   `json:"has_directory"` // true → 支持用户/组目录同步
}

// Describable 可选接口：provider 可返回自己的显示名
type Describable interface {
  DisplayName() string
}

// PasswordProvider 支持用户名密码认证的认证源
type PasswordProvider interface {
  Authenticate(cfg *config.GlobalConfig, username, password string) bool
}

// BrowserProvider 支持浏览器跳转/回调认证的认证源
type BrowserProvider interface {
  AuthURL(cfg *config.GlobalConfig, state string) (string, error)
  CompleteLogin(ctx context.Context, cfg *config.GlobalConfig, code string) (*Identity, error)
}

// DirectoryProvider 支持用户/组目录枚举的认证源
type DirectoryProvider interface {
  FetchUsers(cfg *config.GlobalConfig) ([]string, error)
  FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error)
  FetchGroups(cfg *config.GlobalConfig) (GroupHierarchy, error)
}

var (
  providersMu sync.RWMutex
  providers   = map[string]any{}
)

func Register(name string, provider any) {
  providersMu.Lock()
  defer providersMu.Unlock()
  providers[name] = provider
}

func Provider(name string) (any, bool) {
  providersMu.RLock()
  defer providersMu.RUnlock()
  provider, ok := providers[name]
  return provider, ok
}

func passwordProvider(name string) (PasswordProvider, error) {
  provider, ok := Provider(name)
  if !ok {
    return nil, fmt.Errorf("认证源 %s 未注册", name)
  }
  typed, ok := provider.(PasswordProvider)
  if !ok {
    return nil, fmt.Errorf("认证源 %s 不支持用户名密码认证", name)
  }
  return typed, nil
}

func browserProvider(name string) (BrowserProvider, error) {
  provider, ok := Provider(name)
  if !ok {
    return nil, fmt.Errorf("认证源 %s 未注册", name)
  }
  typed, ok := provider.(BrowserProvider)
  if !ok {
    return nil, fmt.Errorf("认证源 %s 不支持浏览器认证", name)
  }
  return typed, nil
}

func directoryProvider(name string) (DirectoryProvider, error) {
  provider, ok := Provider(name)
  if !ok {
    return nil, fmt.Errorf("认证源 %s 未注册", name)
  }
  typed, ok := provider.(DirectoryProvider)
  if !ok {
    return nil, fmt.Errorf("认证源 %s 不支持目录同步", name)
  }
  return typed, nil
}

// ============================================================
// Provider 元信息
// ============================================================

// DescribeProvider 返回指定认证源的能力描述
func DescribeProvider(name string) ProviderMeta {
  providersMu.RLock()
  defer providersMu.RUnlock()
  p, ok := providers[name]
  if !ok {
    return ProviderMeta{Name: name}
  }
  meta := ProviderMeta{Name: name}
  _, meta.HasPassword = p.(PasswordProvider)
  _, meta.HasBrowser = p.(BrowserProvider)
  _, meta.HasDirectory = p.(DirectoryProvider)
  if d, ok := p.(Describable); ok {
    meta.DisplayName = d.DisplayName()
  } else {
    meta.DisplayName = name
  }
  return meta
}

// ActiveProviderMeta 返回当前认证源的元信息（前端 /api/login/mode 用）
func ActiveProviderMeta(cfg *config.GlobalConfig) ProviderMeta {
  return DescribeProvider(cfg.AuthMode())
}

// ============================================================
// 字段定义 — 用于前端动态渲染配置表单
// ============================================================

// FieldType 表示配置字段的 HTML 输入类型
type FieldType string

const (
  FieldText     FieldType = "text"
  FieldPassword FieldType = "password"
  FieldSelect   FieldType = "select"
)

// FieldOption 是 select 类型字段的可选项
type FieldOption struct {
  Value string `json:"value"`
  Label string `json:"label"`
}

// FieldDefinition 描述一个配置字段，用于前端动态渲染
type FieldDefinition struct {
  Key         string        `json:"key"`
  Label       string        `json:"label"`
  Type        FieldType     `json:"type"`
  Placeholder string        `json:"placeholder,omitempty"`
  Required    bool          `json:"required"`
  Default     string        `json:"default,omitempty"`
  Options     []FieldOption `json:"options,omitempty"`
}

// FieldSection 将相关字段分组到一个命名卡片下
type FieldSection struct {
  Name   string           `json:"name"`
  Fields []FieldDefinition `json:"fields"`
}

// ActionDefinition 描述一个提供者的自定义操作按钮
type ActionDefinition struct {
  ID      string `json:"id"`
  Label   string `json:"label"`
  Section string `json:"section"`
}

// Configurable 是可选的接口：provider 通过它声明自己的配置字段
type Configurable interface {
  ConfigFields() []FieldSection
}

// Actionable 是可选的接口：provider 通过它声明自定义操作按钮
type Actionable interface {
  Actions() []ActionDefinition
}

// ProviderDescriptor 是 provider 的完整描述，返回给前端用于动态渲染
type ProviderDescriptor struct {
  Name         string             `json:"name"`
  DisplayName  string             `json:"display_name"`
  HasPassword  bool               `json:"has_password"`
  HasBrowser   bool               `json:"has_browser"`
  HasDirectory bool               `json:"has_directory"`
  Fields       []FieldSection     `json:"fields"`
  Actions      []ActionDefinition `json:"actions"`
}

// DescribeProviderFull 返回指定 provider 的完整描述（含字段和操作按钮）
func DescribeProviderFull(name string) ProviderDescriptor {
  providersMu.RLock()
  defer providersMu.RUnlock()
  p, ok := providers[name]
  if !ok {
    return ProviderDescriptor{Name: name, DisplayName: name}
  }
  d := ProviderDescriptor{Name: name, DisplayName: name}
  _, d.HasPassword = p.(PasswordProvider)
  _, d.HasBrowser = p.(BrowserProvider)
  _, d.HasDirectory = p.(DirectoryProvider)
  if desc, ok := p.(Describable); ok {
    d.DisplayName = desc.DisplayName()
  }
  if cfg, ok := p.(Configurable); ok {
    d.Fields = cfg.ConfigFields()
  }
  if act, ok := p.(Actionable); ok {
    d.Actions = act.Actions()
  }
  return d
}

// ListProviders 返回所有已注册 provider 的描述列表
func ListProviders() []ProviderDescriptor {
  providersMu.RLock()
  defer providersMu.RUnlock()
  names := make([]string, 0, len(providers))
  for name := range providers {
    names = append(names, name)
  }
  sort.Strings(names)
  result := make([]ProviderDescriptor, 0, len(names))
  for _, name := range names {
    p := providers[name]
    d := ProviderDescriptor{Name: name, DisplayName: name}
    _, d.HasPassword = p.(PasswordProvider)
    _, d.HasBrowser = p.(BrowserProvider)
    _, d.HasDirectory = p.(DirectoryProvider)
    if desc, ok := p.(Describable); ok {
      d.DisplayName = desc.DisplayName()
    }
    if cfg, ok := p.(Configurable); ok {
      d.Fields = cfg.ConfigFields()
    }
    if act, ok := p.(Actionable); ok {
      d.Actions = act.Actions()
    }
    result = append(result, d)
  }
  return result
}

// RegisteredProviderNames 返回所有已注册 provider 的名称列表
func RegisteredProviderNames() []string {
  providersMu.RLock()
  defer providersMu.RUnlock()
  names := make([]string, 0, len(providers))
  for name := range providers {
    names = append(names, name)
  }
  return names
}

// ============================================================
// 通用分发函数（web 层通过这些函数调用，不直接调 provider 实现）
// ============================================================

// Authenticate 根据当前认证模式进行密码认证
func Authenticate(cfg *config.GlobalConfig, username, password string) bool {
  p, err := passwordProvider(cfg.AuthMode())
  if err != nil {
    return false
  }
  return p.Authenticate(cfg, username, password)
}

// AuthURL 获取当前认证源的浏览器认证 URL
func AuthURL(cfg *config.GlobalConfig, state string) (string, error) {
  p, err := browserProvider(cfg.AuthMode())
  if err != nil {
    return "", err
  }
  return p.AuthURL(cfg, state)
}

// CompleteLogin 完成浏览器认证回调
func CompleteLogin(ctx context.Context, cfg *config.GlobalConfig, code string) (*Identity, error) {
  p, err := browserProvider(cfg.AuthMode())
  if err != nil {
    return nil, err
  }
  return p.CompleteLogin(ctx, cfg, code)
}

// FetchUsers 从当前目录提供者获取用户列表
func FetchUsers(cfg *config.GlobalConfig) ([]string, error) {
  p, err := directoryProvider(cfg.AuthMode())
  if err != nil {
    return nil, err
  }
  return p.FetchUsers(cfg)
}

// FetchUserGroups 获取用户在目录提供者中的组
func FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error) {
  p, err := directoryProvider(cfg.AuthMode())
  if err != nil {
    return nil, err
  }
  return p.FetchUserGroups(cfg, username)
}

// FetchGroups 从目录提供者获取所有组
func FetchGroups(cfg *config.GlobalConfig) (GroupHierarchy, error) {
  p, err := directoryProvider(cfg.AuthMode())
  if err != nil {
    return nil, err
  }
  return p.FetchGroups(cfg)
}

// HasPasswordProvider 返回当前认证源是否支持密码认证
func HasPasswordProvider(cfg *config.GlobalConfig) bool {
  _, err := passwordProvider(cfg.AuthMode())
  return err == nil
}

// HasBrowserProvider 返回当前认证源是否支持浏览器认证
func HasBrowserProvider(cfg *config.GlobalConfig) bool {
  _, err := browserProvider(cfg.AuthMode())
  return err == nil
}

// HasDirectoryProvider 返回当前认证源是否支持目录同步
func HasDirectoryProvider(cfg *config.GlobalConfig) bool {
  _, err := directoryProvider(cfg.AuthMode())
  return err == nil
}
