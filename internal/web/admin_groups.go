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
  "github.com/picoaide/picoaide/internal/auth"
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
  groups, err := auth.ListGroups()
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  if groups == nil {
    groups = []auth.GroupInfo{}
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
    Groups      []auth.GroupInfo `json:"groups"`
    Page        int              `json:"page,omitempty"`
    PageSize    int              `json:"page_size,omitempty"`
    Total       int              `json:"total,omitempty"`
    TotalPages  int              `json:"total_pages,omitempty"`
  }{true, s.cfg.UnifiedAuthEnabled(), groups, page, pageSize, total, totalPages})
}

func (s *Server) handleAdminGroupCreate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
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
  if err := auth.CreateGroup(name, "local", description, parentID); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  writeSuccess(c, "组 "+name+" 创建成功")
}

func (s *Server) handleAdminGroupDelete(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
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
  if name == "" {
    writeError(c, http.StatusBadRequest, "组名不能为空")
    return
  }
  // 删除组之前获取成员、组绑定的技能和 ID
  gid, err := auth.GetGroupID(name)
  groupMembers := []string{}
  groupSkills := []string{}
  if err == nil {
    groupMembers, _ = auth.GetGroupMembersForDeploy(name)
    groupSkills, _ = auth.GetGroupSkills(name)
  }

  if err := auth.DeleteGroup(name); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 清理组绑定的技能文件
  for _, username := range groupMembers {
    for _, skillName := range groupSkills {
      has, err := auth.UserHasSkillFromAnySource(username, skillName)
      if err != nil {
        continue
      }
      if !has {
        targetDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills", skillName)
        os.RemoveAll(targetDir)
      }
    }
  }

  // 共享文件夹清理：移除该组的关联
  if gid > 0 {
    affectedFolders, _ := auth.RemoveGroupFromAllSharedFolders(gid)
    // 如果有孤立文件夹，通知管理员
    if len(affectedFolders) > 0 {
      // 需要重启的用户 = 原组成员中失去共享访问的用户
      restartUsers := []string{}
      for _, username := range groupMembers {
        needsRestart := false
        for _, folderID := range affectedFolders {
          ok, _ := auth.IsUserInSharedFolder(folderID, username)
          if !ok {
            needsRestart = true
            break
          }
        }
        if needsRestart {
          restartUsers = append(restartUsers, username)
        }
      }
      restartUsers = uniqueStrings(restartUsers)
      if len(restartUsers) > 0 {
        enqueueTask("mount-shared-folder", restartUsers, func(username string) error {
          return s.recreateUserContainerWithSharedMounts(username)
        })
      }
    }
  }

  writeSuccess(c, "组 "+name+" 已删除")
}

func (s *Server) handleAdminGroupMembersAdd(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
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
  if err := auth.AddUsersToGroup(groupName, usernames); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 审计日志：记录谁添加了哪些用户到组
  logger.Audit("group.members.add", "group", groupName, "usernames", usernames, "operator", s.getSessionUser(c))

  // 部署组绑定的技能到新加入成员
  for _, username := range usernames {
    user.DeployGroupSkillsToUser(s.cfg, username)
  }

  // 检查共享文件夹影响，自动重启新加入成员的容器
  gid, err := auth.GetGroupID(groupName)
  if err == nil {
    affected, _ := auth.OnGroupMembersAdded(gid, usernames)
    if len(affected) > 0 {
      enqueueTask("mount-shared-folder", affected, func(username string) error {
        return s.recreateUserContainerWithSharedMounts(username)
      })
    }
  }

  writeSuccess(c, fmt.Sprintf("已添加 %d 个用户到组 %s", len(usernames), groupName))
}

func validateLocalGroupMembers(usernames []string) error {
  for _, username := range usernames {
    if err := user.ValidateUsername(username); err != nil {
      return fmt.Errorf("用户 %s 不合法: %w", username, err)
    }
    if !auth.UserExists(username) {
      return fmt.Errorf("用户 %s 不存在", username)
    }
    if auth.GetUserRole(username) != "user" || auth.GetUserSource(username) != "local" {
      return fmt.Errorf("用户 %s 不是本地普通用户", username)
    }
  }
  return nil
}

func (s *Server) handleAdminGroupMembersRemove(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
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

  gid, _ := auth.GetGroupID(groupName)

  groupSkills := []string{}
  if gid > 0 {
    groupSkills, _ = auth.GetGroupSkills(groupName)
  }

  if err := auth.RemoveUserFromGroup(groupName, username); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 智能清理技能
  for _, skillName := range groupSkills {
    if err := util.SafePathSegment(skillName); err != nil {
      continue
    }
    has, err := auth.UserHasSkillFromAnySource(username, skillName)
    if err != nil {
      continue
    }
    if !has {
      targetDir := filepath.Clean(filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills", skillName))
      skillsBase := filepath.Clean(filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills"))
      if strings.HasPrefix(targetDir, skillsBase+string(os.PathSeparator)) {
        os.RemoveAll(targetDir)
      }
    }
  }

  // 检查共享文件夹影响，自动重启失去访问的用户容器
  if gid > 0 {
    affected, _ := auth.OnGroupMembersRemoved(gid, username)
    if len(affected) > 0 {
      enqueueTask("mount-shared-folder", affected, func(u string) error {
        return s.recreateUserContainerWithSharedMounts(u)
      })
    }
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
  if err := auth.BindSkillToGroup(groupName, skillName, ""); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 绑定后立即部署到组内所有用户（强制覆盖）
  members, err := auth.GetGroupMembersForDeploy(groupName)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "绑定成功但获取组成员失败: "+err.Error())
    return
  }
  userCount := 0
  deployFailCount := 0
  for _, username := range members {
    if err := s.deploySkillToUser(skillName, username); err == nil {
      userCount++
    } else {
      deployFailCount++
      slog.Warn("部署技能到组成员失败", "skill", skillName, "group", groupName, "username", username, "error", err)
    }
  }

  writeJSON(c, http.StatusOK, struct {
    Success   bool   `json:"success"`
    Message   string `json:"message"`
    UserCount int    `json:"user_count"`
  }{true, fmt.Sprintf("技能 %s 已绑定到组 %s 并部署到 %d 个用户", skillName, groupName, userCount), userCount})
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
  if err := auth.UnbindSkillFromGroup(groupName, skillName); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 智能清理：遍历组成员，检查是否还有其他来源
  members, _ := auth.GetGroupMembersForDeploy(groupName)
  cleanedCount := 0
  for _, username := range members {
    has, err := auth.UserHasSkillFromAnySource(username, skillName)
    if err != nil {
      continue
    }
    if !has {
      targetDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills", skillName)
      if err := os.RemoveAll(targetDir); err == nil {
        cleanedCount++
      }
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
  members, inheritedMembers, err := auth.GetGroupMembersWithSubGroups(groupName)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }
  skills, err := auth.GetGroupSkills(groupName)
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
  if skills == nil {
    skills = []string{}
  }
  writeJSON(c, http.StatusOK, struct {
    Success          bool     `json:"success"`
    Members          []string `json:"members"`
    InheritedMembers []string `json:"inherited_members"`
    Skills           []string `json:"skills"`
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
  if !authsource.HasDirectoryProvider(s.cfg) {
    writeError(c, http.StatusBadRequest, "当前认证方式不支持目录同步")
    return
  }

  authMode := s.cfg.AuthMode()
  result, err := s.syncUsersFromDirectory(false)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "同步账号失败: "+err.Error())
    return
  }

  groupCount := 0
  userCount := 0
  groupResult, err := authsource.SyncGroups(authMode, s.cfg, func(username string) error {
    if rec, _ := auth.GetContainerByUsername(username); rec == nil {
      return s.initExternalUser(username)
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
  }{true, fmt.Sprintf("同步完成，新初始化 %d 个账号，补齐镜像 %d 个，发现 %d 个组，共 %d 个组成员关系", result.InitializedCount, result.ImageUpdatedCount, groupCount, userCount), groupCount, userCount})
}
