package web

import (
  "archive/zip"
  "bytes"
  "fmt"
  "io"
  "net/http"
  "os"
  "path/filepath"
  "sort"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 技能库管理
// ============================================================

// skillReposDir 技能仓库克隆区路径
func skillReposDir() string {
  wd, _ := os.Getwd()
  return filepath.Join(wd, "skill-repos")
}

func (s *Server) handleAdminSkills(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
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
  sort.Slice(skills, func(i, j int) bool {
    return skills[i].Name < skills[j].Name
  })
  pager := parsePagination(c, 50, 200)
  if pager.Search != "" {
    filtered := skills[:0]
    for _, sk := range skills {
      if strings.Contains(strings.ToLower(sk.Name), pager.Search) {
        filtered = append(filtered, sk)
      }
    }
    skills = filtered
  }
  skills, total, totalPages, page, pageSize := paginateSlice(skills, pager)

  writeJSON(c, http.StatusOK, struct {
    Success    bool               `json:"success"`
    Skills     []SkillInfo        `json:"skills"`
    Repos      []config.SkillRepo `json:"repos"`
    Page       int                `json:"page,omitempty"`
    PageSize   int                `json:"page_size,omitempty"`
    Total      int                `json:"total,omitempty"`
    TotalPages int                `json:"total_pages,omitempty"`
  }{true, skills, s.cfg.Skills.Repos, page, pageSize, total, totalPages})
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

  if skillName != "" {
    if err := util.SafePathSegment(skillName); err != nil {
      writeError(c, http.StatusBadRequest, "技能名称不合法: "+err.Error())
      return
    }
  }
  if targetUser != "" {
    if err := user.ValidateUsername(targetUser); err != nil {
      writeError(c, http.StatusBadRequest, err.Error())
      return
    }
  }

  skillDir := config.SkillsDirPath()
  entries, err := os.ReadDir(skillDir)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "读取技能目录失败: "+err.Error())
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
    writeError(c, http.StatusBadRequest, "没有找到可部署的技能")
    return
  }

  deployFn := func(username string) error {
    targetSkillsDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills")
    for _, sn := range deploySkills {
      srcPath := filepath.Join(skillDir, sn)
      dstPath := filepath.Join(targetSkillsDir, sn)
      if err := util.CopyDir(srcPath, dstPath); err != nil {
        return fmt.Errorf("复制技能 %s 失败: %w", sn, err)
      }
    }
    return nil
  }

  if targetUser != "" {
    // 单个用户直接执行
    if err := deployFn(targetUser); err != nil {
      writeError(c, http.StatusInternalServerError, err.Error())
      return
    }
    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success":     true,
      "message":     fmt.Sprintf("已将 %d 个技能部署到 %s", len(deploySkills), targetUser),
      "skill_count": len(deploySkills),
      "user_count":  1,
    })
    return
  }

  // 组或全部用户走队列
  var targets []string
  if targetGroup != "" {
    targets, err = auth.GetGroupMembersForDeploy(targetGroup)
    if err != nil {
      writeError(c, http.StatusBadRequest, "获取组成员失败: "+err.Error())
      return
    }
  } else {
    targets, err = user.GetUserList(s.cfg)
    if err != nil {
      writeError(c, http.StatusInternalServerError, "获取用户列表失败: "+err.Error())
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
    "skill_count": len(deploySkills),
  })
}

func (s *Server) handleAdminSkillsDownload(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  name := c.Query("name")
  if name == "" {
    writeError(c, http.StatusBadRequest, "技能名称不能为空")
    return
  }
  if err := util.SafePathSegment(name); err != nil {
    writeError(c, http.StatusBadRequest, "技能名称不合法")
    return
  }
  skillPath := filepath.Join(config.SkillsDirPath(), name)
  if _, err := os.Stat(skillPath); err != nil {
    writeError(c, http.StatusNotFound, "技能不存在")
    return
  }
  w := c.Writer
  w.Header().Set("Content-Type", "application/zip")
  w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
  zw := zip.NewWriter(w)
  filepath.WalkDir(skillPath, func(path string, d os.DirEntry, err error) error {
    if err != nil {
      return nil
    }
    relPath, _ := filepath.Rel(skillPath, path)
    // 安全检查：防止 zip slip — 确保相对路径不以 .. 开头
    relPath = filepath.ToSlash(relPath)
    if strings.HasPrefix(relPath, "../") || relPath == ".." {
      return nil
    }
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
  skillPath := filepath.Join(config.SkillsDirPath(), name)
  if err := os.RemoveAll(skillPath); err != nil {
    writeError(c, http.StatusInternalServerError, "删除失败: "+err.Error())
    return
  }
  writeSuccess(c, "技能已删除")
}

// handleAdminSkillsUpload 从 zip 包安装一个技能
func (s *Server) handleAdminSkillsUpload(c *gin.Context) {
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

  skillName, err := cleanPathSegment(c.PostForm("name"))
  if skillName == "" {
    writeError(c, http.StatusBadRequest, "技能名称不能为空")
    return
  }
  if err != nil {
    writeError(c, http.StatusBadRequest, "技能名称不合法")
    return
  }

  file, header, err := c.Request.FormFile("file")
  if err != nil {
    writeError(c, http.StatusBadRequest, "请上传技能 zip 包")
    return
  }
  defer file.Close()
  if header != nil && !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
    writeError(c, http.StatusBadRequest, "仅支持 zip 压缩包")
    return
  }
  data, err := io.ReadAll(io.LimitReader(file, 128<<20))
  if err != nil {
    writeError(c, http.StatusBadRequest, "读取上传文件失败: "+err.Error())
    return
  }
  if len(data) == 0 {
    writeError(c, http.StatusBadRequest, "上传文件为空")
    return
  }
  reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
  if err != nil {
    writeError(c, http.StatusBadRequest, "zip 包格式错误: "+err.Error())
    return
  }
  if err := copySkillZipContents(reader, skillName); err != nil {
    writeError(c, http.StatusBadRequest, "安装技能失败: "+err.Error())
    return
  }
  writeSuccess(c, "技能已上传并安装")
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
