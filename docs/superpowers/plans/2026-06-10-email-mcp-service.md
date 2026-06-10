# Email MCP 服务实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 增加 Email MCP 服务，用户通过 Web UI 配置邮箱（SMTP+IMAP），Agent 通过 MCP 工具收发邮件。

**Architecture:** 独立 `user_emails` 表存储配置（密码 AES-GCM 加密）→ `internal/email/` 包实现 SMTP/IMAP 操作 → 注册为 `"email"` MCP 服务（`RegisterPicoaideService`）→ 新增 9 个 MCP 工具 → 新增 REST API + Web UI 标签页。

**Tech Stack:** Go, `github.com/emersion/go-imap`, `net/smtp`, AES-256-GCM, xorm

---

## 文件变更清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/store/models.go` | 新增 `UserEmail` 结构体 |
| 修改 | `internal/store/store.go` | `syncSchema()` 中追加 `user_emails` 建表 SQL |
| 新增 | `internal/store/user_emails.go` | CRUD + 密码加密/解密 |
| 新增 | `internal/store/migrations/20260610_120000_create_user_emails.go` | 建表迁移 |
| 新增 | `internal/email/types.go` | `Config`, `OutgoingMessage`, `MessageSummary`, `Message`, `Attachment`, `Folder` |
| 新增 | `internal/email/errors.go` | `AuthError`, `NetworkError`, `ProtocolError`, `TimeoutError` |
| 新增 | `internal/email/smtp.go` | SMTP 发送实现 |
| 新增 | `internal/email/imap.go` | IMAP 读取/搜索/管理实现 |
| 新增 | `internal/web/email_tools.go` | MCP 工具定义 + handlers |
| 新增 | `internal/web/email_api.go` | REST API handlers (get/save/test/delete) |
| 修改 | `internal/web/server.go` | 注册 email API 路由 |
| 修改 | `internal/web/mcp_service.go` | 注册 email 服务 + 路由 email_ 前缀工具 |
| 新增 | `internal/web/ui/manage/modules/email.js` | 前端逻辑 |
| 新增 | `internal/web/ui/manage/templates/email.html` | UI 模板 |
| 修改 | `go.mod` | 新增 `go-imap` 依赖 |

---

### Task 1: 依赖安装 + 数据库模型

**Files:**
- Modify: `go.mod`
- Modify: `internal/store/models.go`
- Modify: `internal/store/store.go`

- [ ] **Step 1: 添加 go-imap 依赖**

```bash
go get github.com/emersion/go-imap@v1.2.1 github.com/emersion/go-imap/client@v1.2.1 github.com/emersion/go-message@v0.18.1
```

- [ ] **Step 2: 在 models.go 中新增 UserEmail 结构体**

在 `UserChannel` 结构体后追加：

```go
// ============================================================
// UserEmail（邮件配置）
// ============================================================

// UserEmail 存储用户邮件账号配置（SMTP + IMAP）
type UserEmail struct {
  ID            int64  `xorm:"pk autoincr 'id'"`
  Username      string `xorm:"unique notnull 'username'"`
  Email         string `xorm:"notnull 'email'"`
  SMTPHost      string `xorm:"notnull 'smtp_host'"`
  SMTPPort      int    `xorm:"notnull default 587 'smtp_port'"`
  SMTPTLS       bool   `xorm:"default 1 'smtp_tls'"`
  IMAPHost      string `xorm:"notnull 'imap_host'"`
  IMAPPort      int    `xorm:"notnull default 993 'imap_port'"`
  IMAPTLS       bool   `xorm:"default 1 'imap_tls'"`
  LoginUser     string `xorm:"notnull 'login_user'"`
  LoginPassword string `xorm:"notnull 'login_password'"`
  Enabled       bool   `xorm:"default 0 'enabled'"`
  TestResult    string `xorm:"'test_result'"`
  CreatedAt     string `xorm:"notnull 'created_at'"`
  UpdatedAt     string `xorm:"notnull 'updated_at'"`
}

func (UserEmail) TableName() string { return "user_emails" }
```

- [ ] **Step 3: 在 syncSchema() 中追加建表 SQL**

在 `syncSchema()` 末尾、`migrations.RunAll(engine)` 之前追加：

```go
_, err = engine.Exec(`CREATE TABLE IF NOT EXISTS user_emails (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  email TEXT NOT NULL,
  smtp_host TEXT NOT NULL,
  smtp_port INTEGER NOT NULL DEFAULT 587,
  smtp_tls INTEGER NOT NULL DEFAULT 1,
  imap_host TEXT NOT NULL,
  imap_port INTEGER NOT NULL DEFAULT 993,
  imap_tls INTEGER NOT NULL DEFAULT 1,
  login_user TEXT NOT NULL,
  login_password TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 0,
  test_result TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
  updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime'))
)`)
if err != nil { return err }
```

- [ ] **Step 4: 验证编译通过**

```bash
go build ./...
```

---

### Task 2: 密码加密工具 + Store CRUD

**Files:**
- Create: `internal/store/user_emails.go`
- Test: `internal/store/user_emails_test.go`

- [ ] **Step 1: 编写密码加解密函数**

在 `internal/store/user_emails.go` 中：

```go
package store

import (
  "crypto/aes"
  "crypto/cipher"
  "crypto/rand"
  "encoding/hex"
  "errors"
  "fmt"
  "io"
)

// deriveEncryptionKey 从 session secret 派生 AES-256 密钥
func deriveEncryptionKey() ([]byte, error) {
  secret := GetSessionSecret()
  if secret == "" {
    return nil, errors.New("session secret 未配置")
  }
  // 使用 SHA-256 派生 32 字节密钥
  h := sha256.Sum256([]byte(secret))
  return h[:], nil
}

// encryptPassword AES-GCM 加密
func encryptPassword(plaintext string) (string, error) {
  key, err := deriveEncryptionKey()
  if err != nil {
    return "", err
  }
  block, err := aes.NewCipher(key)
  if err != nil {
    return "", fmt.Errorf("创建 AES cipher 失败: %w", err)
  }
  gcm, err := cipher.NewGCM(block)
  if err != nil {
    return "", fmt.Errorf("创建 GCM 失败: %w", err)
  }
  nonce := make([]byte, gcm.NonceSize())
  if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
    return "", fmt.Errorf("生成 nonce 失败: %w", err)
  }
  ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
  return hex.EncodeToString(ciphertext), nil
}

// decryptPassword AES-GCM 解密
func decryptPassword(cipherHex string) (string, error) {
  key, err := deriveEncryptionKey()
  if err != nil {
    return "", err
  }
  ciphertext, err := hex.DecodeString(cipherHex)
  if err != nil {
    return "", fmt.Errorf("hex 解码失败: %w", err)
  }
  block, err := aes.NewCipher(key)
  if err != nil {
    return "", fmt.Errorf("创建 AES cipher 失败: %w", err)
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
```

- [ ] **Step 2: 编写 GetSessionSecret 函数**

找 `GetSessionSecret` 的定义位置，它应该在 store 包中。添加或确认已有：

```go
// 如果不存在，在 user_emails.go 中添加
var sessionSecret string

func SetSessionSecret(secret string) {
  sessionSecret = secret
}

func GetSessionSecret() string {
  return sessionSecret
}
```

- [ ] **Step 3: 编写 CRUD 函数**

```go
// GetUserEmail 获取用户邮箱配置（密码保持加密状态）
func GetUserEmail(username string) (*UserEmail, error) {
  engine, err := GetEngine()
  if err != nil {
    return nil, fmt.Errorf("获取数据库引擎失败: %w", err)
  }
  var ue UserEmail
  has, err := engine.Where("username = ?", username).Get(&ue)
  if err != nil {
    return nil, fmt.Errorf("查询用户邮箱配置失败: %w", err)
  }
  if !has {
    return nil, nil
  }
  return &ue, nil
}

// GetUserEmailWithDecryptedPassword 获取邮箱配置并解密密码
func GetUserEmailWithDecryptedPassword(username string) (*UserEmail, error) {
  ue, err := GetUserEmail(username)
  if err != nil || ue == nil {
    return ue, err
  }
  plain, err := decryptPassword(ue.LoginPassword)
  if err != nil {
    return nil, fmt.Errorf("解密密码失败: %w", err)
  }
  ue.LoginPassword = plain
  return ue, nil
}

// UpsertUserEmail 创建或更新邮箱配置（自动加密密码）
func UpsertUserEmail(ue *UserEmail) error {
  engine, err := GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  // 加密密码
  encrypted, err := encryptPassword(ue.LoginPassword)
  if err != nil {
    return fmt.Errorf("加密密码失败: %w", err)
  }
  ue.LoginPassword = encrypted

  var existing UserEmail
  has, err := engine.Where("username = ?", ue.Username).Get(&existing)
  if err != nil {
    return fmt.Errorf("查询用户邮箱配置失败: %w", err)
  }

  now := time.Now().Format("2006-01-02 15:04:05")
  if has {
    ue.ID = existing.ID
    ue.CreatedAt = existing.CreatedAt
    ue.UpdatedAt = now
    if _, err := engine.ID(ue.ID).Update(ue); err != nil {
      return fmt.Errorf("更新用户邮箱配置失败: %w", err)
    }
  } else {
    ue.CreatedAt = now
    ue.UpdatedAt = now
    if _, err := engine.Insert(ue); err != nil {
      return fmt.Errorf("创建用户邮箱配置失败: %w", err)
    }
  }
  return nil
}

// DeleteUserEmail 删除用户邮箱配置
func DeleteUserEmail(username string) error {
  engine, err := GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库引擎失败: %w", err)
  }
  _, err = engine.Where("username = ?", username).Delete(&UserEmail{})
  if err != nil {
    return fmt.Errorf("删除用户邮箱配置失败: %w", err)
  }
  return nil
}
```

- [ ] **Step 4: 编写测试**

```go
// internal/store/user_emails_test.go
package store

import (
  "testing"
)

func TestEncryptDecryptPassword(t *testing.T) {
  SetSessionSecret("test-secret-key-for-testing-only-12345")
  original := "my-email-password-123!@#"
  encrypted, err := encryptPassword(original)
  if err != nil {
    t.Fatalf("encryptPassword 失败: %v", err)
  }
  if encrypted == original {
    t.Error("加密后的密码不能和原文相同")
  }
  decrypted, err := decryptPassword(encrypted)
  if err != nil {
    t.Fatalf("decryptPassword 失败: %v", err)
  }
  if decrypted != original {
    t.Errorf("解密结果不匹配: got %q, want %q", decrypted, original)
  }
}

func TestEncryptDecryptWithDifferentSecret(t *testing.T) {
  SetSessionSecret("secret-1")
  encrypted, _ := encryptPassword("password")
  SetSessionSecret("secret-2")
  _, err := decryptPassword(encrypted)
  if err == nil {
    t.Error("使用不同的密钥解密应该失败")
  }
  SetSessionSecret("secret-1")
}

func TestUpsertGetDeleteUserEmail(t *testing.T) {
  // 使用 ResetDB 初始化
  ResetDB()
  SetSessionSecret("test-secret")

  ue := &UserEmail{
    Username: "testuser",
    Email:    "test@example.com",
    SMTPHost: "smtp.example.com",
    SMTPPort: 587,
    IMAPHost: "imap.example.com",
    IMAPPort: 993,
    LoginUser: "test@example.com",
    LoginPassword: "supersecret",
    Enabled: true,
  }

  // Create
  if err := UpsertUserEmail(ue); err != nil {
    t.Fatalf("UpsertUserEmail 创建失败: %v", err)
  }

  // Get (encrypted)
  got, err := GetUserEmail("testuser")
  if err != nil {
    t.Fatalf("GetUserEmail 失败: %v", err)
  }
  if got == nil {
    t.Fatal("GetUserEmail 返回 nil")
  }
  if got.LoginPassword == "supersecret" {
    t.Error("密码应该被加密存储")
  }

  // Get with decryption
  got2, err := GetUserEmailWithDecryptedPassword("testuser")
  if err != nil {
    t.Fatalf("GetUserEmailWithDecryptedPassword 失败: %v", err)
  }
  if got2.LoginPassword != "supersecret" {
    t.Errorf("解密密码不匹配: got %q, want %q", got2.LoginPassword, "supersecret")
  }

  // Update
  ue.SMTPPort = 465
  if err := UpsertUserEmail(ue); err != nil {
    t.Fatalf("UpsertUserEmail 更新失败: %v", err)
  }
  got3, _ := GetUserEmail("testuser")
  if got3.SMTPPort != 465 {
    t.Errorf("更新 SMTPPort 失败: got %d", got3.SMTPPort)
  }

  // Delete
  if err := DeleteUserEmail("testuser"); err != nil {
    t.Fatalf("DeleteUserEmail 失败: %v", err)
  }
  got4, _ := GetUserEmail("testuser")
  if got4 != nil {
    t.Error("删除后 GetUserEmail 应返回 nil")
  }
}
```

- [ ] **Step 5: 运行测试**

```bash
cd /data/picoaide && go test ./internal/store/ -run "TestEncrypt|TestUserEmail" -v
```

- [ ] **Step 6: 提交**

```bash
git add internal/store/models.go internal/store/store.go internal/store/user_emails.go internal/store/user_emails_test.go go.mod go.sum
git commit -m "feat: add user_emails table with encrypted password storage"
```

---

### Task 3: 迁移文件

**Files:**
- Create: `internal/store/migrations/20260610_120000_create_user_emails.go`

- [ ] **Step 1: 编写迁移**

```go
package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20260610120000",
    Desc:      "创建 user_emails 表",
    Up: func(engine *xorm.Engine) error {
      _, err := engine.Exec(`CREATE TABLE IF NOT EXISTS user_emails (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        username TEXT NOT NULL UNIQUE,
        email TEXT NOT NULL,
        smtp_host TEXT NOT NULL,
        smtp_port INTEGER NOT NULL DEFAULT 587,
        smtp_tls INTEGER NOT NULL DEFAULT 1,
        imap_host TEXT NOT NULL,
        imap_port INTEGER NOT NULL DEFAULT 993,
        imap_tls INTEGER NOT NULL DEFAULT 1,
        login_user TEXT NOT NULL,
        login_password TEXT NOT NULL,
        enabled INTEGER NOT NULL DEFAULT 0,
        test_result TEXT NOT NULL DEFAULT '',
        created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
        updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime'))
      )`)
      return err
    },
  })
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./...
```

- [ ] **Step 3: 提交**

```bash
git add internal/store/migrations/20260610_120000_create_user_emails.go
git commit -m "feat: add user_emails migration"
```

---

### Task 4: Email 底层包 — 类型和错误

**Files:**
- Create: `internal/email/types.go`
- Create: `internal/email/errors.go`

- [ ] **Step 1: 编写 types.go**

```go
package email

import "time"

// ============================================================
// Config
// ============================================================

// Config 邮件服务配置（已解密密码）
type Config struct {
  Email     string
  SMTPHost  string
  SMTPPort  int
  SMTPTLS   bool
  IMAPHost  string
  IMAPPort  int
  IMAPTLS   bool
  LoginUser string
  LoginPass string
}

// ============================================================
// OutgoingMessage
// ============================================================

// Attachment 邮件附件
type Attachment struct {
  Filename string
  MimeType string
  Content  []byte
}

// OutgoingMessage 待发送的邮件
type OutgoingMessage struct {
  To       []string
  Cc       []string
  Bcc      []string
  Subject  string
  BodyHTML string
  ReplyTo  string
  InReplyTo string
  References string
  Attachments []Attachment
}

// ============================================================
// IncomingMessage
// ============================================================

// MessageSummary 邮件列表摘要
type MessageSummary struct {
  UID     uint32    `json:"uid"`
  Subject string    `json:"subject"`
  From    string    `json:"from"`
  Date    time.Time `json:"date"`
  Flags   []string  `json:"flags"`
}

// Message 完整邮件
type Message struct {
  UID         uint32       `json:"uid"`
  Subject     string       `json:"subject"`
  From        string       `json:"from"`
  To          []string     `json:"to"`
  Cc          []string     `json:"cc"`
  Date        time.Time    `json:"date"`
  BodyText    string       `json:"bodyText"`
  BodyHTML    string       `json:"bodyHtml"`
  Attachments []AttachmentInfo `json:"attachments"`
  Flags       []string     `json:"flags"`
  MessageID   string       `json:"messageId"`
  InReplyTo   string       `json:"inReplyTo"`
  References  string       `json:"references"`
}

// AttachmentInfo 附件信息（不含内容，仅元数据）
type AttachmentInfo struct {
  Filename string `json:"filename"`
  MimeType string `json:"mimeType"`
  Size     int64  `json:"size"`
}

// ============================================================
// Folder
// ============================================================

// Folder 邮件文件夹
type Folder struct {
  Name    string `json:"name"`
  Delimiter string `json:"delimiter"`
  Unread  uint32 `json:"unread"`
}
```

- [ ] **Step 2: 编写 errors.go**

```go
package email

import "fmt"

// ============================================================
// 错误类型
// ============================================================

type AuthError struct {
  Err error
}

func (e *AuthError) Error() string {
  return fmt.Sprintf("邮件认证失败: %v", e.Err)
}

func (e *AuthError) Unwrap() error { return e.Err }

type NetworkError struct {
  Err error
}

func (e *NetworkError) Error() string {
  return fmt.Sprintf("邮件网络连接失败: %v", e.Err)
}

func (e *NetworkError) Unwrap() error { return e.Err }

type ProtocolError struct {
  Err error
}

func (e *ProtocolError) Error() string {
  return fmt.Sprintf("邮件协议错误: %v", e.Err)
}

func (e *ProtocolError) Unwrap() error { return e.Err }

type TimeoutError struct {
  Msg string
}

func (e *TimeoutError) Error() string {
  return e.Msg
}

// ============================================================
// 辅助函数
// ============================================================

func IsAuthError(err error) bool {
  var e *AuthError
  return err != nil && (As(err, &e) || isAuthRelated(err))
}

// isAuthRelated 检查底层错误是否是认证相关
func isAuthRelated(err error) bool {
  s := err.Error()
  return containsAny(s, []string{"authentication", "login", "auth", "bad credentials", "535", "AUTHENTICATE", "LOGIN failed"})
}

func containsAny(s string, substrs []string) bool {
  for _, sub := range substrs {
    if containsIgnoreCase(s, sub) {
      return true
    }
  }
  return false
}

func containsIgnoreCase(s, substr string) bool {
  sLower := toLower(s)
  subLower := toLower(substr)
  return len(sLower) >= len(subLower) && containsString(sLower, subLower)
}

// As 是 errors.As 的别名，用于避免 import 冲突
func As(err error, target interface{}) bool {
  return fmt.Errorf("%w", err) == err // placeholder
}
```

实际使用 `errors.As` 而非自定义实现。写为：

```go
func IsAuthError(err error) bool {
  var e *AuthError
  return errors.As(err, &e)
}
```

- [ ] **Step 3: 编译验证**

```bash
go build ./internal/email/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/email/
git commit -m "feat: add email package types and errors"
```

---

### Task 5: Email 包 — SMTP 发送

**Files:**
- Create: `internal/email/smtp.go`
- Test: `internal/email/smtp_test.go`

- [ ] **Step 1: 编写 SMTP 发送函数**

```go
package email

import (
  "bytes"
  "crypto/tls"
  "fmt"
  "mime"
  "net"
  "net/smtp"
  "strings"
  "time"
)

// dialSMTPServer 拨号 SMTP 服务器，支持 SSL/TLS 和 STARTTLS
func dialSMTPServer(cfg *Config) (*smtp.Client, error) {
  addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
  var conn net.Conn
  var err error

  if cfg.SMTPTLS {
    // SSL/TLS 直连（465）
    conn, err = tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, &tls.Config{
      ServerName: cfg.SMTPHost,
    })
  } else {
    // 明文连接（587），后续 STARTTLS
    conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
  }
  if err != nil {
    return nil, &NetworkError{Err: fmt.Errorf("连接 SMTP 服务器失败: %w", err)}
  }

  client, err := smtp.NewClient(conn, cfg.SMTPHost)
  if err != nil {
    conn.Close()
    return nil, &ProtocolError{Err: fmt.Errorf("创建 SMTP 客户端失败: %w", err)}
  }

  // STARTTLS
  if !cfg.SMTPTLS {
    if ok, _ := client.Extension("STARTTLS"); ok {
      if err := client.StartTLS(&tls.Config{ServerName: cfg.SMTPHost}); err != nil {
        client.Close()
        return nil, &ProtocolError{Err: fmt.Errorf("STARTTLS 失败: %w", err)}
      }
    }
  }

  // 认证
  if ok, _ := client.Extension("AUTH"); ok {
    auth := smtp.PlainAuth("", cfg.LoginUser, cfg.LoginPass, cfg.SMTPHost)
    if err := client.Auth(auth); err != nil {
      client.Close()
      return nil, &AuthError{Err: fmt.Errorf("SMTP 认证失败: %w", err)}
    }
  }

  return client, nil
}

// BuildMIMEMessage 构建 MIME 多部分消息
func BuildMIMEMessage(msg *OutgoingMessage, cfg *Config) ([]byte, error) {
  var buf bytes.Buffer
  boundary := fmt.Sprintf("=_%d", time.Now().UnixNano())

  // 通用头
  buf.WriteString(fmt.Sprintf("From: %s\r\n", cfg.Email))
  buf.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(msg.To, ", ")))
  if len(msg.Cc) > 0 {
    buf.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(msg.Cc, ", ")))
  }
  buf.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("utf-8", msg.Subject)))
  buf.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
  buf.WriteString(fmt.Sprintf("Message-ID: <%s@%s>\r\n", fmt.Sprintf("%d", time.Now().UnixNano()), cfg.SMTPHost))
  buf.WriteString("MIME-Version: 1.0\r\n")

  if msg.InReplyTo != "" {
    buf.WriteString(fmt.Sprintf("In-Reply-To: %s\r\n", msg.InReplyTo))
  }
  if msg.References != "" {
    buf.WriteString(fmt.Sprintf("References: %s\r\n", msg.References))
  }
  if msg.ReplyTo != "" {
    buf.WriteString(fmt.Sprintf("Reply-To: %s\r\n", msg.ReplyTo))
  }

  if len(msg.Attachments) > 0 {
    buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
    buf.WriteString("\r\n--" + boundary + "\r\n")
    buf.WriteString("Content-Type: multipart/alternative; boundary=\"alt-" + boundary + "\"\r\n\r\n")
    buf.WriteString("--alt-" + boundary + "\r\n")
    buf.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
    buf.WriteString(stripHTML(msg.BodyHTML) + "\r\n")
    buf.WriteString("--alt-" + boundary + "\r\n")
    buf.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n\r\n")
    buf.WriteString(msg.BodyHTML + "\r\n")
    buf.WriteString("--alt-" + boundary + "--\r\n")
    for _, a := range msg.Attachments {
      buf.WriteString("\r\n--" + boundary + "\r\n")
      buf.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", a.MimeType, mime.QEncoding.Encode("utf-8", a.Filename)))
      buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", mime.QEncoding.Encode("utf-8", a.Filename)))
      buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
      // Base64 编码内容
      encoded := make([]byte, base64.StdEncoding.EncodedLen(len(a.Content)))
      base64.StdEncoding.Encode(encoded, a.Content)
      // 每 76 字符换行
      for i := 0; i < len(encoded); i += 76 {
        end := i + 76
        if end > len(encoded) {
          end = len(encoded)
        }
        buf.Write(encoded[i:end])
        buf.WriteString("\r\n")
      }
    }
    buf.WriteString("\r\n--" + boundary + "--\r\n")
  } else {
    buf.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
    buf.WriteString("--" + boundary + "\r\n")
    buf.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
    buf.WriteString(stripHTML(msg.BodyHTML) + "\r\n")
    buf.WriteString("--" + boundary + "\r\n")
    buf.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n\r\n")
    buf.WriteString(msg.BodyHTML + "\r\n")
    buf.WriteString("--" + boundary + "--\r\n")
  }

  return buf.Bytes(), nil
}

// stripHTML 去除 HTML 标签，得到纯文本
func stripHTML(html string) string {
  var result strings.Builder
  inTag := false
  for _, r := range html {
    if r == '<' {
      inTag = true
    } else if r == '>' {
      inTag = false
    } else if !inTag {
      result.WriteRune(r)
    }
  }
  return result.String()
}

// SendMail 发送邮件，返回 Message-ID
func SendMail(cfg *Config, msg *OutgoingMessage) (string, error) {
  client, err := dialSMTPServer(cfg)
  if err != nil {
    return "", err
  }
  defer client.Close()

  // 发件人
  if err := client.Mail(cfg.Email); err != nil {
    return "", &ProtocolError{Err: fmt.Errorf("设置发件人失败: %w", err)}
  }

  // 收件人
  allRecipients := append(append(append([]string{}, msg.To...), msg.Cc...), msg.Bcc...)
  for _, addr := range allRecipients {
    if err := client.Rcpt(addr); err != nil {
      return "", &ProtocolError{Err: fmt.Errorf("添加收件人 %s 失败: %w", addr, err)}
    }
  }

  // 构建消息体
  data, err := BuildMIMEMessage(msg, cfg)
  if err != nil {
    return "", fmt.Errorf("构建邮件消息失败: %w", err)
  }

  // 发送
  w, err := client.Data()
  if err != nil {
    return "", &ProtocolError{Err: fmt.Errorf("获取 Data writer 失败: %w", err)}
  }
  if _, err := w.Write(data); err != nil {
    return "", &ProtocolError{Err: fmt.Errorf("写入邮件数据失败: %w", err)}
  }
  if err := w.Close(); err != nil {
    return "", &ProtocolError{Err: fmt.Errorf("关闭 Data writer 失败: %w", err)}
  }

  // 从消息体中提取 Message-ID
  return extractMessageID(data), nil
}

// extractMessageID 从原始邮件中提取 Message-ID
func extractMessageID(data []byte) string {
  lines := bytes.Split(data, []byte("\r\n"))
  for _, line := range lines {
    if bytes.HasPrefix(bytes.ToLower(line), []byte("message-id:")) {
      return strings.TrimSpace(string(bytes.TrimPrefix(line, []byte("Message-ID:"))))
    }
  }
  return ""
}
```

需要添加 import：
```go
import (
  "bytes"
  "crypto/tls"
  "encoding/base64"  // 需要添加
  "fmt"
  "mime"
  "net"
  "net/smtp"
  "strings"
  "time"
)
```

- [ ] **Step 2: 编写 SMTP 单元测试**

```go
package email

import (
  "testing"
)

func TestStripHTML(t *testing.T) {
  tests := []struct {
    name     string
    input    string
    expected string
  }{
    {"simple", "<p>Hello</p>", "Hello"},
    {"mixed", "Hello <b>World</b>!", "Hello World!"},
    {"no tags", "Plain text", "Plain text"},
    {"empty", "", ""},
  }
  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      got := stripHTML(tt.input)
      if got != tt.expected {
        t.Errorf("stripHTML(%q) = %q, want %q", tt.input, got, tt.expected)
      }
    })
  }
}

func TestBuildMIMEMessage_Simple(t *testing.T) {
  cfg := &Config{Email: "test@example.com"}
  msg := &OutgoingMessage{
    To:      []string{"user@example.com"},
    Subject: "Test Subject with 中文",
    BodyHTML: "<p>Hello World</p>",
  }
  data, err := BuildMIMEMessage(msg, cfg)
  if err != nil {
    t.Fatalf("BuildMIMEMessage 失败: %v", err)
  }
  // 检查关键头
  checks := []string{
    "From: test@example.com",
    "To: user@example.com",
    "Subject:",
  }
  for _, check := range checks {
    if !bytes.Contains(data, []byte(check)) {
      t.Errorf("生成的邮件缺少: %s", check)
    }
  }
  // 检查 MIME 结构
  if !bytes.Contains(data, []byte("Content-Type: multipart/alternative")) {
    t.Error("缺少 multipart/alternative")
  }
  if !bytes.Contains(data, []byte("text/html")) {
    t.Error("缺少 text/html")
  }
}

func TestBuildMIMEMessage_WithAttachments(t *testing.T) {
  cfg := &Config{Email: "test@example.com"}
  msg := &OutgoingMessage{
    To:      []string{"user@example.com"},
    Subject: "With attachment",
    BodyHTML: "See attached",
    Attachments: []Attachment{
      {Filename: "test.txt", MimeType: "text/plain", Content: []byte("hello")},
    },
  }
  data, err := BuildMIMEMessage(msg, cfg)
  if err != nil {
    t.Fatalf("BuildMIMEMessage 失败: %v", err)
  }
  if !bytes.Contains(data, []byte("Content-Disposition: attachment")) {
    t.Error("缺少 attachment")
  }
  if !bytes.Contains(data, []byte("Content-Type: multipart/mixed")) {
    t.Error("缺少 multipart/mixed")
  }
}
```

- [ ] **Step 3: 运行测试**

```bash
cd /data/picoaide && go test ./internal/email/ -run "TestStripHTML|TestBuildMIMEMessage" -v
```

- [ ] **Step 4: 提交**

```bash
git add internal/email/smtp.go internal/email/smtp_test.go
git commit -m "feat: implement SMTP send with MIME message builder"
```

---

### Task 6: Email 包 — IMAP 读取

**Files:**
- Create: `internal/email/imap.go`
- Test: `internal/email/imap_test.go`（单元测试仅测辅助函数）

- [ ] **Step 1: 编写 IMAP 连接和列表函数

```go
package email

import (
  "context"
  "crypto/tls"
  "fmt"
  "net"
  "time"

  "github.com/emersion/go-imap/client"
  "github.com/emersion/go-imap"
)

// dialIMAP 拨号 IMAP 服务器
func dialIMAP(cfg *Config) (*client.Client, error) {
  addr := fmt.Sprintf("%s:%d", cfg.IMAPHost, cfg.IMAPPort)
  var c *client.Client
  var err error

  if cfg.IMAPTLS {
    c, err = client.DialTLS(addr, &tls.Config{ServerName: cfg.IMAPHost})
  } else {
    c, err = client.Dial(addr)
    if err == nil {
      if err := c.StartTLS(&tls.Config{ServerName: cfg.IMAPHost}); err != nil {
        c.Logout()
        return nil, &ProtocolError{Err: fmt.Errorf("IMAP STARTTLS 失败: %w", err)}
      }
    }
  }
  if err != nil {
    return nil, &NetworkError{Err: fmt.Errorf("连接 IMAP 服务器失败: %w", err)}
  }

  if err := c.Login(cfg.LoginUser, cfg.LoginPass); err != nil {
    c.Logout()
    return nil, &AuthError{Err: fmt.Errorf("IMAP 登录失败: %w", err)}
  }

  return c, nil
}

// ListMessages 列出文件夹中的邮件
func ListMessages(cfg *Config, folder string, limit, offset int) ([]*MessageSummary, uint32, error) {
  c, err := dialIMAP(cfg)
  if err != nil {
    return nil, 0, err
  }
  defer c.Logout()

  mbox, err := c.Select(folder, false)
  if err != nil {
    return nil, 0, &ProtocolError{Err: fmt.Errorf("选择文件夹 %s 失败: %w", folder, err)}
  }

  total := mbox.Messages
  if limit <= 0 {
    limit = 20
  }
  if limit > 100 {
    limit = 100
  }

  // 从最新邮件开始取
  start := uint32(1)
  if total > uint32(offset) {
    start = total - uint32(offset)
  }
  if total > uint32(offset+limit) {
    start = total - uint32(offset) - uint32(limit) + 1
  }
  if start < 1 {
    start = 1
  }

  seqSet := new(imap.SeqSet)
  seqSet.AddRange(start, total-uint32(offset))

  messages := make(chan *imap.Message, limit)
  done := make(chan error, 1)

  go func() {
    done <- c.Fetch(seqSet, []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags}, messages)
  }()

  var result []*MessageSummary
  for msg := range messages {
    summary := &MessageSummary{
      UID:   msg.SeqNum,
      Flags: msg.Flags,
    }
    if msg.Envelope != nil {
      summary.Subject = msg.Envelope.Subject
      summary.Date = msg.Envelope.Date
      if len(msg.Envelope.From) > 0 {
        summary.From = msg.Envelope.From[0].Address()
      }
    }
    result = append([]*MessageSummary{summary}, result...) // 保持顺序：最新的在前
  }

  if err := <-done; err != nil {
    return nil, 0, &ProtocolError{Err: fmt.Errorf("获取邮件列表失败: %w", err)}
  }

  return result, total, nil
}
```

- [ ] **Step 2: 添加函数签名声明**（完整实现在 Task 10 中补充）

先在文件中声明这些函数的签名（返回 stub 错误），确保编译通过：

```go
var errNotImpl = fmt.Errorf("IMAP 操作将在 Task 10 中完整实现")

func FetchMessage(cfg *Config, uid uint32, markSeen bool) (*Message, error) {
  return nil, errNotImpl
}
func SearchMessages(cfg *Config, folder, query string, limit int) ([]*MessageSummary, error) {
  return nil, errNotImpl
}
func DeleteMessage(cfg *Config, uid uint32, hard bool) error { return errNotImpl }
func MoveMessage(cfg *Config, uid uint32, targetFolder string) error { return errNotImpl }
func ListFolders(cfg *Config) ([]*Folder, error) { return nil, errNotImpl }
func Reply(cfg *Config, uid uint32, body string, replyAll bool) (string, error) { return "", errNotImpl }
func Forward(cfg *Config, uid uint32, to []string, body string) (string, error) { return "", errNotImpl }
func TestConnection(cfg *Config) (smtpOK bool, imapOK bool, err error) { return false, false, errNotImpl }
```

- [ ] **Step 3: 编译验证**

```bash
go build ./internal/email/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/email/imap.go
git commit -m "feat: add IMAP operations (list, fetch, search, delete, move, folders)"
```

---

### Task 7: MCP 工具定义和 Handlers

**Files:**
- Create: `internal/web/email_tools.go`
- Modify: `internal/web/mcp_service.go`

- [ ] **Step 1: 编写 email_tools.go（工具定义）**

```go
package web

var emailToolDefs = []ToolDef{
  {
    Name:        "email_send",
    Description: "发送邮件。需要先在用户面板配置邮箱账号。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "to":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "收件人地址列表"},
        "cc":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "抄送地址列表（可选）"},
        "bcc":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "密送地址列表（可选）"},
        "subject": map[string]interface{}{"type": "string", "description": "邮件主题"},
        "body":    map[string]interface{}{"type": "string", "description": "邮件正文（HTML 格式，支持富文本）"},
      },
      "required": []string{"to", "subject", "body"},
    },
  },
  {
    Name:        "email_list",
    Description: "列出邮件文件夹中的邮件。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "folder": map[string]interface{}{"type": "string", "description": "文件夹名，默认 INBOX", "enum": []string{"INBOX", "SENT", "DRAFTS", "TRASH", "SPAM"}},
        "limit":  map[string]interface{}{"type": "integer", "description": "返回条数，默认 20，最大 100"},
        "offset": map[string]interface{}{"type": "integer", "description": "偏移量，默认 0"},
      },
    },
  },
  {
    Name:        "email_read",
    Description: "读取一封邮件的完整内容。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "uid":      map[string]interface{}{"type": "integer", "description": "邮件 UID"},
        "markSeen": map[string]interface{}{"type": "boolean", "description": "是否标记为已读，默认 true"},
      },
      "required": []string{"uid"},
    },
  },
  {
    Name:        "email_reply",
    Description: "回复一封邮件。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "uid":      map[string]interface{}{"type": "integer", "description": "原邮件 UID"},
        "body":     map[string]interface{}{"type": "string", "description": "回复正文"},
        "replyAll": map[string]interface{}{"type": "boolean", "description": "是否回复全部，默认 false"},
      },
      "required": []string{"uid", "body"},
    },
  },
  {
    Name:        "email_forward",
    Description: "转发一封邮件。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "uid":  map[string]interface{}{"type": "integer", "description": "原邮件 UID"},
        "to":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "转发收件人"},
        "body": map[string]interface{}{"type": "string", "description": "附加正文（可选）"},
      },
      "required": []string{"uid", "to"},
    },
  },
  {
    Name:        "email_search",
    Description: "在邮件文件夹中搜索邮件。支持标准 IMAP 搜索语法：FROM/SUBJECT/BODY/TEXT/TO/CC/SINCE/BEFORE/UNSEEN 等关键词。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "query":  map[string]interface{}{"type": "string", "description": "搜索关键词，如 'FROM user@example.com SUBJECT meeting'"},
        "folder": map[string]interface{}{"type": "string", "description": "搜索范围文件夹，默认 INBOX"},
        "limit":  map[string]interface{}{"type": "integer", "description": "最大结果数，默认 50"},
      },
      "required": []string{"query"},
    },
  },
  {
    Name:        "email_delete",
    Description: "删除一封邮件。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "uid":  map[string]interface{}{"type": "integer", "description": "邮件 UID"},
        "hard": map[string]interface{}{"type": "boolean", "description": "true=永久删除，false=移到垃圾箱（默认）"},
      },
      "required": []string{"uid"},
    },
  },
  {
    Name:        "email_move",
    Description: "将邮件移动到其他文件夹。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "uid":    map[string]interface{}{"type": "integer", "description": "邮件 UID"},
        "folder": map[string]interface{}{"type": "string", "description": "目标文件夹名，如 'Archive'/'Work'/'INBOX'"},
      },
      "required": []string{"uid", "folder"},
    },
  },
  {
    Name:        "email_folders",
    Description: "列出邮箱中的所有文件夹及未读数。",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
}
```

- [ ] **Step 2: 编写 Handler 函数**

```go
// emailHandleMCPToolCall 分派 email MCP 工具调用
func emailHandleMCPToolCall(s *Server, c *gin.Context, id json.Number, name string, args map[string]interface{}, username string) {
  handler, ok := emailHandlers[name]
  if !ok {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{{"type": "text", "text": "未知 email 工具: " + name}},
      "isError": true,
    })
    return
  }
  handler(s, c, id, args, username)
}

var emailHandlers = map[string]func(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string){
  "email_send":    handleEmailSend,
  "email_list":    handleEmailList,
  "email_read":    handleEmailRead,
  "email_reply":   handleEmailReply,
  "email_forward": handleEmailForward,
  "email_search":  handleEmailSearch,
  "email_delete":  handleEmailDelete,
  "email_move":    handleEmailMove,
  "email_folders": handleEmailFolders,
}

// getEmailConfig 从 store 读取并解密邮箱配置
func getEmailConfig(username string) (*email.Config, error) {
  ue, err := store.GetUserEmailWithDecryptedPassword(username)
  if err != nil {
    return nil, fmt.Errorf("读取邮箱配置失败: %w", err)
  }
  if ue == nil {
    return nil, fmt.Errorf("未配置邮箱，请先在用户面板中配置")
  }
  if !ue.Enabled {
    return nil, fmt.Errorf("邮箱未启用，请先在用户面板中启用")
  }
  return &email.Config{
    Email:     ue.Email,
    SMTPHost:  ue.SMTPHost,
    SMTPPort:  ue.SMTPPort,
    SMTPTLS:   ue.SMTPTLS,
    IMAPHost:  ue.IMAPHost,
    IMAPPort:  ue.IMAPPort,
    IMAPTLS:   ue.IMAPTLS,
    LoginUser: ue.LoginUser,
    LoginPass: ue.LoginPassword, // 已解密
  }, nil
}

func handleEmailSend(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{{"type": "text", "text": err.Error()}},
      "isError": true,
    })
    return
  }

  to := toStringSlice(args["to"])
  if len(to) == 0 {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{{"type": "text", "text": "收件人列表不能为空"}},
      "isError": true,
    })
    return
  }

  subject, _ := args["subject"].(string)
  body, _ := args["body"].(string)

  msg := &email.OutgoingMessage{
    To:      to,
    Cc:     toStringSlice(args["cc"]),
    Bcc:    toStringSlice(args["bcc"]),
    Subject: subject,
    BodyHTML: body,
  }

  messageID, err := email.SendMail(cfg, msg)
  if err != nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("发送失败: %s", err.Error())}},
      "isError": true,
    })
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("邮件发送成功，Message-ID: %s", messageID)}},
  })
}

// toStringSlice 将 interface{} 转为 []string
func toStringSlice(v interface{}) []string {
  if v == nil {
    return nil
  }
  switch val := v.(type) {
  case []interface{}:
    result := make([]string, len(val))
    for i, item := range val {
      result[i], _ = item.(string)
    }
    return result
  case []string:
    return val
  default:
    return nil
  }
}
```

- [ ] **Step 3: 在 mcp_service.go 中注册 email 服务 + 路由**

在 `init()` 中添加：
```go
RegisterPicoaideService("email", emailToolDefs, "picoaide-email")
```

在 `handleMCPToolCall` 函数中，在 `picoaideHandlers` 检查之后、`browser_` 前缀检查之前，添加：
```go
// Email 工具（服务端处理）
if strings.HasPrefix(p.Name, "email_") {
  emailHandleMCPToolCall(s, c, id, p.Name, p.Arguments, username)
  return
}
```

- [ ] **Step 4: 验证编译**

```bash
go build ./...
```

- [ ] **Step 5: 提交**

```bash
git add internal/web/email_tools.go internal/web/mcp_service.go
git commit -m "feat: add email MCP tools and register email service"
```

---

### Task 8: REST API（邮箱配置 CRUD）

**Files:**
- Create: `internal/web/email_api.go`
- Modify: `internal/web/server.go`

- [ ] **Step 1: 编写 email_api.go**

```go
package web

import (
  "fmt"
  "net/http"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/store"
  "github.com/picoaide/picoaide/internal/email"
)

// handleEmailGet 获取当前用户邮箱配置（不含密码）
func (s *Server) handleEmailGet(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }
  ue, err := store.GetUserEmail(username)
  if err != nil {
    writeError(c, http.StatusInternalServerError, fmt.Sprintf("读取邮箱配置失败: %s", err.Error()))
    return
  }
  if ue == nil {
    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success": true,
      "configured": false,
      "email": nil,
    })
    return
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":     true,
    "configured":  true,
    "email": map[string]interface{}{
      "email":      ue.Email,
      "smtpHost":  ue.SMTPHost,
      "smtpPort":  ue.SMTPPort,
      "smtpTls":   ue.SMTPTLS,
      "imapHost":  ue.IMAPHost,
      "imapPort":  ue.IMAPPort,
      "imapTls":   ue.IMAPTLS,
      "loginUser": ue.LoginUser,
      "enabled":   ue.Enabled,
      "testResult": ue.TestResult,
      "updatedAt": ue.UpdatedAt,
    },
  })
}

// handleEmailSave 保存邮箱配置
func (s *Server) handleEmailSave(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }
  // 解析表单
  emailAddr := c.PostForm("email")
  smtpHost := c.PostForm("smtpHost")
  smtpPort := parseIntPostForm(c, "smtpPort", 587)
  smtpTls := c.PostForm("smtpTls") == "true"
  imapHost := c.PostForm("imapHost")
  imapPort := parseIntPostForm(c, "imapPort", 993)
  imapTls := c.PostForm("imapTls") == "true"
  loginUser := c.PostForm("loginUser")
  loginPass := c.PostForm("loginPassword")

  // 验证必填字段
  if emailAddr == "" || smtpHost == "" || imapHost == "" || loginUser == "" || loginPass == "" {
    writeError(c, http.StatusBadRequest, "所有字段均为必填")
    return
  }

  ue := &store.UserEmail{
    Username:  username,
    Email:     emailAddr,
    SMTPHost:  smtpHost,
    SMTPPort:  smtpPort,
    SMTPTLS:   smtpTls,
    IMAPHost:  imapHost,
    IMAPPort:  imapPort,
    IMAPTLS:   imapTls,
    LoginUser: loginUser,
    LoginPassword: loginPass,
    Enabled:   true,
  }

  // 测试连接
  cfg := &email.Config{
    Email:     emailAddr,
    SMTPHost:  smtpHost,
    SMTPPort:  smtpPort,
    SMTPTLS:   smtpTls,
    IMAPHost:  imapHost,
    IMAPPort:  imapPort,
    IMAPTLS:   imapTls,
    LoginUser: loginUser,
    LoginPass: loginPass,
  }
  smtpOK, imapOK, testErr := email.TestConnection(cfg)
  _ = imapOK // 暂不强制要求 IMAP 可用
  testResult := ""
  if testErr != nil {
    testResult = testErr.Error()
  }

  if !smtpOK {
    writeError(c, http.StatusBadRequest, fmt.Sprintf("SMTP 连接测试失败: %s", testResult))
    return
  }

  ue.TestResult = testResult
  if err := store.UpsertUserEmail(ue); err != nil {
    writeError(c, http.StatusInternalServerError, fmt.Sprintf("保存邮箱配置失败: %s", err.Error()))
    return
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "邮箱配置已保存",
  })
}

// handleEmailTest 测试邮箱连接
func (s *Server) handleEmailTest(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }
  ue, err := store.GetUserEmailWithDecryptedPassword(username)
  if err != nil {
    writeError(c, http.StatusInternalServerError, fmt.Sprintf("读取邮箱配置失败: %s", err.Error()))
    return
  }
  if ue == nil {
    writeError(c, http.StatusBadRequest, "未配置邮箱")
    return
  }
  cfg := &email.Config{
    Email:     ue.Email,
    SMTPHost:  ue.SMTPHost,
    SMTPPort:  ue.SMTPPort,
    SMTPTLS:   ue.SMTPTLS,
    IMAPHost:  ue.IMAPHost,
    IMAPPort:  ue.IMAPPort,
    IMAPTLS:   ue.IMAPTLS,
    LoginUser: ue.LoginUser,
    LoginPass: ue.LoginPassword,
  }
  smtpOK, imapOK, testErr := email.TestConnection(cfg)
  result := ""
  if testErr != nil {
    result = testErr.Error()
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "smtp":    smtpOK,
    "imap":    imapOK,
    "error":   result,
  })
}

// handleEmailDelete 删除邮箱配置
func (s *Server) handleEmailDelete(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }
  if err := store.DeleteUserEmail(username); err != nil {
    writeError(c, http.StatusInternalServerError, fmt.Sprintf("删除邮箱配置失败: %s", err.Error()))
    return
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "邮箱配置已删除",
  })
}

// parseIntPostForm 解析整数表单字段
func parseIntPostForm(c *gin.Context, key string, defaultVal int) int {
  val := c.PostForm(key)
  if val == "" {
    return defaultVal
  }
  var result int
  if _, err := fmt.Sscanf(val, "%d", &result); err != nil {
    return defaultVal
  }
  return result
}
```

- [ ] **Step 2: 在 server.go 中注册路由**

在 `registerExternalAPIRoutes` 函数中，在渠道路由之后添加：

```go
// 邮箱配置
g.GET("/user/email", s.handleEmailGet)
g.POST("/user/email", s.handleEmailSave)
g.POST("/user/email/test", s.handleEmailTest)
g.POST("/user/email/delete", s.handleEmailDelete)
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

- [ ] **Step 4: 提交**

```bash
git add internal/web/email_api.go internal/web/server.go
git commit -m "feat: add email config REST API"
```

---

### Task 9: Web UI — 邮箱配置页面

**Files:**
- Create: `internal/web/ui/manage/templates/email.html`
- Create: `internal/web/ui/manage/modules/email.js`

- [ ] **Step 1: 编写 email.html**

```html
<div class="surface-panel">
  <div class="toolbar">
    <div>
      <div class="toolbar-title">邮箱配置</div>
      <div class="toolbar-subtitle">配置后可通过 Agent 发送和读取邮件</div>
    </div>
    <div>
      <button class="btn btn-primary" id="email-save-btn" style="display:none">保存</button>
      <button class="btn btn-danger" id="email-delete-btn" style="display:none">删除配置</button>
    </div>
  </div>
  <div id="email-status" class="msg" style="display:none"></div>
  <div id="email-form">
    <div class="field">
      <label class="field-label">邮箱地址</label>
      <input type="email" id="email-addr" class="input" placeholder="user@example.com">
    </div>
    <div class="field-group">
      <div class="field-group-title">SMTP 发送服务器</div>
      <div class="field-row">
        <div class="field flex-2">
          <label class="field-label">服务器地址</label>
          <input type="text" id="smtp-host" class="input" placeholder="smtp.example.com">
        </div>
        <div class="field flex-1">
          <label class="field-label">端口</label>
          <input type="number" id="smtp-port" class="input" value="587">
        </div>
        <div class="field flex-1">
          <label class="field-label">加密方式</label>
          <select id="smtp-tls" class="input">
            <option value="true">SSL/TLS (465)</option>
            <option value="false" selected>STARTTLS (587)</option>
          </select>
        </div>
      </div>
    </div>
    <div class="field-group">
      <div class="field-group-title">IMAP 接收服务器</div>
      <div class="field-row">
        <div class="field flex-2">
          <label class="field-label">服务器地址</label>
          <input type="text" id="imap-host" class="input" placeholder="imap.example.com">
        </div>
        <div class="field flex-1">
          <label class="field-label">端口</label>
          <input type="number" id="imap-port" class="input" value="993">
        </div>
        <div class="field flex-1">
          <label class="field-label">加密方式</label>
          <select id="imap-tls" class="input">
            <option value="true" selected>SSL/TLS (993)</option>
            <option value="false">STARTTLS (143)</option>
          </select>
        </div>
      </div>
    </div>
    <div class="field">
      <label class="field-label">登录用户名</label>
      <input type="text" id="login-user" class="input" placeholder="通常为邮箱地址">
    </div>
    <div class="field">
      <label class="field-label">登录密码</label>
      <div class="input-group">
        <input type="password" id="login-password" class="input" placeholder="邮箱密码">
        <button class="btn btn-outline" id="email-test-btn">测试连接</button>
      </div>
    </div>
  </div>
  <div id="email-config-card" style="display:none">
    <div class="card">
      <div class="card-body">
        <div class="card-row"><span class="card-label">邮箱：</span><span id="card-email"></span></div>
        <div class="card-row"><span class="card-label">SMTP：</span><span id="card-smtp"></span></div>
        <div class="card-row"><span class="card-label">IMAP：</span><span id="card-imap"></span></div>
        <div class="card-row"><span class="card-label">状态：</span><span id="card-status"></span></div>
        <div class="card-row"><span class="card-label">最后测试：</span><span id="card-test"></span></div>
        <button class="btn btn-outline" id="email-edit-btn" style="margin-top:0.8rem">修改配置</button>
      </div>
    </div>
  </div>
</div>
```

- [ ] **Step 2: 编写 email.js**

```javascript
export async function init(ctx) {
  var $ = ctx.$, esc = ctx.esc, showMsg = ctx.showMsg, Api = ctx.Api;
  var isEditing = false;

  async function loadConfig() {
    var data = await Api.get('/api/user/email');
    if (!data.success) {
      showMsg('#email-status', data.error || '加载失败', false);
      return;
    }
    if (data.configured && data.email) {
      showConfigCard(data.email);
    } else {
      showForm(null);
    }
  }

  function showConfigCard(email) {
    $('#email-form').style.display = 'none';
    $('#email-config-card').style.display = 'block';
    $('#email-save-btn').style.display = 'none';
    $('#email-delete-btn').style.display = 'inline-block';
    $('#card-email').textContent = esc(email.email);
    $('#card-smtp').textContent = email.smtpHost + ':' + email.smtpPort + (email.smtpTls ? ' (SSL)' : ' (STARTTLS)');
    $('#card-imap').textContent = email.imapHost + ':' + email.imapPort + (email.imapTls ? ' (SSL)' : ' (STARTTLS)');
    $('#card-status').textContent = email.enabled ? '已启用' : '未启用';
    $('#card-test').textContent = email.testResult || '—';
  }

  function showForm(email) {
    $('#email-form').style.display = 'block';
    $('#email-config-card').style.display = 'none';
    $('#email-save-btn').style.display = 'inline-block';
    $('#email-delete-btn').style.display = 'none';
    if (email) {
      $('#email-addr').value = email.email || '';
      $('#smtp-host').value = email.smtpHost || '';
      $('#smtp-port').value = email.smtpPort || 587;
      $('#smtp-tls').value = email.smtpTls ? 'true' : 'false';
      $('#imap-host').value = email.imapHost || '';
      $('#imap-port').value = email.imapPort || 993;
      $('#imap-tls').value = email.imapTls ? 'true' : 'false';
      $('#login-user').value = email.loginUser || '';
      $('#login-password').value = '';
      isEditing = true;
    } else {
      $('#email-addr').value = '';
      $('#smtp-host').value = '';
      $('#smtp-port').value = 587;
      $('#smtp-tls').value = 'false';
      $('#imap-host').value = '';
      $('#imap-port').value = 993;
      $('#imap-tls').value = 'true';
      $('#login-user').value = '';
      $('#login-password').value = '';
      isEditing = false;
    }
  }

  function getFormValues() {
    return {
      email: $('#email-addr').value.trim(),
      smtpHost: $('#smtp-host').value.trim(),
      smtpPort: parseInt($('#smtp-port').value) || 587,
      smtpTls: $('#smtp-tls').value === 'true',
      imapHost: $('#imap-host').value.trim(),
      imapPort: parseInt($('#imap-port').value) || 993,
      imapTls: $('#imap-tls').value === 'true',
      loginUser: $('#login-user').value.trim(),
      loginPassword: $('#login-password').value,
    };
  }

  async function testConnection() {
    var vals = getFormValues();
    if (!vals.email || !vals.smtpHost || !vals.imapHost || !vals.loginUser || !vals.loginPassword) {
      showMsg('#email-status', '请先填写所有必填字段', false);
      return;
    }
    $('#email-test-btn').disabled = true;
    $('#email-test-btn').textContent = '测试中...';
    try {
      var res = await Api.post('/api/user/email/test', vals);
      if (res.smtp && res.imap) {
        showMsg('#email-status', 'SMTP 和 IMAP 连接均成功 ✓', true);
      } else if (res.smtp) {
        showMsg('#email-status', 'SMTP 连接成功 ✓，IMAP 连接失败: ' + (res.error || '未知错误'), false);
      } else {
        showMsg('#email-status', 'SMTP 连接失败: ' + (res.error || '未知错误'), false);
      }
    } catch(e) {
      showMsg('#email-status', '测试失败: ' + e.message, false);
    } finally {
      $('#email-test-btn').disabled = false;
      $('#email-test-btn').textContent = '测试连接';
    }
  }

  async function saveConfig() {
    var vals = getFormValues();
    if (!vals.email || !vals.smtpHost || !vals.imapHost || !vals.loginUser || !vals.loginPassword) {
      showMsg('#email-status', '所有字段均为必填', false);
      return;
    }
    $('#email-save-btn').disabled = true;
    try {
      var res = await Api.post('/api/user/email', vals);
      if (res.success) {
        showMsg('#email-status', '配置已保存', true);
        loadConfig();
      } else {
        showMsg('#email-status', res.error || '保存失败', false);
      }
    } catch(e) {
      showMsg('#email-status', '保存失败: ' + e.message, false);
    } finally {
      $('#email-save-btn').disabled = false;
    }
  }

  async function deleteConfig() {
    var confirmed = await confirmModal('确定要删除邮箱配置吗？');
    if (!confirmed) return;
    var res = await Api.post('/api/user/email/delete', {});
    if (res.success) {
      showMsg('#email-status', '配置已删除', true);
      showForm(null);
    } else {
      showMsg('#email-status', res.error || '删除失败', false);
    }
  }

  $('#email-save-btn').addEventListener('click', saveConfig);
  $('#email-delete-btn').addEventListener('click', deleteConfig);
  $('#email-test-btn').addEventListener('click', testConnection);
  $('#email-edit-btn').addEventListener('click', function() {
    var emailData = {
      email: $('#card-email').textContent,
      smtpHost: $('#card-smtp').textContent.split(':')[0],
    };
    loadConfig(); // 重新加载编辑模式
  });

  loadConfig();
}
```

- [ ] **Step 3: 验证 Web UI 文件存在**

```bash
ls -la internal/web/ui/manage/templates/ internal/web/ui/manage/modules/
```

- [ ] **Step 4: 提交**

```bash
git add internal/web/ui/manage/templates/email.html internal/web/ui/manage/modules/email.js
git commit -m "feat: add email config UI"
```

---

### Task 10: 实现 IMAP 全部操作 + TestConnection

**Files:**
- Modify: `internal/email/imap.go`

补充完整的 IMAP 实现。单次实现，不需 TDD 拆子任务。

- [ ] **Step 1: 实现 FetchMessage**

```go
// FetchMessage 获取完整邮件内容
func FetchMessage(cfg *Config, uid uint32, markSeen bool) (*Message, error) {
  c, err := dialIMAP(cfg)
  if err != nil {
    return nil, err
  }
  defer c.Logout()

  _, err = c.Select("INBOX", false)
  if err != nil {
    return nil, &ProtocolError{Err: fmt.Errorf("选择 INBOX 失败: %w", err)}
  }

  seqSet := new(imap.SeqSet)
  seqSet.AddNum(uid)

  section := &imap.BodySectionName{}
  items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags, imap.FetchBodyStructure, section.FetchItem()}

  messages := make(chan *imap.Message, 1)
  done := make(chan error, 1)
  go func() {
    done <- c.Fetch(seqSet, items, messages)
  }()

  if err := <-done; err != nil {
    return nil, &ProtocolError{Err: fmt.Errorf("获取邮件失败: %w", err)}
  }

  msg := <-messages
  if msg == nil {
    return nil, fmt.Errorf("未找到 UID %d 的邮件", uid)
  }

  result := &Message{
    UID:   uid,
    Flags: msg.Flags,
    Subject: msg.Envelope.Subject,
    Date:    msg.Envelope.Date,
  }
  if len(msg.Envelope.From) > 0 {
    result.From = msg.Envelope.From[0].Address()
  }
  for _, addr := range msg.Envelope.To {
    result.To = append(result.To, addr.Address())
  }
  for _, addr := range msg.Envelope.Cc {
    result.Cc = append(result.Cc, addr.Address())
  }
  if msg.Envelope.InReplyTo != nil {
    result.InReplyTo = *msg.Envelope.InReplyTo
  }

  // 解析正文
  for _, literal := range msg.Body {
    // 使用 go-message 解析 MIME
    reader, err := imap.NewBodyReader(msg, literal)
    if err == nil {
      mr, err := mail.CreateReader(reader)
      if err == nil {
        for {
          part, err := mr.NextPart()
          if err != nil {
            break
          }
          switch h := part.Header.(type) {
          case mail.TextHeader:
            content, _ := io.ReadAll(part.Body)
            result.BodyText = string(content)
          case mail.HTMLHeader:
            content, _ := io.ReadAll(part.Body)
            result.BodyHTML = string(content)
          }
        }
      }
    }
  }

  // 标记已读
  if markSeen {
    seqSet = new(imap.SeqSet)
    seqSet.AddNum(uid)
    c.Store(seqSet, imap.AddFlags, []interface{}{imap.SeenFlag})
  }

  return result, nil
}
```

- [ ] **Step 2: 实现 SearchMessages**

```go
// SearchMessages 搜索邮件
func SearchMessages(cfg *Config, folder, query string, limit int) ([]*MessageSummary, error) {
  c, err := dialIMAP(cfg)
  if err != nil {
    return nil, err
  }
  defer c.Logout()

  _, err = c.Select(folder, true)
  if err != nil {
    return nil, &ProtocolError{Err: fmt.Errorf("选择文件夹 %s 失败: %w", folder, err)}
  }

  // 解析简单搜索语法，构建 IMAP 搜索条件
  criteria := parseSearchQuery(query)
  seqNums, err := c.Search(criteria)
  if err != nil {
    return nil, &ProtocolError{Err: fmt.Errorf("搜索失败: %w", err)}
  }

  if len(seqNums) == 0 {
    return nil, nil
  }

  if limit <= 0 {
    limit = 50
  }
  if len(seqNums) > limit {
    seqNums = seqNums[len(seqNums)-limit:]
  }

  seqSet := new(imap.SeqSet)
  for _, num := range seqNums {
    seqSet.AddNum(num)
  }

  messages := make(chan *imap.Message, len(seqNums))
  done := make(chan error, 1)
  go func() {
    done <- c.Fetch(seqSet, []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags}, messages)
  }()

  var result []*MessageSummary
  for msg := range messages {
    summary := &MessageSummary{
      UID:   msg.SeqNum,
      Flags: msg.Flags,
    }
    if msg.Envelope != nil {
      summary.Subject = msg.Envelope.Subject
      summary.Date = msg.Envelope.Date
      if len(msg.Envelope.From) > 0 {
        summary.From = msg.Envelope.From[0].Address()
      }
    }
    result = append(result, summary)
  }

  if err := <-done; err != nil {
    return nil, err
  }
  return result, nil
}

// parseSearchQuery 解析用户输入的搜索查询为 IMAP 搜索条件
func parseSearchQuery(query string) *imap.SearchCriteria {
  criteria := imap.NewSearchCriteria()
  parts := strings.Fields(query)
  for i := 0; i < len(parts); i++ {
    upper := strings.ToUpper(parts[i])
    switch upper {
    case "FROM":
      if i+1 < len(parts) {
        criteria.WithFrom = append(criteria.WithFrom, parts[i+1])
        i++
      }
    case "SUBJECT":
      if i+1 < len(parts) {
        criteria.WithSubject = parts[i+1]
        // 处理引号内的多词主题
        if strings.HasPrefix(parts[i+1], "\"") {
          // 简单处理，忽略复杂引号
        }
        i++
      }
    case "BODY":
      if i+1 < len(parts) {
        criteria.WithBody = parts[i+1]
        i++
      }
    case "TO":
      if i+1 < len(parts) {
        criteria.WithTo = append(criteria.WithTo, parts[i+1])
        i++
      }
    case "CC":
      if i+1 < len(parts) {
        criteria.WithCc = append(criteria.WithCc, parts[i+1])
        i++
      }
    case "UNSEEN":
      criteria.WithoutFlags = []string{imap.SeenFlag}
    case "SEEN":
      criteria.WithFlags = []string{imap.SeenFlag}
    case "FLAGGED":
      criteria.WithFlags = []string{imap.FlaggedFlag}
    default:
      criteria.WithBody = parts[i]
    }
  }
  return criteria
}
```

- [ ] **Step 3: 实现 DeleteMessage、MoveMessage、ListFolders**

```go
// DeleteMessage 删除邮件
func DeleteMessage(cfg *Config, uid uint32, hard bool) error {
  c, err := dialIMAP(cfg)
  if err != nil {
    return err
  }
  defer c.Logout()

  _, err = c.Select("INBOX", false)
  if err != nil {
    return &ProtocolError{Err: fmt.Errorf("选择 INBOX 失败: %w", err)}
  }

  seqSet := new(imap.SeqSet)
  seqSet.AddNum(uid)

  if hard {
    // 标记删除并 expunge
    if err := c.Store(seqSet, imap.AddFlags, []interface{}{imap.DeletedFlag}); err != nil {
      return &ProtocolError{Err: fmt.Errorf("标记删除失败: %w", err)}
    }
    if err := c.Expunge(nil); err != nil {
      return &ProtocolError{Err: fmt.Errorf("永久删除失败: %w", err)}
    }
  } else {
    // 移到 Trash 文件夹
    if err := c.Move(seqSet, "Trash"); err != nil {
      if err := c.Move(seqSet, "INBOX.Trash"); err != nil {
        // 如果 Trash 文件夹不存在，则标记删除
        c.Store(seqSet, imap.AddFlags, []interface{}{imap.DeletedFlag})
        c.Expunge(nil)
      }
    }
  }
  return nil
}

// MoveMessage 移动邮件到其他文件夹
func MoveMessage(cfg *Config, uid uint32, targetFolder string) error {
  c, err := dialIMAP(cfg)
  if err != nil {
    return err
  }
  defer c.Logout()

  _, err = c.Select("INBOX", false)
  if err != nil {
    return &ProtocolError{Err: fmt.Errorf("选择 INBOX 失败: %w", err)}
  }

  seqSet := new(imap.SeqSet)
  seqSet.AddNum(uid)

  if err := c.Move(seqSet, targetFolder); err != nil {
    return &ProtocolError{Err: fmt.Errorf("移动邮件到 %s 失败: %w", targetFolder, err)}
  }
  return nil
}

// ListFolders 列出所有文件夹及未读数
func ListFolders(cfg *Config) ([]*Folder, error) {
  c, err := dialIMAP(cfg)
  if err != nil {
    return nil, err
  }
  defer c.Logout()

  mailboxes := make(chan *imap.MailboxInfo, 50)
  done := make(chan error, 1)
  go func() {
    done <- c.List("", "*", mailboxes)
  }()

  var result []*Folder
  for mbox := range mailboxes {
    // 获取未读数
    status, err := c.Status(mbox.Name, []imap.StatusItem{imap.StatusUnseen})
    unread := uint32(0)
    if err == nil && status != nil {
      unread = status.Unseen
    }
    result = append(result, &Folder{
      Name:    mbox.Name,
      Delimiter: mbox.Delimiter,
      Unread:  unread,
    })
  }

  if err := <-done; err != nil {
    return nil, &ProtocolError{Err: fmt.Errorf("列出文件夹失败: %w", err)}
  }
  return result, nil
}
```

- [ ] **Step 4: 实现 TestConnection**

在 `imap.go` 中添加以下函数（也可以放在单独的 `client.go`）：

```go
// TestConnection 测试 SMTP 和 IMAP 连接
func TestConnection(cfg *Config) (smtpOK bool, imapOK bool, err error) {
  // 测试 SMTP
  smtpClient, smtpErr := dialSMTPServer(cfg)
  if smtpErr == nil {
    smtpClient.Close()
    smtpOK = true
  }

  // 测试 IMAP
  imapClient, imapErr := dialIMAP(cfg)
  if imapErr == nil {
    imapClient.Logout()
    imapOK = true
  }

  if smtpOK && imapOK {
    return true, true, nil
  }

  var errMsg string
  if !smtpOK {
    errMsg = "SMTP: " + smtpErr.Error()
  }
  if !imapOK {
    if errMsg != "" {
      errMsg += "; "
    }
    errMsg += "IMAP: " + imapErr.Error()
  }
  return smtpOK, imapOK, fmt.Errorf(errMsg)
}
```

- [ ] **Step 5: 编译验证**

```bash
go build ./...
```

- [ ] **Step 6: 提交**

```bash
git add internal/email/
git commit -m "feat: complete IMAP operations and TestConnection"
```

---

### Task 11: 补充 MCP Handler（回复、转发、列表、读取、搜索、删除、移动、文件夹）

**Files:**
- Modify: `internal/web/email_tools.go`

- [ ] **Step 1: 实现 handleEmailList**

```go
func handleEmailList(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }
  folder, _ := args["folder"].(string)
  if folder == "" {
    folder = "INBOX"
  }
  limit := parseIntArg(args["limit"], 20)
  offset := parseIntArg(args["offset"], 0)

  msgs, total, err := email.ListMessages(cfg, folder, limit, offset)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("获取邮件列表失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "total":    total,
    "messages": msgs,
  })
}
```

需要辅助函数：
```go
func parseIntArg(v interface{}, defaultVal int) int {
  if v == nil {
    return defaultVal
  }
  switch val := v.(type) {
  case float64:
    return int(val)
  case int:
    return val
  case json.Number:
    n, _ := val.Int64()
    return int(n)
  default:
    return defaultVal
  }
}

func writeMCPError(c *gin.Context, id json.Number, text string) {
  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{{"type": "text", "text": text}},
    "isError": true,
  })
}
```

- [ ] **Step 2: 实现 handleEmailRead**

```go
func handleEmailRead(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }
  uid := uint32(parseIntArg(args["uid"], 0))
  if uid == 0 {
    writeMCPError(c, id, "uid 参数无效")
    return
  }
  markSeen := true
  if v, ok := args["markSeen"]; ok {
    markSeen, _ = v.(bool)
  }

  msg, err := email.FetchMessage(cfg, uid, markSeen)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("读取邮件失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "subject":     msg.Subject,
    "from":        msg.From,
    "to":          msg.To,
    "cc":          msg.Cc,
    "date":        msg.Date.Format("2006-01-02 15:04:05"),
    "bodyText":    msg.BodyText,
    "bodyHtml":    msg.BodyHTML,
    "attachments": msg.Attachments,
    "flags":       msg.Flags,
  })
}
```

- [ ] **Step 3: 实现 handleEmailReply**

```go
func handleEmailReply(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }
  uid := uint32(parseIntArg(args["uid"], 0))
  if uid == 0 {
    writeMCPError(c, id, "uid 参数无效")
    return
  }
  body, _ := args["body"].(string)
  if body == "" {
    writeMCPError(c, id, "回复正文不能为空")
    return
  }
  replyAll, _ := args["replyAll"].(bool)

  messageID, err := email.Reply(cfg, uid, body, replyAll)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("回复失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "messageId": messageID,
  })
}
```

- [ ] **Step 4: 实现 handleEmailForward**

```go
func handleEmailForward(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }
  uid := uint32(parseIntArg(args["uid"], 0))
  if uid == 0 {
    writeMCPError(c, id, "uid 参数无效")
    return
  }
  to := toStringSlice(args["to"])
  if len(to) == 0 {
    writeMCPError(c, id, "转发收件人不能为空")
    return
  }
  body, _ := args["body"].(string)

  messageID, err := email.Forward(cfg, uid, to, body)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("转发失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "messageId": messageID,
  })
}
```

- [ ] **Step 5: 实现 handleEmailSearch、handleEmailDelete、handleEmailMove、handleEmailFolders**

```go
func handleEmailSearch(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }
  query, _ := args["query"].(string)
  if query == "" {
    writeMCPError(c, id, "搜索关键词不能为空")
    return
  }
  folder, _ := args["folder"].(string)
  if folder == "" {
    folder = "INBOX"
  }
  limit := parseIntArg(args["limit"], 50)

  msgs, err := email.SearchMessages(cfg, folder, query, limit)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("搜索失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "messages": msgs,
  })
}

func handleEmailDelete(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }
  uid := uint32(parseIntArg(args["uid"], 0))
  if uid == 0 {
    writeMCPError(c, id, "uid 参数无效")
    return
  }
  hard, _ := args["hard"].(bool)

  if err := email.DeleteMessage(cfg, uid, hard); err != nil {
    writeMCPError(c, id, fmt.Sprintf("删除失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{{"type": "text", "text": "删除成功"}},
  })
}

func handleEmailMove(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }
  uid := uint32(parseIntArg(args["uid"], 0))
  if uid == 0 {
    writeMCPError(c, id, "uid 参数无效")
    return
  }
  folder, _ := args["folder"].(string)
  if folder == "" {
    writeMCPError(c, id, "目标文件夹不能为空")
    return
  }

  if err := email.MoveMessage(cfg, uid, folder); err != nil {
    writeMCPError(c, id, fmt.Sprintf("移动失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{{"type": "text", "text": "移动成功"}},
  })
}

func handleEmailFolders(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }

  folders, err := email.ListFolders(cfg)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("获取文件夹列表失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "folders": folders,
  })
}
```

- [ ] **Step 6: 编译验证**

```bash
go build ./...
```

- [ ] **Step 7: 提交**

```bash
git add internal/web/email_tools.go
git commit -m "feat: implement all email MCP tool handlers"
```

---

### Task 12: 验证完整编译

- [ ] **Step 1: 全量编译**

```bash
go build ./...
```

- [ ] **Step 2: 运行所有测试**

```bash
go test ./internal/store/ -v -run "TestEncrypt|TestUserEmail"
go test ./internal/email/ -v -run "TestStrip|TestBuildMI"
```

- [ ] **Step 3: 最终提交**

```bash
git add -A
git commit -m "feat: complete email MCP service implementation"
```
