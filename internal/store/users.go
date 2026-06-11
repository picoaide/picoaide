package store

import (
  "crypto/rand"
  "encoding/base64"
  "fmt"
  "log/slog"
  "math/big"

  "golang.org/x/crypto/argon2"
)

// ============================================================
// 用户数据访问
// ============================================================

func HashPassword(password string) (string, error) {
  salt := make([]byte, passwordHashParams.saltLen)
  if _, err := rand.Read(salt); err != nil {
    return "", err
  }
  key := argon2.IDKey([]byte(password), salt, passwordHashParams.time, passwordHashParams.memory, passwordHashParams.threads, passwordHashParams.keyLen)
  return fmt.Sprintf("%sv=%d$m=%d,t=%d,p=%d$%s$%s",
    argon2idHashPrefix,
    argon2.Version,
    passwordHashParams.memory,
    passwordHashParams.time,
    passwordHashParams.threads,
    base64.RawStdEncoding.EncodeToString(salt),
    base64.RawStdEncoding.EncodeToString(key),
  ), nil
}

// CreateUser 创建本地用户
func CreateUser(username, password, role string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  hash, err := HashPassword(password)
  if err != nil {
    return fmt.Errorf("密码哈希失败: %w", err)
  }
  user := &LocalUser{
    Username:     username,
    PasswordHash: string(hash),
    Role:         role,
    Source:       "local",
  }
  _, err = engine.Insert(user)
  if err != nil {
    return fmt.Errorf("创建用户失败: %w", err)
  }
  return nil
}

// EnsureExternalUser 确保统一认证用户在本地用户表存在。
func EnsureExternalUser(username, role, source string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  if role == "" {
    role = "user"
  }
  if source == "" {
    source = "external"
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    return err
  }
  var existing LocalUser
  has, err := session.Where("username = ?", username).Get(&existing)
  if err != nil {
    _ = session.Rollback()
    return fmt.Errorf("查询用户失败: %w", err)
  }
  if has && existing.Role == "superadmin" {
    _ = session.Rollback()
    return fmt.Errorf("用户 %s 是本地超管，不能转换为统一认证用户", username)
  }
  randomPassword := GenerateRandomPassword(32)
  hash, err := HashPassword(randomPassword)
  if err != nil {
    _ = session.Rollback()
    return fmt.Errorf("密码哈希失败: %w", err)
  }
  if has {
    _, err = session.Where("username = ?", username).
      Cols("password_hash", "role", "source").
      Update(&LocalUser{PasswordHash: hash, Role: role, Source: source})
    if err != nil {
      _ = session.Rollback()
      return fmt.Errorf("更新统一认证用户失败: %w", err)
    }
    return session.Commit()
  }
  _, err = session.Insert(&LocalUser{
    Username:     username,
    PasswordHash: hash,
    Role:         role,
    Source:       source,
  })
  if err != nil {
    _ = session.Rollback()
    return fmt.Errorf("创建统一认证用户失败: %w", err)
  }
  return session.Commit()
}

// ChangePassword 修改本地用户密码
func ChangePassword(username, newPassword string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  hash, err := HashPassword(newPassword)
  if err != nil {
    return fmt.Errorf("密码哈希失败: %w", err)
  }
  affected, err := engine.Where("username = ?", username).
    Cols("password_hash").
    Update(&LocalUser{PasswordHash: string(hash)})
  if err != nil {
    return fmt.Errorf("更新密码失败: %w", err)
  }
  if affected == 0 {
    return fmt.Errorf("用户 %s 不存在", username)
  }
  return nil
}

// GenerateRandomPassword 生成指定长度的随机密码
func GenerateRandomPassword(length int) string {
  const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%&*"
  b := make([]byte, length)
  for i := range b {
    n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
    b[i] = charset[n.Int64()]
  }
  return string(b)
}

// UserExists 检查本地用户是否存在
func UserExists(username string) bool {
  if ensureDB() != nil {
    return false
  }
  has, err := engine.Where("username = ?", username).Exist(&LocalUser{})
  if err != nil {
    slog.Error("检查用户是否存在失败", "username", username, "error", err)
    return false
  }
  return has
}

// GetUserSource 返回用户来源：local、ldap 或其他外部认证来源。
func GetUserSource(username string) string {
  if ensureDB() != nil {
    return ""
  }
  var user LocalUser
  has, err := engine.Where("username = ?", username).Get(&user)
  if err != nil || !has {
    return ""
  }
  if user.Source == "" {
    return "local"
  }
  return user.Source
}

// IsExternalUser 检查用户是否来自统一认证源。
func IsExternalUser(username string) bool {
  source := GetUserSource(username)
  return source != "" && source != "local"
}

// GetAllLocalUsers 返回所有本地用户
func GetAllLocalUsers() ([]LocalUser, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var users []LocalUser
  err := engine.OrderBy("username").Find(&users)
  if err != nil {
    return nil, err
  }
  // 转换为精简结构
  result := make([]LocalUser, 0, len(users))
  for _, u := range users {
    result = append(result, LocalUser{Username: u.Username, Role: u.Role, Source: u.Source, IP: u.IP})
  }
  return result, nil
}

// GetExternalUsers 返回已同步到本地的统一认证用户。
func GetExternalUsers(source string) ([]LocalUser, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var users []LocalUser
  query := engine.Where("source != '' AND source != ?", "local")
  if source != "" {
    query = engine.Where("source = ?", source)
  }
  if err := query.OrderBy("username").Find(&users); err != nil {
    return nil, err
  }
  result := make([]LocalUser, 0, len(users))
  for _, u := range users {
    result = append(result, LocalUser{Username: u.Username, Role: u.Role, Source: u.Source})
  }
  return result, nil
}

// GetSuperadmins 返回所有超管列表
func GetSuperadmins() ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var users []LocalUser
  err := engine.Where("role = ?", "superadmin").OrderBy("username").Find(&users)
  if err != nil {
    return nil, err
  }
  list := make([]string, 0, len(users))
  for _, u := range users {
    list = append(list, u.Username)
  }
  return list, nil
}

// IsSuperadmin 检查指定用户是否是超管
func IsSuperadmin(username string) bool {
  if ensureDB() != nil {
    return false
  }
  var user LocalUser
  has, err := engine.Where("username = ?", username).Get(&user)
  if err != nil || !has {
    return false
  }
  return user.Role == "superadmin"
}

// GetUserRole 获取用户角色
func GetUserRole(username string) string {
  if ensureDB() != nil {
    return ""
  }
  var user LocalUser
  has, err := engine.Where("username = ?", username).Get(&user)
  if err != nil || !has {
    return ""
  }
  return user.Role
}

// DeleteUser 删除本地用户（含所有关联数据）
func DeleteUser(username string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    return err
  }
  if _, err := session.Where("username = ?", username).Delete(&UserSkill{}); err != nil {
    _ = session.Rollback()
    return fmt.Errorf("删除技能绑定失败: %w", err)
  }
  if _, err := session.Where("username = ?", username).Delete(&UserGroup{}); err != nil {
    _ = session.Rollback()
    return fmt.Errorf("删除组成员关系失败: %w", err)
  }
  if _, err := session.Exec("DELETE FROM mcp_tokens WHERE username = ?", username); err != nil {
    _ = session.Rollback()
    return fmt.Errorf("删除 MCP token 失败: %w", err)
  }
  if _, err := session.Where("username = ?", username).Delete(&UserCookie{}); err != nil {
    _ = session.Rollback()
    return fmt.Errorf("删除 Cookie 授权失败: %w", err)
  }
  if _, err := session.Where("username = ?", username).Delete(&UserChannel{}); err != nil {
    _ = session.Rollback()
    return fmt.Errorf("删除渠道配置失败: %w", err)
  }
  if _, err := session.Where("username = ?", username).Delete(&SharedFolderMount{}); err != nil {
    _ = session.Rollback()
    return fmt.Errorf("删除共享文件夹挂载失败: %w", err)
  }
  if _, err := session.Exec("DELETE FROM mcp_server_grants WHERE grant_type='user' AND grant_value=?", username); err != nil {
    _ = session.Rollback()
    return fmt.Errorf("删除 MCP server 授权失败: %w", err)
  }
  affected, err := session.Where("username = ?", username).Delete(&LocalUser{})
  if err != nil {
    _ = session.Rollback()
    return fmt.Errorf("删除用户失败: %w", err)
  }
  if affected == 0 {
    _ = session.Rollback()
    return fmt.Errorf("用户 %s 不存在", username)
  }
  return session.Commit()
}

// DeleteAllRegularUsers removes all non-superadmin user records and related
// user-scoped database state. Local superadmin accounts are intentionally kept.
// Returns the list of deleted usernames and the count.
func DeleteAllRegularUsers() ([]string, int64, error) {
  if err := ensureDB(); err != nil {
    return nil, 0, err
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    return nil, 0, err
  }

  users := make([]LocalUser, 0)
  if err := session.Where("role != ?", "superadmin").Find(&users); err != nil {
    _ = session.Rollback()
    return nil, 0, err
  }
  usernames := make([]string, 0, len(users))
  for _, u := range users {
    usernames = append(usernames, u.Username)
  }

  for _, username := range usernames {
    if _, err := session.Where("username = ?", username).Delete(&UserGroup{}); err != nil {
      _ = session.Rollback()
      return nil, 0, err
    }
    if _, err := session.Where("username = ?", username).Delete(&UserChannel{}); err != nil {
      _ = session.Rollback()
      return nil, 0, err
    }
    if _, err := session.Where("username = ?", username).Delete(&UserSkill{}); err != nil {
      _ = session.Rollback()
      return nil, 0, err
    }
    if _, err := session.Exec("DELETE FROM mcp_tokens WHERE username = ?", username); err != nil {
      _ = session.Rollback()
      return nil, 0, err
    }
    if _, err := session.Where("username = ?", username).Delete(&UserCookie{}); err != nil {
      _ = session.Rollback()
      return nil, 0, err
    }
    if _, err := session.Where("username = ?", username).Delete(&SharedFolderMount{}); err != nil {
      _ = session.Rollback()
      return nil, 0, err
    }
    if _, err := session.Exec("DELETE FROM mcp_server_grants WHERE grant_type='user' AND grant_value=?", username); err != nil {
      _ = session.Rollback()
      return nil, 0, err
    }
  }
  affected, err := session.Where("role != ?", "superadmin").Delete(&LocalUser{})
  if err != nil {
    _ = session.Rollback()
    return nil, 0, err
  }
  return usernames, affected, session.Commit()
}

// ClearAllGroups removes all groups and their bindings. Group membership is
// source-owned and must be rehydrated by the active authentication provider.
func ClearAllGroups() error {
  if err := ensureDB(); err != nil {
    return err
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    return err
  }
  if _, err := session.Exec("DELETE FROM user_groups"); err != nil {
    _ = session.Rollback()
    return err
  }
  if _, err := session.Exec("DELETE FROM groups"); err != nil {
    _ = session.Rollback()
    return err
  }
  return session.Commit()
}
