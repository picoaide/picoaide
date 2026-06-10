# Email MCP 服务设计文档

## 概述

为 PicoAide 增加 Email 能力，使 AI Agent（PicoClaw）能通过 MCP 工具代表用户收发邮件。用户通过 Web UI 自行配置邮箱账号（SMTP + IMAP），Agent 调用服务端 MCP 工具完成邮件操作。

## 设计原则

1. **独立于现有渠道系统**：邮件是双向通信，有别于钉钉/飞书/企微等单向通知渠道，使用独立 `user_emails` 表
2. **密码加密存储**：基于 `internal.session_secret` 派生 AES-256-GCM 密钥加密邮箱密码
3. **即用即连**：SMTP/IMAP 不维护长连接池，每次 MCP 调用新建连接，操作超时 30s
4. **服务端处理**：所有邮件操作在 Go 服务端执行（`RegisterPicoaideService`），不依赖 WebSocket 代理
5. **每个用户一个邮箱账号**：简化设计，一个用户绑定一个邮箱配置

---

## 1. 数据库设计

### 1.1 表结构

```go
// internal/store/models.go
type UserEmail struct {
  ID            int64  `xorm:"pk autoincr 'id'"`
  Username      string `xorm:"unique notnull 'username'"`    // 与 local_users 关联
  Email         string `xorm:"notnull 'email'"`              // 发件地址 (From header)
  SMTPHost      string `xorm:"notnull 'smtp_host'"`
  SMTPPort      int    `xorm:"notnull default 587 'smtp_port'"`
  SMTPTLS       bool   `xorm:"default 1 'smtp_tls'"`        // true=SSL/TLS, false=STARTTLS
  IMAPHost      string `xorm:"notnull 'imap_host'"`
  IMAPPort      int    `xorm:"notnull default 993 'imap_port'"`
  IMAPTLS       bool   `xorm:"default 1 'imap_tls'"`        // true=SSL/TLS, false=STARTTLS
  LoginUser     string `xorm:"notnull 'login_user'"`         // 登录用户名
  LoginPassword string `xorm:"notnull 'login_password'"`     // AES-GCM 加密存储
  Enabled       bool   `xorm:"default 0 'enabled'"`
  TestResult    string `xorm:"'test_result'"`                // 上次测试结果 JSON
  CreatedAt     string `xorm:"notnull 'created_at'"`
  UpdatedAt     string `xorm:"notnull 'updated_at'"`
}
```

### 1.2 迁移文件

```go
// internal/store/migrations/20260610_120000_create_user_emails.go
func init() {
  Register(Migration{
    Timestamp: "20260610120000",
    Desc:      "创建 user_emails 表",
    Up: func(engine *xorm.Engine) error {
      return engine.Sync2(&UserEmail{})
    },
  })
}
```

### 1.3 Store 层

```go
// internal/store/user_emails.go
func GetUserEmail(username string) (*UserEmail, error)  // 查询
func UpsertUserEmail(email *UserEmail) error             // 插入或更新
func DeleteUserEmail(username string) error              // 删除
```

加密/解密在 store 层静默处理：

```go
// 写入时加密
func encryptPassword(sessionSecret, plain string) (string, error)
func decryptPassword(sessionSecret, cipher string) (string, error)
```

---

## 2. 底层实现包 `internal/email/`

### 2.1 目录结构

```
internal/email/
  client.go        # EmailClient 封装 SMTP + IMAP 生命周期
  smtp.go          # SMTP 发送
  imap.go          # IMAP 读取、搜索、管理
  types.go         # 公共类型
  errors.go        # 错误类型
```

### 2.2 核心类型

```go
// types.go
type EmailConfig struct {
  Email     string
  SMTPHost  string
  SMTPPort  int
  SMTPTLS   bool
  IMAPHost  string
  IMAPPort  int
  IMAPTLS   bool
  LoginUser string
  LoginPass string // 已解密
}

type EmailMessage struct {
  UID         uint32
  Subject     string
  From        string
  To          []string
  Cc          []string
  Date        time.Time
  BodyText    string
  BodyHTML    string
  Attachments []Attachment
  Flags       []string // SEEN, FLAGGED, DELETED, etc.
}

type Attachment struct {
  Filename string
  MimeType string
  Size     int64
}

type Folder struct {
  Name    string
  Delimiter string
  Unread  uint32
}
```

### 2.3 SMTP 发送 `smtp.go`

```go
func SendMail(cfg *EmailConfig, msg *OutgoingMessage) (messageID string, err error)
```

- 基于 `net/smtp` + 自定义 SSL/TLS 封装
- SSL/TLS (465) 模式：直连 TLS
- STARTTLS (587) 模式：明文连接后 `STARTTLS` 升级
- 日志中脱敏密码（`***`）

### 2.4 IMAP 操作 `imap.go`

基于 `github.com/emersion/go-imap` 和 `github.com/emersion/go-imap/client`。

```go
func ListMessages(cfg *EmailConfig, folder string, limit, offset int) (msgs []EmailMessage, total uint32, err error)
func FetchMessage(cfg *EmailConfig, uid uint32, markSeen bool) (*EmailMessage, error)
func SearchMessages(cfg *EmailConfig, folder, query string, limit int) (msgs []EmailMessage, err error)
func DeleteMessage(cfg *EmailConfig, uid uint32, hard bool) error
func MoveMessage(cfg *EmailConfig, uid uint32, targetFolder string) error
func ListFolders(cfg *EmailConfig) ([]Folder, error)
func TestConnection(smtpCfg, imapCfg *EmailConfig) (smtpOK bool, imapOK bool, err error)
```

IMAP 搜索语法：支持标准 IMAP SEARCH 关键词 `FROM`/`SUBJECT`/`BODY`/`TEXT`/`TO`/`CC`/`BEFORE`/`SINCE`/`UNSEEN` 等组合。

### 2.5 错误类型 `errors.go`

```go
type AuthError      struct{ Err error }
type NetworkError   struct{ Err error }
type ProtocolError  struct{ Err error }
type TimeoutError   struct{ Err error }
type ConfigError    struct{ Msg string }

func IsAuthError(err error) bool
func IsNetworkError(err error) bool
// ... etc
```

---

## 3. MCP 工具定义

### 3.1 注册

```go
// mcp_service.go init()
RegisterPicoaideService("email", emailToolDefs, "picoaide-email")
```

### 3.2 工具列表（9 个）

| 工具 | 必填参数 | 可选参数 | 返回 |
|------|---------|---------|------|
| `email_send` | to[], subject, body | cc[], bcc[], replyTo | messageId |
| `email_list` | — | folder, limit(20), offset(0) | messages[], total |
| `email_read` | uid | markSeen(true) | EmailMessage |
| `email_reply` | uid, body | replyAll(false) | messageId |
| `email_forward` | uid, to[] | body | messageId |
| `email_search` | query | folder("INBOX"), limit(50) | messages[] |
| `email_delete` | uid | hard(false) | — |
| `email_move` | uid, folder | — | — |
| `email_folders` | — | — | folders[] |

### 3.3 Handler 模式

```go
// email_tools.go
var emailToolDefs = []ToolDef{ /* 9 个工具定义 */ }

var emailHandlers = map[string]func(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string){
  "email_send":    handleEmailSend,
  "email_list":    handleEmailList,
  "email_read":    handleEmailRead,
  // ...
}

// mcp_service.go handleMCPToolCall 中新增路由
if strings.HasPrefix(toolName, "email_") {
  if h, ok := emailHandlers[toolName]; ok {
    h(s, c, id, args, username)
    return
  }
}
```

每个 handler 流程：
1. 从数据库读取用户邮箱配置
2. 解密密码
3. 调用 `internal/email/` 对应函数
4. 构造 MCP 响应

---

## 4. REST API 端点

### 4.1 路由注册

```go
// server.go 普通用户路由组
userGroup.GET("/email", s.handleEmailGet)
userGroup.POST("/email", s.handleEmailSave)
userGroup.POST("/email/test", s.handleEmailTest)
userGroup.DELETE("/email", s.handleEmailDelete)
```

### 4.2 端点详情

| 方法 | 路径 | 请求 | 响应 | 错误 |
|------|------|------|------|------|
| GET | `/api/user/email` | — | `UserEmail` JSON（密码不返回） | 404 未配置 |
| POST | `/api/user/email` | 表单字段 | `{"status": "ok"}` | 400 参数验证失败 |
| POST | `/api/user/email/test` | 表单字段 | `{"smtp": true, "imap": true, "error": ""}` | 连接失败 |
| DELETE | `/api/user/email` | — | `{"status": "ok"}` | 404 未配置 |

POST 保存时自动重新测试连接。

---

## 5. Web UI 设计

### 5.1 新增文件

```
internal/web/ui/manage/modules/email.js
internal/web/ui/manage/templates/email.html
```

### 5.2 UI 交互

**未配置状态：** 表单页面，逐项填写邮箱配置。

**已配置状态：** 状态卡片 + 修改按钮：
```
邮箱：user@example.com
SMTP：smtp.example.com:587 ✓
IMAP：imap.example.com:993 ✓
状态：已启用 ✓   最后测试：2026-06-10 12:00
[修改配置] [删除配置]
```

**测试结果展示：** SMTP ✓ / ✗ 和 IMAP ✓ / ✗，失败时显示具体错误。

**表单校验：**
- 邮箱格式正则验证
- SMTP/IMAP 主机非空
- 端口范围 1-65535，默认值预填
- 密码至少 1 字符

---

## 6. 测试策略

### 6.1 单元测试

| 包 | 测试 | 工具 |
|----|------|------|
| `internal/store` | `TestGetUserEmail`, `TestUpsertUserEmail`, `TestDeleteUserEmail` | `t.TempDir()` + `ResetDB()` |
| `internal/store` | `TestEncryptDecryptPassword` | 密钥固定，验证加解密一致性 |
| `internal/web` | `TestEmailAPI` | 模拟 HTTP 请求测试 CRUD 端点 |
| `internal/web` | `TestEmailMCPTools` | 模拟 MCP JSON-RPC 请求 |

### 6.2 集成测试

- 启动 Docker `reachdigital/mock-smtp` 和 `dovecot/test` 容器
- `TestSendMail` + `TestListMessages` 端到端验证
- 在 GitHub Actions 中通过 `services` 配置启动容器

### 6.3 安全测试

- 密码永不出现于日志（`log.Printf` 中脱敏）
- 密码永不出现于 MCP 响应
- 加密/解密边界测试（空密码、超长密码、无效密钥）
- 删除用户时级联删除邮箱配置

---

## 7. 文件变更清单

| 操作 | 文件 |
|------|------|
| 新增 | `internal/store/user_emails.go` |
| 新增 | `internal/store/migrations/20260610_120000_create_user_emails.go` |
| 修改 | `internal/store/models.go`（加 UserEmail 结构体） |
| 修改 | `internal/store/store.go`（syncSchema + Register 表） |
| 新增 | `internal/email/client.go` |
| 新增 | `internal/email/smtp.go` |
| 新增 | `internal/email/imap.go` |
| 新增 | `internal/email/types.go` |
| 新增 | `internal/email/errors.go` |
| 新增 | `internal/web/email_tools.go`（工具定义 + MCP handlers） |
| 新增 | `internal/web/email_api.go`（REST API handlers） |
| 修改 | `internal/web/server.go`（注册路由） |
| 修改 | `internal/web/mcp_service.go`（注册 email 服务） |
| 新增 | `internal/web/ui/manage/modules/email.js` |
| 新增 | `internal/web/ui/manage/templates/email.html` |
| 修改 | `go.mod`（加 go-imap 依赖） |
| 新增 | `docs/superpowers/specs/2026-06-10-email-mcp-service-design.md` |
