package user

import (
  "encoding/json"
  "fmt"
  "os"
  "path/filepath"
  "strings"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/util"
)

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
  migrator, err := NewPicoClawMigrationService(config.RuleCacheDir())
  if err != nil {
    return err
  }
  if err := migrator.EnsureUpgradeable(fromTag, targetTag); err != nil {
    return err
  }

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
  globalPico = projectGlobalPicoConfigForTag(globalPico, targetTag)
  stripGlobalChannelPolicy(globalPico)

  merged := util.MergeMap(existing, globalPico)
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

func projectGlobalPicoConfigForTag(globalPico map[string]interface{}, targetTag string) map[string]interface{} {
  if globalPico == nil {
    return map[string]interface{}{}
  }
  configVersion := configVersionForPicoClawTag(targetTag)
  schema, hasSchema := picoClawConfigSchemaForVersion(configVersion)
  if !hasSchema {
    return globalPico
  }

  projected := util.DeepCopyMap(globalPico)
  projectDefaultModel(projected, schema.DefaultModelPath)
  projectChannels(projected, schema)
  projected["version"] = float64(configVersion)
  return projected
}

func configVersionForPicoClawTag(targetTag string) int {
  pkg, err := NewPicoClawAdapterPackage(config.RuleCacheDir())
  if err != nil {
    return PicoAideSupportedPicoClawConfigVersion
  }
  normalized := normalizeVersion(targetTag)
  if normalized != "" {
    for _, version := range pkg.Index.PicoClawVersions {
      if normalizeVersion(version.Version) == normalized {
        return version.ConfigVersion
      }
    }
  }
  return pkg.Index.LatestSupportedConfigVersion
}

func picoClawConfigSchemaForVersion(configVersion int) (PicoClawConfigSchema, bool) {
  pkg, err := NewPicoClawAdapterPackage(config.RuleCacheDir())
  if err != nil {
    return PicoClawConfigSchema{}, false
  }
  schema, ok := pkg.ConfigSchemas[configVersion]
  return schema, ok
}

func projectDefaultModel(cfg map[string]interface{}, targetPath string) {
  if targetPath == "" {
    return
  }
  value, ok := firstDeepValue(cfg, "agents.defaults.model_name", "agents.defaults.model")
  if !ok {
    return
  }
  deleteByPath(cfg, "agents.defaults.model_name")
  deleteByPath(cfg, "agents.defaults.model")
  setByPath(cfg, targetPath, value)
}

func projectChannels(cfg map[string]interface{}, schema PicoClawConfigSchema) {
  targetRoot := strings.TrimSpace(schema.ChannelsPath)
  if targetRoot == "" {
    return
  }
  sourceChannels := collectProjectedChannels(cfg)
  delete(cfg, "channels")
  delete(cfg, "channel_list")
  if len(sourceChannels) == 0 {
    return
  }
  allowed := stringSet(schema.ChannelTypes)
  targetChannels := make(map[string]interface{})
  for name, channel := range sourceChannels {
    if len(allowed) > 0 && !allowed[name] {
      continue
    }
    if targetRoot == "channel_list" {
      targetChannels[name] = projectChannelToV3(name, channel)
      continue
    }
    targetChannels[name] = projectChannelToLegacy(channel)
  }
  if len(targetChannels) > 0 {
    cfg[targetRoot] = targetChannels
  }
}

func collectProjectedChannels(cfg map[string]interface{}) map[string]map[string]interface{} {
  out := map[string]map[string]interface{}{}
  roots := []string{"channels", "channel_list"}
  for _, root := range roots {
    channels, _ := cfg[root].(map[string]interface{})
    for name, raw := range channels {
      channel, _ := raw.(map[string]interface{})
      if channel == nil {
        continue
      }
      if existing, ok := out[name]; ok {
        out[name] = util.MergeMap(existing, util.DeepCopyMap(channel))
        continue
      }
      out[name] = util.DeepCopyMap(channel)
    }
  }
  return out
}

func projectChannelToLegacy(channel map[string]interface{}) map[string]interface{} {
  out := make(map[string]interface{})
  settings, _ := channel["settings"].(map[string]interface{})
  for key, value := range channel {
    if key == "settings" || key == "type" {
      continue
    }
    out[key] = deepCopyInterface(value)
  }
  for key, value := range settings {
    out[key] = deepCopyInterface(value)
  }
  return out
}

func projectChannelToV3(name string, channel map[string]interface{}) map[string]interface{} {
  out := make(map[string]interface{})
  settings, _ := channel["settings"].(map[string]interface{})
  if settings != nil {
    settings = util.DeepCopyMap(settings)
  } else {
    settings = make(map[string]interface{})
  }
  for key, value := range channel {
    if key == "settings" {
      continue
    }
    if isPicoClawChannelBaseField(key) || key == "type" {
      out[key] = deepCopyInterface(value)
      continue
    }
    if _, exists := settings[key]; !exists {
      settings[key] = deepCopyInterface(value)
    }
  }
  if _, ok := out["type"]; !ok {
    out["type"] = name
  }
  if len(settings) > 0 {
    out["settings"] = settings
  }
  return out
}

func firstDeepValue(root map[string]interface{}, paths ...string) (interface{}, bool) {
  for _, path := range paths {
    if value, ok := deepGet(root, path); ok {
      return value, true
    }
  }
  return nil, false
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
  targetAtLeast028 := picoclawTagAtLeast(targetTag, 0, 2, 8)
  if normalizeVersion(targetTag) == "" {
    targetAtLeast028 = picoclawTagAtLeast(fallbackTag, 0, 2, 8)
  }
  if !targetAtLeast028 && !picoclawConfigVersionAtLeast(cfg, 3) {
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
    }), true, []interface{}{"Thinking..."}), true),
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
    }), true, []interface{}{"Thinking..."}), true),
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
