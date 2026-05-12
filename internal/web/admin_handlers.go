package web

import (
  "context"
  "fmt"
  "net/http"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/authsource"
  "github.com/picoaide/picoaide/internal/config"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// 认证源切换清理
// ============================================================

type authProviderSwitchCleanupResult struct {
  ContainersRemoved int   `json:"containers_removed"`
  ContainerRecords  int64 `json:"container_records"`
  UsersRemoved      int64 `json:"users_removed"`
  UsersScanned      int   `json:"users_scanned"`
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
  if !auth.IsSuperadmin(username) {
    writeError(c, http.StatusForbidden, "仅超级管理员可访问")
    return ""
  }
  return username
}

// ============================================================
// 默认镜像标签
// ============================================================

func (s *Server) defaultUserImageTag(ctx context.Context) (string, error) {
  if s.dockerAvailable {
    localTags, err := dockerpkg.ListLocalTags(ctx, s.cfg.Image.Name)
    if err != nil {
      return "", err
    }
    if len(localTags) > 0 {
      sortTagsForDisplay(localTags)
      return localTags[0], nil
    }
  }
  return strings.TrimSpace(s.cfg.Image.Tag), nil
}

// ============================================================
// 外部用户初始化 & LDAP 用户初始化
// ============================================================

func (s *Server) initLDAPUser(username string) error {
  return s.initExternalUser(username)
}

func (s *Server) initExternalUser(username string) error {
  if err := user.ValidateUsername(username); err != nil {
    return err
  }
  ctx := contextWithTimeout(10)
  imageTag, err := s.defaultUserImageTag(ctx)
  if err != nil {
    return fmt.Errorf("获取默认镜像失败: %w", err)
  }
  if imageTag == "" {
    return fmt.Errorf("未找到可用镜像，请先在镜像管理中拉取镜像")
  }
  if err := user.InitUser(s.cfg, username, imageTag); err != nil {
    return err
  }
  if s.dockerAvailable {
    go s.autoStartUserContainer(username)
  }
  return nil
}

// ============================================================
// 认证源切换清理
// ============================================================

func (s *Server) purgeOrdinaryAuthProviderStateForConfig(cfg *config.GlobalConfig) (*authProviderSwitchCleanupResult, error) {
  result := &authProviderSwitchCleanupResult{}

  containers, err := auth.GetAllContainers()
  if err != nil {
    return result, err
  }
  result.UsersScanned = len(containers)
  if s.dockerAvailable {
    ctx := context.Background()
    for _, rec := range containers {
      if rec.ContainerID != "" {
        _ = dockerpkg.Remove(ctx, rec.ContainerID)
        result.ContainersRemoved++
      }
      _ = dockerpkg.RemoveByUsername(ctx, rec.Username)
    }
  }
  records, err := auth.ClearAllContainers()
  if err != nil {
    return result, err
  }
  result.ContainerRecords = records

  usersRemoved, err := auth.DeleteAllRegularUsers()
  if err != nil {
    return result, err
  }
  result.UsersRemoved = usersRemoved

  if err := auth.ClearAllGroups(); err != nil {
    return result, err
  }
  result.GroupsCleared = true

  if err := user.RemoveAllUserData(cfg); err != nil {
    return result, err
  }
  result.DirectoriesPurged = true

  return result, nil
}

// ============================================================
// LDAP 用户同步
// ============================================================

type ldapUserSyncResult struct {
  LDAPUserCount        int
  AllowedUserCount     int
  LocalUserSynced      int
  InitializedCount     int
  ImageUpdatedCount    int
  DeletedLocalAuth     int
  ArchivedStaleUsers   int
  InvalidUsernameCount int
  GroupMemberCount     int
}

func (s *Server) syncLDAPUsersFromDirectory(cleanupStaleUsers bool) (*ldapUserSyncResult, error) {
  if !s.cfg.LDAPEnabled() {
    return nil, fmt.Errorf("LDAP 未启用")
  }

  userResult, err := authsource.SyncLDAPUserDirectory(s.cfg)
  if err != nil {
    return nil, err
  }
  result := &ldapUserSyncResult{
    LDAPUserCount:        userResult.ProviderUserCount,
    AllowedUserCount:     userResult.AllowedUserCount,
    LocalUserSynced:      userResult.LocalUserSynced,
    DeletedLocalAuth:     userResult.DeletedLocalAuth,
    InvalidUsernameCount: userResult.InvalidUsernameCount,
    GroupMemberCount:     userResult.GroupMemberCount,
  }

  ctx := contextWithTimeout(10)
  imageTag, err := s.defaultUserImageTag(ctx)
  if err != nil {
    return nil, fmt.Errorf("获取默认镜像失败: %w", err)
  }
  defaultImageRef := ""
  if imageTag != "" {
    defaultImageRef = s.cfg.Image.Name + ":" + imageTag
  }

  for _, username := range userResult.AllowedUsers {
    rec, err := auth.GetContainerByUsername(username)
    if err != nil {
      return nil, err
    }
    if rec == nil {
      if imageTag == "" {
        return nil, fmt.Errorf("用户 %s 需要初始化，但未找到可用镜像，请先在镜像管理中拉取镜像", username)
      }
      if err := user.InitUser(s.cfg, username, imageTag); err != nil {
        return nil, err
      }
      result.InitializedCount++
      if s.dockerAvailable {
        go s.autoStartUserContainer(username)
      }
    } else if rec.Image == "" && defaultImageRef != "" {
      if err := auth.UpdateContainerImage(username, defaultImageRef); err != nil {
        return nil, err
      }
      result.ImageUpdatedCount++
      if s.dockerAvailable && rec.ContainerID == "" {
        go s.autoStartUserContainer(username)
      }
    } else if rec.Image == "" {
      return nil, fmt.Errorf("用户 %s 缺少镜像，但未找到可用镜像，请先在镜像管理中拉取镜像", username)
    }
  }

  if cleanupStaleUsers {
    containers, err := auth.GetAllContainers()
    if err != nil {
      return nil, err
    }
    for _, rec := range containers {
      if userResult.AllowedSet[rec.Username] || auth.IsSuperadmin(rec.Username) {
        continue
      }
      if rec.ContainerID != "" && s.dockerAvailable {
        _ = dockerpkg.Remove(context.Background(), rec.ContainerID)
      }
      _ = auth.DeleteContainer(rec.Username)
      if err := user.ArchiveUser(s.cfg, rec.Username); err == nil {
        result.ArchivedStaleUsers++
      }
    }
  }

  if err := user.RemoveAllUserData(s.cfg); err != nil {
    return nil, fmt.Errorf("清空用户目录和归档目录失败: %w", err)
  }

  return result, nil
}
