package config

import (
  "fmt"
)

func DefaultGlobalConfig() *GlobalConfig {
  return &GlobalConfig{
    LDAP: LDAPConfig{
      Host:              "ldap://ldap.example.com:389",
      BindDN:            "cn=admin,dc=example,dc=com",
      BindPassword:      "your-password-here",
      BaseDN:            "ou=users,dc=example,dc=com",
      Filter:            "(objectClass=inetOrgPerson)",
      UsernameAttribute: "uid",
      SyncInterval:      "24h",
    },
    OIDC: OIDCConfig{
      Scopes:        "openid profile email",
      UsernameClaim: "preferred_username",
      GroupsClaim:   "groups",
      SyncInterval:  "0",
    },
    UsersRoot:   "./users",
    ArchiveRoot: "./archive",
    Web: WebConfig{
      Listen:       ":80",
      LogRetention: "6m",
      DebugMode:    false,
    },
    Security: map[string]interface{}{
      "model_list": map[string]interface{}{
        "gpt-5.4:0": map[string]interface{}{
          "api_keys": []interface{}{"sk-openai-replace-me"},
        },
      },
    },
    Skills: SkillsConfig{
      Sources: []SkillsSourceWrapper{},
    },
  }
}

// InitDBDefaults 将默认配置写入数据库（不覆盖已有值）
func InitDBDefaults() error {
  cfg := DefaultGlobalConfig()

  engine, err := getEngine()
  if err != nil {
    return fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  session := engine.NewSession()
  defer session.Close()

  if err := session.Begin(); err != nil {
    return fmt.Errorf("开启事务失败: %w", err)
  }

  raw := structToRaw(cfg)
  flat := flattenConfig(raw)
  for key, value := range flat {
    if _, err := session.Exec(
      "INSERT OR IGNORE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now','localtime'))",
      key, value,
    ); err != nil {
      return fmt.Errorf("写入默认配置失败: %w", err)
    }
  }

  return session.Commit()
}
