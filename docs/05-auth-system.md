# 认证系统

## 架构

认证系统采用 Provider 注册表模式，三种认证源可插拔切换。

```
认证请求 → authsource.Authenticate()
           ├── local → auth.AuthenticateLocal()    // 本地用户，argon2id/bcrypt
           ├── ldap  → ldap.Authenticate()          // LDAP 双步认证
           └── oidc  → 浏览器跳转 → 回调登录

用户同步 → authsource.SyncUserDirectory()
           ├── ldap → ldap.FetchUsers() → store.EnsureExternalUser()
           └── oidc → oidc provider → store.EnsureExternalUser()
```

## 认证源接口

`internal/authsource/provider.go` 定义三个能力接口：

```go
type PasswordProvider interface {
  Authenticate(cfg *config.GlobalConfig, username, password string) bool
}

type BrowserProvider interface {
  AuthURL(cfg *config.GlobalConfig) (string, error)
  CompleteLogin(cfg *config.GlobalConfig, code, state string) (*Identity, error)
}

type DirectoryProvider interface {
  FetchUsers(cfg *config.GlobalConfig) ([]string, error)
  FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error)
}
```

## 认证源切换

| 模式 | 认证方式 | Provider | 用户管理 |
|------|---------|----------|---------|
| `local` | 用户名+密码 | LocalProvider | 手动创建 |
| `ldap` | 用户名+密码 | LDAPProvider | LDAP 同步 |
| `oidc` | 浏览器跳转 | OIDCProvider | OIDC 同步 |

切换认证模式时自动清理旧模式的普通用户快照、组成员、容器记录、用户目录和归档目录。

## CSRF 防护

- `GET /api/csrf` 返回 HMAC 签名的 token（基于 session secret + 时间窗口）
- POST/PUT/DELETE 请求需在 `X-CSRF-Token` header 中携带
- token 按小时滚动

## 会话管理

- Session 存储为 `username:timestamp:signature` 格式的 HMAC-SHA256 签名 cookie
- `session` cookie，HttpOnly，SameSite=Lax，24 小时过期
- 服务端无 session 存储，完全基于 cookie 签名验证

## 速率限制

- 登录接口：10 次 / 5 分钟（内存存储 + 后台清理）
- 审计日志 API：独立限速器

## 相关文件

| 文件 | 职责 |
|------|------|
| `internal/authsource/provider.go` | 接口定义、注册表、泛型 dispatch |
| `internal/authsource/ldap.go` | LDAP Provider 实现 |
| `internal/authsource/oidc.go` | OIDC Provider 实现 |
| `internal/authsource/sync.go` | 用户/组同步编排 |
| `internal/authsource/claims.go` | 公共 claim/字段解析 |
| `internal/ldap/ldap.go` | LDAP 底层客户端 |
| `internal/web/handlers.go` | 登录/登出 handler |
| `internal/web/server.go` | CSRF 生成/验证 |
