package web

import (
  "net/http"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/config"
)

func (s *Server) handleAdminSkillInstallPolicyGet(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true, "disabled": false})
}

func (s *Server) handleAdminSkillInstallPolicySet(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  disabled := c.PostForm("disabled") == "true"

  // 加载当前配置（嵌套结构）
  rawCfg, err := config.LoadRawFromDB()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "加载配置失败: "+err.Error())
    return
  }

  // 确保 tools.install_skill 路径存在
  tools, _ := rawCfg["tools"].(map[string]interface{})
  if tools == nil {
    tools = make(map[string]interface{})
    rawCfg["tools"] = tools
  }
  installSkill, _ := tools["install_skill"].(map[string]interface{})
  if installSkill == nil {
    installSkill = make(map[string]interface{})
    tools["install_skill"] = installSkill
  }
  installSkill["enabled"] = !disabled

  // 保存合并后的配置
  if err := config.SaveRawToDB(rawCfg, c.PostForm("changed_by")); err != nil {
    writeError(c, http.StatusInternalServerError, "保存配置失败: "+err.Error())
    return
  }

  // 重载配置到内存
  newCfg, err := config.LoadFromDB()
  if err == nil {
    s.cfg.Store(newCfg)
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}
