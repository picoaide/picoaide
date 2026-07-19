# 架构

## 入口点

| 入口 | 路径 | 职责 |
|------|------|------|
| picoaide CLI | `cmd/picoaide/main.go` | CLI 命令路由（init/serve/reset-password），初始化 DB、配置、启动 Web 服务 |
| picoagent | `cmd/picoagent/main.go` | 沙箱内 AI Agent 二进制，使用 ADK 运行 LLM 会话 |

## 包结构

```
cmd/picoaide/              CLI 入口：命令路由、初始化引导、超管设置、reset-password
cmd/picoagent/             沙箱 Agent 入口：配置获取、ADK 运行循环

internal/
├── agent/                 AI Agent：ADK 运行、工具注册表、MCP 工具、会话存储、LLM 提供者、记忆演化
├── authsource/            认证源注册表：LDAP、OIDC、Local 三种 Provider
├── config/                全局配置：GlobalConfig 结构体、YAML/JSON 加载保存、展平 KV 存储
├── daemon/                Daemon 管理器：任务队列、RPC、事件流
├── email/                 Email MCP：IMAP/SMTP 客户端
├── im/                    IM 网关：钉钉、飞书、企业微信
├── ldap/                  LDAP 底层客户端：用户查询、认证、组查询
├── logger/                结构化日志：slog + JSON + lumberjack 轮转
├── sandbox/               overlayfs + netns 沙箱管理
├── scheduler/             Cron 调度器
├── skill/                 技能管理：YAML 解析、Git 源、注册中心
├── store/                 数据访问层：xorm ORM、表模型、迁移
├── user/                  用户生命周期：目录创建、IP 分配、配置合并
├── util/                  通用工具函数
└── web/                   Gin HTTP 服务器：57 个文件，含路由、handler、MCP SSE、WebSocket 代理

web/ui/                    Vue 3 TypeScript 前端 SPA
```

## 核心设计模式

### 1. 配置展平存储

全局配置在 SQLite `settings` 表中以点分隔的键值对存储。`flattenConfig()` 将嵌套 map 展平，`buildNested()` 反向重建嵌套结构。

文件：`internal/config/dbconfig.go`

### 2. 认证源注册表模式

三种能力接口：
- `PasswordProvider`：用户名密码认证（LDAP/Local）
- `BrowserProvider`：浏览器跳转/回调认证（OIDC）
- `DirectoryProvider`：可枚举用户/组（LDAP）

通过 `init()` 自动注册到全局 `providers` map：

```go
// internal/authsource/provider.go
func init() {
  Register("ldap", LDAPProvider{})
  Register("oidc", OIDCProvider{})
  Register("local", LocalProvider{})
}
```

### 3. 双层 HTTP 路由

服务端根据请求来源分发到不同的 Gin engine：
- **内部 handler**：仅沙箱内 API（health、picoagent 配置、文件），通过 Unix socket 访问
- **外部 handler**：全部路由（UI + 全部 API），通过 TCP 端口访问

文件：`internal/web/server.go` 的 `sandboxAwareHandler()`

### 4. SPA 架构

前端所有路由由 Vue Router 处理。Go 服务端对 `/login`、`/user/*`、`/admin/*` 等路径统一返回 `index.html`，由前端 JS 根据 URL 渲染对应组件。Go 端只做认证守卫（session cookie 检查 + 角色重定向）。

文件：`internal/web/ui.go`、`web/ui/src/router/index.ts`

### 5. 静态文件嵌入

前端构建产物输出到 `internal/web/dist/`，通过 `go:embed all:dist` 嵌入 Go 二进制。构建时必须先执行 `make build-ui`。

文件：`internal/web/ui.go`
