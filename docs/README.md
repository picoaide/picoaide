# PicoAide LLM Wiki

PicoAide（`picoaide`）是一个 Go 语言编写的 CLI 工具，用于管理多个用户的 PicoClaw AI 代理沙箱。它使用 overlayfs + network namespace 实现沙箱隔离，SQLite（xorm + modernc）记录状态，通过 Gin 框架提供 JSON API 和 Vue 3 Web 管理面板。

## 文档索引

| 文档 | 内容 |
|------|------|
| [01-architecture.md](01-architecture.md) | 项目架构、包结构、核心设计模式 |
| [02-build-deploy.md](02-build-deploy.md) | 构建系统、部署流程 |
| [03-frontend.md](03-frontend.md) | Vue 3 前端架构 |
| [04-api-reference.md](04-api-reference.md) | API 端点参考 |
| [05-auth-system.md](05-auth-system.md) | 认证系统（LDAP/OIDC/Local） |
| [06-agent-system.md](06-agent-system.md) | AI Agent（ADK） |
| [07-database.md](07-database.md) | 数据库表结构、迁移 |
| [08-development.md](08-development.md) | 开发规范、工作流 |

## 快速链接

- 项目入口: `cmd/picoaide/main.go`
- AI Agent 入口: `cmd/picoagent/main.go`
- 前端源码: `web/ui/`
- API 路由注册: `internal/web/server.go`
- 全局配置: `internal/config/types.go`
- 数据库操作: `internal/store/`
