package web

import (
  "fmt"
  "log/slog"
  "net/http"
  "os"
  "path/filepath"
  "strconv"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/store"
  "github.com/picoaide/picoaide/internal/authsource"
  "github.com/picoaide/picoaide/internal/logger"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 用户组管理
// ============================================================

func (s *Server) handleAdminGroups(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  groups, err := store.ListGroups()
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  if groups == nil {
    groups = []store.GroupInfo{}
  }
  pager := parsePagination(c, 50, 200)
  if pager.Search != "" {
    filtered := groups[:0]
    for _, g := range groups {
      if strings.Contains(strings.ToLower(g.Name), pager.Search) ||
        strings.Contains(strings.ToLower(g.Source), pager.Search) ||
        strings.Contains(strings.ToLower(g.Description), pager.Search) {
        filtered = append(filtered, g)
      }
    }
    groups = filtered
  }
  groups, total, totalPages, page, pageSize := paginateSlice(groups, pager)
  writeJSON(c, http.StatusOK, struct {
    Success     bool             `json:"success"`
    UnifiedAuth bool             `json:"unified_auth"`
    Groups      []store.GroupInfo `json:"groups"`
    Page        int              `json:"page,omitempty"`
    PageSize    int              `json:"page_size,omitempty"`
    Total       int              `json:"total,omitempty"`
    TotalPages  int              `json:"total_pages,omitempty"`
  }{true, s.loadConfig().UnifiedAuthEnabled(), groups, page, pageSize, total, totalPages})
}

func (s *Server) handleAdminGroupCreate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.loadConfig().UnifiedAuthEnabled() {
    writeError(c, http.StatusForbidden, "用户组由当前认证源同步，不允许手动创建")
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
  description := strings.TrimSpace(c.PostForm("description"))
  parentIDStr := strings.TrimSpace(c.PostForm("parent_id"))
  logger.DebugRecv("POST", "/api/admin/groups/create", "group", name, "operator", s.getSessionUser(c))
  if name == "" {
    writeError(c, http.StatusBadRequest, "组名不能为空")
    return
  }
  var parentID *int64
  if parentIDStr != "" {
    pid, err := strconv.ParseInt(parentIDStr, 10, 64)
    if err != nil {
      writeError(c, http.StatusBadRequest, "无效的父组 ID")
      return
    }
    parentID = &pid
  }
  logger.DebugProcess("create_group", "group", name, "parent_id", parentID)
  if err := store.CreateGroup(name, "local", description, parentID); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  logger.DebugSend("POST", "/api/admin/groups/create", http.StatusOK, "group", name)
  writeSuccess(c, "组 "+name+" 创建成功")
}

func (s *Server) handleAdminGroupDelete(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.loadConfig().UnifiedAuthEnabled() {
    writeError(c, http.StatusForbidden, "用户组由当前认证源同步，不允许手动删除")
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
  logger.DebugRecv("POST", "/api/admin/groups/delete", "group", name, "operator", s.getSessionUser(c))
  if name == "" {
    writeError(c, http.StatusBadRequest, "组名不能为空")
    return
  }
  gid, _ := store.GetGroupID(name)

  logger.DebugProcess("delete_group", "group", name)
  if err := store.DeleteGroup(name); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 共享文件夹清理：移除该组的关联
  if gid > 0 {
    store.RemoveGroupFromAllSharedFolders(gid)
  }

  logger.DebugSend("POST", "/api/admin/groups/delete", http.StatusOK, "group", name)
  writeSuccess(c, "组 "+name+" 已删除")
}

func (s *Server) handleAdminGroupMembersAdd(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.loadConfig().UnifiedAuthEnabled() {
    writeError(c, http.StatusForbidden, "用户组成员由当前认证源同步，不允许手动修改")
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
  groupName := strings.TrimSpace(c.PostForm("group_name"))
  usersStr := strings.TrimSpace(c.PostForm("usernames"))
  if groupName == "" || usersStr == "" {
    writeError(c, http.StatusBadRequest, "组名和用户名不能为空")
    return
  }
  usernames := parseBatchUsernames(usersStr)
  if len(usernames) == 0 {
    writeError(c, http.StatusBadRequest, "用户名不能为空")
    return
  }
  if err := validateLocalGroupMembers(usernames); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  if err := store.AddUsersToGroup(groupName, usernames); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 审计日志：记录谁添加了哪些用户到组
  logger.Audit("group.members.add", "group", groupName, "usernames", usernames, "operator", s.getSessionUser(c))

  writeSuccess(c, fmt.Sprintf("已添加 %d 个用户到组 %s", len(usernames), groupName))
}

func validateLocalGroupMembers(usernames []string) error {
  for _, username := range usernames {
    if err := user.ValidateUsername(username); err != nil {
      return fmt.Errorf("用户 %s 不合法: %w", username, err)
    }
    if !store.UserExists(username) {
      return fmt.Errorf("用户 %s 不存在", username)
    }
    if store.GetUserRole(username) != "user" || store.GetUserSource(username) != "local" {
      return fmt.Errorf("用户 %s 不是本地普通用户", username)
    }
  }
  return nil
}

func (s *Server) handleAdminGroupMembersRemove(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.loadConfig().UnifiedAuthEnabled() {
    writeError(c, http.StatusForbidden, "用户组成员由当前认证源同步，不允许手动修改")
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
  groupName := strings.TrimSpace(c.PostForm("group_name"))
  username := strings.TrimSpace(c.PostForm("username"))
  if groupName == "" || username == "" {
    writeError(c, http.StatusBadRequest, "组名和用户名不能为空")
    return
  }
  // 审计日志：记录谁从组移除了用户
  logger.Audit("group.members.remove", "group", groupName, "username", username, "operator", s.getSessionUser(c))

  if err := store.RemoveUserFromGroup(groupName, username); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  writeSuccess(c, "已从组 "+groupName+" 移除 "+username)
}

func (s *Server) handleAdminGroupSkillsBind(c *gin.Context) {
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
  groupName := strings.TrimSpace(c.PostForm("group_name"))
  skillName := strings.TrimSpace(c.PostForm("skill_name"))
  if groupName == "" || skillName == "" {
    writeError(c, http.StatusBadRequest, "组名和技能名不能为空")
    return
  }
  if err := util.SafePathSegment(skillName); err != nil {
    writeError(c, http.StatusBadRequest, "技能名称不合法")
    return
  }

  // 展开组成员，部署成功后绑定到每个用户
  members, err := store.GetGroupMembersForDeploy(groupName)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "获取组成员失败: "+err.Error())
    return
  }
  userCount := 0
  for _, username := range members {
    if err := store.BindSkillToUser(username, skillName, "group"); err == nil {
      userCount++
    } else {
      slog.Warn("绑定技能到组成员失败", "skill", skillName, "group", groupName, "username", username, "error", err)
    }
  }

  writeJSON(c, http.StatusOK, struct {
    Success   bool   `json:"success"`
    Message   string `json:"message"`
    UserCount int    `json:"user_count"`
  }{true, fmt.Sprintf("技能 %s 已绑定到组 %s 的 %d 个用户", skillName, groupName, userCount), userCount})
}

func (s *Server) handleAdminGroupSkillsUnbind(c *gin.Context) {
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
  groupName := strings.TrimSpace(c.PostForm("group_name"))
  skillName := strings.TrimSpace(c.PostForm("skill_name"))
  if groupName == "" || skillName == "" {
    writeError(c, http.StatusBadRequest, "组名和技能名不能为空")
    return
  }
  if err := util.SafePathSegment(skillName); err != nil {
    writeError(c, http.StatusBadRequest, "技能名称不合法")
    return
  }

  // 展开组成员，仅对 source 为 "group" 的记录解绑 + 清理文件
  members, _ := store.GetGroupMembersForDeploy(groupName)
  cleanedCount := 0
  for _, username := range members {
    src, _ := store.GetUserSkillSource(username, skillName)
    if src != "group" {
      continue
    }
    if err := store.UnbindSkillFromUser(username, skillName); err != nil {
      slog.Error("解绑技能失败", "skill", skillName, "username", username, "error", err)
    }
    targetDir := filepath.Join(user.UserDir(s.loadConfig(), username), "skills", skillName)
    if err := os.RemoveAll(targetDir); err == nil {
      cleanedCount++
    }
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":       true,
    "message":       fmt.Sprintf("已从组 %s 解绑技能 %s，已清理 %d 个用户文件", groupName, skillName, cleanedCount),
    "cleaned_users": cleanedCount,
  })
}

func (s *Server) handleAdminGroupMembers(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  groupName := c.Query("name")
  if groupName == "" {
    writeError(c, http.StatusBadRequest, "组名不能为空")
    return
  }
  members, inheritedMembers, err := store.GetGroupMembersWithSubGroups(groupName)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  if members == nil {
    members = []string{}
  }
  if inheritedMembers == nil {
    inheritedMembers = []string{}
  }
  pager := parsePagination(c, 50, 200)
  if pager.Search != "" {
    filterMembers := func(input []string) []string {
      filtered := input[:0]
      for _, username := range input {
        if strings.Contains(strings.ToLower(username), pager.Search) {
          filtered = append(filtered, username)
        }
      }
      return filtered
    }
    members = filterMembers(members)
    inheritedMembers = filterMembers(inheritedMembers)
  }
  members, memberTotal, memberTotalPages, memberPage, memberPageSize := paginateSlice(members, pager)

  // 查询该组绑定的技能（通过 user_skills.source='group' 去重）
  skills := getGroupSkills(groupName)

  writeJSON(c, http.StatusOK, struct {
    Success          bool     `json:"success"`
    Members          []string `json:"members"`
    InheritedMembers []string `json:"inherited_members"`
    Skills           []string `json:"skills,omitempty"`
    Page             int      `json:"page,omitempty"`
    PageSize         int      `json:"page_size,omitempty"`
    Total            int      `json:"total,omitempty"`
    TotalPages       int      `json:"total_pages,omitempty"`
  }{true, members, inheritedMembers, skills, memberPage, memberPageSize, memberTotal, memberTotalPages})
}

// handleAdminAuthSyncGroups 手动触发 LDAP 组同步
func (s *Server) handleAdminAuthSyncGroups(c *gin.Context) {
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
  if !authsource.HasDirectoryProvider(s.loadConfig()) {
    writeError(c, http.StatusBadRequest, "当前认证方式不支持目录同步")
    return
  }

  authMode := s.loadConfig().AuthMode()
  result, err := s.syncUsersFromDirectory(false)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "同步账号失败: "+err.Error())
    return
  }

  groupCount := 0
  userCount := 0
  groupResult, err := authsource.SyncGroups(authMode, s.loadConfig(), func(username string) error {
    if !store.UserExists(username) {
      return s.initializeUser(username)
    }
    return nil
  })
  if err != nil {
    writeError(c, http.StatusInternalServerError, "同步组失败: "+err.Error())
    return
  }
  groupCount = groupResult.GroupCount
  userCount = groupResult.MemberCount
  s.syncGroupParents(groupResult.Hierarchy)

  writeJSON(c, http.StatusOK, struct {
    Success     bool   `json:"success"`
    Message     string `json:"message"`
    GroupCount  int    `json:"group_count"`
    MemberCount int    `json:"member_count"`
  }{true, fmt.Sprintf("同步完成，新初始化 %d 个账号，发现 %d 个组，共 %d 个组成员关系", result.InitializedCount, groupCount, userCount), groupCount, userCount})
}

// getGroupSkills 查询指定组绑定的技能（通过 user_skills.source='group' 去重）
func getGroupSkills(groupName string) []string {
  members, _, err := store.GetGroupMembersWithSubGroups(groupName)
  if err != nil || len(members) == 0 {
    return nil
  }
  // 查询这些成员中 source='group' 的技能
  skillMap := make(map[string]bool)
  for _, username := range members {
    skills, _ := store.GetUserSkills(username)
    for _, sk := range skills {
      src, _ := store.GetUserSkillSource(username, sk)
      if src == "group" {
        skillMap[sk] = true
      }
    }
  }
  result := make([]string, 0, len(skillMap))
  for sk := range skillMap {
    result = append(result, sk)
  }
  return result
}
