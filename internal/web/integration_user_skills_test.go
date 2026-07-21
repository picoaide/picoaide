package web

import (
  "encoding/json"
  "os"
  "path/filepath"
  "testing"

  "github.com/picoaide/picoaide/internal/skill"
  "github.com/picoaide/picoaide/internal/store"
)

// createTestSkill 在测试环境下创建一个技能目录并写入 SKILL.md
func createTestSkill(t *testing.T, env *testEnv, sourceName, skillName string) {
  t.Helper()
  dir := filepath.Join(skill.SkillsRootDir(), sourceName, skillName)
  if err := os.MkdirAll(dir, 0755); err != nil {
    t.Fatalf("MkdirAll skill dir: %v", err)
  }
  content := "---\nname: " + skillName + "\ndescription: Test skill for " + skillName + "\n---\n# Content\n"
  if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
    t.Fatalf("WriteFile SKILL.md: %v", err)
  }
}

// TestAdminSkillsList_DeployedUsers 验证管理端技能列表返回 deployed_users 字段
func TestAdminSkillsList_DeployedUsers(t *testing.T) {
  env := setupTestServer(t)
  skillName := "deploy-skill"
  sourceName := "test-source"

  createTestSkill(t, env, sourceName, skillName)

  // 部署技能给 testuser（带上 source）
  resp := env.postMap(t, "/api/admin/skills/deploy", "testadmin", map[string]interface{}{
    "skill_name": skillName,
    "username":   "testuser",
    "source":     sourceName,
  })
  assertStatus(t, resp, 200)

  // 查询管理端技能列表
  resp = env.get(t, "/api/admin/skills", "testadmin")
  assertStatus(t, resp, 200)

  var result struct {
    Success bool `json:"success"`
    Skills  []struct {
      Name          string   `json:"name"`
      DeployedUsers []string `json:"deployed_users"`
    } `json:"skills"`
  }
  if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  resp.Body.Close()

  if !result.Success {
    t.Fatal("success = false")
  }

  var found bool
  for _, sk := range result.Skills {
    if sk.Name == skillName {
      found = true
      if len(sk.DeployedUsers) == 0 {
        t.Errorf("deploy-skill.deployed_users = 空, 期望包含 testuser")
      } else {
        var hasUser bool
        for _, u := range sk.DeployedUsers {
          if u == "testuser" {
            hasUser = true
            break
          }
        }
        if !hasUser {
          t.Errorf("deploy-skill.deployed_users = %v, 期望包含 testuser", sk.DeployedUsers)
        }
      }
      break
    }
  }
  if !found {
    t.Errorf("技能 %q 未在管理端列表中找到", skillName)
  }

  // 验证未部署的技能 deployed_users 为空
  resp = env.get(t, "/api/admin/skills", "testadmin")
  assertStatus(t, resp, 200)
  var list2 struct {
    Skills []struct {
      Name          string   `json:"name"`
      DeployedUsers []string `json:"deployed_users"`
    } `json:"skills"`
  }
  if err := json.NewDecoder(resp.Body).Decode(&list2); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  resp.Body.Close()
}

// TestUserSkillUninstall_AdminDeployed 验证管理员部署的技能不能由用户卸载
func TestUserSkillUninstall_AdminDeployed(t *testing.T) {
  env := setupTestServer(t)
  skillName := "admin-only-skill"
  sourceName := "admin-source"

  createTestSkill(t, env, sourceName, skillName)

  // 管理员部署给 testuser
  resp := env.postMap(t, "/api/admin/skills/deploy", "testadmin", map[string]interface{}{
    "skill_name": skillName,
    "username":   "testuser",
    "source":     sourceName,
  })
  assertStatus(t, resp, 200)

  // 验证 skill 确实已绑定
  src, err := store.GetUserSkillSource("testuser", skillName)
  if err != nil {
    t.Fatalf("GetUserSkillSource: %v", err)
  }
  if src != sourceName {
    t.Fatalf("source = %q, 期望 %q", src, sourceName)
  }

  // testuser 尝试卸载 → 应拒绝
  resp = env.postMap(t, "/api/user/skills/uninstall", "testuser", map[string]interface{}{
    "skill_name": skillName,
  })
  if resp.StatusCode != 403 {
    t.Errorf("status=%d, 期望 403（管理员安装的技能不允许卸载）", resp.StatusCode)
  }
}

// TestUserSkillUninstall_SelfInstalled 验证用户自安装的技能可以卸载
func TestUserSkillUninstall_SelfInstalled(t *testing.T) {
  env := setupTestServer(t)
  skillName := "self-skill"
  sourceName := "self-source"

  createTestSkill(t, env, sourceName, skillName)

  // testuser 自安装
  resp := env.postMap(t, "/api/user/skills/install", "testuser", map[string]interface{}{
    "skill_name": skillName,
  })
  assertStatus(t, resp, 200)

  // 验证 source = "self"
  src, err := store.GetUserSkillSource("testuser", skillName)
  if err != nil {
    t.Fatalf("GetUserSkillSource: %v", err)
  }
  if src != "self" {
    t.Fatalf("source = %q, 期望 %q", src, "self")
  }

  // testuser 卸载 → 应成功
  resp = env.postMap(t, "/api/user/skills/uninstall", "testuser", map[string]interface{}{
    "skill_name": skillName,
  })
  assertStatus(t, resp, 200)

  // 验证已解除绑定
  src, _ = store.GetUserSkillSource("testuser", skillName)
  if src != "" {
    t.Errorf("卸载后 source = %q, 期望为空", src)
  }
}

// TestUserSkills_InstallStatus 验证不同安装方式的技能状态正确
func TestUserSkills_InstallStatus(t *testing.T) {
  env := setupTestServer(t)
  adminSkill := "admin-skill"
  selfSkill := "self-skill"
  sourceName := "deploy-source"

  // 创建两个技能
  createTestSkill(t, env, sourceName, adminSkill)
  createTestSkill(t, env, sourceName, selfSkill)

  // 管理员部署 adminSkill
  resp := env.postMap(t, "/api/admin/skills/deploy", "testadmin", map[string]interface{}{
    "skill_name": adminSkill,
    "username":   "testuser",
    "source":     sourceName,
  })
  assertStatus(t, resp, 200)

  // 用户自安装 selfSkill
  resp = env.postMap(t, "/api/user/skills/install", "testuser", map[string]interface{}{
    "skill_name": selfSkill,
  })
  assertStatus(t, resp, 200)

  // 查询用户技能列表
  resp = env.get(t, "/api/user/skills", "testuser")
  assertStatus(t, resp, 200)

  var result struct {
    Success bool `json:"success"`
    Skills  []struct {
      Name          string `json:"name"`
      InstallStatus string `json:"install_status"`
      UserInstalled bool   `json:"user_installed"`
    } `json:"skills"`
  }
  if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  resp.Body.Close()

  if !result.Success {
    t.Fatal("success = false")
  }

  for _, sk := range result.Skills {
    switch sk.Name {
    case adminSkill:
      if sk.InstallStatus != "installed" {
        t.Errorf("admin-skill install_status = %q, 期望 %q", sk.InstallStatus, "installed")
      }
      if sk.UserInstalled {
        t.Error("admin-skill user_installed = true, 期望 false（管理员部署）")
      }
    case selfSkill:
      if sk.InstallStatus != "installed" {
        t.Errorf("self-skill install_status = %q, 期望 %q", sk.InstallStatus, "installed")
      }
      if !sk.UserInstalled {
        t.Error("self-skill user_installed = false, 期望 true（用户自安装）")
      }
    }
  }
}
