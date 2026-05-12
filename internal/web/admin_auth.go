package web

import (
  "fmt"
  "net/http"
  "sort"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/authsource"
  "github.com/picoaide/picoaide/internal/logger"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// 认证配置 — LDAP 测试 & 用户搜索
// ============================================================

func (s *Server) handleAdminAuthTestLDAP(c *gin.Context) {
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

  host := c.PostForm("host")
  bindDN := c.PostForm("bind_dn")
  bindPassword := c.PostForm("bind_password")
  baseDN := c.PostForm("base_dn")
  filter := c.PostForm("filter")
  usernameAttr := c.PostForm("username_attribute")
  groupSearchMode := c.PostForm("group_search_mode")
  groupBaseDN := c.PostForm("group_base_dn")
  groupFilter := c.PostForm("group_filter")
  groupMemberAttr := c.PostForm("group_member_attribute")

  if host == "" || bindDN == "" || baseDN == "" {
    writeError(c, http.StatusBadRequest, "LDAP 地址、Bind DN 和 Base DN 不能为空")
    return
  }

  users, err := authsource.LDAPTestConnection(host, bindDN, bindPassword, baseDN, filter, usernameAttr)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 测试组查询（失败不影响用户测试结果）
  var groupError string
  groups, gerr := authsource.LDAPTestGroups(host, bindDN, bindPassword, baseDN, groupSearchMode, groupBaseDN, groupFilter, groupMemberAttr, usernameAttr)
  if gerr != nil {
    groupError = gerr.Error()
  }
  if groups == nil {
    groups = []authsource.GroupPreview{}
  }

  writeJSON(c, http.StatusOK, struct {
    Success    bool                      `json:"success"`
    Message    string                    `json:"message"`
    UserCount  int                       `json:"user_count"`
    Users      []string                  `json:"users"`
    Groups     []authsource.GroupPreview `json:"groups"`
    GroupError string                    `json:"group_error"`
  }{true, fmt.Sprintf("连接成功，找到 %d 个用户", len(users)), len(users), users, groups, groupError})
}

func (s *Server) handleAdminAuthLDAPUsers(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  var users []string
  source := strings.ToLower(strings.TrimSpace(c.Query("source")))
  if source == "directory" || source == "remote" || !s.cfg.UnifiedAuthEnabled() {
    var err error
    users, err = authsource.FetchUsers(s.cfg)
    if err != nil {
      writeError(c, http.StatusInternalServerError, err.Error())
      return
    }
  } else {
    localUsers, err := auth.GetExternalUsers(source)
    if err != nil {
      writeError(c, http.StatusInternalServerError, err.Error())
      return
    }
    users = make([]string, 0, len(localUsers))
    for _, u := range localUsers {
      if u.Role != "superadmin" {
        users = append(users, u.Username)
      }
    }
  }

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
  writeJSON(c, http.StatusOK, struct {
    Success    bool     `json:"success"`
    Users      []string `json:"users"`
    Page       int      `json:"page,omitempty"`
    PageSize   int      `json:"page_size,omitempty"`
    Total      int      `json:"total,omitempty"`
    TotalPages int      `json:"total_pages,omitempty"`
  }{true, users, page, pageSize, total, totalPages})
}

func (s *Server) handleAdminAuthSyncUsers(c *gin.Context) {
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
  if !authsource.HasDirectoryProvider(s.cfg) {
    writeError(c, http.StatusBadRequest, "当前认证方式不支持目录同步")
    return
  }

  authMode := s.cfg.AuthMode()
  result, err := s.syncUsersFromDirectory(true)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "同步账号失败: "+err.Error())
    return
  }

  message := fmt.Sprintf("同步完成，%s %d 个账号，允许 %d 个，写入本地用户 %d 个，新初始化 %d 个，补齐镜像 %d 个，移除本地普通登录凭据 %d 个",
    authMode,
    result.ProviderUserCount,
    result.AllowedUserCount,
    result.LocalUserSynced,
    result.InitializedCount,
    result.ImageUpdatedCount,
    result.DeletedLocalAuth,
  )
  if result.ArchivedStaleUsers > 0 {
    message += fmt.Sprintf("，清理过期账号 %d 个", result.ArchivedStaleUsers)
  }
  if result.InvalidUsernameCount > 0 {
    message += fmt.Sprintf("，跳过非法用户名 %d 个", result.InvalidUsernameCount)
  }

  logger.Audit("directory.users_sync", "operator", s.getSessionUser(c), "auth_mode", authMode, "initialized", result.InitializedCount, "image_updated", result.ImageUpdatedCount, "deleted_local_auth", result.DeletedLocalAuth, "cleanup", true)
  writeJSON(c, http.StatusOK, struct {
    Success bool              `json:"success"`
    Message string            `json:"message"`
    Result  *userSyncResult `json:"result"`
  }{true, message, result})
}

// ============================================================
// 白名单管理
// ============================================================

// handleAdminWhitelistGet 返回白名单列表
func (s *Server) handleAdminWhitelistGet(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  whitelist, err := user.LoadWhitelist()
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
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
  writeJSON(c, http.StatusOK, struct {
    Success    bool     `json:"success"`
    Users      []string `json:"users"`
    Page       int      `json:"page,omitempty"`
    PageSize   int      `json:"page_size,omitempty"`
    Total      int      `json:"total,omitempty"`
    TotalPages int      `json:"total_pages,omitempty"`
  }{true, users, page, pageSize, total, totalPages})
}

// handleAdminWhitelistPost 更新白名单
func (s *Server) handleAdminWhitelistPost(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }
  addStr := strings.TrimSpace(c.PostForm("add"))
  removeStr := strings.TrimSpace(c.PostForm("remove"))
  usersStr := c.PostForm("users")
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
  operator := s.getSessionUser(c)

  // 写入数据库（使用 xorm）
  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  changedCount := len(users)
  if addStr != "" || removeStr != "" {
    for _, u := range strings.Split(addStr, ",") {
      u = strings.TrimSpace(u)
      if u != "" {
        session.Exec("INSERT OR IGNORE INTO whitelist (username, added_by) VALUES (?, ?)", u, operator)
        changedCount++
      }
    }
    for _, u := range strings.Split(removeStr, ",") {
      u = strings.TrimSpace(u)
      if u != "" {
        session.Exec("DELETE FROM whitelist WHERE username = ?", u)
        changedCount++
      }
    }
  } else {
    session.Exec("DELETE FROM whitelist")
    for _, u := range users {
      session.Exec("INSERT OR IGNORE INTO whitelist (username, added_by) VALUES (?, ?)", u, operator)
    }
  }
  if err := session.Commit(); err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  writeSuccess(c, "白名单已更新")
  logger.Audit("whitelist.update", "count", changedCount, "operator", operator)
}
