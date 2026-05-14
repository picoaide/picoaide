package web

import (
  "fmt"
  "log/slog"
  "net/http"
  "os"
  "path/filepath"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/skill"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 用户技能中心
// ============================================================

// SkillStatus 技能状态
type SkillStatus struct {
  skill.SkillInfo
  InstallStatus string `json:"install_status"` // "installed" / "group" / "available"
  UserInstalled bool   `json:"user_installed"`  // 用户自安装（source="self"，可卸载）
}

func (s *Server) handleUserSkills(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  allSkills, err := skill.ListAllSkills()
  if err != nil {
    allSkills = []skill.SkillInfo{}
  }

  result := make([]SkillStatus, 0, len(allSkills))
  for _, sk := range allSkills {
    status := "available"
    userInstalled := false

    src, _ := auth.GetUserSkillSource(username, sk.Name)
    if src == "self" {
      status = "installed"
      userInstalled = true
    } else if src != "" {
      status = "installed"
    } else if has, _ := auth.UserHasSkillFromAnySource(username, sk.Name); has {
      status = "group"
    }

    result = append(result, SkillStatus{
      SkillInfo:     sk,
      InstallStatus: status,
      UserInstalled: userInstalled,
    })
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "skills":  result,
  })
}

func (s *Server) handleUserSkillsInstall(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  skillName := strings.TrimSpace(c.PostForm("skill_name"))
  if skillName == "" {
    writeError(c, http.StatusBadRequest, "技能名称不能为空")
    return
  }
  if err := util.SafePathSegment(skillName); err != nil {
    writeError(c, http.StatusBadRequest, "技能名称不合法: "+err.Error())
    return
  }

  source := findSkillSource(skillName)
  if source == "" {
    writeError(c, http.StatusBadRequest, "技能不存在")
    return
  }

  has, _ := auth.UserHasSkillFromAnySource(username, skillName)
  if has {
    writeError(c, http.StatusBadRequest, "你已拥有此技能")
    return
  }

  if err := s.deploySkillToUser(skillName, username); err != nil {
    writeError(c, http.StatusInternalServerError, "安装失败: "+err.Error())
    return
  }

  if err := auth.BindSkillToUser(username, skillName, "self"); err != nil {
    slog.Error("绑定记录写入失败（文件已部署）", "skill", skillName, "username", username, "error", err)
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": fmt.Sprintf("技能 %s 安装成功", skillName),
  })
}

func (s *Server) handleUserSkillsUninstall(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  skillName := strings.TrimSpace(c.PostForm("skill_name"))
  if skillName == "" {
    writeError(c, http.StatusBadRequest, "技能名称不能为空")
    return
  }
  if err := util.SafePathSegment(skillName); err != nil {
    writeError(c, http.StatusBadRequest, "技能名称不合法: "+err.Error())
    return
  }

  src, err := auth.GetUserSkillSource(username, skillName)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询技能状态失败")
    return
  }
  if src == "" {
    writeError(c, http.StatusBadRequest, "你没有安装此技能")
    return
  }
  if src != "self" {
    writeError(c, http.StatusForbidden, "管理员安装的技能不允许卸载")
    return
  }

  if err := auth.UnbindSkillFromUser(username, skillName); err != nil {
    writeError(c, http.StatusInternalServerError, "解绑失败: "+err.Error())
    return
  }

  removeUserSkillDir(s.cfg, username, skillName)

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": fmt.Sprintf("技能 %s 已卸载", skillName),
  })
}

func removeUserSkillDir(cfg *config.GlobalConfig, username, skillName string) {
  if err := util.SafePathSegment(skillName); err != nil {
    slog.Error("删除技能目录时校验失败", "skill", skillName, "error", err)
    return
  }
  targetDir := filepath.Join(user.UserDir(cfg, username), ".picoclaw", "workspace", "skills", skillName)
  if _, err := os.Stat(targetDir); os.IsNotExist(err) {
    return
  }
  if err := os.RemoveAll(targetDir); err != nil {
    slog.Error("删除技能目录失败", "skill", skillName, "username", username, "error", err)
  }
}
