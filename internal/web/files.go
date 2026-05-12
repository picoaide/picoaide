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
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 文件管理 Handler
// ============================================================

// FileEntry 代表文件列表中的一条记录
type FileEntry struct {
  Name    string `json:"name"`
  IsDir   bool   `json:"is_dir"`
  Size    int64  `json:"size"`
  SizeStr string `json:"size_str"`
  ModTime string `json:"mod_time"`
  RelPath string `json:"rel_path"`
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
  workspaceDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace")
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
    shareName := parts[1]
    sf, err := auth.GetSharedFolderByName(shareName)
    if err != nil {
      return nil, fmt.Errorf("共享文件夹不存在")
    }
    ok, err := auth.IsUserInSharedFolder(sf.ID, username)
    if err != nil || !ok {
      return nil, fmt.Errorf("无权限访问此共享文件夹")
    }
    shareDir := filepath.Join(filepath.Dir(s.cfg.UsersRoot), "shared", shareName)
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
        RelPath: e.Name(),
      })
    }

    // 追加用户可访问的共享文件夹
    folders, _ := auth.GetAccessibleSharedFolders(username)
    for _, sf := range folders {
      fileEntries = append(fileEntries, FileEntry{
        Name:    "share/" + sf.Name,
        IsDir:   true,
        SizeStr: "共享文件夹",
        ModTime: "-",
        RelPath: "share/" + sf.Name,
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

  var breadcrumb []Crumb
  breadcrumb = append(breadcrumb, Crumb{Name: "工作区", Path: ""})
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

  writeJSON(c, http.StatusOK, filesResponse{
    Success:    true,
    Path:       relPath,
    Entries:    fileEntries,
    Breadcrumb: breadcrumb,
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

  // 限制请求体大小
  c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSize)
  if err := c.Request.ParseMultipartForm(maxUploadSize); err != nil {
    writeError(c, http.StatusBadRequest, "文件过大或格式错误")
    return
  }

  relPath := c.PostForm("path")

  fr, err := s.resolveFileRoot(username, relPath)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  defer fr.root.Close()

  if fr.isShare {
    writeError(c, http.StatusForbidden, "共享文件夹不支持上传操作")
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

  writeSuccess(c, fmt.Sprintf("文件 %s 上传成功", filename))
}

// handleFileDownload 下载文件
func (s *Server) handleFileDownload(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  relPath := c.Query("path")

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

  fr, err := s.resolveFileRoot(username, relPath)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  defer fr.root.Close()

  if fr.isShare {
    writeError(c, http.StatusForbidden, "共享文件夹不支持删除操作")
    return
  }

  if fr.safePath == "." {
    writeError(c, http.StatusBadRequest, "不能删除工作区根目录")
    return
  }

  info, err := fr.root.Stat(fr.safePath)
  if err != nil {
    writeError(c, http.StatusNotFound, "文件不存在")
    return
  }

  name := filepath.Base(fr.safePath)
  if info.IsDir() {
    err = fr.root.RemoveAll(fr.safePath)
  } else {
    err = fr.root.Remove(fr.safePath)
  }
  if err != nil {
    writeError(c, http.StatusInternalServerError, "删除失败")
    return
  }

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

  if fr.isShare {
    writeError(c, http.StatusForbidden, "共享文件夹不支持创建目录操作")
    return
  }

  if err := fr.root.Mkdir(filepath.Join(fr.safePath, name), 0755); err != nil {
    writeError(c, http.StatusInternalServerError, "创建目录失败")
    return
  }

  writeSuccess(c, fmt.Sprintf("目录 %s 已创建", name))
}

// handleFileEditGet 读取文本文件内容
func (s *Server) handleFileEditGet(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  relPath := c.Query("path")
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

  if fr.isShare {
    writeError(c, http.StatusForbidden, "共享文件夹不支持编辑保存操作")
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
  if _, err := file.WriteString(c.PostForm("content")); err != nil {
    writeError(c, http.StatusInternalServerError, "保存文件失败")
    return
  }

  writeSuccess(c, fmt.Sprintf("文件 %s 已保存", filepath.Base(fr.safePath)))
}
