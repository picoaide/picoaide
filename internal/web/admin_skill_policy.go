package web

import (
  "net/http"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/config"
)

// handleAdminSkillInstallPolicyGet 返回当前是否禁止用户通过三方市场安装技能
func (s *Server) handleAdminSkillInstallPolicyGet(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  pico, ok := s.cfg.PicoClaw.(map[string]interface{})
  if !ok {
    writeJSON(c, http.StatusOK, gin.H{"success": true, "disabled": true})
    return
  }

  disabled := isSkillInstallDisabled(pico)
  writeJSON(c, http.StatusOK, gin.H{"success": true, "disabled": disabled})
}

// handleAdminSkillInstallPolicySet 设置是否禁止用户通过三方市场安装技能
func (s *Server) handleAdminSkillInstallPolicySet(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  disabled := c.PostForm("disabled") == "true"

  pico, ok := s.cfg.PicoClaw.(map[string]interface{})
  if !ok {
    pico = make(map[string]interface{})
  }

  setSkillInstallDisabled(pico, disabled)
  s.cfg.PicoClaw = pico

  if err := config.SaveToDB(s.cfg, c.PostForm("changed_by")); err != nil {
    writeError(c, http.StatusInternalServerError, "保存配置失败: "+err.Error())
    return
  }

  newCfg, err := config.LoadFromDB()
  if err == nil {
    s.cfg = newCfg
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true})
}

// isSkillInstallDisabled 检查 picoclaw 配置中三个开关是否均为禁用状态
func isSkillInstallDisabled(pico map[string]interface{}) bool {
  tools, _ := pico["tools"].(map[string]interface{})
  if tools == nil {
    return true
  }

  // 检查 install_skill.enabled
  if installSkill, ok := tools["install_skill"].(map[string]interface{}); ok {
    if enabled, ok := installSkill["enabled"].(bool); ok && enabled {
      return false
    }
  }

  // 检查 skills.registries.clawhub.enabled
  if skills, ok := tools["skills"].(map[string]interface{}); ok {
    if registries, ok := skills["registries"].(map[string]interface{}); ok {
      if clawhub, ok := registries["clawhub"].(map[string]interface{}); ok {
        if enabled, ok := clawhub["enabled"].(bool); ok && enabled {
          return false
        }
      }
      if github, ok := registries["github"].(map[string]interface{}); ok {
        if enabled, ok := github["enabled"].(bool); ok && enabled {
          return false
        }
      }
    }
  }

  return true
}

// setSkillInstallDisabled 设置 picoclaw 配置中三个开关的状态
func setSkillInstallDisabled(pico map[string]interface{}, disabled bool) {
  tools, ok := pico["tools"].(map[string]interface{})
  if !ok {
    tools = make(map[string]interface{})
    pico["tools"] = tools
  }

  tools["install_skill"] = map[string]interface{}{
    "enabled": !disabled,
  }

  skills, ok := tools["skills"].(map[string]interface{})
  if !ok {
    skills = make(map[string]interface{})
    tools["skills"] = skills
  }

  registries, ok := skills["registries"].(map[string]interface{})
  if !ok {
    registries = make(map[string]interface{})
    skills["registries"] = registries
  }

  registries["clawhub"] = map[string]interface{}{
    "enabled": !disabled,
  }
  registries["github"] = map[string]interface{}{
    "enabled": !disabled,
  }
}
