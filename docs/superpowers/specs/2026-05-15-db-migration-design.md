# 数据库 Schema 迁移系统设计

## 概述

为 PicoAide 引入基于时间戳的数据库 Schema 迁移系统，替代当前在 `syncSchema()` 中暴力 `ALTER TABLE ADD COLUMN`（忽略错误）的方式。新系统允许开发者以可追溯、可排序的方式逐步演进数据库 Schema，并支持改名、删列、数据迁移等复杂操作。

## 核心设计

### 版本存储

利用现有的 `settings` 表存储当前 Schema 版本：

| 键 | 值 | 说明 |
|----|-----|------|
| `internal.schema_version` | `20250515000000` | 14 位时间戳 `YYYYMMDDHHMMSS` |

### 迁移文件

每个迁移是一个独立的 Go 文件，位于 `internal/auth/migrations/`，遵循命名规范：

```
YYYYMMDD_HHMMSS_description.go
```

每个文件在 `init()` 中通过 `Register()` 注册自身：

```go
func init() {
  Register(Migration{
    Timestamp: "20250520103000",
    Desc:      "添加 containers.os 列",
    Up: func(engine *xorm.Engine) error {
      exists, err := ColumnExists(engine, "containers", "os")
      if err != nil {
        return err
      }
      if !exists {
        _, err = engine.Exec("ALTER TABLE containers ADD COLUMN os TEXT DEFAULT ''")
      }
      return err
    },
  })
}
```

### 注册表 (`migrations.go`)

- `Migration` 结构体：`Timestamp`、`Desc`、`Up` 函数
- `Register()` 在 `init()` 中将 migration 加入全局 registry
- `All()` 按时间戳排序返回所有已注册 migration
- `RunAll(engine)` 执行未执行过的 migration

### 执行时机

在 `syncSchema()` 末尾调用 `migrations.RunAll(engine)`：

```
InitDB → syncSchema → CREATE TABLE IF NOT EXISTS ... → migrations.RunAll()
```

`syncSchema` 保留所有 `CREATE TABLE IF NOT EXISTS` 语句（确保表结构基础存在），但**移除所有 ALTER TABLE 语句**，将其转换为独立的 migration 文件。

## 初始化与升级流程

### 全新安装

1. `syncSchema()` 创建所有表（包含当前完整列定义）
2. `migrations.RunAll()` 执行所有 migration
3. 每个 migration 先检查列是否存在（`PRAGMA table_info`），存在则跳过
4. 全部执行完毕后，`internal.schema_version` = 最大 migration 时间戳

### 旧版本升级（无 schema_version）

1. `syncSchema()` 创建不存在的表
2. `migrations.RunAll()` 执行所有 migration
3. 缺失的列被逐个添加，已存在的列被跳过
4. `internal.schema_version` = 最大 migration 时间戳

### 已迁移版本升级

1. `syncSchema()` 创建不存在的表
2. `migrations.RunAll()` 跳过所有 `Timestamp <= cur` 的 migration
3. 只执行新加的 migration
4. `internal.schema_version` 更新到最新

## 开发指南

### 添加新 migration

1. 在 `internal/auth/migrations/` 创建新文件，命名 `YYYYMMDD_HHMMSS_description.go`
2. 实现 `init()` + `Register()`
3. migration 函数必须是幂等的（可重复执行不报错）
4. 使用 `ColumnExists()` 辅助函数检查列是否已存在

### 分支合并

时间戳格式天然避免合并冲突：
- 不同分支在不同时间点创建 migration 文件，时间戳不同
- 唯一冲突场景：同一秒创建两个 migration 文件
- 解法：合并时重命名其中一个，加 1 秒

### 删除或重命名列

不直接支持。需要两步：
1. 新 migration 创建新列/新表
2. 后续发布中清理旧列/旧表

## 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `internal/auth/migrations/migrations.go` | **新增** | 注册表、RunAll、ColumnExists |
| `internal/auth/migrations/20250501_000000_add_local_users_source.go` | **新增** | 历史迁移：local_users.source |
| `internal/auth/migrations/20250502_000000_add_containers_mcp_token.go` | **新增** | 历史迁移：containers.mcp_token |
| `internal/auth/migrations/20250503_000000_add_groups_parent_id.go` | **新增** | 历史迁移：groups.parent_id |
| `internal/auth/migrations/20250504_000000_add_user_skills_source.go` | **新增** | 历史迁移：user_skills.source |
| `internal/auth/migrations/20250505_000000_add_user_skills_updated_at.go` | **新增** | 历史迁移：user_skills.updated_at |
| `internal/auth/migrations/20250506_000000_add_shared_folders_columns.go` | **新增** | 历史迁移：shared_folders 5 列 |
| `internal/auth/migrations/migrations_test.go` | **新增** | 迁移系统单元测试 |
| `internal/auth/auth.go` | 修改 | syncSchema 移除 ALTER TABLE，接入 RunAll |
| `AGENTS.md` | 修改 | 加入 migration 开发注意事项 |
