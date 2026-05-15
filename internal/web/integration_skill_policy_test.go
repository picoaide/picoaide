package web

import (
  "net/url"
  "testing"

  "github.com/picoaide/picoaide/internal/config"
)

func TestSkillInstallPolicy_DefaultIsDisabled(t *testing.T) {
  env := setupTestServer(t)

  resp := env.get(t, "/api/admin/skill-install-policy", "testadmin")
  assertStatus(t, resp, 200)

  var result struct {
    Disabled bool `json:"disabled"`
  }
  parseJSON(t, resp, &result)

  if !result.Disabled {
    t.Error("默认应禁止用户安装技能")
  }
}

func TestSkillInstallPolicy_NonAdminGetsForbidden(t *testing.T) {
  env := setupTestServer(t)

  resp := env.get(t, "/api/admin/skill-install-policy", "testuser")
  assertStatus(t, resp, 403)
}

func TestSkillInstallPolicy_ToggleEnableAndDisable(t *testing.T) {
  env := setupTestServer(t)

  // 默认禁用
  resp := env.get(t, "/api/admin/skill-install-policy", "testadmin")
  assertStatus(t, resp, 200)
  var result struct {
    Disabled bool `json:"disabled"`
  }
  parseJSON(t, resp, &result)
  if !result.Disabled {
    t.Fatal("初始应为禁用状态")
  }

  // 开启：允许安装
  resp = env.postForm(t, "/api/admin/skill-install-policy", "testadmin", url.Values{
    "disabled": {"false"},
  })
  assertStatus(t, resp, 200)

  resp = env.get(t, "/api/admin/skill-install-policy", "testadmin")
  assertStatus(t, resp, 200)
  parseJSON(t, resp, &result)
  if result.Disabled {
    t.Error("开启后 disabled 应为 false")
  }

  // 再次关闭
  resp = env.postForm(t, "/api/admin/skill-install-policy", "testadmin", url.Values{
    "disabled": {"true"},
  })
  assertStatus(t, resp, 200)

  resp = env.get(t, "/api/admin/skill-install-policy", "testadmin")
  assertStatus(t, resp, 200)
  parseJSON(t, resp, &result)
  if !result.Disabled {
    t.Error("关闭后 disabled 应为 true")
  }
}

func TestSkillInstallPolicy_VerifyPersistsInDB(t *testing.T) {
  env := setupTestServer(t)

  // 开启
  env.postForm(t, "/api/admin/skill-install-policy", "testadmin", url.Values{
    "disabled": {"false"},
  })

  // 从数据库重新加载配置
  cfg, err := config.LoadFromDB()
  if err != nil {
    t.Fatalf("LoadFromDB: %v", err)
  }

  pico, ok := cfg.PicoClaw.(map[string]interface{})
  if !ok {
    t.Fatal("PicoClaw should be a map")
  }
  tools, _ := pico["tools"].(map[string]interface{})
  if tools == nil {
    t.Fatal("tools should exist")
  }

  // 验证 install_skill.enabled = true
  installSkill, _ := tools["install_skill"].(map[string]interface{})
  if installSkill == nil {
    t.Fatal("install_skill should exist")
  }
  if enabled, ok := installSkill["enabled"].(bool); !ok || !enabled {
    t.Error("install_skill.enabled 应为 true")
  }

  // 验证 skills.registries
  skills, _ := tools["skills"].(map[string]interface{})
  if skills == nil {
    t.Fatal("skills should exist")
  }
  registries, _ := skills["registries"].(map[string]interface{})
  if registries == nil {
    t.Fatal("registries should exist")
  }

  for _, name := range []string{"clawhub", "github"} {
    reg, _ := registries[name].(map[string]interface{})
    if reg == nil {
      t.Fatalf("registries.%s should exist", name)
    }
    if enabled, ok := reg["enabled"].(bool); !ok || !enabled {
      t.Errorf("registries.%s.enabled 应为 true", name)
    }
  }
}
