package config

import (
  "encoding/json"
  "fmt"
  "strconv"
  "strings"

  "github.com/picoaide/picoaide/internal/auth"
)

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
