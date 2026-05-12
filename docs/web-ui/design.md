# 关键设计决策

## 一、认证架构决策

### 三种认证模式（local/ldap/oidc）

PicoAide 支持三种认证模式，覆盖从单机部署到企业集成的全部场景：

- **local（本地模式）**：默认模式，用户账户存储在 SQLite 中，密码使用 argon2id 哈希。适用于小团队或个人部署，无需外部依赖
- **ldap（LDAP 模式）**：通过 LDAP 协议对接企业目录服务（如 OpenLDAP、AD），用户账号由目录源统一管理
- **oidc（OIDC 模式）**：通过 OpenID Connect 协议对接 SSO 平台（如 Keycloak、Azure AD），浏览器跳转登录

设计理由：三种模式涵盖了从小到大、从简单到复杂的所有部署场景。同一种认证源同时具备密码验证和 SSO 能力时（如 Keycloak 同时支持 LDAP 和 OIDC），Web UI 可择一部署。

### 认证源注册表模式

`internal/authsource/provider.go` 定义三个能力接口（`PasswordProvider`、`BrowserProvider`、`DirectoryProvider`），每种认证源通过 `init()` 函数自动注册到全局 `providers` map：

- 新增认证源只需创建新文件实现对应接口，在 `init()` 中调用 `Register()` 即可
- 无需修改任何现有代码或手动导入新包
- 前端通过 `GET /api/admin/auth/providers` 获取所有可用的认证源及其配置字段定义，动态渲染配置表单

### 切换认证模式时清理旧数据的理由

当管理员在 Web UI 中切换认证模式（如从 LDAP 切换到 OIDC），服务端执行 `purgeOrdinaryAuthProviderStateForConfig()`：

- 移除所有 Docker 容器（物理隔离）
- 清空容器记录（`containers` 表）
- 删除所有普通用户（`local_users` 表，超管保留）
- 清空组和组成员关系（`groups`、`user_groups`）
- 归档用户目录到 `archived/`

设计理由：不同认证源的用户集、组结构和权限模型完全不同。保留旧数据会导致用户权限混乱、组来源不一致、容器指向不存在的用户。清理确保切换后系统状态与新的认证源完全对齐，不留遗留数据风险。

### 超管逃生通道

统一认证模式下（LDAP/OIDC），超管账户仍可通过 `/api/login` 使用本地密码登录。设计理由：

- 防止认证源不可用时管理员被锁在系统外
- 超管账户存储在 `local_users` 表中，不依赖外部认证源
- 在 `handleLogin()` 中，超管在统一认证源认证失败后会尝试本地认证作为兜底

### session 使用 HMAC 签名 Cookie 而非 JWT

PicoAide 使用 `用户名:时间戳:HMAC-SHA256签名` 格式的会话 Cookie：

- 无依赖：只需要 `crypto/hmac` 和 `crypto/sha256`，不需要 JWT 库
- 无需刷新：会话有效期内签名不变，不涉及 AJWT 的过期刷新逻辑
- 简单可控：session secret 持久化在 `internal.session_secret` 配置中，重启服务后会话仍然有效

### CSRF 使用按小时滚动的时间窗口令牌

CSRF token 基于 `HMAC(小时窗口 + 用户名)` 生成，取前 32 位 hex：

- 同一小时内同一用户的 CSRF token 不变
- 跨小时自动切换，当前小时和前一小时的 token 都有效（容忍时钟偏差）
- 无需在服务端存储 token 状态，无状态验证

## 二、配置展平存储设计

### SQLite settings 表

全局配置存储在 SQLite 的 `settings` 表中，仅两列：`key`（主键）和 `value`（文本值）：

| key | value |
|-----|-------|
| `web.auth_mode` | `local` |
| `web.listen` | `:80` |
| `image.name` | `ghcr.io/picoaide/picoaide` |
| `image.tag` | `v1.0.0` |
| `picoclaw` | `{"model":"gpt-4","temperature":0.7}` |

### flattenConfig/buildNested 机制

嵌套的配置 map 通过 `flattenConfig()` 展平为点分隔键值对：

- 字符串/数字/布尔值直接存为字符串
- map 递归展平（如 `web.auth_mode`）
- 数组序列化为 JSON 字符串
- `picoclaw`、`security`、`skills` 三个顶层键整体序列化为 JSON blob

`buildNested()` 反向重建嵌套结构。设计理由：键值对模式支持部分更新，无需读写完整的 YAML/JSON 文件，数据库层面保证原子性。

### 安全过滤

`internal.*` 和 `web.password` 等敏感键不在 API 中暴露，也不受客户端修改影响。`config.go` 中的 `filterOutInternalKeys()` 在加载和保存时进行过滤处理。

### 配置实时生效

配置保存后立即调用 `config.LoadFromDB()` 重新加载内存配置，无需重启服务。认证模式切换、定时同步间隔等修改即时生效。

## 三、MCP 代理架构决策

### 三层架构

```
MCP Service 层（JSON-RPC 2.0 处理）
    ↓
ServiceHub 层（WebSocket 连接管理）
    ↓
代理层（浏览器扩展 / 桌面客户端）
```

- **MCP Service 层**：注册 MCP 服务（`browser`、`computer`），处理 `initialize`、`tools/list`、`tools/call` 等 JSON-RPC 2.0 消息，支持 Streamable HTTP 协议
- **ServiceHub 层**：管理 WebSocket 代理连接，支持连接注册/注销、命令发送、超时处理、keep-alive
- **代理层**：浏览器扩展或桌面代理通过 WebSocket 连接，接收命令并在用户设备上执行

### 使用 WebSocket 而非轮询

设计理由：

- 实时性：代理需要立即响应 MCP 工具调用（如浏览器截图），轮询引入不必要的延迟
- 双向通信：服务端需要主动推送命令到代理，WebSocket 天然支持
- 资源效率：无轮询开销，长连接按需推送

### MCP token 认证机制

MCP token 格式为 `用户名:随机hex`，存储在 `containers` 表的 `mcp_token` 字段：

- 从 WebSocket 连接的 query string 或 `Authorization: Bearer` header 提取
- 支持三种认证方式：query token、Bearer token、Cookie session（SSE 端点）
- 触发后无需额外登录即可使用 MCP 服务

### 同时支持 Legacy SSE 和 Streamable HTTP 协议

- `GET /api/mcp/sse/:service` 提供 SSE 事件流，兼容 Anthropic MCP 的 Legacy SSE 协议
- `POST /api/mcp/sse/:service` 接收 JSON-RPC 2.0 请求，返回 JSON 响应，支持 Streamable HTTP 传输层
- 根据 `Mcp-Protocol-Version` header 自动切换协议版本

## 四、用户配置下发设计

### 配置版本管理：Adapter 包机制

Picoclaw 配置（`config.json` 和 `.security.yml`）通过 Adapter 包实现版本适配：

```
rules/picoclaw/
  index.json         # 包元信息、支持的版本范围
  hash               # SHA256 校验和
  v1/                # 版本 1 的 schema
    config.json        # config 字段定义
    ui.json            # 前端 UI 表单定义
  v2/                # 版本 2
    ...
  v3/
    ...
  migration-rules.json # 迁移规则链
```

- `PicoAideSupportedPicoClawConfigVersion = 3`：指定当前支持的 Picoclaw 版本
- 迁移引擎基于操作规则（`move`、`rename`、`delete`、`set`），自动在版本间迁移配置
- 支持远程 URL 拉取（SHA256 校验）和 ZIP 上传两种刷新方式

### 渠道策略：全局控制 + 用户级覆盖

- 超管在管理端配置全局渠道可用列表和允许/禁止状态
- 用户在个人面板中启用/禁用自己的渠道
- 下发配置时合并全局设置和用户设置，生成最终的 `config.json`

### 配置下发后自动重启容器

`POST /api/admin/config/apply` 和用户级配置修改（钉钉、渠道字段）都会在保存配置后调用 `dockerpkg.Restart()` 或重建容器，确保 Picoclaw 下次启动时使用最新配置。

## 五、安全设计决策

### 路径安全

- `util.SafePathSegment()`：拒绝包含 `/`、`\`、`..` 的路径段，防止目录遍历
- `util.SafeRelPath()`：基于 `os.Root` 的沙盒文件访问，文件管理所有操作都限定在用户的 `.picoclaw/workspace/` 目录内
- 用户名验证：`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`，最长 64 字符

### 请求体大小限制

- 非上传端点：`maxBodyBytes = 1 MB`，通过 `http.MaxBytesReader` 限制
- 上传端点（`/api/files/upload`、`/api/admin/migration-rules/upload`、`/api/admin/skills/upload`）：绕过此限制
- 文件上传端点：`maxUploadSize = 32 MB`

### 速率限制

- 登录接口：10 次/5 分钟，基于内存计数器 + 后台清理 goroutine
- 仅在 `/api/login` 相关路由上生效（通过 `rateLimitLogin()` 中间件）
- 超管登录同样受限

### 密码哈希

- 新用户：argon2id（memory=4KB, time=1, threads=1, keyLen=32, saltLen=16）
  - 低参数适配嵌入式/Pi 类设备，同时保证密码安全
  - 前缀 `$argon2id$` 标识
- 兼容旧 bcrypt 哈希（前缀 `$2a$`、`$2b$`、`$2y$`）
- `auth.ChangePassword()` 自动使用 argon2id 重新哈希

### 其他安全措施

- `X-Frame-Options: DENY`：防止点击劫持
- `X-Content-Type-Options: nosniff`：禁止 MIME 嗅探
- `Referrer-Policy: strict-origin-when-cross-origin`：控制 referrer 传播
- 扩展源 CORS：仅允许 `chrome-extension://` 和 `moz-extension://` 来源，通过 `PICOAIDE_ALLOWED_EXTENSION_ORIGINS` 环境变量进一步限制
- CORS preflight：自动响应 `OPTIONS` 请求
- 会话 Cookie 设置为 `HttpOnly`、`SameSite=LaxMode`，TLS 启用时设置 `Secure`
