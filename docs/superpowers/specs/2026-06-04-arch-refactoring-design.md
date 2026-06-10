# PicoAide 架构重构设计文档

> 基于代码审计发现的 7 类设计问题，采用 TDD 方式系统性修复。

## 一、现状与问题

### 问题清单

| # | 问题 | 严重度 | 影响范围 |
|---|------|--------|---------|
| P1 | `internal/auth` 包名不副实，承担了整个 DAO 层职责（10+ 个领域） | 关键 | 全部 6 个内部包 |
| P2 | `internal/config` → `internal/auth` 反向依赖 | 高 | 启动流程 |
| P3 | Web handler 认证/Method/CSRF 检查重复 25+ 次 | 高 | `internal/web` |
| P4 | Web 包 7+ 个全局可变状态 | 高 | 测试隔离性 |
| P5 | Agent `Process()` 421 行单体方法 + 核心逻辑无测试 | 高 | `internal/agent` |
| P6 | Agent 包职责超载（15+ 领域）、死代码 | 中 | `internal/agent` |
| P7 | Web handler 错误返回模式不一致、JSON 响应三套混用 | 中 | `internal/web` |

## 二、目标架构

### Phase 0: 死代码清理

**目标：** 删除无引用代码，零行为变更。

| 文件 | 原因 |
|------|------|
| `internal/agent/overflow.go` | `IsOverflow()`/`UsableTokens()` 无任何引用，功能已由 `compactor.go` 覆盖 |
| `internal/agent/structured_output.go` | `StructuredOutputConfig`/`BuildStructuredOutputTool` 定义但 `Process()` 从未使用 |

**TDD 策略：** 搜索确认无引用 → 删除 → 编译验证通过。

### Phase 1: Auth 包数据访问层拆分

**目标：** 将 `internal/auth` 中的数据访问职责迁入 `internal/store`，`auth` 仅保留认证逻辑。

#### 新包结构

```
internal/store/              # 数据访问层
  store.go                   # InitDB, ResetDB, GetEngine, syncSchema, StartAuditLogCleaner
  models.go                  # 全部 ORM 模型结构体（17 个）
  users.go                   # 用户 CRUD
  groups.go                  # 组 CRUD + 树形结构 + 成员管理
  skills.go                  # 技能绑定
  channels.go                # 渠道管理
  cookies.go                 # Cookie 存储
  ip_allocation.go           # IP 地址分配
  shared_folders.go          # 共享文件夹 CRUD
  MCP tokens                 # 合并到 store.go 或独立 mcp_tokens.go

internal/auth/               # 仅保留认证逻辑
  auth.go                    # AuthenticateLocal, ChangePassword, 密码哈希
  models.go                  # LocalUser 模型（其他模型移到 store）
```

#### 迁移顺序

1. 创建 `internal/store/store.go`（InitDB/ResetDB/GetEngine 从 auth 复制）
2. 逐个迁移 models、users、groups、skills 等文件
3. 每迁移一个文件，更新该文件的所有调用方
4. 保留 `internal/auth` 中的认证逻辑
5. 删除 `internal/auth` 中已迁移的文件

**TDD 策略：** 每个迁移步骤前，确认被迁移函数的现有测试覆盖。如无测试，先补测试再迁移。

### Phase 2: Config 包依赖反转

**目标：** 消除 `internal/config` → `internal/auth` 的导入依赖。

**方案：** 给 `config` 包添加一个注入点：

```go
// config/globalconfig.go
var getEngine func() *xorm.Engine
func SetEngineProvider(provider func() *xorm.Engine) {
    getEngine = provider
}
```

`cmd/picoaide/main.go` 在启动时注入。

### Phase 3: Web 包修复

#### 3a. 中间件提取

将重复的 `requireSuperadmin + Method 检查 + CSRF 检查` 合成一个 Gin middleware。

#### 3b. handlers.go 拆分

| 新文件 | 迁移内容 |
|--------|---------|
| `handlers.go` | 保留：JSON 工具函数、health、version、login/logout、CSRF |
| `handlers_auth.go` | 迁移：auth start/callback、auth mode、password change |
| `handlers_config.go` | 迁移：config get/save |
| `handlers_cookies.go` | 迁移：cookie sync、user cookies |
| `handlers_user.go` | 迁移：user info、user init-status |

#### 3c. 全局状态治理 + CSRF 一致性

全局变量迁移到 `Server` 结构体，补上 `admin_mcp.go` 缺失的 CSRF 检查。

### Phase 4: Agent 包重构

#### 4a. Process() 拆分

将 421 行的 `Process()` 拆分为 `buildSystemPrompt()`、`executeLLMCall()`、`executeTool()`、`handleCompaction()`。

#### 4b. tool_registry.go 拆分

按命令、文件系统、文件查询拆分为多个工具文件。

#### 4c. 工具错误返回统一

全部使用 `&ToolResult{Success: false, Data: msg}` 格式。

#### 4d. 为 Process() 补充测试

mock Provider 测试：正常流、工具调用、溢出后 compaction、LLM 重试、取消。

## 三、执行顺序

```
Phase 0 → Phase 1 → Phase 2 → Phase 3 → Phase 4
```

Phase 3 和 Phase 4 在 Phase 2 完成后可并行执行。

## 四、测试策略

- 所有测试使用 `testing.T` + 表驱动测试
- 数据库测试使用 `t.TempDir()` + `store.ResetDB()` 隔离
- 每个 TDD 循环：写测试 → 验证失败 → 写实现 → 验证通过 → 重构
