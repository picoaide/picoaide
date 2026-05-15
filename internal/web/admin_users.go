package web

import (
  "context"
  "fmt"
  "net/http"
  "sort"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
  "github.com/picoaide/picoaide/internal/logger"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// 用户管理
// ============================================================

func (s *Server) handleAdminUsers(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  pager := parsePagination(c, 20, 100)

  containers, err := auth.GetAllContainers()
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }

  // 本地用户（local_users 表）
  localUsers, _ := auth.GetAllLocalUsers()
  localRoleMap := make(map[string]string)
  for _, u := range localUsers {
    localRoleMap[u.Username] = u.Role
  }

  type UserInfo struct {
    Username   string   `json:"username"`
    Source     string   `json:"source"`
    Status     string   `json:"status"`
    ImageTag   string   `json:"image_tag"`
    ImageReady bool     `json:"image_ready"`
    IP         string   `json:"ip"`
    Role       string   `json:"role"`
    Groups     []string `json:"groups,omitempty"`
  }

  // 按 username 索引容器记录
  containerMap := make(map[string]*auth.ContainerRecord)
  for i := range containers {
    containerMap[containers[i].Username] = &containers[i]
  }

  var candidates []UserInfo

  // 先输出所有有容器记录的用户
  seen := make(map[string]bool)
  for _, c := range containers {
    if localRoleMap[c.Username] == "superadmin" {
      continue
    }
    seen[c.Username] = true
    imageRef := c.Image
    imageTag := imageRef
    if parts := strings.SplitN(imageRef, ":", 2); len(parts) == 2 {
      imageTag = parts[1]
    }

    role := localRoleMap[c.Username]
    source := auth.GetUserSource(c.Username)
    if source == "" {
      source = "unknown"
    }

    candidates = append(candidates, UserInfo{
      Username:   c.Username,
      Source:     source,
      Status:     c.Status,
      ImageTag:   imageTag,
      ImageReady: imageRef != "",
      IP:         c.IP,
      Role:       role,
    })
  }

  // 补上本地用户中没有容器记录的（如超管）
  for _, u := range localUsers {
    if u.Role == "superadmin" {
      continue
    }
    if !seen[u.Username] {
      source := u.Source
      if source == "" {
        source = "local"
      }
      candidates = append(candidates, UserInfo{
        Username: u.Username,
        Source:   source,
        Status:   "未初始化",
        Role:     u.Role,
      })
    }
  }

  sort.Slice(candidates, func(i, j int) bool {
    return candidates[i].Username < candidates[j].Username
  })
  if pager.Search != "" {
    filtered := candidates[:0]
    for _, u := range candidates {
      if strings.Contains(strings.ToLower(u.Username), pager.Search) ||
        strings.Contains(strings.ToLower(u.Source), pager.Search) ||
        strings.Contains(strings.ToLower(u.Role), pager.Search) {
        filtered = append(filtered, u)
      }
    }
    candidates = filtered
  }

  list, total, totalPages, page, pageSize := paginateSlice(candidates, pager)

  includeRuntime := c.Query("runtime") != "false"
  var ctx context.Context
  if includeRuntime {
    ctx = context.Background()
  }
  for i := range list {
    rec := containerMap[list[i].Username]
    if rec != nil && includeRuntime {
      if rec.ContainerID != "" {
        list[i].Status = dockerpkg.ContainerStatus(ctx, rec.ContainerID)
      }
      list[i].ImageReady = rec.Image != "" && dockerpkg.ImageExists(ctx, rec.Image)
    }
    if groups, err := auth.GetGroupsForUser(list[i].Username); err == nil && len(groups) > 0 {
      list[i].Groups = groups
    }
  }

  if list == nil {
    list = []UserInfo{}
  }

  writeJSON(c, http.StatusOK, struct {
    Success     bool       `json:"success"`
    Users       []UserInfo `json:"users"`
    AuthMode    string     `json:"auth_mode"`
    UnifiedAuth bool       `json:"unified_auth"`
    Page        int        `json:"page,omitempty"`
    PageSize    int        `json:"page_size,omitempty"`
    Total       int        `json:"total,omitempty"`
    TotalPages  int        `json:"total_pages,omitempty"`
  }{true, list, s.cfg.AuthMode(), s.cfg.UnifiedAuthEnabled(), page, pageSize, total, totalPages})
}

// ============================================================
// 用户创建与删除（仅本地模式）
// ============================================================

func (s *Server) handleAdminUserCreate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
    writeError(c, http.StatusForbidden, "普通用户由当前认证源同步，不允许手动创建")
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

  username := c.PostForm("username")
  if err := user.ValidateUsername(username); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  if auth.UserExists(username) {
    writeError(c, http.StatusBadRequest, "用户 "+username+" 已存在")
    return
  }

  password := auth.GenerateRandomPassword(12)
  if err := auth.CreateUser(username, password, "user"); err != nil {
    writeError(c, http.StatusInternalServerError, "创建用户失败: "+err.Error())
    return
  }

  // 获取镜像标签参数，未指定时自动使用本地最新标签
  imageTag := c.PostForm("image_tag")
  if imageTag == "" {
    ctx, cancel := contextWithTimeout(10)
    defer cancel()
    defaultTag, err := s.defaultUserImageTag(ctx)
    if err != nil {
      auth.DeleteUser(username)
      writeError(c, http.StatusInternalServerError, "获取默认镜像失败: "+err.Error())
      return
    }
    imageTag = defaultTag
  }
  if imageTag == "" {
    auth.DeleteUser(username)
    writeError(c, http.StatusBadRequest, "未指定镜像标签且本地无可用镜像，请先拉取镜像")
    return
  }

  // 创建用户容器目录
  if err := user.InitUser(s.cfg, username, imageTag); err != nil {
    // 回滚：删除已创建的用户记录
    auth.DeleteUser(username)
    writeError(c, http.StatusInternalServerError, "初始化用户目录失败: "+err.Error())
    return
  }

  // 部署默认技能
  s.applyDefaultSkillsToUser(username)

  // 异步启动容器并下发配置
  if s.dockerAvailable {
    go s.autoStartUserContainer(username)
  }

  logger.Audit("user.create", "username", username, "operator", s.getSessionUser(c))
  writeJSON(c, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Message  string `json:"message"`
    Username string `json:"username"`
    Password string `json:"password"`
  }{true, "用户创建成功，容器启动中", username, password})
}

type adminUserBatchCreateResult struct {
  Username string `json:"username"`
  Password string `json:"password,omitempty"`
  Success  bool   `json:"success"`
  Error    string `json:"error,omitempty"`
}

func (s *Server) handleAdminUserBatchCreate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
    writeError(c, http.StatusForbidden, "普通用户由当前认证源同步，不允许手动创建")
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

  usernames := parseBatchUsernames(c.PostForm("usernames"))
  if len(usernames) == 0 {
    writeError(c, http.StatusBadRequest, "请至少输入一个用户名")
    return
  }

  imageTag := strings.TrimSpace(c.PostForm("image_tag"))
  if imageTag == "" {
    ctx, cancel := contextWithTimeout(10)
    defer cancel()
    defaultTag, err := s.defaultUserImageTag(ctx)
    if err != nil {
      writeError(c, http.StatusInternalServerError, "获取默认镜像失败: "+err.Error())
      return
    }
    imageTag = defaultTag
  }
  if imageTag == "" {
    writeError(c, http.StatusBadRequest, "未指定镜像标签且本地无可用镜像，请先拉取镜像")
    return
  }

  results := make([]adminUserBatchCreateResult, 0, len(usernames))
  created := 0
  failed := 0
  for _, username := range usernames {
    result := s.createLocalUserWithImage(username, imageTag)
    results = append(results, result)
    if result.Success {
      created++
      continue
    }
    failed++
  }

  logger.Audit("user.batch_create", "count", created, "failed", failed, "operator", s.getSessionUser(c))
  writeJSON(c, http.StatusOK, struct {
    Success bool                         `json:"success"`
    Message string                       `json:"message"`
    Created int                          `json:"created"`
    Failed  int                          `json:"failed"`
    Results []adminUserBatchCreateResult `json:"results"`
  }{failed == 0, fmt.Sprintf("批量创建完成，成功 %d 个，失败 %d 个", created, failed), created, failed, results})
}

func parseBatchUsernames(input string) []string {
  seen := make(map[string]bool)
  var usernames []string
  for _, line := range strings.Split(input, "\n") {
    username := strings.TrimSpace(line)
    if username == "" || seen[username] {
      continue
    }
    seen[username] = true
    usernames = append(usernames, username)
  }
  return usernames
}

func (s *Server) createLocalUserWithImage(username, imageTag string) adminUserBatchCreateResult {
  result := adminUserBatchCreateResult{Username: username}
  if err := user.ValidateUsername(username); err != nil {
    result.Error = err.Error()
    return result
  }
  if auth.UserExists(username) {
    result.Error = "用户 " + username + " 已存在"
    return result
  }

  password := auth.GenerateRandomPassword(12)
  if err := auth.CreateUser(username, password, "user"); err != nil {
    result.Error = "创建用户失败: " + err.Error()
    return result
  }
  if err := user.InitUser(s.cfg, username, imageTag); err != nil {
    _ = auth.DeleteUser(username)
    result.Error = "初始化用户目录失败: " + err.Error()
    return result
  }
  s.applyDefaultSkillsToUser(username)
  if s.dockerAvailable {
    go s.autoStartUserContainer(username)
  }
  result.Success = true
  result.Password = password
  return result
}

func (s *Server) handleAdminUserDelete(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
    writeError(c, http.StatusForbidden, "普通用户由当前认证源同步，不允许手动删除")
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

  username := c.PostForm("username")
  if username == "" {
    writeError(c, http.StatusBadRequest, "用户名不能为空")
    return
  }
  if err := user.ValidateUsername(username); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  if auth.IsSuperadmin(username) {
    writeError(c, http.StatusBadRequest, "不能通过普通用户接口删除超管")
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
    writeError(c, http.StatusInternalServerError, "归档用户目录失败: "+err.Error())
    return
  }

  // 删除本地用户记录
  if err := auth.DeleteUser(username); err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }

  // 清理共享文件夹挂载记录
  auth.DeleteSharedFolderMountsByUser(username)

  logger.Audit("user.delete", "username", username, "operator", s.getSessionUser(c))
  writeSuccess(c, "用户 "+username+" 已删除并归档")
}
