# PicoAide AI 开发指南

本文档是 LLM Wiki 的入口索引。详细文档位于 [`docs/`](docs/) 目录。

## 项目概览

PicoAide（`picoaide`）是一个 Go 语言 CLI 工具，用于管理多个用户的 AI Agent 沙箱。使用 overlayfs + network namespace 沙箱隔离，SQLite 记录状态，Gin 提供 JSON API + Vue 3 Web 面板。

## 快速导航

| 主题 | 文档 |
|------|------|
| 架构设计 | [docs/01-architecture.md](docs/01-architecture.md) |
| 构建与部署 | [docs/02-build-deploy.md](docs/02-build-deploy.md) |
| 前端文档 | [docs/03-frontend.md](docs/03-frontend.md) |
| API 参考 | [docs/04-api-reference.md](docs/04-api-reference.md) |
| 认证系统 | [docs/05-auth-system.md](docs/05-auth-system.md) |
| Agent 系统 | [docs/06-agent-system.md](docs/06-agent-system.md) |
| 数据库 | [docs/07-database.md](docs/07-database.md) |
| 开发规范 | [docs/08-development.md](docs/08-development.md) |

## 构建

```bash
make build    # 前端 + picoagent + rootfs + picoaide（全量编译）
make build-ui # 仅构建前端
make test     # Go 测试
make check    # format + lint + test
```

**重要**：必须使用 `make build` 全量编译，禁止只执行 `go build` 跳过前端和 picoagent。

## 开发

```bash
# 前端开发
cd web/ui && npm install && npm run dev

# 后端开发
go run ./cmd/picoaide/ serve

# 测试
go test ./internal/...
```

## 部署

```bash
make build
scp ./picoaide root@<host>:/usr/sbin/picoaide
ssh root@<host> "systemctl restart picoaide"
```

## 输入验证与安全

- **路径安全**：`util.SafePathSegment()` 拒绝 `/`、`\`、`..`
- **用户名**：`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`，最长 64 字符
- **密码**：argon2id（新用户）或 bcrypt（兼容）
- **CSRF**：HMAC 签名 + 按小时滚动时间窗口
- **速率限制**：登录 10 次/5 分钟
