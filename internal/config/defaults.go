package config

import (
  "fmt"

  "github.com/picoaide/picoaide/internal/auth"
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
    Image: ImageConfig{
      Name:     "ghcr.io/picoaide/picoaide",
      Timezone: "Asia/Shanghai",
      Registry: "github",
    },
    UsersRoot:   "./users",
    ArchiveRoot: "./archive",
    Web: WebConfig{
      Listen:       ":80",
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
        "install_skill": map[string]interface{}{
          "enabled": false,
        },
        "skills": map[string]interface{}{
          "registries": map[string]interface{}{
            "clawhub": map[string]interface{}{
              "enabled": false,
            },
            "github": map[string]interface{}{
              "enabled": false,
            },
          },
        },
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
    Skills: SkillsConfig{
      Repos: []SkillRepo{},
      Sources: []SkillsSourceWrapper{
        {
          Type: "registry",
          Name: "skillhub.cn",
          Reg: &RegistrySource{
            Name:                "skillhub.cn",
            DisplayName:         "SkillHub 中文技能市场",
            IndexURL:            "https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/skills.json",
            SearchURL:           "https://lightmake.site/api/v1/search",
            PrimaryDownloadURL:  "https://lightmake.site/api/v1/download?slug={slug}",
            DownloadURLTemplate: "https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/skills/{slug}.zip",
            Enabled:             true,
          },
        },
      },
    },
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
