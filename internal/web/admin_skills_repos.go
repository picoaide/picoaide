package web

import (
  "encoding/json"
  "fmt"
  "io/fs"
  "net/http"
  "os"
  "os/exec"
  "path/filepath"
  "strconv"
  "strings"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 技能仓库管理（多仓库）
// ============================================================

// handleAdminSkillsReposAdd 添加并克隆技能仓库
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

  if _, err := exec.LookPath("git"); err != nil {
    writeError(c, http.StatusInternalServerError, "Git 未安装")
    return
  }

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
    writeError(c, http.StatusInternalServerError, "Git clone 失败: "+err.Error())
    return
  }
  if err := syncGitRepoToSkill(repo.Name); err != nil {
    writeError(c, http.StatusInternalServerError, "安装技能失败: "+err.Error())
    return
  }

  repo.LastPull = time.Now().Format("2006-01-02 15:04:05")
  s.cfg.Skills.Repos = append(s.cfg.Skills.Repos, repo)
  s.saveSkillsConfig()
  writeSuccess(c, "技能已从 Git 导入")
}

// handleAdminSkillsReposPull 拉取仓库更新
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
  if err := syncGitRepoToSkill(repo.Name); err != nil {
    writeError(c, http.StatusInternalServerError, "同步技能失败: "+err.Error())
    return
  }

  s.cfg.Skills.Repos[idx].LastPull = time.Now().Format("2006-01-02 15:04:05")
  s.saveSkillsConfig()

  updated := !strings.Contains(out, "Already up to date")
  writeJSON(c, http.StatusOK, struct {
    Success bool   `json:"success"`
    Message string `json:"message"`
    Updated bool   `json:"updated"`
  }{true, "技能已更新", updated})
}

// handleAdminSkillsReposRemove 删除仓库
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

// handleAdminSkillsReposSave 保存仓库配置
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
  s.cfg.Skills.Repos[idx] = repo
  s.saveSkillsConfig()
  writeSuccess(c, "仓库配置已保存")
}

// handleAdminSkillsInstall 从仓库安装技能到 skill/ 目录
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
  skillName := strings.TrimSpace(c.PostForm("skill"))
  if repoName == "" {
    writeError(c, http.StatusBadRequest, "仓库名称不能为空")
    return
  }
  if err != nil {
    writeError(c, http.StatusBadRequest, "仓库名称不合法")
    return
  }
  if skillName != "" {
    cleanedSkillName, err := cleanPathSegment(skillName)
    if err != nil {
      writeError(c, http.StatusBadRequest, "技能名称不合法")
      return
    }
    skillName = cleanedSkillName
  }

  repoDir := filepath.Join(skillReposDir(), repoName)
  skillDir := config.SkillsDirPath()
  os.MkdirAll(skillDir, 0755)
  repoRoot, err := os.OpenRoot(repoDir)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "读取仓库失败")
    return
  }
  defer repoRoot.Close()
  skillRoot, err := os.OpenRoot(skillDir)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "读取技能目录失败")
    return
  }
  defer skillRoot.Close()

  if skillName == "" {
    // 安装仓库中所有技能
    entries, err := fs.ReadDir(repoRoot.FS(), ".")
    if err != nil {
      writeError(c, http.StatusInternalServerError, "读取仓库失败")
      return
    }
    count := 0
    for _, e := range entries {
      if !e.IsDir() || e.Name() == ".git" {
        continue
      }
      if err := copyDirBetweenRoots(repoRoot, e.Name(), skillRoot, e.Name()); err != nil {
        writeError(c, http.StatusInternalServerError, fmt.Sprintf("安装 %s 失败: %v", e.Name(), err))
        return
      }
      count++
    }
    writeJSON(c, http.StatusOK, struct {
      Success bool   `json:"success"`
      Message string `json:"message"`
      Count   int    `json:"count"`
    }{true, fmt.Sprintf("已安装 %d 个技能", count), count})
    return
  }

  // 安装单个技能
  info, err := repoRoot.Stat(skillName)
  if err != nil || !info.IsDir() {
    writeError(c, http.StatusNotFound, "技能在仓库中不存在")
    return
  }
  if err := copyDirBetweenRoots(repoRoot, skillName, skillRoot, skillName); err != nil {
    writeError(c, http.StatusInternalServerError, "安装失败: "+err.Error())
    return
  }
  writeSuccess(c, "技能已安装")
}

// handleAdminSkillsReposList 列出仓库中的技能
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
  entries, err := os.ReadDir(repoDir)
  if err != nil {
    writeError(c, http.StatusNotFound, "仓库不存在")
    return
  }

  type SkillEntry struct {
    Name      string `json:"name"`
    Installed bool   `json:"installed"`
  }
  var skills []SkillEntry
  skillDir := config.SkillsDirPath()

  for _, e := range entries {
    if !e.IsDir() || e.Name() == ".git" {
      continue
    }
    _, installed := os.Stat(filepath.Join(skillDir, e.Name()))
    skills = append(skills, SkillEntry{
      Name:      e.Name(),
      Installed: installed == nil,
    })
  }
  if skills == nil {
    skills = []SkillEntry{}
  }

  writeJSON(c, http.StatusOK, struct {
    Success bool         `json:"success"`
    Skills  []SkillEntry `json:"skills"`
  }{true, skills})
}

// ============================================================
// 辅助函数
// ============================================================

func (s *Server) saveSkillsConfig() {
  skillsJSON, err := json.Marshal(map[string]interface{}{
    "repos": s.cfg.Skills.Repos,
  })
  if err != nil {
    return
  }
  engine, err := auth.GetEngine()
  if err != nil {
    return
  }
  engine.Exec("INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES ('skills', ?, datetime('now','localtime'))", string(skillsJSON))
}
