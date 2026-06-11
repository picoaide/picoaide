package store

import (
  "crypto/aes"
  "crypto/cipher"
  "crypto/rand"
  "crypto/sha256"
  "encoding/hex"
  "errors"
  "fmt"
  "io"
  "time"
)

// ============================================================
// 会话密钥管理（供加密使用）
// ============================================================

var sessionSecret string

// SetSessionSecret 设置会话密钥，用于密码加密
func SetSessionSecret(secret string) {
  sessionSecret = secret
}

// ============================================================
// 密码加密/解密（AES-256-GCM）
// ============================================================

// deriveEncryptionKey 从会话密钥派生 AES-256 密钥
func deriveEncryptionKey() ([]byte, error) {
  if sessionSecret == "" {
    return nil, errors.New("会话密钥未设置")
  }
  hash := sha256.Sum256([]byte(sessionSecret))
  return hash[:], nil
}

// encryptPassword 使用 AES-256-GCM 加密明文密码，返回 hex 编码
func encryptPassword(plaintext string) (string, error) {
  key, err := deriveEncryptionKey()
  if err != nil {
    return "", err
  }

  block, err := aes.NewCipher(key)
  if err != nil {
    return "", fmt.Errorf("创建 AES 加密器失败: %w", err)
  }

  gcm, err := cipher.NewGCM(block)
  if err != nil {
    return "", fmt.Errorf("创建 GCM 失败: %w", err)
  }

  nonce := make([]byte, gcm.NonceSize())
  if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
    return "", fmt.Errorf("生成随机数失败: %w", err)
  }

  ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
  return hex.EncodeToString(ciphertext), nil
}

// decryptPassword 解密 hex 编码的 AES-256-GCM 密文
func decryptPassword(cipherHex string) (string, error) {
  key, err := deriveEncryptionKey()
  if err != nil {
    return "", err
  }

  ciphertext, err := hex.DecodeString(cipherHex)
  if err != nil {
    return "", fmt.Errorf("解码 hex 失败: %w", err)
  }

  block, err := aes.NewCipher(key)
  if err != nil {
    return "", fmt.Errorf("创建 AES 解密器失败: %w", err)
  }

  gcm, err := cipher.NewGCM(block)
  if err != nil {
    return "", fmt.Errorf("创建 GCM 失败: %w", err)
  }

  nonceSize := gcm.NonceSize()
  if len(ciphertext) < nonceSize {
    return "", errors.New("密文太短")
  }

  nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
  plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
  if err != nil {
    return "", fmt.Errorf("解密失败: %w", err)
  }

  return string(plaintext), nil
}

// ============================================================
// 用户邮件配置 CRUD
// ============================================================

// GetUserEmail 获取用户的邮件配置（密码保持加密状态）
func GetUserEmail(username string) (*UserEmail, error) {
  engine, err := GetEngine()
  if err != nil {
    return nil, fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  var ue UserEmail
  has, err := engine.Where("username = ?", username).Get(&ue)
  if err != nil {
    return nil, fmt.Errorf("查询用户邮件配置失败: %w", err)
  }
  if !has {
    return nil, nil
  }
  return &ue, nil
}

// GetUserEmailWithDecryptedPassword 获取用户的邮件配置并解密密码
func GetUserEmailWithDecryptedPassword(username string) (*UserEmail, error) {
  ue, err := GetUserEmail(username)
  if err != nil {
    return nil, err
  }
  if ue == nil {
    return nil, nil
  }

  decrypted, err := decryptPassword(ue.LoginPassword)
  if err != nil {
    return nil, fmt.Errorf("解密密码失败: %w", err)
  }
  ue.LoginPassword = decrypted
  return ue, nil
}

// UpsertUserEmail 创建或更新用户的邮件配置（自动加密密码）
func UpsertUserEmail(ue *UserEmail) error {
  engine, err := GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  encrypted, err := encryptPassword(ue.LoginPassword)
  if err != nil {
    return fmt.Errorf("加密密码失败: %w", err)
  }
  ue.LoginPassword = encrypted

  now := time.Now().Format("2006-01-02 15:04:05")

  var existing UserEmail
  has, err := engine.Where("username = ?", ue.Username).Get(&existing)
  if err != nil {
    return fmt.Errorf("查询用户邮件配置失败: %w", err)
  }

  if has {
    ue.ID = existing.ID
    ue.CreatedAt = existing.CreatedAt
    ue.UpdatedAt = now
    if _, err := engine.ID(ue.ID).AllCols().Omit("id", "created_at").Update(ue); err != nil {
      return fmt.Errorf("更新用户邮件配置失败: %w", err)
    }
  } else {
    ue.CreatedAt = now
    ue.UpdatedAt = now
    if _, err := engine.Insert(ue); err != nil {
      return fmt.Errorf("创建用户邮件配置失败: %w", err)
    }
  }

  return nil
}

// DeleteUserEmail 删除用户的邮件配置
func DeleteUserEmail(username string) error {
  engine, err := GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  if _, err := engine.Where("username = ?", username).Delete(&UserEmail{}); err != nil {
    return fmt.Errorf("删除用户邮件配置失败: %w", err)
  }
  return nil
}
