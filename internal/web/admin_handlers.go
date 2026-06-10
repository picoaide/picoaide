package web

import (
  "fmt"
  "net/http"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/store"
  "github.com/picoaide/picoaide/internal/authsource"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/im"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// 认证源切换清理
// ============================================================

type authProviderSwitchCleanupResult struct {
  UsersRemoved      int64 `json:"users_removed"`
  GroupsCleared     bool  `json:"groups_cleared"`
  DirectoriesPurged bool  `json:"directories_purged"`
}

// ============================================================
// 超管权限检查
// ============================================================

func (s *Server) requireSuperadmin(c *gin.Context) string {
  username := s.requireAuth(c)
  if username == "" {
    return ""
  }
  if !store.IsSuperadmin(username) {
    writeError(c, http.StatusForbidden, "仅超级管理员可访问")
    return ""
  }
  return username
}

// ============================================================
// 统一用户初始化（本地/LDAP/OIDC/未来认证源共用）
// ============================================================

func (s *Server) initializeUser(username string) error {
  if err := user.ValidateUsername(username); err != nil {
    return err
  }
  if err := user.InitUser(s.loadConfig(), username); err != nil {
    return err
  }
  if _, err := store.AllocateIP(username); err != nil {
    return err
  }
  s.applyDefaultSkillsToUser(username)
  return nil
}

// ============================================================
// 认证源切换清理
// ============================================================

func (s *Server) purgeOrdinaryAuthProviderStateForConfig(cfg *config.GlobalConfig) (*authProviderSwitchCleanupResult, error) {
  result := &authProviderSwitchCleanupResult{}

  deletedUsers, usersRemoved, err := store.DeleteAllRegularUsers()
  if err != nil {
    return result, err
  }
  result.UsersRemoved = usersRemoved

  if err := store.ClearAllGroups(); err != nil {
    return result, err
  }
  result.GroupsCleared = true

  if err := user.RemoveAllUserData(cfg); err != nil {
    return result, err
  }
  result.DirectoriesPurged = true

  // 断开已删除用户的 IM 连接
  if s.agentIntegration != nil {
    for _, name := range deletedUsers {
      if dt, ok := s.agentIntegration.imGateway.GetProvider("dingtalk").(*im.DingTalkProvider); ok {
        dt.RemoveUser(name)
      }
      if fs, ok := s.agentIntegration.imGateway.GetProvider("feishu").(*im.FeishuProvider); ok {
        fs.RemoveUser(name)
      }
      if wc, ok := s.agentIntegration.imGateway.GetProvider("wecom").(*im.WeComProvider); ok {
        wc.RemoveUser(name)
      }
    }
  }

  return result, nil
}

// ============================================================
// 目录提供者用户同步
// ============================================================

type userSyncResult struct {
  ProviderUserCount    int
  AllowedUserCount     int
  LocalUserSynced      int
  InitializedCount     int
  DeletedLocalAuth     int
  ArchivedStaleUsers   int
  InvalidUsernameCount int
  GroupMemberCount     int
}

func (s *Server) syncUsersFromDirectory(cleanupStaleUsers bool) (*userSyncResult, error) {
  if !authsource.HasDirectoryProvider(s.loadConfig()) {
    return nil, fmt.Errorf("当前认证方式不支持目录同步")
  }

  authMode := s.loadConfig().AuthMode()
  userResult, err := authsource.SyncUserDirectory(authMode, s.loadConfig())
  if err != nil {
    return nil, err
  }
  result := &userSyncResult{
    ProviderUserCount:    userResult.ProviderUserCount,
    AllowedUserCount:     userResult.AllowedUserCount,
    LocalUserSynced:      userResult.LocalUserSynced,
    DeletedLocalAuth:     userResult.DeletedLocalAuth,
    InvalidUsernameCount: userResult.InvalidUsernameCount,
    GroupMemberCount:     userResult.GroupMemberCount,
  }

  for _, username := range userResult.AllowedUsers {
    if !store.UserExists(username) {
      continue
    }
    if err := s.initializeUser(username); err == nil {
      result.InitializedCount++
    }
  }

  if cleanupStaleUsers {
    localUsers, err := store.GetAllLocalUsers()
    if err != nil {
      return nil, err
    }
    for _, u := range localUsers {
      if u.Role == "superadmin" {
        continue
      }
      if u.Source == "" || u.Source == "local" {
        continue
      }
      if userResult.AllowedSet[u.Username] {
        continue
      }
      if err := user.ArchiveUser(s.loadConfig(), u.Username); err == nil {
        result.ArchivedStaleUsers++
      }
      _ = store.DeleteUser(u.Username)
    }
  }

  return result, nil
}


