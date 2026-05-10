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
	"time"

	"github.com/picoaide/picoaide/internal/auth"
)

// ============================================================
// 常量和模板
// ============================================================

const AppName = "picoaide"

var Version = "dev"

const SessionSecret = "picoaide-session-key-change-me"
const SessionMaxAge = 86400 // 24 hours

// ============================================================
// 配置结构体
// ============================================================

type LDAPConfig struct {
	Host                 string `yaml:"host"`
	BindDN               string `yaml:"bind_dn"`
	BindPassword         string `yaml:"bind_password"`
	BaseDN               string `yaml:"base_dn"`
	Filter               string `yaml:"filter"`
	UsernameAttribute    string `yaml:"username_attribute"`
	GroupSearchMode      string `yaml:"group_search_mode"` // "member_of" | "group_search"
	GroupBaseDN          string `yaml:"group_base_dn"`
	GroupFilter          string `yaml:"group_filter"`
	GroupMemberAttribute string `yaml:"group_member_attribute"`
	WhitelistEnabled     bool   `yaml:"whitelist_enabled"`
	SyncInterval         string `yaml:"sync_interval"` // "0" 禁用, "1h", "24h", "30m" 等
}

type ImageConfig struct {
	Name     string `yaml:"name"`
	Tag      string `yaml:"tag"`
	Timezone string `yaml:"timezone"`
	Registry string `yaml:"registry"` // "github" | "tencent"
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
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type WebConfig struct {
	Listen           string    `yaml:"listen"`
	ContainerBaseURL string    `yaml:"container_base_url"`
	Password         string    `yaml:"password"`
	LDAPEnabled      *bool     `yaml:"ldap_enabled"`
	AuthMode         string    `yaml:"auth_mode"`     // "ldap" | "oidc" | "local"
	LogRetention     string    `yaml:"log_retention"` // "1m","3m","6m","1y","3y","5y","forever"
	LogLevel         string    `yaml:"log_level"`     // "debug","info","warn","error"
	TLS              TLSConfig `yaml:"tls"`
}

type SkillRepoCredential struct {
	Name     string `yaml:"name" json:"name"`
	Provider string `yaml:"provider" json:"provider"`
	Mode     string `yaml:"mode" json:"mode"` // "ssh" | "http" | "https"
	Username string `yaml:"username" json:"username"`
	Secret   string `yaml:"secret" json:"secret"`
}

type SkillRepo struct {
	Name        string                `yaml:"name" json:"name"`
	URL         string                `yaml:"url" json:"url"`
	Ref         string                `yaml:"ref" json:"ref"`
	RefType     string                `yaml:"ref_type" json:"ref_type"` // "branch" | "tag"
	Public      bool                  `yaml:"public" json:"public"`
	Credentials []SkillRepoCredential `yaml:"credentials" json:"credentials"`
	LastPull    string                `yaml:"last_pull" json:"last_pull"`
}

type SkillsConfig struct {
	Repos []SkillRepo `yaml:"repos"`
}

type GlobalConfig struct {
	LDAP                         LDAPConfig   `yaml:"ldap"`
	Image                        ImageConfig  `yaml:"image"`
	UsersRoot                    string       `yaml:"users_root"`
	ArchiveRoot                  string       `yaml:"archive_root"`
	PicoClawAdapterRemoteBaseURL string       `yaml:"picoclaw_adapter_remote_base_url"`
	Web                          WebConfig    `yaml:"web"`
	PicoClaw                     interface{}  `yaml:"picoclaw"`
	Security                     interface{}  `yaml:"security"`
	Skills                       SkillsConfig `yaml:"skills"`
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

// SyncIntervalDuration 解析同步间隔配置，返回 time.Duration，0 表示禁用
func (cfg *GlobalConfig) SyncIntervalDuration() time.Duration {
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

// SkillsDirPath 返回技能目录路径（使用工作目录）
func SkillsDirPath() string {
	wd, err := os.Getwd()
	if err != nil {
		return "./skill"
	}
	return filepath.Join(wd, "skill")
}

func RuleCacheDir() string {
	if dir := strings.TrimSpace(os.Getenv("PICOAIDE_RULE_CACHE_DIR")); dir != "" {
		return dir
	}
	if hcfg, err := LoadHome(); err == nil && hcfg != nil && hcfg.RuleCacheDir != "" {
		return hcfg.RuleCacheDir
	}
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return "./rules"
	}
	return filepath.Join(wd, "rules")
}

func PicoClawAdapterRemoteBaseURL() string {
	if cfg, err := LoadFromDB(); err == nil && cfg != nil && cfg.PicoClawAdapterRemoteBaseURL != "" {
		return strings.TrimRight(cfg.PicoClawAdapterRemoteBaseURL, "/")
	}
	if hcfg, err := LoadHome(); err == nil && hcfg != nil && hcfg.PicoClawAdapterRemoteBaseURL != "" {
		return strings.TrimRight(hcfg.PicoClawAdapterRemoteBaseURL, "/")
	}
	if value := os.Getenv("PICOAIDE_PICOCLAW_ADAPTER_URL"); value != "" {
		return strings.TrimRight(value, "/")
	}
	return "https://raw.githubusercontent.com/picoaide/picoaide/main/rules/picoclaw"
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
	cfg.Image.Name = kv["image.name"]
	cfg.Image.Tag = kv["image.tag"]
	cfg.Image.Timezone = kv["image.timezone"]
	cfg.Image.Registry = kv["image.registry"]
	cfg.UsersRoot = kv["users_root"]
	cfg.ArchiveRoot = kv["archive_root"]
	cfg.PicoClawAdapterRemoteBaseURL = kv["picoclaw_adapter_remote_base_url"]
	cfg.Web.Listen = kv["web.listen"]
	cfg.Web.ContainerBaseURL = kv["web.container_base_url"]
	cfg.Web.Password = kv["web.password"]
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

	// TLS 配置
	cfg.Web.TLS.Enabled, _ = strconv.ParseBool(kv["web.tls.enabled"])
	cfg.Web.TLS.CertFile = kv["web.tls.cert_file"]
	cfg.Web.TLS.KeyFile = kv["web.tls.key_file"]

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
	kv["image.name"] = cfg.Image.Name
	kv["image.tag"] = cfg.Image.Tag
	kv["image.timezone"] = cfg.Image.Timezone
	kv["image.registry"] = cfg.Image.Registry
	kv["users_root"] = cfg.UsersRoot
	kv["archive_root"] = cfg.ArchiveRoot
	kv["web.listen"] = cfg.Web.Listen
	kv["web.container_base_url"] = cfg.Web.ContainerBaseURL
	kv["web.password"] = cfg.Web.Password
	kv["web.auth_mode"] = cfg.Web.AuthMode
	kv["web.log_retention"] = cfg.Web.LogRetention
	kv["web.log_level"] = cfg.Web.LogLevel

	if cfg.Web.LDAPEnabled != nil {
		kv["web.ldap_enabled"] = strconv.FormatBool(*cfg.Web.LDAPEnabled)
	}

	// TLS 配置
	kv["web.tls.enabled"] = strconv.FormatBool(cfg.Web.TLS.Enabled)
	kv["web.tls.cert_file"] = cfg.Web.TLS.CertFile
	kv["web.tls.key_file"] = cfg.Web.TLS.KeyFile

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
		kv[s.Key] = s.Value
	}

	return buildNested(kv), nil
}

// SaveRawToDB 将嵌套 JSON 配置保存到数据库
func SaveRawToDB(data map[string]interface{}, changedBy string) error {
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

	return session.Commit()
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
			Password:     "change-me-to-a-random-secret",
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
		Skills: SkillsConfig{Repos: []SkillRepo{}},
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
