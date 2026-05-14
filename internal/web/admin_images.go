package web

import (
  "context"
  "fmt"
  "net/http"
  "sort"
  "strconv"
  "strings"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
)

// ============================================================
// 镜像管理 Handler
// ============================================================

// handleAdminImages 列出本地镜像（含 Image ID、创建时间、用户依赖）
func (s *Server) handleAdminImages(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
    return
  }

  ctx := contextWithTimeout(10)
  images, err := dockerpkg.ListLocalImages(ctx, s.cfg.Image.Name)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "获取镜像列表失败: "+err.Error())
    return
  }

  // 查询所有用户的镜像引用，统计每个镜像的依赖用户
  containers, _ := auth.GetAllContainers()
  imageUsers := make(map[string][]string)
  for _, c := range containers {
    if c.Image != "" {
      imageUsers[c.Image] = append(imageUsers[c.Image], c.Username)
    }
  }

  type ImageInfo struct {
    ID         string   `json:"id"`
    FullID     string   `json:"full_id"`
    RepoTags   []string `json:"repo_tags"`
    Size       int64    `json:"size"`
    SizeStr    string   `json:"size_str"`
    Created    int64    `json:"created"`
    CreatedStr string   `json:"created_str"`
    UserCount  int      `json:"user_count"`
    Users      []string `json:"users"`
  }

  var list []ImageInfo
  for _, img := range images {
    // 短 ID（去掉 sha256: 前缀，取 12 位）
    shortID := img.ID
    if strings.HasPrefix(shortID, "sha256:") {
      shortID = shortID[7:]
    }
    if len(shortID) > 12 {
      shortID = shortID[:12]
    }

    createdStr := ""
    if img.Created > 0 {
      createdStr = time.Unix(img.Created, 0).Format("2006-01-02 15:04")
    }

    // 统计此镜像所有 tag 的用户依赖
    var users []string
    for _, tag := range img.RepoTags {
      users = append(users, imageUsers[tag]...)
    }

    list = append(list, ImageInfo{
      ID:         shortID,
      FullID:     img.ID,
      RepoTags:   img.RepoTags,
      Size:       img.Size,
      SizeStr:    formatSize(img.Size),
      Created:    img.Created,
      CreatedStr: createdStr,
      UserCount:  len(users),
      Users:      users,
    })
  }
  sort.SliceStable(list, func(i, j int) bool {
    return compareImageForDisplay(list[i].RepoTags, list[i].Created, list[j].RepoTags, list[j].Created) < 0
  })
  if list == nil {
    list = []ImageInfo{}
  }

  ps := getImagePullStatus()
  writeJSON(c, http.StatusOK, struct {
    Success    bool          `json:"success"`
    Images     []ImageInfo   `json:"images"`
    Pulling    bool          `json:"pulling"`
    PullingTag string        `json:"pulling_tag,omitempty"`
    PullStatus ImagePullTask `json:"pull_status"`
  }{true, list, ps.Running, ps.Tag, ps})
}

// handleAdminImagePull 拉取镜像（SSE 流式推送，含防重复）
func (s *Server) handleAdminImagePull(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  if getImagePullStatus().Running {
    writeError(c, http.StatusConflict, "已有镜像拉取任务进行中，请稍后再试")
    return
  }

  tag := c.PostForm("tag")
  if tag == "" {
    writeError(c, http.StatusBadRequest, "标签参数不能为空")
    return
  }

  pullRef := s.cfg.Image.PullRef(tag)
  unifiedRef := s.cfg.Image.UnifiedRef(tag)
  startImagePull(tag)

  // SSE 响应
  c.Writer.Header().Set("Content-Type", "text/event-stream")
  c.Writer.Header().Set("Cache-Control", "no-cache")
  c.Writer.Header().Set("Connection", "keep-alive")

  flush := func() {
    c.Writer.Flush()
  }

  ctx := context.Background()
  reader, err := dockerpkg.ImagePull(ctx, pullRef)
  if err != nil {
    failImagePull(err.Error())
    fmt.Fprintf(c.Writer, "data: {\"status\":\"error\",\"error\":\"%s\"}\n\n", err.Error())
    flush()
    return
  }
  defer reader.Close()

  buf := make([]byte, 4096)
  for {
    n, err := reader.Read(buf)
    if n > 0 {
      msg := string(buf[:n])
      updateImagePull(msg)
      fmt.Fprintf(c.Writer, "data: %s\n\n", msg)
      flush()
    }
    if err != nil {
      break
    }
  }

  // 腾讯云模式：拉取后 retag 为统一名称
  if s.cfg.Image.IsTencent() && pullRef != unifiedRef {
    msg := fmt.Sprintf("重命名镜像: %s -> %s", pullRef, unifiedRef)
    updateImagePull(msg)
    fmt.Fprintf(c.Writer, "data: {\"status\":\"%s\"}\n\n", msg)
    flush()
    if err := dockerpkg.RetagImage(ctx, pullRef, unifiedRef); err != nil {
      errMsg := fmt.Sprintf("重命名失败: %s", err.Error())
      failImagePull(errMsg)
      fmt.Fprintf(c.Writer, "data: {\"status\":\"error\",\"error\":\"%s\"}\n\n", errMsg)
      flush()
      return
    }
  }

  finishImagePull()
  fmt.Fprintf(c.Writer, "data: {\"status\":\"done\"}\n\n")
  flush()
}

// handleAdminImagePullStatus 返回当前镜像拉取任务状态
func (s *Server) handleAdminImagePullStatus(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  writeJSON(c, http.StatusOK, getImagePullStatus())
}

// handleAdminImageDelete 删除本地镜像（检查用户依赖）
func (s *Server) handleAdminImageDelete(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  imageRef := c.PostForm("image")
  if imageRef == "" {
    writeError(c, http.StatusBadRequest, "镜像参数不能为空")
    return
  }

  // 检查是否有用户依赖此镜像
  containers, err := auth.GetAllContainers()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询用户列表失败: "+err.Error())
    return
  }
  var dependentUsers []string
  for _, ctr := range containers {
    if ctr.Image == imageRef {
      dependentUsers = append(dependentUsers, ctr.Username)
    }
  }
  if len(dependentUsers) > 0 {
    // 查找本地其他可用镜像作为迁移目标
    ctxList := contextWithTimeout(10)
    localImgs, _ := dockerpkg.ListLocalImages(ctxList, s.cfg.Image.Name)
    var alternatives []string
    for _, img := range localImgs {
      for _, t := range img.RepoTags {
        if t != imageRef {
          alternatives = append(alternatives, t)
        }
      }
    }
    if alternatives == nil {
      alternatives = []string{}
    }
    writeJSON(c, http.StatusConflict, struct {
      Success      bool     `json:"success"`
      Error        string   `json:"error"`
      Users        []string `json:"users"`
      Alternatives []string `json:"alternatives"`
    }{false, "以下用户正在使用此镜像", dependentUsers, alternatives})
    return
  }

  // 检查镜像是否存在
  ctx := contextWithTimeout(30)
  if !dockerpkg.ImageExists(ctx, imageRef) {
    writeError(c, http.StatusNotFound, "镜像 "+imageRef+" 不存在")
    return
  }

  if err := dockerpkg.RemoveImage(ctx, imageRef); err != nil {
    writeError(c, http.StatusInternalServerError, "删除镜像失败: "+err.Error())
    return
  }

  writeSuccess(c, "镜像 "+imageRef+" 已删除")
}

// handleAdminImageRegistry 列出 PicoAide 远程仓库标签
func (s *Server) handleAdminImageRegistry(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
    return
  }

  // 远程标签始终从 GitHub Container Registry 获取
  ctx := contextWithTimeout(15)
  tags, err := dockerpkg.ListRegistryTags(ctx, s.cfg.Image.RepoName())
  if err != nil {
    writeError(c, http.StatusInternalServerError, "获取远程标签失败: "+err.Error())
    return
  }
  if tags == nil {
    tags = []string{}
  }
  sortTagsForDisplay(tags)

  writeJSON(c, http.StatusOK, struct {
    Success bool     `json:"success"`
    Tags    []string `json:"tags"`
  }{true, tags})
}

// ============================================================
// 镜像标签排序与比较工具函数
// ============================================================

func compareImageForDisplay(aTags []string, aCreated int64, bTags []string, bCreated int64) int {
  tagCmp := compareTagsForDisplay(primaryDisplayTag(aTags), primaryDisplayTag(bTags))
  if tagCmp != 0 {
    return tagCmp
  }
  if aCreated != bCreated {
    if aCreated > bCreated {
      return -1
    }
    return 1
  }
  return strings.Compare(strings.Join(aTags, ","), strings.Join(bTags, ","))
}

func primaryDisplayTag(repoTags []string) string {
  if len(repoTags) == 0 {
    return ""
  }
  best := repoTags[0]
  for _, tag := range repoTags[1:] {
    if compareTagsForDisplay(tagNameOnly(tag), tagNameOnly(best)) < 0 {
      best = tag
    }
  }
  return tagNameOnly(best)
}

func tagNameOnly(ref string) string {
  idx := strings.LastIndex(ref, ":")
  if idx < 0 || idx == len(ref)-1 {
    return ref
  }
  slashIdx := strings.LastIndex(ref, "/")
  if slashIdx > idx {
    return ref
  }
  return ref[idx+1:]
}

func sortTagsForDisplay(tags []string) {
  sort.SliceStable(tags, func(i, j int) bool {
    return compareTagsForDisplay(tags[i], tags[j]) < 0
  })
}

func compareTagsForDisplay(a, b string) int {
  av, aOK := parseVersionTag(a)
  bv, bOK := parseVersionTag(b)
  if aOK && bOK {
    for i := 0; i < len(av); i++ {
      if av[i] != bv[i] {
        if av[i] > bv[i] {
          return -1
        }
        return 1
      }
    }
    return strings.Compare(a, b)
  }
  if aOK {
    return -1
  }
  if bOK {
    return 1
  }
  return strings.Compare(a, b)
}

func parseVersionTag(tag string) ([3]int, bool) {
  var out [3]int
  tag = strings.TrimSpace(strings.TrimPrefix(tagNameOnly(tag), "v"))
  parts := strings.Split(tag, ".")
  if len(parts) != 3 {
    return out, false
  }
  for i, part := range parts {
    if part == "" {
      return out, false
    }
    n, err := strconv.Atoi(part)
    if err != nil {
      return out, false
    }
    out[i] = n
  }
  return out, true
}

// handleAdminLocalTags 列出本地镜像的所有标签
func (s *Server) handleAdminLocalTags(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
    return
  }

  ctx := contextWithTimeout(10)
  tags, err := dockerpkg.ListLocalTags(ctx, s.cfg.Image.Name)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "获取本地标签失败: "+err.Error())
    return
  }
  if tags == nil {
    tags = []string{}
  }
  sortTagsForDisplay(tags)

  writeJSON(c, http.StatusOK, struct {
    Success bool     `json:"success"`
    Tags    []string `json:"tags"`
  }{true, tags})
}

// handleAdminImageUsers 返回使用指定镜像的用户列表
func (s *Server) handleAdminImageUsers(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  image := c.Query("image")
  if image == "" {
    writeError(c, http.StatusBadRequest, "缺少 image 参数")
    return
  }

  containers, _ := auth.GetAllContainers()
  var users []string
  for _, ctr := range containers {
    if ctr.Image == image {
      users = append(users, ctr.Username)
    }
  }
  sort.Strings(users)
  if users == nil {
    users = []string{}
  }
  pager := parsePagination(c, 50, 200)
  if pager.Search != "" {
    filtered := users[:0]
    for _, username := range users {
      if strings.Contains(strings.ToLower(username), pager.Search) {
        filtered = append(filtered, username)
      }
    }
    users = filtered
  }
  users, total, totalPages, page, pageSize := paginateSlice(users, pager)

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":     true,
    "users":       users,
    "page":        page,
    "page_size":   pageSize,
    "total":       total,
    "total_pages": totalPages,
  })
}

func imageTagFromRef(imageRef string) string {
  idx := strings.LastIndex(imageRef, ":")
  if idx < 0 || idx == len(imageRef)-1 {
    return ""
  }
  slashIdx := strings.LastIndex(imageRef, "/")
  if slashIdx > idx {
    return ""
  }
  return imageRef[idx+1:]
}
