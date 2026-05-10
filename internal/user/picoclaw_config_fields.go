package user

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/picoaide/picoaide/internal/auth"
	"github.com/picoaide/picoaide/internal/config"
	"gopkg.in/yaml.v3"
)

type PicoClawChannelInfo struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Allowed    bool   `json:"allowed"`
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
}

func ListPicoClawAdminChannels(cacheDir string, configVersion int) ([]PicoClawChannelInfo, error) {
	pkg, err := NewPicoClawAdapterPackage(cacheDir)
	if err != nil {
		return nil, err
	}
	ui, ok := pkg.UISchemas[configVersion]
	if !ok {
		return nil, fmt.Errorf("adapter 缺少 config v%d UI schema", configVersion)
	}
	channels := pkg.ChannelTypesFor(configVersion, "")
	allowed := stringSet(channels)
	var out []PicoClawChannelInfo
	for _, section := range findPicoClawUISections(ui, "channels") {
		if len(allowed) > 0 && !allowed[section.Key] {
			continue
		}
		out = append(out, PicoClawChannelInfo{
			Key:     section.Key,
			Label:   section.Label,
			Allowed: true,
		})
	}
	return out, nil
}

type PicoClawConfigField struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Storage  string   `json:"storage"`
	Path     string   `json:"path"`
	Secret   bool     `json:"secret,omitempty"`
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
	Default  any      `json:"default,omitempty"`
}

type PicoClawFieldValue struct {
	Field PicoClawConfigField `json:"field"`
	Value interface{}         `json:"value,omitempty"`
	Set   bool                `json:"set"`
}

func ListPicoClawUserChannels(cfg *config.GlobalConfig, username string, configVersion int) ([]PicoClawChannelInfo, error) {
	pkg, err := NewPicoClawAdapterPackage(config.RuleCacheDir())
	if err != nil {
		return nil, err
	}
	picoclawDir := filepath.Join(UserDir(cfg, username), ".picoclaw")
	configMap, err := readJSONMap(filepath.Join(picoclawDir, "config.json"))
	if err != nil {
		return nil, err
	}
	if configVersion <= 0 {
		configVersion = configVersionFromMap(configMap)
	}
	ui, ok := pkg.UISchemas[configVersion]
	if !ok {
		return nil, fmt.Errorf("adapter 缺少 config v%d UI schema", configVersion)
	}
	securityMap, err := readYAMLMap(filepath.Join(picoclawDir, ".security.yml"))
	if err != nil {
		return nil, err
	}
	allowed := allowedPicoClawChannelsFromConfig(cfg)
	supported := supportedPicoClawChannelsForUser(pkg, username, configVersion)
	var out []PicoClawChannelInfo
	for _, section := range findPicoClawUISections(ui, "channels") {
		if len(supported) > 0 && !supported[section.Key] {
			continue
		}
		enabled, configured := userChannelState(configMap, securityMap, section.Key, section.Fields)
		if rec, err := auth.GetUserChannelStatus(username, section.Key); err == nil && rec != nil {
			enabled = rec.Enabled
			configured = rec.Configured || configured
		}
		isAllowed := allowed[section.Key]
		if err := auth.UpsertUserChannelStatus(username, section.Key, isAllowed, enabled, configured, configVersion); err != nil {
			return nil, err
		}
		if !isAllowed {
			continue
		}
		out = append(out, PicoClawChannelInfo{
			Key:        section.Key,
			Label:      section.Label,
			Allowed:    true,
			Enabled:    enabled,
			Configured: configured,
		})
	}
	return out, nil
}

func GetPicoClawConfigFields(cfg *config.GlobalConfig, username string, configVersion int, sectionKey string) ([]PicoClawFieldValue, error) {
	pkg, err := NewPicoClawAdapterPackage(config.RuleCacheDir())
	if err != nil {
		return nil, err
	}
	picoclawDir := filepath.Join(UserDir(cfg, username), ".picoclaw")
	configMap, err := readJSONMap(filepath.Join(picoclawDir, "config.json"))
	if err != nil {
		return nil, err
	}
	if configVersion <= 0 {
		configVersion = configVersionFromMap(configMap)
	}
	ui, ok := pkg.UISchemas[configVersion]
	if !ok {
		return nil, fmt.Errorf("adapter 缺少 config v%d UI schema", configVersion)
	}
	if sectionKey != "" {
		supported := supportedPicoClawChannelsForUser(pkg, username, configVersion)
		if len(supported) > 0 && !supported[sectionKey] {
			return nil, fmt.Errorf("渠道 %s 不支持当前 Picoclaw 版本", sectionKey)
		}
	}
	securityMap, err := readYAMLMap(filepath.Join(picoclawDir, ".security.yml"))
	if err != nil {
		return nil, err
	}
	if sectionKey != "" {
		allowed := allowedPicoClawChannelsFromConfig(cfg)
		if !allowed[sectionKey] {
			return nil, fmt.Errorf("渠道 %s 未被管理员允许启用", sectionKey)
		}
	}
	var values []PicoClawFieldValue
	for _, field := range findPicoClawUIFields(ui, sectionKey) {
		source := configMap
		if field.Storage == "security" {
			source = securityMap
		}
		value, ok := deepGet(source, field.Path)
		values = append(values, PicoClawFieldValue{
			Field: field,
			Value: value,
			Set:   ok,
		})
	}
	return values, nil
}

func SavePicoClawConfigFields(cfg *config.GlobalConfig, username string, configVersion int, values map[string]interface{}) error {
	return SavePicoClawConfigSectionFields(cfg, username, configVersion, "", values)
}

func SavePicoClawConfigSectionFields(cfg *config.GlobalConfig, username string, configVersion int, sectionKey string, values map[string]interface{}) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}
	if sectionKey != "" {
		allowed := allowedPicoClawChannelsFromConfig(cfg)
		if !allowed[sectionKey] {
			return fmt.Errorf("渠道 %s 未被管理员允许启用", sectionKey)
		}
	}
	picoclawDir := filepath.Join(UserDir(cfg, username), ".picoclaw")
	if err := os.MkdirAll(picoclawDir, 0755); err != nil {
		return err
	}
	configPath := filepath.Join(picoclawDir, "config.json")
	securityPath := filepath.Join(picoclawDir, ".security.yml")
	configMap, err := readJSONMap(configPath)
	if err != nil {
		return err
	}
	if err := ensureSupportedConfigFileVersion(configMap); err != nil {
		return err
	}
	if configVersion <= 0 {
		configVersion = configVersionFromMap(configMap)
	}
	pkg, err := NewPicoClawAdapterPackage(config.RuleCacheDir())
	if err != nil {
		return err
	}
	ui, ok := pkg.UISchemas[configVersion]
	if !ok {
		return fmt.Errorf("adapter 缺少 config v%d UI schema", configVersion)
	}
	if sectionKey != "" {
		supported := supportedPicoClawChannelsForUser(pkg, username, configVersion)
		if len(supported) > 0 && !supported[sectionKey] {
			return fmt.Errorf("渠道 %s 不支持当前 Picoclaw 版本", sectionKey)
		}
	}
	fields := map[string]PicoClawConfigField{}
	for _, field := range findPicoClawUIFields(ui, sectionKey) {
		fields[field.Key] = field
	}
	securityMap, err := readYAMLMap(securityPath)
	if err != nil {
		return err
	}
	for key, value := range values {
		field, ok := fields[key]
		if !ok {
			return fmt.Errorf("字段 %s 不在 config v%d UI schema 中", key, configVersion)
		}
		if field.Secret && value == "" {
			continue
		}
		coerced, err := coercePicoClawFieldValue(field, value)
		if err != nil {
			return err
		}
		target := configMap
		if field.Storage == "security" {
			target = securityMap
		}
		setByPath(target, field.Path, coerced)
		ensurePicoClawChannelType(target, field.Path)
	}
	enabled, configured := userChannelState(configMap, securityMap, sectionKey, mapFields(fields))
	configJSON, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 config.json 失败: %w", err)
	}
	if err := os.WriteFile(configPath, configJSON, 0644); err != nil {
		return fmt.Errorf("写入 config.json 失败: %w", err)
	}
	securityYAML, err := yaml.Marshal(securityMap)
	if err != nil {
		return fmt.Errorf("序列化 .security.yml 失败: %w", err)
	}
	if err := os.WriteFile(securityPath, securityYAML, 0600); err != nil {
		return fmt.Errorf("写入 .security.yml 失败: %w", err)
	}
	if sectionKey != "" {
		if err := auth.UpsertUserChannelStatus(username, sectionKey, true, enabled, configured, configVersion); err != nil {
			return err
		}
	}
	return nil
}

func ensurePicoClawChannelType(target map[string]interface{}, fieldPath string) {
	parts := strings.Split(fieldPath, ".")
	if len(parts) < 3 || parts[0] != "channel_list" || parts[1] == "*" {
		return
	}
	typePath := "channel_list." + parts[1] + ".type"
	if _, ok := deepGet(target, typePath); ok {
		return
	}
	setByPath(target, typePath, parts[1])
}

func findPicoClawUIFields(ui PicoClawUISchema, sectionKey string) []PicoClawConfigField {
	var fields []PicoClawConfigField
	for _, page := range ui.Pages {
		for _, section := range page.Sections {
			if sectionKey != "" && section.Key != sectionKey {
				continue
			}
			for _, raw := range section.Fields {
				fields = append(fields, PicoClawConfigField{
					Key:      raw.Key,
					Label:    raw.Label,
					Type:     raw.Type,
					Storage:  normalizePicoClawFieldStorage(raw.Storage),
					Path:     raw.Path,
					Secret:   raw.Secret,
					Required: raw.Required,
					Options:  raw.Options,
					Default:  raw.Default,
				})
			}
		}
	}
	return fields
}

type picoClawUISectionView struct {
	Key    string
	Label  string
	Fields []PicoClawConfigField
}

func findPicoClawUISections(ui PicoClawUISchema, pageKey string) []picoClawUISectionView {
	var sections []picoClawUISectionView
	for _, page := range ui.Pages {
		if pageKey != "" && page.Key != pageKey {
			continue
		}
		for _, section := range page.Sections {
			view := picoClawUISectionView{Key: section.Key, Label: section.Label}
			for _, raw := range section.Fields {
				view.Fields = append(view.Fields, PicoClawConfigField{
					Key:      raw.Key,
					Label:    raw.Label,
					Type:     raw.Type,
					Storage:  normalizePicoClawFieldStorage(raw.Storage),
					Path:     raw.Path,
					Secret:   raw.Secret,
					Required: raw.Required,
					Options:  raw.Options,
					Default:  raw.Default,
				})
			}
			sections = append(sections, view)
		}
	}
	return sections
}

func allowedPicoClawChannelsFromConfig(cfg *config.GlobalConfig) map[string]bool {
	out := map[string]bool{}
	if cfg == nil {
		return out
	}
	pico, _ := cfg.GetPicoConfig().(map[string]interface{})
	for _, rootKey := range []string{"channel_list", "channels"} {
		channels, _ := pico[rootKey].(map[string]interface{})
		for key, raw := range channels {
			item, _ := raw.(map[string]interface{})
			if item == nil {
				continue
			}
			if enabled, ok := item["enabled"].(bool); ok && enabled {
				out[key] = true
			}
		}
	}
	return out
}

func userChannelState(configMap map[string]interface{}, securityMap map[string]interface{}, sectionKey string, fields []PicoClawConfigField) (bool, bool) {
	if sectionKey == "" {
		return false, false
	}
	enabled := false
	configured := false
	for _, field := range fields {
		source := configMap
		if field.Storage == "security" {
			source = securityMap
		}
		value, ok := deepGet(source, field.Path)
		if field.Key == "enabled" {
			if b, ok := value.(bool); ok {
				enabled = b
			}
			continue
		}
		if ok && !field.Secret && valueConfigured(value) {
			configured = true
		}
	}
	return enabled, configured
}

func supportedPicoClawChannelsForUser(pkg *PicoClawAdapterPackage, username string, configVersion int) map[string]bool {
	tag := ""
	if rec, err := auth.GetContainerByUsername(username); err == nil && rec != nil {
		tag = picoclawImageTagFromRef(rec.Image)
	}
	channels := pkg.ChannelTypesFor(configVersion, tag)
	if len(channels) == 0 {
		return nil
	}
	return stringSet(channels)
}

func picoclawImageTagFromRef(imageRef string) string {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return ""
	}
	if at := strings.LastIndex(imageRef, "@"); at >= 0 {
		imageRef = imageRef[:at]
	}
	if slash := strings.LastIndex(imageRef, "/"); slash >= 0 {
		colon := strings.LastIndex(imageRef[slash+1:], ":")
		if colon >= 0 {
			return imageRef[slash+1+colon+1:]
		}
		return ""
	}
	if colon := strings.LastIndex(imageRef, ":"); colon >= 0 {
		return imageRef[colon+1:]
	}
	return ""
}

func coercePicoClawFieldValue(field PicoClawConfigField, value interface{}) (interface{}, error) {
	fieldType := strings.ToLower(strings.TrimSpace(field.Type))
	switch fieldType {
	case "boolean", "bool":
		switch v := value.(type) {
		case bool:
			return v, nil
		case string:
			if strings.TrimSpace(v) == "" {
				return false, nil
			}
			parsed, err := strconv.ParseBool(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("字段 %s 需要布尔值", field.Key)
			}
			return parsed, nil
		default:
			return nil, fmt.Errorf("字段 %s 需要布尔值", field.Key)
		}
	case "number", "integer", "int":
		switch v := value.(type) {
		case float64:
			if fieldType == "number" {
				return v, nil
			}
			return int64(v), nil
		case int, int64, int32:
			return v, nil
		case string:
			text := strings.TrimSpace(v)
			if text == "" {
				return nil, nil
			}
			if fieldType == "number" && strings.Contains(text, ".") {
				parsed, err := strconv.ParseFloat(text, 64)
				if err != nil {
					return nil, fmt.Errorf("字段 %s 需要数字", field.Key)
				}
				return parsed, nil
			}
			parsed, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("字段 %s 需要整数", field.Key)
			}
			return parsed, nil
		default:
			return nil, fmt.Errorf("字段 %s 需要数字", field.Key)
		}
	case "string_list", "list", "array":
		switch v := value.(type) {
		case []interface{}:
			return v, nil
		case []string:
			return v, nil
		case string:
			text := strings.TrimSpace(v)
			if text == "" {
				return []string{}, nil
			}
			if strings.HasPrefix(text, "[") {
				var arr []interface{}
				if err := json.Unmarshal([]byte(text), &arr); err != nil {
					return nil, fmt.Errorf("字段 %s JSON 数组格式错误: %w", field.Key, err)
				}
				return arr, nil
			}
			parts := strings.FieldsFunc(text, func(r rune) bool {
				return r == '\n' || r == ','
			})
			out := make([]string, 0, len(parts))
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					out = append(out, part)
				}
			}
			return out, nil
		default:
			return nil, fmt.Errorf("字段 %s 需要数组", field.Key)
		}
	case "json", "object", "map":
		switch v := value.(type) {
		case map[string]interface{}, []interface{}:
			return v, nil
		case string:
			text := strings.TrimSpace(v)
			if text == "" {
				return map[string]interface{}{}, nil
			}
			var out interface{}
			if err := json.Unmarshal([]byte(text), &out); err != nil {
				return nil, fmt.Errorf("字段 %s JSON 格式错误: %w", field.Key, err)
			}
			return out, nil
		default:
			return nil, fmt.Errorf("字段 %s 需要 JSON 对象", field.Key)
		}
	default:
		if value == nil {
			return "", nil
		}
		return value, nil
	}
}

func mapFields(fields map[string]PicoClawConfigField) []PicoClawConfigField {
	out := make([]PicoClawConfigField, 0, len(fields))
	for _, field := range fields {
		out = append(out, field)
	}
	return out
}

func valueConfigured(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case nil:
		return false
	default:
		return true
	}
}

func normalizePicoClawFieldStorage(storage string) string {
	switch strings.ToLower(strings.TrimSpace(storage)) {
	case "security":
		return "security"
	default:
		return "config"
	}
}

func readJSONMap(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]interface{}{}, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s 格式错误: %w", filepath.Base(path), err)
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}

func readYAMLMap(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]interface{}{}, nil
	}
	var out map[string]interface{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s 格式错误: %w", filepath.Base(path), err)
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}
