package web

import (
  "fmt"
  "io"
  "io/fs"
  "net/http"
  "os"
  "path/filepath"
  "sort"
  "strings"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/logger"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 文件管理 Handler
// ============================================================

// FileEntry 代表文件列表中的一条记录
type FileEntry struct {
  Name     string `json:"name"`
  IsDir    bool   `json:"is_dir"`
  Size     int64  `json:"size"`
  SizeStr  string `json:"size_str"`
  ModTime  string `json:"mod_time"`
  RelPath  string `json:"rel_path"`
  Tag      string `json:"tag,omitempty"`
  Readonly bool   `json:"readonly,omitempty"`
}

// Crumb 代表面包屑导航中的一条记录
type Crumb struct {
  Name string `json:"name"`
  Path string `json:"path"`
}

// filesResponse 是文件列表 API 的响应结构
type filesResponse struct {
  Success    bool        `json:"success"`
  Path       string      `json:"path"`
  Entries    []FileEntry `json:"entries"`
  Breadcrumb []Crumb     `json:"breadcrumb"`
}

// editResponse 是文件编辑 API 的响应结构
type editResponse struct {
  Success  bool   `json:"success"`
  Filename string `json:"filename"`
  Content  string `json:"content"`
  Path     string `json:"path"`
}

const maxUploadSize = 32 << 20 // 32 MB

// buildFileBreadcrumb 构建文件管理器的面包屑导航
func buildFileBreadcrumb(relPath string) []Crumb {
  breadcrumb := []Crumb{{Name: "工作区", Path: ""}}
  parts := strings.Split(strings.Trim(relPath, "/"), "/")
  for i, p := range parts {
    if p == "" {
      continue
    }
    breadcrumb = append(breadcrumb, Crumb{
      Name: p,
      Path: strings.Join(parts[:i+1], "/"),
    })
  }
  return breadcrumb
}

// isInstalledSkillPath 检查路径是否属于技能中心安装的只读技能
// 自建技能（真正 source="self" 且不在技能中心）可编辑，不在此列
func isInstalledSkillPath(username, safePath string) bool {
  if !strings.HasPrefix(safePath, "skills/") {
    return false
  }
  parts := strings.SplitN(safePath, "/", 3)
  if len(parts) < 2 || parts[1] == "" {
    return false
  }
  skillName := parts[1]
  if err := util.SafePathSegment(skillName); err != nil {
    return false
  }
  src, _ := auth.GetUserSkillSource(username, skillName)
  if src == "" {
    return false
  }
  if src == "self" {
    // source="self" 但实际来自技能中心 → 只读
    return findSkillSource(skillName) != ""
  }
  return true
}

func safeWorkspaceRelPath(relPath string) string {
  cleaned := filepath.Clean("/" + relPath)
  if cleaned == string(os.PathSeparator) {
    return "."
  }
  return strings.TrimPrefix(cleaned, string(os.PathSeparator))
}

func (s *Server) openWorkspaceRoot(username string) (*os.Root, error) {
  if err := user.ValidateUsername(username); err != nil {
    return nil, err
  }
  workspaceDir := user.UserDir(s.loadConfig(), username)
  if err := os.MkdirAll(workspaceDir, 0755); err != nil {
    return nil, err
  }
  return os.OpenRoot(workspaceDir)
}

// fileRoot 文件操作目标（工作区或共享文件夹）
type fileRoot struct {
  root     *os.Root
  safePath string
  isShare  bool // 共享文件夹为只读
}

// resolveFileRoot 解析文件路径，返回正确的 os.Root
// 路径以 share/<名称>/ 开头时，路由到共享文件夹目录
func (s *Server) resolveFileRoot(username, relPath string) (*fileRoot, error) {
  relPath = strings.TrimPrefix(relPath, "/")
  parts := strings.SplitN(relPath, "/", 3)

  if parts[0] == "share" {
    // 访问共享文件夹
    shareName := filepath.Base(parts[1])
    if err := util.SafePathSegment(shareName); err != nil {
      return nil, fmt.Errorf("非法共享文件夹名称")
    }
    sf, err := auth.GetSharedFolderByName(shareName)
    if err != nil {
      return nil, fmt.Errorf("共享文件夹不存在")
    }
    ok, err := auth.IsUserInSharedFolder(sf.ID, username)
    if err != nil || !ok {
      return nil, fmt.Errorf("无权限访问此共享文件夹")
    }
    shareDir := filepath.Join(filepath.Dir(s.loadConfig().UsersRoot), "shared", shareName)
    if err := os.MkdirAll(shareDir, 0755); err != nil {
      return nil, err
    }
    root, err := os.OpenRoot(shareDir)
    if err != nil {
      return nil, err
    }
    subPath := "."
    if len(parts) == 3 && parts[2] != "" {
      subPath = parts[2]
    }
    return &fileRoot{root: root, safePath: subPath, isShare: true}, nil
  }

  // 默认：工作区
  root, err := s.openWorkspaceRoot(username)
  if err != nil {
    return nil, err
  }
  return &fileRoot{root: root, safePath: safeWorkspaceRelPath(relPath)}, nil
}

// handleFiles 文件列表 API
func (s *Server) handleFiles(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  relPath := c.Query("path")
  logger.DebugRecv("GET", "/api/files", "username", username, "path", relPath)

  // 根目录特殊处理：合并工作区文件和共享文件夹
  if relPath == "" || relPath == "." || relPath == "/" {
    root, err := s.openWorkspaceRoot(username)
    if err != nil {
      writeError(c, http.StatusBadRequest, "无效路径")
      return
    }
    defer root.Close()

    dirEntries, err := fs.ReadDir(root.FS(), ".")
    if err != nil {
      writeError(c, http.StatusInternalServerError, "读取目录失败")
      return
    }

    // 获取用户可访问的共享文件夹
    folders, _ := auth.GetAccessibleSharedFolders(username)
    hasShares := len(folders) > 0

    var fileEntries []FileEntry
    systemTags := map[string]string{
      "AGENT.md":    "行为",
      "SOUL.md":     "灵魂",
      "USER.md":     "身份",
      "memory":      "记忆",
      "skills":      "技能",
      ".security.yml": "安全",
      "config.json": "配置",
    }
    for _, e := range dirEntries {
      // 如果已挂载共享文件夹，隐藏物理 share 目录避免重复
      if hasShares && e.Name() == "share" && e.IsDir() {
        continue
      }
      fi, _ := e.Info()
      if fi == nil {
        continue
      }
      tag := systemTags[e.Name()]
      fileEntries = append(fileEntries, FileEntry{
        Name:    e.Name(),
        IsDir:   e.IsDir(),
        Size:    fi.Size(),
        SizeStr: util.FormatSize(fi.Size()),
        ModTime: fi.ModTime().Format("2006-01-02 15:04"),
        RelPath: e.Name(),
        Tag:     tag,
      })
    }

    // 追加用户可访问的共享文件夹
    for _, sf := range folders {
      fileEntries = append(fileEntries, FileEntry{
        Name:    "share/" + sf.Name,
        IsDir:   true,
        SizeStr: "共享文件夹",
        ModTime: "-",
        RelPath: "share/" + sf.Name,
        Tag:     "共享",
      })
    }

    sort.Slice(fileEntries, func(i, j int) bool {
      if fileEntries[i].IsDir != fileEntries[j].IsDir {
        return fileEntries[i].IsDir
      }
      return strings.ToLower(fileEntries[i].Name) < strings.ToLower(fileEntries[j].Name)
    })

    writeJSON(c, http.StatusOK, filesResponse{
      Success: true,
      Path:    relPath,
      Entries: fileEntries,
      Breadcrumb: []Crumb{{Name: "工作区", Path: ""}},
    })
    return
  }

  // 非根路径：用 resolveFileRoot 路由到正确目录
  fr, err := s.resolveFileRoot(username, relPath)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  defer fr.root.Close()

  cleanedRelPath := safeWorkspaceRelPath(relPath)

  // 技能目录：自建技能可进可编辑，技能中心安装 readonly
  if cleanedRelPath == "skills" {
    skillMap := make(map[string]string) // skillName → source
    skills, _ := auth.GetUserSkillsWithSource(username)
    for _, s := range skills {
      skillMap[s.Name] = s.Source
    }

    diskNames := make(map[string]bool)
    if info, err := fr.root.Stat(fr.safePath); err == nil && info.IsDir() {
      dirEntries, _ := fs.ReadDir(fr.root.FS(), fr.safePath)
      for _, e := range dirEntries {
        diskNames[e.Name()] = true
      }
    }

    var fileEntries []FileEntry
    for name, source := range skillMap {
      isCenter := false
      tag := source
      // source="self" 的用户安装 → 检查是否实际来自技能中心
      if source == "self" {
        if src := findSkillSource(name); src != "" {
          isCenter = true
          tag = src // 显示实际来源名
        } else {
          tag = "自建"
        }
      } else {
        isCenter = true
      }

      fileEntries = append(fileEntries, FileEntry{
        Name:     name,
        IsDir:    true,
        SizeStr:  "技能目录",
        ModTime:  "-",
        RelPath:  filepath.Join(relPath, name),
        Tag:      tag,
        Readonly: isCenter,
      })
    }

    for name := range diskNames {
      if _, exists := skillMap[name]; !exists {
        fileEntries = append(fileEntries, FileEntry{
          Name:    name,
          IsDir:   true,
          SizeStr: "目录",
          ModTime: "-",
          RelPath: filepath.Join(relPath, name),
        })
      }
    }

    sort.Slice(fileEntries, func(i, j int) bool {
      return strings.ToLower(fileEntries[i].Name) < strings.ToLower(fileEntries[j].Name)
    })
    writeJSON(c, http.StatusOK, filesResponse{
      Success: true,
      Path:    relPath,
      Entries: fileEntries,
      Breadcrumb: []Crumb{
        {Name: "工作区", Path: ""},
        {Name: "skills", Path: "skills"},
      },
    })
    return
  }

  // 进入技能子目录：中心安装 readonly → 空，自建或未知 → 正常浏览
  if strings.HasPrefix(cleanedRelPath, "skills/") {
    parts := strings.SplitN(cleanedRelPath, "/", 3)
    if len(parts) >= 2 {
      skillName := parts[1]
      src, _ := auth.GetUserSkillSource(username, skillName)
      isCenter := false
      if src == "self" {
        isCenter = findSkillSource(skillName) != ""
      } else if src != "" {
        isCenter = true
      }
      if isCenter {
        writeJSON(c, http.StatusOK, filesResponse{
          Success:    true,
          Path:       relPath,
          Entries:    []FileEntry{},
          Breadcrumb: buildFileBreadcrumb(relPath),
        })
        return
      }
    }
  }

  info, err := fr.root.Stat(fr.safePath)
  if err != nil || !info.IsDir() {
    writeError(c, http.StatusNotFound, "目录不存在")
    return
  }

  dirEntries, err := fs.ReadDir(fr.root.FS(), fr.safePath)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "读取目录失败")
    return
  }

  var fileEntries []FileEntry
  for _, e := range dirEntries {
    fi, _ := e.Info()
    if fi == nil {
      continue
    }
    fileEntries = append(fileEntries, FileEntry{
      Name:    e.Name(),
      IsDir:   e.IsDir(),
      Size:    fi.Size(),
      SizeStr: util.FormatSize(fi.Size()),
      ModTime: fi.ModTime().Format("2006-01-02 15:04"),
      RelPath: filepath.Join(relPath, e.Name()),
    })
  }

  sort.Slice(fileEntries, func(i, j int) bool {
    if fileEntries[i].IsDir != fileEntries[j].IsDir {
      return fileEntries[i].IsDir
    }
    return strings.ToLower(fileEntries[i].Name) < strings.ToLower(fileEntries[j].Name)
  })

  writeJSON(c, http.StatusOK, filesResponse{
    Success:    true,
    Path:       relPath,
    Entries:    fileEntries,
    Breadcrumb: buildFileBreadcrumb(relPath),
  })
}

// handleFileUpload 上传文件
func (s *Server) handleFileUpload(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  relPath := c.PostForm("path")
  logger.DebugRecv("POST", "/api/files/upload", "username", username, "path", relPath)

  // 限制请求体大小
  c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSize)
  if err := c.Request.ParseMultipartForm(maxUploadSize); err != nil {
    writeError(c, http.StatusBadRequest, "文件过大或格式错误")
    return
  }

  fr, err := s.resolveFileRoot(username, relPath)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  defer fr.root.Close()
  if isInstalledSkillPath(username, fr.safePath) {
    writeError(c, http.StatusBadRequest, "技能目录为只读，无法上传")
    return
  }

  info, err := fr.root.Stat(fr.safePath)
  if err != nil || !info.IsDir() {
    writeError(c, http.StatusBadRequest, "目标目录不存在")
    return
  }

  file, header, err := c.Request.FormFile("file")
  if err != nil {
    writeError(c, http.StatusBadRequest, "读取上传文件失败")
    return
  }
  defer file.Close()

  filename := filepath.Base(header.Filename)
  logger.DebugProcess("upload_file", "username", username, "path", relPath, "filename", filename, "size", header.Size)
  dst, err := fr.root.OpenFile(filepath.Join(fr.safePath, filename), os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "创建文件失败")
    return
  }
  defer dst.Close()

  if _, err := io.Copy(dst, file); err != nil {
    writeError(c, http.StatusInternalServerError, "写入文件失败")
    return
  }

  logger.DebugSend("POST", "/api/files/upload", http.StatusOK, "filename", filename)
  writeSuccess(c, fmt.Sprintf("文件 %s 上传成功", filename))
}

// handleFileDownload 下载文件
func (s *Server) handleFileDownload(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  relPath := c.Query("path")
  logger.DebugRecv("GET", "/api/files/download", "username", username, "path", relPath)

  fr, err := s.resolveFileRoot(username, relPath)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  defer fr.root.Close()

  info, err := fr.root.Stat(fr.safePath)
  if err != nil || info.IsDir() {
    writeError(c, http.StatusNotFound, "文件不存在")
    return
  }

  file, err := fr.root.Open(fr.safePath)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "读取文件失败")
    return
  }
  defer file.Close()

  logger.DebugSend("GET", "/api/files/download", http.StatusOK, "path", relPath, "size", info.Size())
  c.Writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(fr.safePath)))
  http.ServeContent(c.Writer, c.Request, filepath.Base(fr.safePath), info.ModTime(), file)
}

// handleFileDelete 删除文件或目录
func (s *Server) handleFileDelete(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  relPath := c.PostForm("path")
  logger.DebugRecv("POST", "/api/files/delete", "username", username, "path", relPath)

  fr, err := s.resolveFileRoot(username, relPath)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  defer fr.root.Close()

  if fr.safePath == "." {
    writeError(c, http.StatusBadRequest, "不能删除工作区根目录")
    return
  }
  if isInstalledSkillPath(username, fr.safePath) {
    writeError(c, http.StatusBadRequest, "技能目录为只读，无法删除")
    return
  }

  info, err := fr.root.Stat(fr.safePath)
  if err != nil {
    writeError(c, http.StatusNotFound, "文件不存在")
    return
  }

  name := filepath.Base(fr.safePath)
  logger.DebugProcess("delete_file", "username", username, "path", relPath, "is_dir", info.IsDir())
  if info.IsDir() {
    err = fr.root.RemoveAll(fr.safePath)
  } else {
    err = fr.root.Remove(fr.safePath)
  }
  if err != nil {
    writeError(c, http.StatusInternalServerError, "删除失败")
    return
  }

  logger.DebugSend("POST", "/api/files/delete", http.StatusOK, "path", relPath)
  writeSuccess(c, fmt.Sprintf("%s 已删除", name))
}

// handleFileMkdir 新建目录
func (s *Server) handleFileMkdir(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  relPath := c.PostForm("path")
  name := filepath.Base(c.PostForm("name"))
  logger.DebugRecv("POST", "/api/files/mkdir", "username", username, "path", relPath, "name", name)

  if name == "" || name == "." || name == ".." {
    writeError(c, http.StatusBadRequest, "目录名无效")
    return
  }

  fr, err := s.resolveFileRoot(username, relPath)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  defer fr.root.Close()
  if isInstalledSkillPath(username, filepath.Join(fr.safePath, name)) {
    writeError(c, http.StatusBadRequest, "技能目录为只读，无法创建目录")
    return
  }

  logger.DebugProcess("mkdir", "username", username, "path", relPath, "name", name)
  if err := fr.root.Mkdir(filepath.Join(fr.safePath, name), 0755); err != nil {
    writeError(c, http.StatusInternalServerError, "创建目录失败")
    return
  }

  logger.DebugSend("POST", "/api/files/mkdir", http.StatusOK, "path", relPath+"/"+name)
  writeSuccess(c, fmt.Sprintf("目录 %s 已创建", name))
}

// handleFileEditGet 读取文本文件内容
func (s *Server) handleFileEditGet(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  relPath := c.Query("path")
  logger.DebugRecv("GET", "/api/files/edit", "username", username, "path", relPath)
  if relPath == "" {
    writeError(c, http.StatusBadRequest, "缺少文件路径")
    return
  }

  fr, err := s.resolveFileRoot(username, relPath)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  defer fr.root.Close()

  // 只允许编辑文本文件
  if !util.IsTextFile(fr.safePath) {
    writeError(c, http.StatusBadRequest, "不支持的文件类型")
    return
  }

  file, err := fr.root.Open(fr.safePath)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "读取文件失败")
    return
  }
  defer file.Close()

  data, err := io.ReadAll(file)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "读取文件失败")
    return
  }

  logger.DebugSend("GET", "/api/files/edit", http.StatusOK, "path", relPath, "size", len(data))
  writeJSON(c, http.StatusOK, editResponse{
    Success:  true,
    Filename: filepath.Base(fr.safePath),
    Content:  string(data),
    Path:     relPath,
  })
}

// handleFileEditSave 保存文本文件内容
func (s *Server) handleFileEditSave(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  relPath := c.PostForm("path")
  if relPath == "" {
    relPath = c.Query("path")
  }
  logger.DebugRecv("POST", "/api/files/edit", "username", username, "path", relPath)
  if relPath == "" {
    writeError(c, http.StatusBadRequest, "缺少文件路径")
    return
  }

  fr, err := s.resolveFileRoot(username, relPath)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  defer fr.root.Close()
  if isInstalledSkillPath(username, fr.safePath) {
    writeError(c, http.StatusBadRequest, "技能目录为只读，无法编辑")
    return
  }

  // 只允许编辑文本文件
  if !util.IsTextFile(fr.safePath) {
    writeError(c, http.StatusBadRequest, "不支持的文件类型")
    return
  }

  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  file, err := fr.root.OpenFile(fr.safePath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "保存文件失败")
    return
  }
  defer file.Close()
  content := c.PostForm("content")
  logger.DebugProcess("save_file", "username", username, "path", relPath, "size", len(content))
  if _, err := file.WriteString(content); err != nil {
    writeError(c, http.StatusInternalServerError, "保存文件失败")
    return
  }

  logger.DebugSend("POST", "/api/files/edit", http.StatusOK, "path", relPath)
  writeSuccess(c, fmt.Sprintf("文件 %s 已保存", filepath.Base(fr.safePath)))
}
