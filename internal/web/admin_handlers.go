package web

import (
  "archive/zip"
  "context"
  "fmt"
  "io"
  "net/http"
  "os"
  "os/exec"
  "path/filepath"
  "sort"
  "strings"
  "sync"
  "time"

  "github.com/PicoAide/PicoAide/internal/auth"
  "github.com/PicoAide/PicoAide/internal/config"
  dockerpkg "github.com/PicoAide/PicoAide/internal/docker"
  "github.com/PicoAide/PicoAide/internal/user"
  "github.com/PicoAide/PicoAide/internal/util"
  "gopkg.in/yaml.v3"
)

var gitMutex sync.Mutex

func (s *Server) requireSuperadmin(w http.ResponseWriter, r *http.Request) string {
  username := s.requireAuth(w, r)
  if username == "" {
    return ""
  }
  if !auth.IsSuperadmin(username) {
    writeError(w, http.StatusForbidden, "仅超级管理员可访问")
    return ""
  }
  return username
}

// ============================================================
// 用户管理
// ============================================================

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  containers, err := auth.GetAllContainers()
  if err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }

  type UserInfo struct {
    Username   string `json:"username"`
    Status     string `json:"status"`
    ImageTag   string `json:"image_tag"`
    ImageReady bool   `json:"image_ready"`
    IP         string `json:"ip"`
  }

  ctx := context.Background()
  var list []UserInfo
  for _, c := range containers {
    status := c.Status
    if c.ContainerID != "" {
      status = dockerpkg.ContainerStatus(ctx, c.ContainerID)
    }

    imageRef := c.Image
    imageReady := imageRef != "" && dockerpkg.ImageExists(ctx, imageRef)

    // 从镜像引用提取 tag
    imageTag := imageRef
    if parts := strings.SplitN(imageRef, ":", 2); len(parts) == 2 {
      imageTag = parts[1]
    }

    list = append(list, UserInfo{
      Username:   c.Username,
      Status:     status,
      ImageTag:   imageTag,
      ImageReady: imageReady,
      IP:         c.IP,
    })
  }
  if list == nil {
    list = []UserInfo{}
  }

  writeJSON(w, http.StatusOK, struct {
    Success     bool       `json:"success"`
    Users       []UserInfo `json:"users"`
    AuthMode    string     `json:"auth_mode"`
    UnifiedAuth bool       `json:"unified_auth"`
  }{true, list, s.cfg.AuthMode(), s.cfg.UnifiedAuthEnabled()})
}

// ============================================================
// 用户创建与删除（仅本地模式）
// ============================================================

func (s *Server) handleAdminUserCreate(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
    writeError(w, http.StatusForbidden, "统一认证模式下不允许手动创建用户")
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  username := r.FormValue("username")
  if err := user.ValidateUsername(username); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  if auth.UserExists(username) {
    writeError(w, http.StatusBadRequest, "用户 "+username+" 已存在")
    return
  }

  password := auth.GenerateRandomPassword(12)
  if err := auth.CreateUser(username, password, "user"); err != nil {
    writeError(w, http.StatusInternalServerError, "创建用户失败: "+err.Error())
    return
  }

  // 创建用户容器目录
  if err := user.InitUser(s.cfg, username); err != nil {
    // 回滚：删除已创建的用户记录
    auth.DeleteUser(username)
    writeError(w, http.StatusInternalServerError, "初始化用户目录失败: "+err.Error())
    return
  }

  writeJSON(w, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Message  string `json:"message"`
    Username string `json:"username"`
    Password string `json:"password"`
  }{true, "用户创建成功", username, password})
}

func (s *Server) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
    writeError(w, http.StatusForbidden, "统一认证模式下不允许删除用户")
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  username := r.FormValue("username")
  if username == "" {
    writeError(w, http.StatusBadRequest, "用户名不能为空")
    return
  }

  // 停止并移除容器
  rec, _ := auth.GetContainerByUsername(username)
  if rec != nil && rec.ContainerID != "" {
    ctx := context.Background()
    _ = dockerpkg.Remove(ctx, rec.ContainerID)
  }
  auth.DeleteContainer(username)

  // 归档用户目录
  if err := user.ArchiveUser(s.cfg, username); err != nil {
    writeError(w, http.StatusInternalServerError, "归档用户目录失败: "+err.Error())
    return
  }

  // 删除本地用户记录
  if err := auth.DeleteUser(username); err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }

  writeSuccess(w, "用户 "+username+" 已删除并归档")
}

// ============================================================
// 容器操作
// ============================================================

func (s *Server) handleAdminContainerStart(w http.ResponseWriter, r *http.Request) {
  s.handleContainerAction(w, r, "start")
}
func (s *Server) handleAdminContainerStop(w http.ResponseWriter, r *http.Request) {
  s.handleContainerAction(w, r, "stop")
}
func (s *Server) handleAdminContainerRestart(w http.ResponseWriter, r *http.Request) {
  s.handleContainerAction(w, r, "restart")
}

func (s *Server) handleContainerAction(w http.ResponseWriter, r *http.Request, action string) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  username := r.FormValue("username")
  if username == "" {
    writeError(w, http.StatusBadRequest, "用户名不能为空")
    return
  }

  rec, err := auth.GetContainerByUsername(username)
  if err != nil || rec == nil {
    writeError(w, http.StatusBadRequest, "用户 "+username+" 未初始化")
    return
  }

  ctx := context.Background()

  // 启动/重启前检查镜像是否存在
  if action == "start" || action == "restart" {
    if rec.Image == "" || !dockerpkg.ImageExists(ctx, rec.Image) {
      writeError(w, http.StatusBadRequest, "容器镜像 "+rec.Image+" 不存在，请先拉取镜像")
      return
    }
  }

  switch action {
  case "start":
    // 如果容器未创建，先创建
    if rec.ContainerID == "" || !dockerpkg.ContainerExists(ctx, username) {
      ud := user.UserDir(s.cfg, username)
      cid, createErr := dockerpkg.CreateContainer(ctx, username, rec.Image, ud, rec.IP, rec.CPULimit, rec.MemoryLimit)
      if createErr != nil {
        writeError(w, http.StatusInternalServerError, "创建容器失败: "+createErr.Error())
        return
      }
      auth.UpdateContainerID(username, cid)
      rec.ContainerID = cid
    }
    if err := dockerpkg.Start(ctx, rec.ContainerID); err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    auth.UpdateContainerStatus(username, "running")

  case "stop":
    if rec.ContainerID == "" {
      writeError(w, http.StatusBadRequest, "容器未创建")
      return
    }
    if err := dockerpkg.Stop(ctx, rec.ContainerID); err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    auth.UpdateContainerStatus(username, "stopped")

  case "restart":
    if rec.ContainerID == "" || !dockerpkg.ContainerExists(ctx, username) {
      writeError(w, http.StatusBadRequest, "容器未创建")
      return
    }
    if err := dockerpkg.Restart(ctx, rec.ContainerID); err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    auth.UpdateContainerStatus(username, "running")
  }

  labels := map[string]string{"start": "启动", "stop": "停止", "restart": "重启"}
  writeSuccess(w, fmt.Sprintf("容器已%s", labels[action]))
}

// ============================================================
// 白名单管理
// ============================================================

func (s *Server) handleAdminWhitelist(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }

  if r.Method == "GET" {
    whitelist, err := user.LoadWhitelist()
    if err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    var users []string
    if whitelist != nil {
      for u := range whitelist {
        users = append(users, u)
      }
      sort.Strings(users)
    }
    if users == nil {
      users = []string{}
    }
    writeJSON(w, http.StatusOK, struct {
      Success bool     `json:"success"`
      Users   []string `json:"users"`
    }{true, users})
    return
  }

  if r.Method == "POST" {
    if !s.checkCSRF(r) {
      writeError(w, http.StatusForbidden, "无效请求")
      return
    }
    usersStr := r.FormValue("users")
    var users []string
    if usersStr != "" {
      for _, u := range strings.Split(usersStr, ",") {
        u = strings.TrimSpace(u)
        if u != "" {
          users = append(users, u)
        }
      }
    }
    sort.Strings(users)
    wl := config.WhitelistFile{Users: users}
    data, err := yaml.Marshal(&wl)
    if err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    if err := os.WriteFile(config.WhitelistPath(), data, 0644); err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    writeSuccess(w, "白名单已更新")
    return
  }

  writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 和 POST 方法")
}

// ============================================================
// 技能库管理
// ============================================================

// skillReposDir 技能仓库克隆区路径
func skillReposDir() string {
  return filepath.Join(filepath.Dir(config.ConfigPath()), "skill-repos")
}

func (s *Server) handleAdminSkills(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  type SkillInfo struct {
    Name      string `json:"name"`
    FileCount int    `json:"file_count"`
    Size      int64  `json:"size"`
    SizeStr   string `json:"size_str"`
    ModTime   string `json:"mod_time"`
  }

  var skills []SkillInfo
  skillDir := config.SkillsDirPath()
  entries, err := os.ReadDir(skillDir)
  if err == nil {
    for _, e := range entries {
      if !e.IsDir() {
        continue
      }
      info, ierr := e.Info()
      if ierr != nil {
        continue
      }
      fullPath := filepath.Join(skillDir, e.Name())
      var fileCount int
      var totalSize int64
      filepath.WalkDir(fullPath, func(_ string, d os.DirEntry, err error) error {
        if err != nil {
          return nil
        }
        if !d.IsDir() {
          fileCount++
          if fi, e := d.Info(); e == nil {
            totalSize += fi.Size()
          }
        }
        return nil
      })
      skills = append(skills, SkillInfo{
        Name:      e.Name(),
        FileCount: fileCount,
        Size:      totalSize,
        SizeStr:   formatSize(totalSize),
        ModTime:   info.ModTime().Format("2006-01-02 15:04"),
      })
    }
  }
  if skills == nil {
    skills = []SkillInfo{}
  }

  writeJSON(w, http.StatusOK, struct {
    Success bool        `json:"success"`
    Skills  []SkillInfo `json:"skills"`
    Repos   []config.SkillRepo `json:"repos"`
  }{true, skills, s.cfg.Skills.Repos})
}

func (s *Server) handleAdminSkillsDeploy(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  skillName := strings.TrimSpace(r.FormValue("skill_name"))
  targetUser := strings.TrimSpace(r.FormValue("username"))

  skillDir := config.SkillsDirPath()
  entries, err := os.ReadDir(skillDir)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "读取技能目录失败: "+err.Error())
    return
  }

  var deploySkills []string
  for _, e := range entries {
    if !e.IsDir() {
      continue
    }
    if skillName == "" || e.Name() == skillName {
      deploySkills = append(deploySkills, e.Name())
    }
  }
  if len(deploySkills) == 0 {
    writeError(w, http.StatusBadRequest, "没有找到可部署的技能")
    return
  }

  userCount := 0
  deployFn := func(username string) error {
    targetSkillsDir := filepath.Join(user.UserDir(s.cfg, username), "root", ".picoclaw", "workspace", "skills")
    for _, sn := range deploySkills {
      srcPath := filepath.Join(skillDir, sn)
      dstPath := filepath.Join(targetSkillsDir, sn)
      if err := util.CopyDir(srcPath, dstPath); err != nil {
        return fmt.Errorf("复制技能 %s 失败: %w", sn, err)
      }
    }
    userCount++
    return nil
  }

  if targetUser != "" {
    if err := deployFn(targetUser); err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
  } else {
    if err := user.ForEachUser(s.cfg, deployFn); err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
  }

  writeJSON(w, http.StatusOK, struct {
    Success    bool   `json:"success"`
    Message    string `json:"message"`
    SkillCount int    `json:"skill_count"`
    UserCount  int    `json:"user_count"`
  }{true, fmt.Sprintf("已将 %d 个技能部署到 %d 个用户", len(deploySkills), userCount), len(deploySkills), userCount})
}

func (s *Server) handleAdminSkillsDownload(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  name := r.URL.Query().Get("name")
  if name == "" {
    writeError(w, http.StatusBadRequest, "技能名称不能为空")
    return
  }
  skillPath := filepath.Join(config.SkillsDirPath(), name)
  if _, err := os.Stat(skillPath); err != nil {
    writeError(w, http.StatusNotFound, "技能不存在")
    return
  }
  w.Header().Set("Content-Type", "application/zip")
  w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
  zw := zip.NewWriter(w)
  filepath.WalkDir(skillPath, func(path string, d os.DirEntry, err error) error {
    if err != nil {
      return nil
    }
    relPath, _ := filepath.Rel(skillPath, path)
    if d.IsDir() {
      zw.Create(relPath + "/")
      return nil
    }
    fw, err := zw.Create(relPath)
    if err != nil {
      return nil
    }
    f, err := os.Open(path)
    if err != nil {
      return nil
    }
    defer f.Close()
    io.Copy(fw, f)
    return nil
  })
  zw.Close()
}

// handleAdminSkillsRemove 从 skill/ 删除技能
func (s *Server) handleAdminSkillsRemove(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }
  name := strings.TrimSpace(r.FormValue("name"))
  if name == "" {
    writeError(w, http.StatusBadRequest, "技能名称不能为空")
    return
  }
  skillPath := filepath.Join(config.SkillsDirPath(), name)
  if err := os.RemoveAll(skillPath); err != nil {
    writeError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
    return
  }
  writeSuccess(w, "技能已删除")
}

// ============================================================
// 技能仓库管理（多仓库）
// ============================================================

// handleAdminSkillsReposAdd 添加并克隆技能仓库
func (s *Server) handleAdminSkillsReposAdd(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  repoName := strings.TrimSpace(r.FormValue("name"))
  repoURL := strings.TrimSpace(r.FormValue("url"))
  if repoName == "" || repoURL == "" {
    writeError(w, http.StatusBadRequest, "仓库名称和地址不能为空")
    return
  }

  gitMutex.Lock()
  defer gitMutex.Unlock()

  if _, err := exec.LookPath("git"); err != nil {
    writeError(w, http.StatusInternalServerError, "Git 未安装")
    return
  }

  // 检查重名
  for _, r := range s.cfg.Skills.Repos {
    if r.Name == repoName {
      writeError(w, http.StatusBadRequest, "仓库名称已存在")
      return
    }
  }

  reposDir := skillReposDir()
  targetDir := filepath.Join(reposDir, repoName)
  if _, err := os.Stat(targetDir); err == nil {
    writeError(w, http.StatusBadRequest, "仓库目录已存在")
    return
  }

  os.MkdirAll(reposDir, 0755)
  if _, err := gitCmd(reposDir, "clone", repoURL, repoName); err != nil {
    writeError(w, http.StatusInternalServerError, "Git clone 失败: "+err.Error())
    return
  }

  s.cfg.Skills.Repos = append(s.cfg.Skills.Repos, config.SkillRepo{
    Name:     repoName,
    URL:      repoURL,
    LastPull: time.Now().Format("2006-01-02 15:04:05"),
  })
  s.saveSkillsConfig()
  writeSuccess(w, "仓库已克隆")
}

// handleAdminSkillsReposPull 拉取仓库更新
func (s *Server) handleAdminSkillsReposPull(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  repoName := strings.TrimSpace(r.FormValue("name"))
  if repoName == "" {
    writeError(w, http.StatusBadRequest, "仓库名称不能为空")
    return
  }

  gitMutex.Lock()
  defer gitMutex.Unlock()

  repoDir := filepath.Join(skillReposDir(), repoName)
  if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
    writeError(w, http.StatusBadRequest, "不是 Git 仓库")
    return
  }

  gitCmd(repoDir, "reset", "--hard")
  out, err := gitCmd(repoDir, "pull")
  if err != nil {
    writeError(w, http.StatusInternalServerError, "Git pull 失败: "+err.Error())
    return
  }

  for i := range s.cfg.Skills.Repos {
    if s.cfg.Skills.Repos[i].Name == repoName {
      s.cfg.Skills.Repos[i].LastPull = time.Now().Format("2006-01-02 15:04:05")
      break
    }
  }
  s.saveSkillsConfig()

  updated := !strings.Contains(out, "Already up to date")
  writeJSON(w, http.StatusOK, struct {
    Success bool   `json:"success"`
    Message string `json:"message"`
    Updated bool   `json:"updated"`
  }{true, "已拉取更新", updated})
}

// handleAdminSkillsReposRemove 删除仓库
func (s *Server) handleAdminSkillsReposRemove(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  repoName := strings.TrimSpace(r.FormValue("name"))
  if repoName == "" {
    writeError(w, http.StatusBadRequest, "仓库名称不能为空")
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
  writeSuccess(w, "仓库已删除")
}

// handleAdminSkillsInstall 从仓库安装技能到 skill/ 目录
func (s *Server) handleAdminSkillsInstall(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  repoName := strings.TrimSpace(r.FormValue("repo"))
  skillName := strings.TrimSpace(r.FormValue("skill"))
  if repoName == "" {
    writeError(w, http.StatusBadRequest, "仓库名称不能为空")
    return
  }

  repoDir := filepath.Join(skillReposDir(), repoName)
  skillDir := config.SkillsDirPath()
  os.MkdirAll(skillDir, 0755)

  if skillName == "" {
    // 安装仓库中所有技能
    entries, err := os.ReadDir(repoDir)
    if err != nil {
      writeError(w, http.StatusInternalServerError, "读取仓库失败")
      return
    }
    count := 0
    for _, e := range entries {
      if !e.IsDir() || e.Name() == ".git" {
        continue
      }
      src := filepath.Join(repoDir, e.Name())
      dst := filepath.Join(skillDir, e.Name())
      if err := util.CopyDir(src, dst); err != nil {
        writeError(w, http.StatusInternalServerError, fmt.Sprintf("安装 %s 失败: %v", e.Name(), err))
        return
      }
      count++
    }
    writeJSON(w, http.StatusOK, struct {
      Success bool   `json:"success"`
      Message string `json:"message"`
      Count   int    `json:"count"`
    }{true, fmt.Sprintf("已安装 %d 个技能", count), count})
    return
  }

  // 安装单个技能
  src := filepath.Join(repoDir, skillName)
  if _, err := os.Stat(src); err != nil {
    writeError(w, http.StatusNotFound, "技能在仓库中不存在")
    return
  }
  dst := filepath.Join(skillDir, skillName)
  if err := util.CopyDir(src, dst); err != nil {
    writeError(w, http.StatusInternalServerError, "安装失败: "+err.Error())
    return
  }
  writeSuccess(w, "技能已安装")
}

// handleAdminSkillsReposList 列出仓库中的技能
func (s *Server) handleAdminSkillsReposList(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  repoName := r.URL.Query().Get("name")
  if repoName == "" {
    writeError(w, http.StatusBadRequest, "仓库名称不能为空")
    return
  }

  repoDir := filepath.Join(skillReposDir(), repoName)
  entries, err := os.ReadDir(repoDir)
  if err != nil {
    writeError(w, http.StatusNotFound, "仓库不存在")
    return
  }

  type SkillEntry struct {
    Name string `json:"name"`
    Installed bool `json:"installed"`
  }
  var skills []SkillEntry
  skillDir := config.SkillsDirPath()

  for _, e := range entries {
    if !e.IsDir() || e.Name() == ".git" {
      continue
    }
    _, installed := os.Stat(filepath.Join(skillDir, e.Name()))
    skills = append(skills, SkillEntry{
      Name: e.Name(),
      Installed: installed == nil,
    })
  }
  if skills == nil {
    skills = []SkillEntry{}
  }

  writeJSON(w, http.StatusOK, struct {
    Success bool         `json:"success"`
    Skills  []SkillEntry `json:"skills"`
  }{true, skills})
}

// ============================================================
// 辅助函数
// ============================================================

func gitCmd(dir string, args ...string) (string, error) {
  cmd := exec.Command("git", args...)
  cmd.Dir = dir
  var stdout, stderr strings.Builder
  cmd.Stdout = &stdout
  cmd.Stderr = &stderr
  if err := cmd.Run(); err != nil {
    return "", fmt.Errorf("%w\n%s", err, stderr.String())
  }
  return stdout.String(), nil
}

func (s *Server) saveSkillsConfig() {
  data, err := os.ReadFile(config.ConfigPath())
  if err != nil {
    return
  }
  var raw map[string]interface{}
  if err := yaml.Unmarshal(data, &raw); err != nil {
    return
  }
  raw["skills"] = map[string]interface{}{
    "repos": s.cfg.Skills.Repos,
  }
  out, err := yaml.Marshal(raw)
  if err != nil {
    return
  }
  os.WriteFile(config.ConfigPath(), out, 0644)
}

func formatSize(size int64) string {
  if size < 1024 {
    return fmt.Sprintf("%d B", size)
  }
  if size < 1024*1024 {
    return fmt.Sprintf("%.1f KB", float64(size)/1024)
  }
  return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func minInt(a, b int) int {
  if a < b {
    return a
  }
  return b
}
