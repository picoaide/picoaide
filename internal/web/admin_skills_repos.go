package web

import (
  "encoding/json"
  "fmt"
  "log/slog"
  "net/http"
  "os"
  "path/filepath"
  "strconv"
  "strings"
  "time"

  gogit "github.com/go-git/go-git/v5"
  "github.com/go-git/go-git/v5/plumbing"
  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/skill"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 技能仓库管理（单技能仓库）
// ============================================================

func (s *Server) handleAdminSkillsReposAdd(c *gin.Context) {
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

  var input skillRepoInput
  if err := json.Unmarshal([]byte(c.PostForm("repo")), &input); err != nil {
    input.Name = c.PostForm("name")
    input.URL = c.PostForm("url")
    input.Ref = c.PostForm("ref")
    input.RefType = c.PostForm("ref_type")
    input.Public, _ = strconv.ParseBool(c.PostForm("public"))
  }
  repo, err := skillRepoFromInput(input)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  gitMutex.Lock()
  defer gitMutex.Unlock()

  if _, _, ok := skillRepoByName(s.cfg.Skills.Repos, repo.Name); ok {
    writeError(c, http.StatusBadRequest, "仓库名称已存在")
    return
  }

  reposDir := skillReposDir()
  targetDir := filepath.Join(reposDir, repo.Name)
  if _, err := os.Stat(targetDir); err == nil {
    writeError(c, http.StatusBadRequest, "仓库目录已存在")
    return
  }

  os.MkdirAll(reposDir, 0755)

  if _, err := cloneGitRepoWithCredentials(reposDir, repo.Name, repo.URL, repo); err != nil {
    errStr := err.Error()
    if strings.Contains(errStr, "Authentication failed") ||
      strings.Contains(errStr, "Permission denied") ||
      strings.Contains(errStr, "access denied") ||
      strings.Contains(errStr, "401") ||
      strings.Contains(errStr, "could not read Username") {
      writeError(c, http.StatusBadRequest, "需要鉴权，请在仓库设置中补充凭证后重试")
      return
    }
    writeError(c, http.StatusInternalServerError, "Git clone 失败: "+errStr)
    return
  }

  meta, err := syncGitRepoToSkill(repo.Name)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "安装技能失败: "+err.Error())
    return
  }

  auth.UpsertSkill(meta.Name, meta.Description)

  users, _ := auth.GetUsersForSkill(meta.Name)
  if len(users) > 0 {
    skillName := meta.Name
    enqueueTask("skills-deploy-auto", users, func(username string) error {
      return s.deploySkillToUser(skillName, username)
    })
  }

  repo.LastPull = time.Now().Format("2006-01-02 15:04:05")
  s.cfg.Skills.Repos = append(s.cfg.Skills.Repos, repo)
  s.saveSkillsConfig()

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":    true,
    "message":    fmt.Sprintf("技能 %s 已从 Git 导入", meta.Name),
    "skill_name": meta.Name,
  })
}

func (s *Server) handleAdminSkillsReposPull(c *gin.Context) {
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

  repoName := strings.TrimSpace(c.PostForm("name"))
  if repoName == "" {
    writeError(c, http.StatusBadRequest, "仓库名称不能为空")
    return
  }
  if err := util.SafePathSegment(repoName); err != nil {
    writeError(c, http.StatusBadRequest, "仓库名称不合法")
    return
  }

  gitMutex.Lock()
  defer gitMutex.Unlock()

  repo, idx, ok := skillRepoByName(s.cfg.Skills.Repos, repoName)
  if !ok {
    writeError(c, http.StatusNotFound, "仓库不存在")
    return
  }

  repoDir := filepath.Join(skillReposDir(), repoName)
  if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
    writeError(c, http.StatusBadRequest, "不是 Git 仓库")
    return
  }

  out, err := pullGitRepoWithCredentials(repoDir, repo)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "Git pull 失败: "+err.Error())
    return
  }

  meta, err := syncGitRepoToSkill(repo.Name)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "同步技能失败: "+err.Error())
    return
  }

  auth.UpsertSkill(meta.Name, meta.Description)

  s.cfg.Skills.Repos[idx].LastPull = time.Now().Format("2006-01-02 15:04:05")
  s.saveSkillsConfig()

  users, _ := auth.GetUsersForSkill(meta.Name)
  if len(users) > 0 {
    skillName := meta.Name
    enqueueTask("skills-deploy-auto", users, func(username string) error {
      return s.deploySkillToUser(skillName, username)
    })
  }

  updated := !strings.Contains(out, "Already up to date")
  writeJSON(c, http.StatusOK, struct {
    Success bool   `json:"success"`
    Message string `json:"message"`
    Updated bool   `json:"updated"`
  }{true, "技能已更新", updated})
}

func (s *Server) handleAdminSkillsReposRemove(c *gin.Context) {
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

  repoName := strings.TrimSpace(c.PostForm("name"))
  if repoName == "" {
    writeError(c, http.StatusBadRequest, "仓库名称不能为空")
    return
  }
  if err := util.SafePathSegment(repoName); err != nil {
    writeError(c, http.StatusBadRequest, "仓库名称不合法")
    return
  }

  os.RemoveAll(filepath.Join(skillReposDir(), repoName))

  var filtered []config.SkillRepo
  for _, r := range s.cfg.Skills.Repos {
    if r.Name != repoName {
      filtered = append(filtered, r)
    }
  }
  s.cfg.Skills.Repos = filtered
  s.saveSkillsConfig()
  writeSuccess(c, "仓库已删除")
}

func (s *Server) handleAdminSkillsReposSave(c *gin.Context) {
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

  var input skillRepoInput
  if err := json.Unmarshal([]byte(c.PostForm("repo")), &input); err != nil {
    writeError(c, http.StatusBadRequest, "仓库配置格式错误")
    return
  }
  repo, err := skillRepoFromInput(input)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  gitMutex.Lock()
  defer gitMutex.Unlock()

  oldRepo, idx, ok := skillRepoByName(s.cfg.Skills.Repos, repo.Name)
  if !ok {
    writeError(c, http.StatusNotFound, "仓库不存在")
    return
  }
  repo.LastPull = oldRepo.LastPull

  if repo.URL != oldRepo.URL || !repoCredentialsEqual(repo, oldRepo) {
    reposDir := skillReposDir()
    repoDir := filepath.Join(reposDir, repo.Name)
    // 先克隆到临时目录，成功后再替换旧目录
    tmpDir, err := os.MkdirTemp(reposDir, ".clone-*")
    if err != nil {
      writeError(c, http.StatusInternalServerError, "创建临时目录失败: "+err.Error())
      return
    }
    defer os.RemoveAll(tmpDir)
    if _, err := cloneGitRepoWithCredentials(tmpDir, repo.Name, repo.URL, repo); err != nil {
      writeError(c, http.StatusInternalServerError, "重新克隆失败: "+err.Error())
      return
    }
    clonedDir := filepath.Join(tmpDir, repo.Name)
    if _, err := os.Stat(filepath.Join(clonedDir, "SKILL.md")); err != nil {
      writeError(c, http.StatusBadRequest, "克隆成功但仓库中没有 SKILL.md")
      return
    }
    // 校验通过后替换
    os.RemoveAll(repoDir)
    if err := os.Rename(clonedDir, repoDir); err != nil {
      writeError(c, http.StatusInternalServerError, "替换仓库目录失败: "+err.Error())
      return
    }
    meta, err := syncGitRepoToSkill(repo.Name)
    if err != nil {
      writeError(c, http.StatusInternalServerError, "同步技能失败: "+err.Error())
      return
    }
    auth.UpsertSkill(meta.Name, meta.Description)
  }

  s.cfg.Skills.Repos[idx] = repo
  s.saveSkillsConfig()
  writeSuccess(c, "仓库配置已保存")
}

func (s *Server) handleAdminSkillsInstall(c *gin.Context) {
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

  repoName, err := cleanPathSegment(c.PostForm("repo"))
  if repoName == "" {
    writeError(c, http.StatusBadRequest, "仓库名称不能为空")
    return
  }
  if err != nil {
    writeError(c, http.StatusBadRequest, "仓库名称不合法")
    return
  }

  gitMutex.Lock()
  defer gitMutex.Unlock()

  repoDir := filepath.Join(skillReposDir(), repoName)
  if _, err := os.Stat(repoDir); err != nil {
    writeError(c, http.StatusNotFound, "仓库不存在，请先添加仓库")
    return
  }

  meta, err := syncGitRepoToSkill(repoName)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "安装技能失败: "+err.Error())
    return
  }
  if meta != nil {
    auth.UpsertSkill(meta.Name, meta.Description)
  }

  skillName := ""
  if meta != nil {
    skillName = meta.Name
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":    true,
    "message":    fmt.Sprintf("技能 %s 已安装", skillName),
    "skill_name": skillName,
  })
}

func (s *Server) handleAdminSkillsReposList(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  repoName := c.Query("name")
  if repoName == "" {
    writeError(c, http.StatusBadRequest, "仓库名称不能为空")
    return
  }
  if err := util.SafePathSegment(repoName); err != nil {
    writeError(c, http.StatusBadRequest, "仓库名称不合法")
    return
  }

  repoDir := filepath.Join(skillReposDir(), repoName)
  if _, err := os.Stat(repoDir); err != nil {
    writeError(c, http.StatusNotFound, "仓库不存在")
    return
  }

  type RepoSkillInfo struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Installed   bool   `json:"installed"`
    Valid       bool   `json:"valid"`
    ValidateMsg string `json:"validate_msg,omitempty"`
  }

  skillEntry := RepoSkillInfo{Valid: true}
  meta, pErr := skill.ParseAndValidate(repoDir)
  if pErr != nil {
    skillEntry.Name = repoName
    skillEntry.Valid = false
    skillEntry.ValidateMsg = pErr.Error()
  } else {
    skillEntry.Name = meta.Name
    skillEntry.Description = meta.Description
  }

  if skillEntry.Valid {
    skillDir := config.SkillsDirPath()
    _, installed := os.Stat(filepath.Join(skillDir, skillEntry.Name))
    skillEntry.Installed = installed == nil
  }

  writeJSON(c, http.StatusOK, struct {
    Success bool          `json:"success"`
    Repo    RepoSkillInfo `json:"repo"`
  }{true, skillEntry})
}

// handleAdminSkillsReposAddStream SSE 流式添加仓库（实时显示克隆进度）
func (s *Server) handleAdminSkillsReposAddStream(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    return
  }
  if !s.checkCSRF(c) {
    return
  }

  var input skillRepoInput
  if err := json.Unmarshal([]byte(c.PostForm("repo")), &input); err != nil {
    input.Name = c.PostForm("name")
    input.URL = c.PostForm("url")
    input.Ref = c.PostForm("ref")
    input.RefType = c.PostForm("ref_type")
    input.Public, _ = strconv.ParseBool(c.PostForm("public"))
  }
  repo, err := skillRepoFromInput(input)
  if err != nil {
    fmt.Fprintf(c.Writer, "data: {\"error\":%q}\n\n", err.Error())
    return
  }

  // SSE headers
  c.Writer.Header().Set("Content-Type", "text/event-stream")
  c.Writer.Header().Set("Cache-Control", "no-cache")
  c.Writer.Header().Set("Connection", "keep-alive")
  flush := func() { c.Writer.Flush() }

  gitMutex.Lock()
  defer gitMutex.Unlock()

  if _, _, ok := skillRepoByName(s.cfg.Skills.Repos, repo.Name); ok {
    writeSSE(c.Writer, flush, `{"step":"error","error":"仓库名称已存在"}`)
    return
  }

  reposDir := skillReposDir()
  targetDir := filepath.Join(reposDir, repo.Name)
  if _, err := os.Stat(targetDir); err == nil {
    writeSSE(c.Writer, flush, `{"step":"error","error":"仓库目录已存在"}`)
    return
  }

  os.MkdirAll(reposDir, 0755)
  writeSSE(c.Writer, flush, `{"step":"clone","message":"正在克隆仓库..."}`)

  // 尝试凭据（公共仓库无凭据时也尝试无鉴权克隆）
  cloneErr := error(nil)

  attempts := repo.Credentials
  if repo.Public || len(attempts) == 0 {
    attempts = []config.SkillRepoCredential{{Name: "public", Mode: "https"}}
  }

  for _, cred := range attempts {
    auth, authErr := goGitAuth(cred)
    if authErr != nil {
      cloneErr = authErr
      writeSSE(c.Writer, flush, `{"step":"clone_retry","message":"凭据失败，尝试下一个..."}`)
      continue
    }

    opts := &gogit.CloneOptions{
      URL:  repo.URL,
      Auth: auth,
    }
    if repo.Ref != "" {
      refName := "refs/heads/" + repo.Ref
      if repo.RefType == "tag" {
        refName = "refs/tags/" + repo.Ref
      }
      opts.ReferenceName = plumbing.ReferenceName(refName)
      opts.SingleBranch = true
      opts.Depth = 1
    }

    _, err := gogit.PlainClone(targetDir, false, opts)
    if err == nil {
      cloneErr = nil
      break
    }
    cloneErr = err
    writeSSE(c.Writer, flush, `{"step":"clone_retry","message":"凭据失败，尝试下一个..."}`)
  }

  if cloneErr != nil {
    errStr := cloneErr.Error()
    writeSSE(c.Writer, flush, `{"step":"clone_error","message":"克隆失败","error":`+quoteJSON(errStr)+`}`)
    if strings.Contains(errStr, "Authentication failed") ||
      strings.Contains(errStr, "Permission denied") ||
      strings.Contains(errStr, "access denied") ||
      strings.Contains(errStr, "401") ||
      strings.Contains(errStr, "could not read Username") {
      writeSSE(c.Writer, flush, `{"step":"auth_required","message":"需要鉴权，请在仓库设置中补充凭证后重试"}`)
    }
    return
  }

  writeSSE(c.Writer, flush, `{"step":"validate","message":"校验 SKILL.md..."}`)

  if _, err := skill.ParseAndValidate(targetDir); err != nil {
    os.RemoveAll(targetDir)
    writeSSE(c.Writer, flush, `{"step":"error","error":"SKILL.md 校验失败: `+quoteJSON(err.Error())+`"}`)
    return
  }

  writeSSE(c.Writer, flush, `{"step":"sync","message":"同步到技能目录..."}`)

  meta, err := syncGitRepoToSkill(repo.Name)
  if err != nil {
    writeSSE(c.Writer, flush, `{"step":"error","error":"安装技能失败: `+quoteJSON(err.Error())+`"}`)
    return
  }

  auth.UpsertSkill(meta.Name, meta.Description)

  repo.LastPull = time.Now().Format("2006-01-02 15:04:05")
  s.cfg.Skills.Repos = append(s.cfg.Skills.Repos, repo)
  s.saveSkillsConfig()

  // 自动部署
  users, _ := auth.GetUsersForSkill(meta.Name)
  if len(users) > 0 {
    writeSSE(c.Writer, flush, `{"step":"deploy","message":"正在部署到 `+fmt.Sprintf("%d", len(users))+` 个用户..."}`)
    enqueueTask("skills-deploy-auto", users, func(username string) error {
      return s.deploySkillToUser(meta.Name, username)
    })
  }

  writeSSE(c.Writer, flush, `{"step":"done","message":"技能 `+meta.Name+` 安装成功"}`)
}

// quoteJSON 转义字符串为 JSON 安全格式（简单版）
func quoteJSON(s string) string {
  s = strings.ReplaceAll(s, "\\", "\\\\")
  s = strings.ReplaceAll(s, "\"", "\\\"")
  s = strings.ReplaceAll(s, "\n", "\\n")
  s = strings.ReplaceAll(s, "\r", "\\r")
  return s
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

func repoCredentialsEqual(a, b config.SkillRepo) bool {
  if len(a.Credentials) != len(b.Credentials) {
    return false
  }
  for i := range a.Credentials {
    if a.Credentials[i].Name != b.Credentials[i].Name ||
      a.Credentials[i].Provider != b.Credentials[i].Provider ||
      a.Credentials[i].Username != b.Credentials[i].Username ||
      a.Credentials[i].Secret != b.Credentials[i].Secret ||
      a.Credentials[i].Mode != b.Credentials[i].Mode {
      return false
    }
  }
  return true
}
