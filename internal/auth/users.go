package auth

import (
  "crypto/rand"
  "crypto/subtle"
  "encoding/base64"
  "fmt"
  "math/big"
  "strings"

  "golang.org/x/crypto/argon2"
  "golang.org/x/crypto/bcrypt"
)

// ============================================================
// 用户认证管理
// ============================================================

func hashPassword(password string) (string, error) {
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

func verifyPassword(storedHash, password string) (ok bool, needsUpgrade bool, err error) {
  if strings.HasPrefix(storedHash, argon2idHashPrefix) {
    ok, err := verifyArgon2idPassword(storedHash, password)
    return ok, false, err
  }

  if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)); err != nil {
    return false, false, nil
  }
  return true, true, nil
}

func verifyArgon2idPassword(storedHash, password string) (bool, error) {
  parts := strings.Split(storedHash, "$")
  if len(parts) != 6 || parts[1] != "argon2id" {
    return false, nil
  }

  var version int
  if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
    return false, nil
  }

  var memory, iterations uint32
  var threads uint8
  if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
    return false, nil
  }

  salt, err := base64.RawStdEncoding.DecodeString(parts[4])
  if err != nil {
    return false, nil
  }
  expectedKey, err := base64.RawStdEncoding.DecodeString(parts[5])
  if err != nil {
    return false, nil
  }
  actualKey := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(expectedKey)))
  return subtle.ConstantTimeCompare(actualKey, expectedKey) == 1, nil
}

// CreateUser 创建本地用户
func CreateUser(username, password, role string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  hash, err := hashPassword(password)
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
// 密码写入随机不可知值；统一认证模式下登录仍只走外部认证。
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

  var existing LocalUser
  has, err := engine.Where("username = ?", username).Get(&existing)
  if err != nil {
    return fmt.Errorf("查询用户失败: %w", err)
  }
  if has && existing.Role == "superadmin" {
    return fmt.Errorf("用户 %s 是本地超管，不能转换为统一认证用户", username)
  }

  randomPassword := GenerateRandomPassword(32)
  hash, err := hashPassword(randomPassword)
  if err != nil {
    return fmt.Errorf("密码哈希失败: %w", err)
  }

  if has {
    _, err = engine.Where("username = ?", username).
      Cols("password_hash", "role", "source").
      Update(&LocalUser{PasswordHash: hash, Role: role, Source: source})
    if err != nil {
      return fmt.Errorf("更新统一认证用户失败: %w", err)
    }
    return nil
  }

  _, err = engine.Insert(&LocalUser{
    Username:     username,
    PasswordHash: hash,
    Role:         role,
    Source:       source,
  })
  if err != nil {
    return fmt.Errorf("创建统一认证用户失败: %w", err)
  }
  return nil
}

// AuthenticateLocal 校验本地用户，返回 (是否成功, 角色, 错误)
func AuthenticateLocal(username, password string) (bool, string, error) {
  if err := ensureDB(); err != nil {
    return false, "", err
  }
  var user LocalUser
  has, err := engine.Where("username = ?", username).Get(&user)
  if err != nil {
    return false, "", fmt.Errorf("查询用户失败: %w", err)
  }
  if !has {
    return false, "", nil
  }

  ok, needsUpgrade, err := verifyPassword(user.PasswordHash, password)
  if err != nil {
    return false, "", fmt.Errorf("校验密码失败: %w", err)
  }
  if !ok {
    return false, "", nil
  }
  if needsUpgrade {
    if hash, err := hashPassword(password); err == nil {
      _, _ = engine.ID(user.ID).Cols("password_hash").Update(&LocalUser{PasswordHash: hash})
    }
  }

  return true, user.Role, nil
}

// UserExists 检查本地用户是否存在
func UserExists(username string) bool {
  if ensureDB() != nil {
    return false
  }
  has, _ := engine.Where("username = ?", username).Exist(&LocalUser{})
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
  // 转换为只含 Username/Role 的结构（保持原返回类型）
  result := make([]LocalUser, 0, len(users))
  for _, u := range users {
    result = append(result, LocalUser{Username: u.Username, Role: u.Role, Source: u.Source})
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

// ChangePassword 修改本地用户密码
func ChangePassword(username, newPassword string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  hash, err := hashPassword(newPassword)
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

// DeleteUser 删除本地用户（含技能绑定）
func DeleteUser(username string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  if _, err := engine.Where("username = ?", username).Delete(&UserSkill{}); err != nil {
    return fmt.Errorf("删除技能绑定失败: %w", err)
  }
  affected, err := engine.Where("username = ?", username).Delete(&LocalUser{})
  if err != nil {
    return fmt.Errorf("删除用户失败: %w", err)
  }
  if affected == 0 {
    return fmt.Errorf("用户 %s 不存在", username)
  }
  return nil
}

// DeleteAllRegularUsers removes all non-superadmin user records and related
// user-scoped database state. Local superadmin accounts are intentionally kept.
func DeleteAllRegularUsers() (int64, error) {
  if err := ensureDB(); err != nil {
    return 0, err
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    return 0, err
  }

  users := make([]LocalUser, 0)
  if err := session.Where("role != ?", "superadmin").Find(&users); err != nil {
    _ = session.Rollback()
    return 0, err
  }
  usernames := make([]string, 0, len(users))
  for _, u := range users {
    usernames = append(usernames, u.Username)
  }

  for _, username := range usernames {
    if _, err := session.Where("username = ?", username).Delete(&UserGroup{}); err != nil {
      _ = session.Rollback()
      return 0, err
    }
    if _, err := session.Where("username = ?", username).Delete(&UserChannel{}); err != nil {
      _ = session.Rollback()
      return 0, err
    }
    if _, err := session.Where("username = ?", username).Delete(&UserSkill{}); err != nil {
      _ = session.Rollback()
      return 0, err
    }
  }
  affected, err := session.Where("role != ?", "superadmin").Delete(&LocalUser{})
  if err != nil {
    _ = session.Rollback()
    return 0, err
  }
  return affected, session.Commit()
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

// ClearAllContainers removes all ordinary user container records.
func ClearAllContainers() (int64, error) {
  if err := ensureDB(); err != nil {
    return 0, err
  }
  res, err := engine.Exec("DELETE FROM containers")
  if err != nil {
    return 0, err
  }
  return res.RowsAffected()
}
