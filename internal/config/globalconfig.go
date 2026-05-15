package config

import (
  "bytes"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "strconv"
  "strings"
  "text/template"
  "time"
)

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
