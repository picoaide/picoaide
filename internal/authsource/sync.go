package authsource

import (
  "strings"

  "github.com/picoaide/picoaide/internal/store"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/user"
)

type UserSyncResult struct {
  ProviderUserCount    int
  AllowedUserCount     int
  LocalUserSynced      int
  DeletedLocalAuth     int
  InvalidUsernameCount int
  GroupMemberCount     int
  AllowedUsers         []string
  AllowedSet           map[string]bool
}

type GroupSyncResult struct {
  GroupCount  int
  MemberCount int
  Hierarchy   GroupHierarchy
}

func SyncUserDirectory(providerName string, cfg *config.GlobalConfig) (*UserSyncResult, error) {
  provider, err := directoryProvider(providerName)
  if err != nil {
    return nil, err
  }
  providerUsers, err := provider.FetchUsers(cfg)
  if err != nil {
    return nil, err
  }
  var whitelist map[string]bool
  if cfg.WhitelistEnabledForProvider(providerName) {
    whitelist, _ = user.LoadWhitelist()
  }
  result := &UserSyncResult{
    ProviderUserCount: len(providerUsers),
    AllowedSet:        make(map[string]bool),
  }

  for _, username := range providerUsers {
    username = strings.TrimSpace(username)
    if username == "" || !user.IsWhitelisted(whitelist, username) {
      continue
    }
    if err := user.ValidateUsername(username); err != nil {
      result.InvalidUsernameCount++
      continue
    }
    if !result.AllowedSet[username] {
      result.AllowedSet[username] = true
      result.AllowedUsers = append(result.AllowedUsers, username)
    }
  }
  result.AllowedUserCount = len(result.AllowedUsers)

  for _, username := range result.AllowedUsers {
    if err := store.EnsureExternalUser(username, "user", providerName); err != nil {
      return nil, err
    }
    result.LocalUserSynced++
    if groups, err := provider.FetchUserGroups(cfg, username); err == nil {
      if err := store.SyncUserGroups(username, groups, providerName); err == nil {
        result.GroupMemberCount += len(groups)
      }
    }
  }

  localUsers, err := store.GetAllLocalUsers()
  if err != nil {
    return nil, err
  }
  for _, localUser := range localUsers {
    if localUser.Role == "superadmin" {
      continue
    }
    if localUser.Source != "" && localUser.Source != "local" {
      continue
    }
    if err := store.DeleteUser(localUser.Username); err == nil {
      result.DeletedLocalAuth++
    }
  }

  return result, nil
}

func SyncGroups(providerName string, cfg *config.GlobalConfig, ensureUser func(string) error) (*GroupSyncResult, error) {
  provider, err := directoryProvider(providerName)
  if err != nil {
    return nil, err
  }
  groupMap, err := provider.FetchGroups(cfg)
  if err != nil {
    return nil, err
  }
  if len(groupMap) == 0 {
    return &GroupSyncResult{Hierarchy: GroupHierarchy{}}, nil
  }
  var whitelist map[string]bool
  if cfg.WhitelistEnabledForProvider(providerName) {
    whitelist, _ = user.LoadWhitelist()
  }
  groupMembers := make(map[string][]string, len(groupMap))
  result := &GroupSyncResult{Hierarchy: groupMap}

  for groupName, group := range groupMap {
    _ = store.CreateGroup(groupName, providerName, "", nil)
    result.GroupCount++
    var filtered []string
    for _, member := range group.Members {
      if whitelist != nil && !whitelist[member] {
        continue
      }
      if err := store.EnsureExternalUser(member, "user", providerName); err != nil {
        continue
      }
      if ensureUser != nil {
        if err := ensureUser(member); err != nil {
          continue
        }
      }
      filtered = append(filtered, member)
    }
    groupMembers[groupName] = filtered
    result.MemberCount += len(filtered)
  }
  if err := store.ReplaceGroupMembersBySource(providerName, groupMembers); err != nil {
    return nil, err
  }

  return result, nil
}
