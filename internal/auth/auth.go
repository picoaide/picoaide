package auth

import (
  "crypto/subtle"
  "encoding/base64"
  "fmt"
  "log/slog"
  "strings"

  "golang.org/x/crypto/argon2"
  "golang.org/x/crypto/bcrypt"

  "github.com/picoaide/picoaide/internal/store"
)

const argon2idHashPrefix = "$argon2id$"

// hashPassword 委托给 store.HashPassword，避免重复实现。
// store.argon2idHashPrefix 与 auth.argon2idHashPrefix 值相同，均为 "$argon2id$"。
func hashPassword(password string) (string, error) {
  return store.HashPassword(password)
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

// AuthenticateLocal 校验本地用户，返回 (是否成功, 角色, 错误)
func AuthenticateLocal(username, password string) (bool, string, error) {
  engine, err := store.GetEngine()
  if err != nil {
    return false, "", err
  }
  var user store.LocalUser
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
      if _, err := engine.ID(user.ID).Cols("password_hash").Update(&store.LocalUser{PasswordHash: hash}); err != nil {
        slog.Warn("密码哈希升级写入失败", "username", username, "error", err)
      }
    } else {
      slog.Warn("密码哈希升级生成失败", "username", username, "error", err)
    }
  }

  return true, user.Role, nil
}
