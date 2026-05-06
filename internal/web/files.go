package web

import (
  "fmt"
  "io"
  "net/http"
  "os"
  "path/filepath"
  "sort"
  "strings"

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

// workspaceRoot 返回指定用户的工作区根目录路径
func (s *Server) workspaceRoot(username string) string {
  base := filepath.Base(username) // 防御性截取：确保不会拼出子路径
  return filepath.Join(user.UserDir(s.cfg, base), ".picoclaw", "workspace")
}

// safePath 安全解析相对路径，防止路径遍历和符号链接逃逸
func (s *Server) safePath(username, relPath string) (string, error) {
  root := s.workspaceRoot(username)
  cleaned := filepath.Clean("/" + relPath)
  absPath := filepath.Join(root, cleaned)

  evalRoot, err := filepath.EvalSymlinks(root)
  if err != nil {
    evalRoot = root
  }

  evalPath, err := filepath.EvalSymlinks(absPath)
  if err != nil {
    if !os.IsNotExist(err) {
      return "", fmt.Errorf("路径验证失败")
    }
    // 文件不存在时，验证父目录
    parent := filepath.Dir(absPath)
    evalParent, err2 := filepath.EvalSymlinks(parent)
    if err2 != nil {
      return "", fmt.Errorf("路径验证失败")
    }
    if !strings.HasPrefix(evalParent, evalRoot+string(os.PathSeparator)) && evalParent != evalRoot {
      return "", fmt.Errorf("路径越界")
    }
    return absPath, nil
  }

  if !strings.HasPrefix(evalPath, evalRoot+string(os.PathSeparator)) && evalPath != evalRoot {
    return "", fmt.Errorf("路径越界")
  }
  // 返回解析后的路径，防止 TOCTOU 竞态
  return evalPath, nil
}

// parentDir 返回 relPath 的父目录相对路径
func parentDir(relPath string) string {
  p := filepath.Dir(relPath)
  if p == "." {
    return ""
  }
  return p
}

// handleFiles 文件列表 API
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  relPath := r.URL.Query().Get("path")

  root := s.workspaceRoot(username)
  os.MkdirAll(root, 0755)

  absPath, err := s.safePath(username, relPath)
  if err != nil {
    writeError(w, http.StatusBadRequest, "无效路径")
    return
  }

  info, err := os.Stat(absPath)
  if err != nil || !info.IsDir() {
    writeError(w, http.StatusNotFound, "目录不存在")
    return
  }

  entries, err := os.ReadDir(absPath)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "读取目录失败")
    return
  }

  var fileEntries []FileEntry
  for _, e := range entries {
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

  writeJSON(w, http.StatusOK, filesResponse{
    Success:    true,
    Path:       relPath,
    Entries:    fileEntries,
    Breadcrumb: breadcrumb,
  })
}

// handleFileUpload 上传文件
func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
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

  // 限制请求体大小
  r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
  if err := r.ParseMultipartForm(maxUploadSize); err != nil {
    writeError(w, http.StatusBadRequest, "文件过大或格式错误")
    return
  }

  relPath := r.FormValue("path")

  targetDir, err := s.safePath(username, relPath)
  if err != nil {
    writeError(w, http.StatusBadRequest, "无效路径")
    return
  }

  info, err := os.Stat(targetDir)
  if err != nil || !info.IsDir() {
    writeError(w, http.StatusBadRequest, "目标目录不存在")
    return
  }

  file, header, err := r.FormFile("file")
  if err != nil {
    writeError(w, http.StatusBadRequest, "读取上传文件失败")
    return
  }
  defer file.Close()

  filename := filepath.Base(header.Filename)
  dstPath := filepath.Join(targetDir, filename)

  dst, err := os.Create(dstPath)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "创建文件失败")
    return
  }
  defer dst.Close()

  if _, err := io.Copy(dst, file); err != nil {
    writeError(w, http.StatusInternalServerError, "写入文件失败")
    return
  }

  writeSuccess(w, fmt.Sprintf("文件 %s 上传成功", filename))
}

// handleFileDownload 下载文件
func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
    return
  }

  relPath := r.URL.Query().Get("path")

  absPath, err := s.safePath(username, relPath)
  if err != nil {
    writeError(w, http.StatusBadRequest, "无效路径")
    return
  }

  info, err := os.Stat(absPath)
  if err != nil || info.IsDir() {
    writeError(w, http.StatusNotFound, "文件不存在")
    return
  }

  w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(absPath)))
  http.ServeFile(w, r, absPath)
}

// handleFileDelete 删除文件或目录
func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
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

  relPath := r.FormValue("path")

  absPath, err := s.safePath(username, relPath)
  if err != nil {
    writeError(w, http.StatusBadRequest, "无效路径")
    return
  }

  root := s.workspaceRoot(username)
  if absPath == root {
    writeError(w, http.StatusBadRequest, "不能删除工作区根目录")
    return
  }

  info, err := os.Stat(absPath)
  if err != nil {
    writeError(w, http.StatusNotFound, "文件不存在")
    return
  }

  name := filepath.Base(absPath)
  if info.IsDir() {
    err = os.RemoveAll(absPath)
  } else {
    err = os.Remove(absPath)
  }
  if err != nil {
    writeError(w, http.StatusInternalServerError, "删除失败")
    return
  }

  writeSuccess(w, fmt.Sprintf("%s 已删除", name))
}

// handleFileMkdir 新建目录
func (s *Server) handleFileMkdir(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
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

  relPath := r.FormValue("path")
  name := filepath.Base(r.FormValue("name"))

  if name == "" || name == "." || name == ".." {
    writeError(w, http.StatusBadRequest, "目录名无效")
    return
  }

  parentAbsDir, err := s.safePath(username, relPath)
  if err != nil {
    writeError(w, http.StatusBadRequest, "无效路径")
    return
  }

  if err := os.Mkdir(filepath.Join(parentAbsDir, name), 0755); err != nil {
    writeError(w, http.StatusInternalServerError, "创建目录失败")
    return
  }

  writeSuccess(w, fmt.Sprintf("目录 %s 已创建", name))
}

// handleFileEdit 编辑文本文件
func (s *Server) handleFileEdit(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
    return
  }

  relPath := r.FormValue("path")
  if relPath == "" {
    relPath = r.URL.Query().Get("path")
  }
  if relPath == "" {
    writeError(w, http.StatusBadRequest, "缺少文件路径")
    return
  }

  absPath, err := s.safePath(username, relPath)
  if err != nil {
    writeError(w, http.StatusBadRequest, "无效路径")
    return
  }

  // 只允许编辑文本文件
  if !util.IsTextFile(absPath) {
    writeError(w, http.StatusBadRequest, "不支持的文件类型")
    return
  }

  if r.Method == "GET" {
    data, err := os.ReadFile(absPath)
    if err != nil {
      writeError(w, http.StatusInternalServerError, "读取文件失败")
      return
    }

    writeJSON(w, http.StatusOK, editResponse{
      Success:  true,
      Filename: filepath.Base(absPath),
      Content:  string(data),
      Path:     relPath,
    })
    return
  }

  // POST: 保存文件
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }
  if err := os.WriteFile(absPath, []byte(r.FormValue("content")), 0644); err != nil {
    writeError(w, http.StatusInternalServerError, "保存文件失败")
    return
  }

  writeSuccess(w, fmt.Sprintf("文件 %s 已保存", filepath.Base(absPath)))
}
