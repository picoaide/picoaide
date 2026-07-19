# 开发规范

## 语言与惯例

- 界面文字使用中文（API 消息、用户提示、错误提示）
- 代码注释使用中文
- Commit 信息使用中文
- 缩进：**两个空格**，禁止制表符（运行 `./format.sh`）
- 参数解析：手工实现（`util.ParseFlags`），不使用 `flag` 或 `cobra`

## 文件组织

### 大小限制

- 单个 Go 源文件不超过 **1000 行**（含注释和空行）
- 测试文件不受 1000 行限制

### 分离原则

- **工具/纯函数**：无副作用、不依赖外部状态 → `internal/util/` 或包内 `*_util.go`
- **业务逻辑**：依赖数据库、Docker、LDAP → 按资源域组织在各自 handler/service 文件中

### Handler 拆分

Web handler 按功能模块拆分为独立文件：

```
internal/web/
├── server.go           # Server 结构体、路由注册、启动/关闭、会话/CSRF
├── ui.go               # 前端 SPA 路由 + go:embed
├── handlers.go         # 基础 handler（health、login、logout、config）
├── admin_*.go          # 超管 handler（users/groups/skills/auth/config 等）
├── files.go            # 文件管理
├── mcp_*.go            # MCP SSE、WebSocket 代理
├── service_hub.go      # ServiceHub 连接管理
├── taskqueue.go        # 异步任务队列
├── daemon_*.go         # Daemon 任务/事件流
├── chat_stream.go      # 聊天 SSE
├── channels.go         # 通讯渠道
├── email_api.go        # 邮箱配置
├── user_*.go           # 用户端 handler（skills/cron）
├── ratelimit.go        # 速率限制
└── pagination.go       # 分页工具
```

## 开发流程

1. **同步 `main`**：`git fetch origin main && git rebase origin/main`
2. **在 `dev` 分支开发**
3. **测试**：`go test ./internal/... -v -count=1`
4. **构建**：`make build`
5. **推送**：`git push origin dev`
6. **PR**：从 `dev` 创建 PR 到 `main`（需 1 人审批 + CI 通过）

## 分支策略

- `main`：稳定分支
- `dev`：开发分支（与 `main` 对齐，禁止分叉）
- 所有代码必须通过 PR 合并到 `main`
- 禁止直接推送 `main`

## 测试

| 类型 | 框架 | 命令 |
|------|------|------|
| Go 测试 | testing.T + 表驱动 | `make test-go` |
| Python 测试 | pytest | `make test-python` |
| JS 测试 | node:test | `make test-js` |
| 全部 | - | `make check` |

测试要求：
- 每个测试使用 `t.TempDir()` 创建独立临时目录
- 数据库测试使用 `auth.ResetDB()` 重置状态
- 新功能必须包含对应的单元测试或集成测试

## 代码审计

所有新功能需经独立 agent 审计，覆盖：
1. **功能正确性**：逻辑完整、边界覆盖
2. **健壮性**：错误处理、资源泄漏、并发安全
3. **安全性**：注入攻击、路径遍历、认证绕过
4. **代码质量**：命名规范、重复代码、复杂度

## 数据库迁移

新增迁移步骤：

```go
// internal/store/migrations/20250630_120000_add_column.go
func init() {
  Register(Migration{
    Timestamp: "20250630120000",
    Desc:      "添加 xxx 表的 yyy 列",
    Up: func(engine *xorm.Engine) error {
      exists, err := ColumnExists(engine, "table_name", "column_name")
      if err != nil { return err }
      if !exists {
        _, err = engine.Exec("ALTER TABLE table_name ADD COLUMN column_name TEXT DEFAULT ''")
      }
      return err
    },
  })
}
```

- 时间戳用 `date +%Y%m%d%H%M%S` 生成
- 迁移函数必须幂等
- 禁止直接在 `syncSchema()` 中加 `ALTER TABLE`

## 命名规范

- **PicoAide**（`picoaide`）：管理工具——本仓库
- **PicoClaw**（`picoclaw`）：上游 AI 代理项目（sipeed/picoclaw）
- Go 模块路径：`github.com/picoaide/picoaide`
