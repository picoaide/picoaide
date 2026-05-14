package web

import (
  "encoding/json"
  "fmt"
  "log/slog"
  "net/http"
  "os"
  "path/filepath"
  "strings"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/skill"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 技能源管理 API
// ============================================================

// handleAdminSkillsSources 列出所有技能源
func (s *Server) handleAdminSkillsSources(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  type SourceInfo struct {
    Name        string `json:"name"`
    Type        string `json:"type"`
    DisplayName string `json:"display_name,omitempty"`
    URL         string `json:"url,omitempty"`
    Ref         string `json:"ref,omitempty"`
    RefType     string `json:"ref_type,omitempty"`
    Enabled     bool   `json:"enabled"`
    LastRefresh string `json:"last_refresh,omitempty"`
    LastPull    string `json:"last_pull,omitempty"`
    SkillCount  int    `json:"skill_count"`
  }

  var sources []SourceInfo
  for _, sw := range s.cfg.Skills.Sources {
    skills, _ := skill.ListSourceSkills(sw.Name)
    info := SourceInfo{
      Name:       sw.Name,
      Type:       sw.Type,
      Enabled:    true,
      SkillCount: len(skills),
    }
    if sw.Reg != nil {
      info.DisplayName = sw.Reg.DisplayName
      info.URL = sw.Reg.IndexURL
      info.LastRefresh = sw.Reg.LastRefresh
    }
    if sw.Git != nil {
      info.URL = sw.Git.URL
      info.Ref = sw.Git.Ref
      info.RefType = sw.Git.RefType
      info.LastPull = sw.Git.LastPull
    }
    sources = append(sources, info)
  }
  if sources == nil {
    sources = []SourceInfo{}
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "sources": sources,
  })
}

// handleAdminSkillsSourcesGitAdd 添加 Git 源
func (s *Server) handleAdminSkillsSourcesGitAdd(c *gin.Context) {
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
  repoURL := strings.TrimSpace(c.PostForm("url"))
  ref := strings.TrimSpace(c.PostForm("ref"))
  refType := strings.TrimSpace(c.PostForm("ref_type"))

  if name == "" || repoURL == "" {
    writeError(c, http.StatusBadRequest, "名称和 URL 不能为空")
    return
  }
  if err := util.SafePathSegment(name); err != nil {
    writeError(c, http.StatusBadRequest, "名称不合法: "+err.Error())
    return
  }

  targetDir := filepath.Join(skill.SkillsRootDir(), name)
  if _, err := os.Stat(targetDir); err == nil {
    writeError(c, http.StatusBadRequest, "源目录已存在")
    return
  }

  os.MkdirAll(skill.SkillsRootDir(), 0755)

  if err := skill.CloneGitSource(name, repoURL, ref, refType); err != nil {
    writeError(c, http.StatusInternalServerError, "Git clone 失败: "+err.Error())
    return
  }

  found, _ := skill.RescanSource(name)
  if len(found) == 0 {
    writeError(c, http.StatusBadRequest, "仓库中未发现任何有效技能（需含 SKILL.md 的子目录）")
    os.RemoveAll(targetDir)
    return
  }

  gitSource := &config.GitSource{
    Name:    name,
    URL:     repoURL,
    Ref:     ref,
    RefType: refType,
    Enabled: true,
  }
  s.cfg.Skills.Sources = append(s.cfg.Skills.Sources, config.SkillsSourceWrapper{
    Type: "git",
    Name: name,
    Git:  gitSource,
  })
  s.saveSkillsConfig()

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":  true,
    "message":  fmt.Sprintf("Git 源 %s 已添加，发现 %d 个技能：%s", name, len(found), strings.Join(found, "、")),
    "skills":   found,
  })
}

// handleAdminSkillsSourcesRemove 删除技能源
func (s *Server) handleAdminSkillsSourcesRemove(c *gin.Context) {
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
    writeError(c, http.StatusBadRequest, "源名称不能为空")
    return
  }
  if err := util.SafePathSegment(name); err != nil {
    writeError(c, http.StatusBadRequest, "源名称不合法: "+err.Error())
    return
  }

  // 不允许删除 skillhub.cn
  if name == "skillhub.cn" {
    writeError(c, http.StatusBadRequest, "内置源 skillhub.cn 不可删除")
    return
  }

  // 解绑所有来自该源的技能
  skills, _ := skill.ListSourceSkills(name)
  for _, sk := range skills {
    auth.DeleteSkill(sk.Name)
    users, _ := auth.GetUsersForSkill(sk.Name)
    for _, username := range users {
      targetDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills", sk.Name)
      os.RemoveAll(targetDir)
    }
  }

  // 删目录
  sourceDir := filepath.Join(skill.SkillsRootDir(), name)
  os.RemoveAll(sourceDir)

  // 删配置
  var filtered []config.SkillsSourceWrapper
  for _, sw := range s.cfg.Skills.Sources {
    if sw.Name != name {
      filtered = append(filtered, sw)
    }
  }
  s.cfg.Skills.Sources = filtered
  s.saveSkillsConfig()

  writeSuccess(c, fmt.Sprintf("源 %s 已删除，已清理 %d 个关联技能", name, len(skills)))
}

// handleAdminSkillsSourcesPull Git 源拉取更新
func (s *Server) handleAdminSkillsSourcesPull(c *gin.Context) {
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
    writeError(c, http.StatusBadRequest, "源名称不能为空")
    return
  }

  var gs *config.GitSource
  for _, sw := range s.cfg.Skills.Sources {
    if sw.Name == name && sw.Git != nil {
      gs = sw.Git
      break
    }
  }
  if gs == nil {
    writeError(c, http.StatusBadRequest, "未找到 Git 源: "+name)
    return
  }

  result, err := skill.PullGitSource(name, gs.Ref, gs.RefType)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "拉取失败: "+err.Error())
    return
  }

  // 对更新和新增的技能，重新部署到已绑用户
  needsDeploy := append(result.Added, result.Updated...)
  for _, skillName := range needsDeploy {
    users, _ := auth.GetUsersForSkill(skillName)
    for _, username := range users {
      if err := s.deploySkillToUser(skillName, username); err != nil {
        slog.Warn("自动重部署失败", "skill", skillName, "username", username, "error", err)
      }
    }
  }

  // 更新 LastPull
  s.cfg.Skills.Sources = updateSourceLastPull(s.cfg.Skills.Sources, name)
  s.saveSkillsConfig()

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": fmt.Sprintf("已更新 %s：新增 %d、更新 %d、移除 %d",
      name, len(result.Added), len(result.Updated), len(result.Removed)),
    "result": result,
  })
}

// handleAdminSkillsSourcesRefresh 刷新注册源索引
func (s *Server) handleAdminSkillsSourcesRefresh(c *gin.Context) {
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
    writeError(c, http.StatusBadRequest, "源名称不能为空")
    return
  }

  var rs *config.RegistrySource
  for _, sw := range s.cfg.Skills.Sources {
    if sw.Name == name && sw.Reg != nil {
      rs = sw.Reg
      break
    }
  }
  if rs == nil {
    writeError(c, http.StatusBadRequest, "未找到注册源: "+name)
    return
  }

  index, err := skill.FetchIndex(rs.IndexURL)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "刷新索引失败: "+err.Error())
    return
  }

  // 更新 LastRefresh
  s.cfg.Skills.Sources = updateSourceLastRefresh(s.cfg.Skills.Sources, name)
  s.saveSkillsConfig()

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": fmt.Sprintf("索引已刷新，共 %d 个技能", index.Total),
    "total":   index.Total,
  })
}

// handleAdminSkillsRegistryInstall 从注册源安装技能
func (s *Server) handleAdminSkillsRegistryInstall(c *gin.Context) {
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

  sourceName := strings.TrimSpace(c.PostForm("source"))
  slug := strings.TrimSpace(c.PostForm("slug"))
  if sourceName == "" || slug == "" {
    writeError(c, http.StatusBadRequest, "源名称和技能 slug 不能为空")
    return
  }

  var rs *config.RegistrySource
  for _, sw := range s.cfg.Skills.Sources {
    if sw.Name == sourceName && sw.Reg != nil {
      rs = sw.Reg
      break
    }
  }
  if rs == nil {
    writeError(c, http.StatusBadRequest, "未找到注册源: "+sourceName)
    return
  }

  if err := skill.DownloadAndInstall(sourceName, slug,
    rs.PrimaryDownloadURL, rs.DownloadURLTemplate, ""); err != nil {
    writeError(c, http.StatusInternalServerError, "安装失败: "+err.Error())
    return
  }

  meta, pErr := skill.ParseAndValidate(filepath.Join(skill.SkillsRootDir(), sourceName, slug))
  if pErr != nil {
    os.RemoveAll(filepath.Join(skill.SkillsRootDir(), sourceName, slug))
    writeError(c, http.StatusInternalServerError, "技能格式校验失败: "+pErr.Error())
    return
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": fmt.Sprintf("技能 %s 安装成功", meta.Name),
    "source":  sourceName,
  })
}

// handleAdminSkillsRegistryList 列出注册源中的可用技能
func (s *Server) handleAdminSkillsRegistryList(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  sourceName := c.Query("source")
  query := c.Query("q")

  if sourceName == "" {
    writeError(c, http.StatusBadRequest, "源名称不能为空")
    return
  }

  var rs *config.RegistrySource
  for _, sw := range s.cfg.Skills.Sources {
    if sw.Name == sourceName && sw.Reg != nil {
      rs = sw.Reg
      break
    }
  }
  if rs == nil {
    writeError(c, http.StatusBadRequest, "未找到注册源: "+sourceName)
    return
  }

  if query != "" && rs.SearchURL != "" {
    results, err := skill.SearchRegistry(rs.SearchURL, query, 50)
    if err != nil {
      writeError(c, http.StatusInternalServerError, "搜索失败: "+err.Error())
      return
    }
    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success": true,
      "skills":  results,
      "source":  sourceName,
    })
    return
  }

  index, err := skill.FetchIndex(rs.IndexURL)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "获取索引失败: "+err.Error())
    return
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "skills":  index.Skills,
    "total":   index.Total,
    "source":  sourceName,
  })
}

// ============================================================
// 辅助函数
// ============================================================

func updateSourceLastPull(sources []config.SkillsSourceWrapper, name string) []config.SkillsSourceWrapper {
  for i, sw := range sources {
    if sw.Name == name && sw.Git != nil {
      sources[i].Git.LastPull = timeNow()
    }
  }
  return sources
}

func updateSourceLastRefresh(sources []config.SkillsSourceWrapper, name string) []config.SkillsSourceWrapper {
  for i, sw := range sources {
    if sw.Name == name && sw.Reg != nil {
      sources[i].Reg.LastRefresh = timeNow()
    }
  }
  return sources
}

func timeNow() string {
  return time.Now().Format("2006-01-02 15:04:05")
}

// ============================================================
// 辅助函数
// ============================================================

func (s *Server) saveSkillsConfig() {
  skillsJSON, err := json.Marshal(s.cfg.Skills)
  if err != nil {
    slog.Error("序列化技能配置失败", "error", err)
    return
  }
  engine, err := auth.GetEngine()
  if err != nil {
    slog.Error("获取数据库连接失败", "error", err)
    return
  }
  if _, err := engine.Exec("INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES ('skills', ?, datetime('now','localtime'))", string(skillsJSON)); err != nil {
    slog.Error("保存技能配置失败", "error", err)
  }
}
