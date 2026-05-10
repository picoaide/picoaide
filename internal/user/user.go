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

// ============================================================
// 用户配置文件操作
// ============================================================

// ApplyConfigToJSON 将全局 picoclaw 配置合并到用户的 config.json，并注入 MCP 配置
func ApplyConfigToJSON(cfg *config.GlobalConfig, picoclawDir string, username string) error {
	return ApplyConfigToJSONForTag(cfg, picoclawDir, username, cfg.Image.Tag)
}

// ApplyConfigToJSONForTag 按目标 PicoClaw 镜像版本迁移并下发 config.json
func ApplyConfigToJSONForTag(cfg *config.GlobalConfig, picoclawDir string, username string, targetTag string) error {
	return ApplyConfigToJSONWithMigration(cfg, picoclawDir, username, targetTag, targetTag)
}

// ApplyConfigToJSONWithMigration 按迁移规则链升级 config.json，再下发全局配置和 MCP 配置
func ApplyConfigToJSONWithMigration(cfg *config.GlobalConfig, picoclawDir string, username string, fromTag string, targetTag string) error {
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
	stripGlobalChannelPolicy(globalPico)

	merged := util.MergeMap(existing, globalPico)
	migrator, err := NewPicoClawMigrationService(config.RuleCacheDir())
	if err != nil {
		return err
	}
	if err := migrator.Migrate(merged, fromTag, targetTag); err != nil {
		return err
	}
	if err := applyPicoClawCompatibilityFixups(merged, targetTag, cfg.Image.Tag); err != nil {
		return err
	}

	// 注入 MCP 配置。历史用户可能没有 mcp_token，配置下发时自动补齐。
	mcpToken, err := ensureMCPToken(username)
	if err != nil {
		return fmt.Errorf("生成 MCP token 失败: %w", err)
	}
	injectMCPConfig(merged, mcpToken, cfg)

	jsonData, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("格式化 config.json 失败: %w", err)
	}

	return os.WriteFile(configPath, jsonData, 0644)
}

func ensureMCPToken(username string) (string, error) {
	token, err := auth.GetMCPToken(username)
	if err != nil {
		return "", err
	}
	if token != "" {
		return token, nil
	}
	return auth.GenerateMCPToken(username)
}

func stripGlobalChannelPolicy(globalPico map[string]interface{}) {
	for _, rootKey := range []string{"channel_list", "channels"} {
		channels, _ := globalPico[rootKey].(map[string]interface{})
		for _, raw := range channels {
			channel, _ := raw.(map[string]interface{})
			if channel == nil {
				continue
			}
			delete(channel, "enabled")
		}
	}
}

// injectMCPConfig 向 config.json 注入 MCP server 配置
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

	baseURL := containerBaseURL(cfg)

	servers["browser"] = map[string]interface{}{
		"enabled": true,
		"type":    "sse",
		"url":     fmt.Sprintf("%s/api/mcp/sse/browser?token=%s", baseURL, mcpToken),
	}

	servers["computer"] = map[string]interface{}{
		"enabled": true,
		"type":    "sse",
		"url":     fmt.Sprintf("%s/api/mcp/sse/computer?token=%s", baseURL, mcpToken),
	}

	// 清理旧配置
	delete(servers, "chrome-devtools")
}

func containerBaseURL(cfg *config.GlobalConfig) string {
	if cfg.Web.ContainerBaseURL != "" {
		return strings.TrimRight(cfg.Web.ContainerBaseURL, "/")
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
	if host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "100.64.0.1"
	}

	scheme := "http"
	if cfg.Web.TLS.Enabled && port == "443" {
		port = "80"
	} else if cfg.Web.TLS.Enabled {
		scheme = "https"
	}

	return fmt.Sprintf("%s://%s:%s", scheme, host, port)
}

func applyPicoClawCompatibilityFixups(cfg map[string]interface{}, targetTag string, fallbackTag string) error {
	if !picoclawTagAtLeast(targetTag, 0, 2, 8) && !picoclawTagAtLeast(fallbackTag, 0, 2, 8) && !picoclawConfigVersionAtLeast(cfg, 3) {
		return nil
	}

	ensurePicoClaw028ModelDefaults(cfg)

	channels, ok := cfg["channels"].(map[string]interface{})
	if !ok {
		ensurePicoClaw028ChannelDefaults(cfg)
		return nil
	}
	if len(channels) == 0 {
		delete(cfg, "channels")
		ensurePicoClaw028ChannelDefaults(cfg)
		return nil
	}

	channelList := make(map[string]interface{}, len(channels))
	for name, raw := range channels {
		channelCfg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fixed := make(map[string]interface{}, len(channelCfg)+1)
		for key, val := range channelCfg {
			if key == "transport" {
				continue
			}
			fixed[key] = val
		}
		if _, ok := fixed["type"]; !ok {
			fixed["type"] = name
		}
		if _, ok := fixed["settings"]; !ok {
			settings := make(map[string]interface{})
			for key, val := range channelCfg {
				if key == "enabled" || key == "type" || key == "allow_from" || key == "reasoning_channel_id" || key == "group_trigger" || key == "typing" || key == "placeholder" {
					continue
				}
				if key == "transport" {
					continue
				}
				settings[key] = val
			}
			if len(settings) > 0 {
				fixed["settings"] = settings
			}
		}
		channelList[name] = fixed
	}

	if len(channelList) > 0 {
		cfg["channel_list"] = channelList
		delete(cfg, "channels")
	}
	ensurePicoClaw028ChannelDefaults(cfg)
	return nil
}

func ensurePicoClaw028ModelDefaults(cfg map[string]interface{}) {
	modelList, ok := cfg["model_list"].([]interface{})
	if !ok {
		return
	}
	firstModelName := ""
	for _, raw := range modelList {
		model, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if firstModelName == "" {
			if name, ok := model["model_name"].(string); ok && name != "" {
				firstModelName = name
			}
		}
		if _, exists := model["enabled"]; !exists {
			model["enabled"] = true
		}
	}
	if firstModelName == "" {
		return
	}
	agents, _ := cfg["agents"].(map[string]interface{})
	if agents == nil {
		agents = make(map[string]interface{})
		cfg["agents"] = agents
	}
	defaults, _ := agents["defaults"].(map[string]interface{})
	if defaults == nil {
		defaults = make(map[string]interface{})
		agents["defaults"] = defaults
	}
	if modelName, _ := defaults["model_name"].(string); modelName == "" {
		defaults["model_name"] = firstModelName
	}
}

func ensurePicoClaw028ChannelDefaults(cfg map[string]interface{}) {
	channelList, _ := cfg["channel_list"].(map[string]interface{})
	if channelList == nil {
		channelList = make(map[string]interface{})
		cfg["channel_list"] = channelList
	}
	for name, defaults := range picoClaw028ChannelDefaults() {
		current, _ := channelList[name].(map[string]interface{})
		if current == nil {
			channelList[name] = deepCopyInterface(defaults)
			continue
		}
		mergeDefaults(current, defaults)
	}
}

func picoClaw028ChannelDefaults() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"dingtalk": baseChannelDefault("dingtalk"),
		"discord":  baseChannelDefault("discord"),
		"feishu":   baseChannelDefault("feishu"),
		"slack":    baseChannelDefault("slack"),
		"irc": withSettings(baseChannelDefault("irc"), map[string]interface{}{
			"channels": []interface{}{},
			"nick":     "picoclaw",
			"server":   "",
			"tls":      true,
		}),
		"line": withGroupTrigger(withSettings(baseChannelDefault("line"), map[string]interface{}{
			"webhook_host": "0.0.0.0",
			"webhook_path": "/webhook/line",
			"webhook_port": float64(18791),
		}), true),
		"maixcam": withSettings(baseChannelDefault("maixcam"), map[string]interface{}{
			"host": "0.0.0.0",
			"port": float64(18790),
		}),
		"matrix": withGroupTrigger(withPlaceholder(withSettings(baseChannelDefault("matrix"), map[string]interface{}{
			"homeserver":     "https://matrix.org",
			"join_on_invite": true,
		}), true, []interface{}{"Thinking... 💭"}), true),
		"onebot": withSettings(baseChannelDefault("onebot"), map[string]interface{}{
			"reconnect_interval": float64(5),
			"ws_url":             "ws://127.0.0.1:3001",
		}),
		"pico": withSettings(baseChannelDefault("pico"), map[string]interface{}{
			"max_connections": float64(100),
			"ping_interval":   float64(30),
			"read_timeout":    float64(60),
			"write_timeout":   float64(10),
		}),
		"qq": withSettings(baseChannelDefault("qq"), map[string]interface{}{
			"max_message_length": float64(2000),
		}),
		"telegram": withTyping(withPlaceholder(withSettings(baseChannelDefault("telegram"), map[string]interface{}{
			"streaming": map[string]interface{}{
				"enabled":          true,
				"min_growth_chars": float64(200),
				"throttle_seconds": float64(3),
			},
			"use_markdown_v2": false,
		}), true, []interface{}{"Thinking... 💭"}), true),
		"wecom": withSettings(baseChannelDefault("wecom"), map[string]interface{}{
			"send_thinking_message": true,
			"websocket_url":         "wss://openws.work.weixin.qq.com",
		}),
		"weixin": withSettings(baseChannelDefault("weixin"), map[string]interface{}{
			"base_url":     "https://ilinkai.weixin.qq.com/",
			"cdn_base_url": "https://novac2c.cdn.weixin.qq.com/c2c",
		}),
		"whatsapp": withSettings(baseChannelDefault("whatsapp"), map[string]interface{}{
			"bridge_url": "ws://localhost:3001",
		}),
	}
}

func baseChannelDefault(channelType string) map[string]interface{} {
	return map[string]interface{}{
		"enabled":              false,
		"group_trigger":        map[string]interface{}{},
		"placeholder":          map[string]interface{}{"enabled": false},
		"reasoning_channel_id": "",
		"type":                 channelType,
		"typing":               map[string]interface{}{},
	}
}

func withSettings(base map[string]interface{}, settings map[string]interface{}) map[string]interface{} {
	base["settings"] = settings
	return base
}

func withGroupTrigger(base map[string]interface{}, mentionOnly bool) map[string]interface{} {
	base["group_trigger"] = map[string]interface{}{"mention_only": mentionOnly}
	return base
}

func withPlaceholder(base map[string]interface{}, enabled bool, text []interface{}) map[string]interface{} {
	placeholder := map[string]interface{}{"enabled": enabled}
	if len(text) > 0 {
		placeholder["text"] = text
	}
	base["placeholder"] = placeholder
	return base
}

func withTyping(base map[string]interface{}, enabled bool) map[string]interface{} {
	base["typing"] = map[string]interface{}{"enabled": enabled}
	return base
}

func mergeDefaults(dst map[string]interface{}, defaults map[string]interface{}) {
	for key, defaultValue := range defaults {
		currentValue, exists := dst[key]
		if !exists {
			dst[key] = deepCopyInterface(defaultValue)
			continue
		}
		currentMap, currentOK := currentValue.(map[string]interface{})
		defaultMap, defaultOK := defaultValue.(map[string]interface{})
		if currentOK && defaultOK {
			mergeDefaults(currentMap, defaultMap)
		}
	}
}

func deepCopyInterface(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		cp := make(map[string]interface{}, len(v))
		for key, val := range v {
			cp[key] = deepCopyInterface(val)
		}
		return cp
	case []interface{}:
		cp := make([]interface{}, len(v))
		for i, val := range v {
			cp[i] = deepCopyInterface(val)
		}
		return cp
	default:
		return v
	}
}

func picoclawConfigVersionAtLeast(cfg map[string]interface{}, minVersion int) bool {
	raw, ok := cfg["version"]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case int:
		return v >= minVersion
	case int64:
		return v >= int64(minVersion)
	case float64:
		return v >= float64(minVersion)
	case json.Number:
		n, err := v.Int64()
		return err == nil && n >= int64(minVersion)
	default:
		return false
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
		yaml.Unmarshal(data, &secMap)
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

// ============================================================
// 旧版目录迁移
// ============================================================
