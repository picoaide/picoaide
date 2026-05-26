package web

import (
  "fmt"
  "net/http"
  "sort"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
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

  // 本地用户（local_users 表）
  localUsers, _ := auth.GetAllLocalUsers()

  type UserInfo struct {
    Username string   `json:"username"`
    Source   string   `json:"source"`
    Role     string   `json:"role"`
    IP       string   `json:"ip"`
    Groups   []string `json:"groups,omitempty"`
  }

  var candidates []UserInfo
  for _, u := range localUsers {
    if u.Role == "superadmin" {
      continue
    }
    source := u.Source
    if source == "" {
      source = "local"
    }
    candidates = append(candidates, UserInfo{
      Username: u.Username,
      Source:   source,
      Role:     u.Role,
      IP:       u.IP,
    })
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

  for i := range list {
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
  }{true, list, s.loadConfig().AuthMode(), s.loadConfig().UnifiedAuthEnabled(), page, pageSize, total, totalPages})
}

// ============================================================
// 用户创建与删除（仅本地模式）
// ============================================================

func (s *Server) handleAdminUserCreate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.loadConfig().UnifiedAuthEnabled() {
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
  logger.DebugRecv("POST", "/api/admin/users/create", "username", username, "operator", s.getSessionUser(c))
  if err := user.ValidateUsername(username); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  if auth.UserExists(username) {
    writeError(c, http.StatusBadRequest, "用户 "+username+" 已存在")
    return
  }

  password := auth.GenerateRandomPassword(12)
  logger.DebugProcess("create_user", "username", username)
  if err := auth.CreateUser(username, password, "user"); err != nil {
    writeError(c, http.StatusInternalServerError, "创建用户失败: "+err.Error())
    return
  }

  logger.DebugProcess("init_user", "username", username)
  if err := s.initializeUser(username); err != nil {
    // 回滚：删除已创建的用户记录
    auth.DeleteUser(username)
    writeError(c, http.StatusInternalServerError, "初始化用户失败: "+err.Error())
    return
  }

  logger.Audit("user.create", "username", username, "operator", s.getSessionUser(c))
  logger.DebugSend("POST", "/api/admin/users/create", http.StatusOK, "username", username)
  writeJSON(c, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Message  string `json:"message"`
    Username string `json:"username"`
    Password string `json:"password"`
  }{true, "用户创建成功", username, password})
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
  if s.loadConfig().UnifiedAuthEnabled() {
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

  results := make([]adminUserBatchCreateResult, 0, len(usernames))
  created := 0
  failed := 0
  for _, username := range usernames {
    result := s.createLocalUser(username)
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

func (s *Server) createLocalUser(username string) adminUserBatchCreateResult {
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
  if err := s.initializeUser(username); err != nil {
    _ = auth.DeleteUser(username)
    result.Error = "初始化用户失败: " + err.Error()
    return result
  }
  result.Success = true
  result.Password = password
  return result
}

func (s *Server) handleAdminUserDelete(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.loadConfig().UnifiedAuthEnabled() {
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
  logger.DebugRecv("POST", "/api/admin/users/delete", "username", username, "operator", s.getSessionUser(c))
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

  // 归档用户目录
  logger.DebugProcess("archive_user", "username", username)
  if err := user.ArchiveUser(s.loadConfig(), username); err != nil {
    writeError(c, http.StatusInternalServerError, "归档用户目录失败: "+err.Error())
    return
  }

  // 删除本地用户记录
  logger.DebugProcess("delete_user_record", "username", username)
  if err := auth.DeleteUser(username); err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }

  // 清理共享文件夹挂载记录
  auth.DeleteSharedFolderMountsByUser(username)

  logger.Audit("user.delete", "username", username, "operator", s.getSessionUser(c))
  logger.DebugSend("POST", "/api/admin/users/delete", http.StatusOK, "username", username)
  writeSuccess(c, "用户 "+username+" 已删除并归档")
}
