package web

import (
  "fmt"
  "log/slog"
  "net/http"
  "os"
  "path/filepath"
  "sort"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/skill"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 技能查找工具
// ============================================================

// findSkillSource 在 skills/ 下查找技能所属的源
func findSkillSource(skillName string) string {
  if err := util.SafePathSegment(skillName); err != nil {
    return ""
  }
  root := skill.SkillsRootDir()
  entries, err := os.ReadDir(root)
  if err != nil {
    return ""
  }
  for _, e := range entries {
    if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
      continue
    }
    skillPath := filepath.Join(root, e.Name(), skillName, "SKILL.md")
    if _, err := os.Stat(skillPath); err == nil {
      return e.Name()
    }
  }
  return ""
}

// ============================================================
// 技能库管理
// ============================================================

func (s *Server) handleAdminSkills(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  source := strings.TrimSpace(c.Query("source"))
  var allSkills []skill.SkillInfo
  var err error
  if source != "" {
    allSkills, err = skill.ListSourceSkills(source)
  } else {
    allSkills, err = skill.ListAllSkills()
  }
  if err != nil {
    allSkills = []skill.SkillInfo{}
  }

  pager := parsePagination(c, 50, 200)
  if pager.Search != "" {
    filtered := allSkills[:0]
    for _, sk := range allSkills {
      if strings.Contains(strings.ToLower(sk.Name), pager.Search) ||
        strings.Contains(strings.ToLower(sk.Description), pager.Search) {
        filtered = append(filtered, sk)
      }
    }
    allSkills = filtered
  }

  // 默认技能排前面
  defaults, _ := auth.LoadDefaultSkills()
  defaultSet := make(map[string]bool, len(defaults))
  for _, d := range defaults {
    defaultSet[d] = true
  }
  sort.SliceStable(allSkills, func(i, j int) bool {
    di, dj := defaultSet[allSkills[i].Name], defaultSet[allSkills[j].Name]
    if di != dj {
      return di
    }
    return allSkills[i].Name < allSkills[j].Name
  })

  pageSkills, total, totalPages, page, pageSize := paginateSlice(allSkills, pager)

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":     true,
    "skills":      pageSkills,
    "total":       total,
    "page":        page,
    "page_size":   pageSize,
    "total_pages": totalPages,
  })
}

// ============================================================
// 部署
// ============================================================

func (s *Server) deploySkillToUser(skillName, username string) error {
  if err := util.SafePathSegment(skillName); err != nil {
    return fmt.Errorf("技能名不合法: %w", err)
  }
  source := findSkillSource(skillName)
  if source == "" {
    return fmt.Errorf("技能 %s 未在任何源中找到", skillName)
  }
  srcPath := filepath.Clean(filepath.Join(skill.SkillsRootDir(), source, skillName))
  targetDir := filepath.Clean(filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills", skillName))
  skillsBase := filepath.Clean(filepath.Join(skill.SkillsRootDir(), source))
  userSkillsBase := filepath.Clean(filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills"))
  if !strings.HasPrefix(srcPath, skillsBase+string(os.PathSeparator)) || !strings.HasPrefix(targetDir, userSkillsBase+string(os.PathSeparator)) {
    return fmt.Errorf("非法技能路径")
  }
  if err := os.RemoveAll(targetDir); err != nil {
    return fmt.Errorf("删除旧技能目录失败: %w", err)
  }
  if err := util.CopyDir(srcPath, targetDir); err != nil {
    return fmt.Errorf("复制技能失败: %w", err)
  }
  return nil
}

func (s *Server) handleAdminSkillsDeploy(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  skillName := strings.TrimSpace(c.PostForm("skill_name"))
  targetUser := strings.TrimSpace(c.PostForm("username"))
  targetGroup := strings.TrimSpace(c.PostForm("group_name"))
  skillSource := strings.TrimSpace(c.PostForm("source"))

  if skillName == "" {
    writeError(c, http.StatusBadRequest, "技能名称不能为空")
    return
  }
  if err := util.SafePathSegment(skillName); err != nil {
    writeError(c, http.StatusBadRequest, "技能名称不合法: "+err.Error())
    return
  }
  if targetUser != "" {
    if err := user.ValidateUsername(targetUser); err != nil {
      writeError(c, http.StatusBadRequest, err.Error())
      return
    }
  }

  if findSkillSource(skillName) == "" {
    writeError(c, http.StatusBadRequest, "技能不存在")
    return
  }

  deployFn := func(username string) error {
    if err := s.deploySkillToUser(skillName, username); err != nil {
      return err
    }
    return auth.BindSkillToUser(username, skillName, skillSource)
  }

  if targetUser != "" {
    if err := deployFn(targetUser); err != nil {
      writeError(c, http.StatusInternalServerError, err.Error())
      return
    }
    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success":     true,
      "message":     fmt.Sprintf("已将技能 %s 部署到 %s", skillName, targetUser),
      "skill_count": 1,
      "user_count":  1,
    })
    return
  }

  var targets []string
  var getErr error
  if targetGroup != "" {
    targets, getErr = auth.GetGroupMembersForDeploy(targetGroup)
    if getErr != nil {
      writeError(c, http.StatusBadRequest, "获取组成员失败: "+getErr.Error())
      return
    }
    if len(targets) == 0 {
      writeError(c, http.StatusBadRequest, "该组没有可部署的用户")
      return
    }
  } else {
    targets, getErr = user.GetUserList(s.cfg)
    if getErr != nil {
      writeError(c, http.StatusInternalServerError, "获取用户列表失败: "+getErr.Error())
      return
    }
  }

  taskID, err := enqueueTask("skills-deploy", targets, deployFn)
  if err != nil {
    writeError(c, http.StatusConflict, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":     true,
    "message":     fmt.Sprintf("已提交技能部署任务，共 %d 个用户", len(targets)),
    "task_id":     taskID,
    "skill_count": 1,
  })
}

// ============================================================
// 删除
// ============================================================

func (s *Server) handleAdminSkillsRemove(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }
  name := strings.TrimSpace(c.PostForm("name"))
  if name == "" {
    writeError(c, http.StatusBadRequest, "技能名称不能为空")
    return
  }
  if err := util.SafePathSegment(name); err != nil {
    writeError(c, http.StatusBadRequest, "技能名称不合法")
    return
  }

  // 在所有源中搜索匹配 SKILL.md name 的技能（目录名可能和 name 不同）
  var foundSource, foundDir string
  skillsRoot := skill.SkillsRootDir()
  srcEntries, _ := os.ReadDir(skillsRoot)
  for _, se := range srcEntries {
    if !se.IsDir() || strings.HasPrefix(se.Name(), ".") {
      continue
    }
    srcDir := filepath.Join(skillsRoot, se.Name())
    skEntries, _ := os.ReadDir(srcDir)
    for _, sk := range skEntries {
      if !sk.IsDir() {
        continue
      }
      meta, pErr := skill.ParseMetadata(filepath.Join(srcDir, sk.Name()))
      if pErr == nil && meta.Name == name {
        foundSource = se.Name()
        foundDir = sk.Name()
        break
      }
    }
    if foundSource != "" {
      break
    }
  }
  if foundSource == "" {
    // 兜底：按目录名查找
    foundSource = findSkillSource(name)
    if foundSource == "" {
      writeError(c, http.StatusBadRequest, "技能不存在")
      return
    }
    foundDir = name
  }

  affectedUsers, _ := auth.GetUsersForSkill(name)

  skillPath := filepath.Join(skill.SkillsRootDir(), foundSource, foundDir)
  if err := os.RemoveAll(skillPath); err != nil {
    writeError(c, http.StatusInternalServerError, "删除目录失败: "+err.Error())
    return
  }

  if err := auth.DeleteSkill(name); err != nil {
    slog.Error("删除技能 DB 记录失败（目录已删除）", "skill", name, "error", err)
  }

  // 从默认技能列表中移除
  if err := auth.RemoveFromDefaultSkills(name); err != nil {
    slog.Error("从默认列表移除失败", "skill", name, "error", err)
  }

  for _, username := range affectedUsers {
    targetDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills", name)
    os.RemoveAll(targetDir)
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":       true,
    "message":       fmt.Sprintf("技能 %s 已删除，已清理 %d 个用户", name, len(affectedUsers)),
    "cleaned_users": len(affectedUsers),
  })
}

// ============================================================
// 用户直接绑定/解绑
// ============================================================

func (s *Server) handleAdminSkillsUserBind(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  skillName := strings.TrimSpace(c.PostForm("skill_name"))
  username := strings.TrimSpace(c.PostForm("username"))
  skillSource := strings.TrimSpace(c.PostForm("source"))

  if skillName == "" || username == "" {
    writeError(c, http.StatusBadRequest, "技能名和用户名不能为空")
    return
  }
  if err := util.SafePathSegment(skillName); err != nil {
    writeError(c, http.StatusBadRequest, "技能名称不合法")
    return
  }
  if err := user.ValidateUsername(username); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  if err := s.deploySkillToUser(skillName, username); err != nil {
    writeError(c, http.StatusInternalServerError, "部署失败: "+err.Error())
    return
  }

  if err := auth.BindSkillToUser(username, skillName, skillSource); err != nil {
    slog.Error("绑定记录写入失败（文件已部署）", "skill", skillName, "username", username, "error", err)
  }

  writeSuccess(c, fmt.Sprintf("技能 %s 已绑定到 %s", skillName, username))
}

func (s *Server) handleAdminSkillsUserUnbind(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  skillName := strings.TrimSpace(c.PostForm("skill_name"))
  username := strings.TrimSpace(c.PostForm("username"))

  if skillName == "" || username == "" {
    writeError(c, http.StatusBadRequest, "技能名和用户名不能为空")
    return
  }
  if err := util.SafePathSegment(skillName); err != nil {
    writeError(c, http.StatusBadRequest, "技能名称不合法")
    return
  }
  if err := user.ValidateUsername(username); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  if err := auth.UnbindSkillFromUser(username, skillName); err != nil {
    writeError(c, http.StatusInternalServerError, "解绑失败: "+err.Error())
    return
  }

  has, err := auth.UserHasSkillFromAnySource(username, skillName)
  if err == nil && !has {
    targetDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills", skillName)
    os.RemoveAll(targetDir)
  }

  writeSuccess(c, fmt.Sprintf("已从 %s 解绑技能 %s", username, skillName))
}

// ============================================================
// 用户技能来源查询
// ============================================================

func (s *Server) handleAdminSkillsUserSources(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  username := c.Query("username")
  skillName := c.Query("skill_name")
  if username == "" || skillName == "" {
    writeError(c, http.StatusBadRequest, "username 和 skill_name 不能为空")
    return
  }
  sources, _ := auth.GetUserAllSkillSources(username, skillName)
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "sources": sources,
  })
}

// ============================================================
// 默认技能
// ============================================================

func (s *Server) handleAdminSkillsDefaults(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  skills, err := auth.LoadDefaultSkills()
  if err != nil {
    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success": true,
      "skills":  []string{},
    })
    return
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "skills":  skills,
  })
}

func (s *Server) handleAdminSkillsDefaultsToggle(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }
  name := strings.TrimSpace(c.PostForm("skill_name"))
  if name == "" {
    writeError(c, http.StatusBadRequest, "技能名称不能为空")
    return
  }
  skills, err := auth.ToggleDefaultSkill(name)
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "skills":  skills,
  })
}

// applyDefaultSkillsToUser 为新用户绑定并部署默认技能
func (s *Server) applyDefaultSkillsToUser(username string) {
  skills, err := auth.LoadDefaultSkills()
  if err != nil {
    return
  }
  for _, skillName := range skills {
    if err := util.SafePathSegment(skillName); err != nil {
      continue
    }
    if err := s.deploySkillToUser(skillName, username); err != nil {
      slog.Warn("部署默认技能失败", "skill", skillName, "username", username, "error", err)
      continue
    }
    if err := auth.BindSkillToUser(username, skillName, "default"); err != nil {
      slog.Error("绑定默认技能失败", "skill", skillName, "username", username, "error", err)
    }
  }
}

// ============================================================
// 辅助函数
// ============================================================

func formatSize(size int64) string {
  if size < 1024 {
    return fmt.Sprintf("%d B", size)
  }
  if size < 1024*1024 {
    return fmt.Sprintf("%.1f KB", float64(size)/1024)
  }
  return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}
