# 构建与部署

## 构建命令

### 完整构建

```bash
make build
```

包含 4 步：
1. 构建前端（`web/ui/` → `internal/web/dist/`）
2. 编译 picoagent（`cmd/picoagent/main.go`）
3. 准备 Alpine rootfs（`bundle/alpine-rootfs.tar.gz`）
4. 构建 picoaide（`cmd/picoaide/main.go`，嵌入前端产物）

### 部分构建

```bash
make build-ui          # 仅构建前端
make clean             # 清理二进制和前端产物
go build ./cmd/picoaide/  # 仅编译 Go（需 dist/ 已存在）
```

### 发布构建

```bash
make release PICOAIDE_VERSION=v1.0.0
```

交叉编译 `linux/amd64` 和 `linux/arm64` 两个架构，注入版本号到 `internal/config.Version`。

## Makefile 目标

| 目标 | 说明 |
|------|------|
| `test` | `go test ./internal/... -v -count=1` |
| `lint` | golangci-lint，回退到 `go vet` |
| `format` | 2 空格替换制表符 |
| `check` | format + lint + test |
| `clean` | 删除二进制 + dist |
| `build-ui` | 编译前端 |
| `build` | 前端 + picoagent + rootfs + picoaide |
| `release` | 交叉编译发布版 |

## 前端开发

```bash
cd web/ui
npm install
npm run dev      # localhost:5173，/api 代理到 :8080
npm run build    # 输出到 internal/web/dist/
```

配置文件：`web/ui/vite.config.ts`。`vue-tsc -b && vite build` 双重检查（类型 + 构建）。

## 部署

```bash
# 1. 完整构建
make build

# 2. 停止服务，拷贝二进制，重启
ssh root@<host> "systemctl stop picoaide"
scp ./picoaide root@<host>:/usr/sbin/picoaide
ssh root@<host> "systemctl start picoaide"

# 3. 验证
curl http://<host>:<port>/api/health
```

服务默认监听 `:80`（可通过 `web.listen` 配置修改），自动启用 TLS 并 301 跳转 HTTPS（如已配置证书）。

## 配置

配置文件存储在 SQLite `settings` 表中。管理后台 "系统配置" 页面提供 JSON 编辑器。关键配置项：

| 键 | 默认值 | 说明 |
|-----|---------|------|
| `web.listen` | `:80` | HTTP 监听地址 |
| `web.auth_mode` | `local` | 认证模式：local/ldap/oidc |
| `web.tls.enabled` | `false` | 启用 HTTPS |
| `web.debug_mode` | `false` | 调试日志 |

## 依赖

| 依赖 | 用途 | 版本 |
|----------|------|---------|
| Gin | HTTP 框架 | v1.12 |
| xorm + modernc/sqlite | ORM + 纯 Go SQLite | - |
| go-ldap/v3 | LDAP 客户端 | v3 |
| go-oidc/v3 | OIDC 认证 | v3 |
| gorilla/websocket | WebSocket | v1.5 |
| ADK v2 | AI Agent 框架 | v2.0.0 |
| MCP SDK | MCP 协议 | v1.5 |
| Vue 3 + Ant Design Vue | 前端框架 | v3.5 + v4.2 |
