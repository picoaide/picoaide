package config

import (
  "bufio"
  "bytes"
  "encoding/json"
  "fmt"
  "net"
  "os"
  "os/exec"
  "path/filepath"
  "strconv"
  "strings"
  "text/template"

  "gopkg.in/yaml.v3"
)

// ============================================================
// 常量和模板
// ============================================================

const AppName = "picoaide"
const configFileName = "config.yaml"
const SessionSecret = "picoaide-session-key-change-me"
const SessionMaxAge = 86400 // 24 hours

const DefaultConfig = `# PicoAide 管理工具配置文件
# 请根据实际情况修改以下配置

ldap:
  host: "ldap://ldap.example.com:389"
  bind_dn: "cn=admin,dc=example,dc=com"
  bind_password: "your-password-here"
  base_dn: "ou=users,dc=example,dc=com"
  filter: "(objectClass=inetOrgPerson)"
  username_attribute: "uid"

image:
  name: "ghcr.io/lostmaniac/picoclaw"
  tag: "v0.2.6"
  timezone: "Asia/Shanghai"

users_root: "./users"
archive_root: "./archive"

web:
  listen: ":80"
  password: "change-me-to-a-random-secret"

picoclaw:
  agents:
    defaults:
      model_name: "gpt-5.4"
      max_tokens: 32768
      max_tool_iterations: 50
  model_list:
    - model_name: "gpt-5.4"
      model: "openai/gpt-5.4"
      api_base: "https://api.openai.com/v1"
      request_timeout: 6000
  channels: {}
  tools:
    web:
      duckduckgo:
        enabled: true
    mcp:
      enabled: true
      max_inline_text_chars: 8192
      servers:
        chrome-devtools:
          enabled: true
          command: npx
          args:
            - chrome-devtools-mcp@latest
            - '--browser-url=http://127.0.0.1:9222'
            - '--autoConnect'
  gateway:
    host: "0.0.0.0"
    port: 18790

security:
  model_list:
    gpt-5.4:0:
      api_keys:
        - "sk-openai-replace-me"

skills:
  repos: []
`

// 白名单文件名，与 config.yaml 同目录
const whitelistFileName = "whitelist.yaml"

const defaultWhitelist = `# PicoAide 用户白名单
# users 列表为空时，所有 LDAP 用户均可使用
# 示例：
# users:
#   - zhangsan
#   - lisi
users: []
`

// ============================================================
// 配置结构体
// ============================================================

type LDAPConfig struct {
  Host              string `yaml:"host"`
  BindDN            string `yaml:"bind_dn"`
  BindPassword      string `yaml:"bind_password"`
  BaseDN            string `yaml:"base_dn"`
  Filter            string `yaml:"filter"`
  UsernameAttribute string `yaml:"username_attribute"`
}

type ImageConfig struct {
  Name     string `yaml:"name"`
  Tag      string `yaml:"tag"`
  Timezone string `yaml:"timezone"`
}

type WebConfig struct {
  Listen      string `yaml:"listen"`
  Password    string `yaml:"password"`
  LDAPEnabled *bool  `yaml:"ldap_enabled"`
  AuthMode    string `yaml:"auth_mode"` // "ldap" | "oidc" | "local"（默认根据 ldap_enabled 推断）
}

type SkillRepo struct {
  Name     string `yaml:"name"`
  URL      string `yaml:"url"`
  LastPull string `yaml:"last_pull"`
}

type SkillsConfig struct {
  Repos []SkillRepo `yaml:"repos"`
}

type GlobalConfig struct {
  LDAP        LDAPConfig  `yaml:"ldap"`
  Image       ImageConfig `yaml:"image"`
  UsersRoot   string      `yaml:"users_root"`
  ArchiveRoot string      `yaml:"archive_root"`
  Web         WebConfig   `yaml:"web"`
  PicoClaw    interface{} `yaml:"picoclaw"`
  Security    interface{} `yaml:"security"`
  Skills      SkillsConfig `yaml:"skills"`
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

// SkillsDirPath 返回技能目录路径
func SkillsDirPath() string {
  return filepath.Join(filepath.Dir(ConfigPath()), "skill")
}

// ============================================================
// 配置加载和自动检测
// ============================================================

func getExeDir() string {
  exe, err := os.Executable()
  if err != nil {
    return "."
  }
  return filepath.Dir(exe)
}

func ConfigPath() string {
  if _, err := os.Stat(configFileName); err == nil {
    return configFileName
  }
  exeDir := getExeDir()
  p := filepath.Join(exeDir, configFileName)
  if _, err := os.Stat(p); err == nil {
    return p
  }
  return configFileName
}

func EnsureConfig() (string, bool) {
  configPath := ConfigPath()
  if _, err := os.Stat(configPath); os.IsNotExist(err) {
    err := os.WriteFile(configPath, []byte(DefaultConfig), 0644)
    if err != nil {
      fmt.Fprintf(os.Stderr, "错误: 无法创建默认配置文件 %s: %v\n", configPath, err)
      os.Exit(1)
    }
    fmt.Printf("已生成默认配置文件: %s\n", configPath)

    // 同时创建白名单文件
    if err := EnsureWhitelistFile(); err != nil {
      fmt.Fprintf(os.Stderr, "警告: 创建白名单文件失败: %v\n", err)
    }

    fmt.Println("请修改配置文件后重新运行此工具。")
    return configPath, false
  }
  return configPath, true
}

func Load(path string) (*GlobalConfig, error) {
  data, err := os.ReadFile(path)
  if err != nil {
    return nil, fmt.Errorf("读取配置文件失败: %w", err)
  }

  var cfg GlobalConfig
  if err := yaml.Unmarshal(data, &cfg); err != nil {
    return nil, fmt.Errorf("解析配置文件失败: %w", err)
  }

  return &cfg, nil
}

func Save(cfg *GlobalConfig, path string) error {
  data, err := yaml.Marshal(cfg)
  if err != nil {
    return fmt.Errorf("序列化配置失败: %w", err)
  }
  return os.WriteFile(path, data, 0644)
}

// ============================================================
// 白名单文件管理（从 user 包迁移，打破循环依赖）
// ============================================================

// WhitelistFile 白名单文件结构
type WhitelistFile struct {
  Users []string `yaml:"users"`
}

// WhitelistPath 返回白名单文件路径（与 config.yaml 同目录）
func WhitelistPath() string {
  return filepath.Join(filepath.Dir(ConfigPath()), whitelistFileName)
}

// EnsureWhitelistFile 首次 init 时创建白名单文件（带注释模板）
func EnsureWhitelistFile() error {
  path := WhitelistPath()
  if _, err := os.Stat(path); err == nil {
    return nil
  }
  return os.WriteFile(path, []byte(defaultWhitelist), 0644)
}

// ============================================================
// 环境预检
// ============================================================

func PreflightChecks() {
  var warnings []string

  // 1. 检查 Docker bridge 网段
  warnings = append(warnings, checkDockerNetwork()...)

  // 2. 检查系统文件描述符限制
  warnings = append(warnings, checkUlimit()...)

  if len(warnings) > 0 {
    fmt.Println("=== 环境检查 ===")
    for _, w := range warnings {
      fmt.Println(w)
    }
    fmt.Println()
  }
}

func checkDockerNetwork() []string {
  var warnings []string

  // 检查 daemon.json 是否存在
  daemonJSON := "/etc/docker/daemon.json"
  data, err := os.ReadFile(daemonJSON)
  if err != nil {
    warnings = append(warnings, "[警告] 未找到 /etc/docker/daemon.json，Docker 使用默认网段 172.17.0.0/16")
    warnings = append(warnings, "  每个用户运行 2 个容器，默认 /16 网段仅支持约 65000 个地址")
    warnings = append(warnings, "  建议创建 /etc/docker/daemon.json 并配置更大的网段：")
    warnings = append(warnings, sep)
    warnings = append(warnings, daemonJSONExample)
    warnings = append(warnings, sep)
    warnings = append(warnings, "  修改后执行: systemctl restart docker")
    return warnings
  }

  // 解析 daemon.json 检查配置
  var cfg map[string]interface{}
  if err := json.Unmarshal(data, &cfg); err != nil {
    warnings = append(warnings, "[警告] /etc/docker/daemon.json 格式错误，无法检查网段配置")
    return warnings
  }

  hasBIP := false
  hasPool := false
  bipOK := false

  if _, ok := cfg["bip"]; ok {
    hasBIP = true
    _, ipNet, err := net.ParseCIDR(fmt.Sprintf("%v", cfg["bip"]))
    if err == nil {
      ones, _ := ipNet.Mask.Size()
      if ones <= 12 {
        bipOK = true
      }
    }
  }

  if pools, ok := cfg["default-address-pools"].([]interface{}); ok && len(pools) > 0 {
    hasPool = true
  }

  if !hasBIP && !hasPool {
    warnings = append(warnings, "[警告] /etc/docker/daemon.json 未配置 bip 或 default-address-pools")
    warnings = append(warnings, "  当前 Docker 使用默认网段 172.17.0.0/16，可能与内网 IP 冲突")
    warnings = append(warnings, "  建议修改为使用保留地址段：")
    warnings = append(warnings, sep)
    warnings = append(warnings, daemonJSONExample)
    warnings = append(warnings, sep)
    warnings = append(warnings, "  修改后执行: systemctl restart docker")
  } else if hasBIP && !bipOK {
    warnings = append(warnings, fmt.Sprintf("[警告] bip 网段掩码为 /%v，建议使用 /10 或 /12 以支持更多容器", cfg["bip"]))
  }

  return warnings
}

func checkUlimit() []string {
  var warnings []string

  // 获取当前 nofile 软限制
  ulimitSoft, ulimitHard := getUlimit()

  minFD := uint64(65536)
  if ulimitSoft < minFD || ulimitHard < minFD {
    warnings = append(warnings, fmt.Sprintf("[警告] 系统文件描述符限制过低 (当前: soft=%d, hard=%d)", ulimitSoft, ulimitHard))
    warnings = append(warnings, "  每个容器会占用多个文件描述符，用户数量多时可能耗尽")
    warnings = append(warnings, "  建议在 /etc/security/limits.d/picoaide.conf 中添加：")
    warnings = append(warnings, sep)
    warnings = append(warnings, limitsConfExample)
    warnings = append(warnings, sep)
    warnings = append(warnings, "  同时在 /etc/docker/daemon.json 中添加（如已有其他配置请合并）：")
    warnings = append(warnings, `    "default-ulimits": { "nofile": { "Name": "nofile", "Hard": 1048576, "Soft": 1048576 } }`)
    warnings = append(warnings, "  修改后执行: systemctl restart docker")
  }

  // 检查 fs.file-max
  fileMax := getSysctl("fs.file-max")
  if fileMax != "" {
    fm, _ := strconv.ParseUint(fileMax, 10, 64)
    if fm > 0 && fm < 1000000 {
      warnings = append(warnings, fmt.Sprintf("[警告] 内核 fs.file-max = %d，建议调大到 1000000 以上", fm))
      warnings = append(warnings, "  执行: sysctl -w fs.file-max=2097152")
      warnings = append(warnings, "  持久化: echo 'fs.file-max = 2097152' >> /etc/sysctl.d/99-picoaide.conf && sysctl -p /etc/sysctl.d/99-picoaide.conf")
    }
  }

  return warnings
}

func getUlimit() (soft, hard uint64) {
  out, err := exec.Command("sh", "-c", "ulimit -Sn").Output()
  if err == nil {
    soft, _ = strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
  }
  out, err = exec.Command("sh", "-c", "ulimit -Hn").Output()
  if err == nil {
    hard, _ = strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
  }
  return
}

func getSysctl(key string) string {
  out, err := exec.Command("sysctl", "-n", key).Output()
  if err != nil {
    return ""
  }
  return strings.TrimSpace(string(out))
}

const sep = "  ─────────────────────────────────────────────────────"

const daemonJSONExample = `  {
    "bip": "100.64.0.1/10",
    "default-address-pools": [
      { "base": "100.64.0.0/10", "size": 24 }
    ]
  }`

const limitsConfExample = `  * soft nofile 1048576
  * hard nofile 1048576
  root soft nofile 1048576
  root hard nofile 1048576`

// ============================================================
// systemd 服务文件管理
// ============================================================

// SystemServiceTemplate systemd 服务文件模板
const SystemServiceTemplate = `[Unit]
Description=PicoAide Management API Server
After=network.target docker.service

[Service]
Type=simple
ExecStart=/usr/sbin/picoaide serve -listen {{.ListenAddr}}
WorkingDirectory={{.WorkingDir}}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

// ServiceTemplateData 服务模板数据
type ServiceTemplateData struct {
  WorkingDir string
  ListenAddr string
}

const serviceFilePath = "/etc/systemd/system/picoaide.service"

// InstallService 生成并安装 systemd 服务文件
func InstallService(cfg *GlobalConfig) error {
  workDir, _ := os.Getwd()
  if workDir == "" {
    workDir = "/data/picoaide"
  }

  listenAddr := cfg.Web.Listen
  if listenAddr == "" {
    listenAddr = ":80"
  }

  data := ServiceTemplateData{
    WorkingDir: workDir,
    ListenAddr: listenAddr,
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

  // 检查现有服务文件
  existing, err := os.ReadFile(serviceFilePath)
  if err == nil {
    if bytes.Equal(existing, newContent) {
      fmt.Println("服务文件已存在且一致，跳过")
      return nil
    }
    fmt.Println("检测到服务文件内容不一致:")
    fmt.Printf("  现有文件: %s\n", serviceFilePath)
    fmt.Printf("  是否覆盖？[y/N] ")

    reader := bufio.NewReader(os.Stdin)
    answer, _ := reader.ReadString('\n')
    answer = strings.TrimSpace(strings.ToLower(answer))
    if answer != "y" && answer != "yes" {
      fmt.Println("已跳过服务文件更新")
      return nil
    }
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

// LoadRaw 读取 config.yaml 并转换为 JSON（供 API 使用）
func LoadRaw() ([]byte, error) {
  data, err := os.ReadFile(ConfigPath())
  if err != nil {
    return nil, fmt.Errorf("读取配置文件失败: %w", err)
  }
  var raw map[string]interface{}
  if err := yaml.Unmarshal(data, &raw); err != nil {
    return nil, fmt.Errorf("解析配置文件失败: %w", err)
  }
  return json.MarshalIndent(raw, "", "  ")
}

// SaveRaw 将 YAML 字节写入 config.yaml
func SaveRaw(data []byte) error {
  return os.WriteFile(ConfigPath(), data, 0644)
}
