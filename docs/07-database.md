# 数据库

基于 xorm ORM + modernc/sqlite（纯 Go SQLite，无 CGO）。

## 表结构

### local_users

| 列 | 类型 | 说明 |
|-----|------|------|
| username | TEXT PK | 用户名 |
| password_hash | TEXT | argon2id/bcrypt 哈希 |
| role | TEXT | superadmin / user |
| source | TEXT | local / ldap / oidc |
| created_at | TEXT | 创建时间 |
| updated_at | TEXT | 更新时间 |

### containers

| 列 | 类型 | 说明 |
|-----|------|------|
| username | TEXT PK | 用户名 |
| container_id | TEXT | Docker 容器 ID |
| image | TEXT | 镜像名 |
| status | TEXT | 容器状态 |
| ip | TEXT | 分配的 IP |
| cpu_limit | TEXT | CPU 限制 |
| memory_limit | TEXT | 内存限制 |
| mcp_token | TEXT | MCP 认证 token |

### settings

| 列 | 类型 | 说明 |
|-----|------|------|
| key | TEXT PK | 点分隔的配置键 |
| value | TEXT | 配置值 |

### settings_history

配置变更审计日志：key, old_value, new_value, changed_by, changed_at

### whitelist

| 列 | 类型 | 说明 |
|-----|------|------|
| username | TEXT PK | 白名单用户 |
| added_by | TEXT | 添加人 |
| added_at | TEXT | 添加时间 |

### groups

| 列 | 类型 | 说明 |
|-----|------|------|
| id | INTEGER PK | 组 ID |
| name | TEXT UNIQUE | 组名 |
| parent_id | INTEGER | 父组 ID（树形结构） |
| description | TEXT | 描述 |
| source | TEXT | local / ldap |

### user_groups

| 列 | 类型 | 说明 |
|-----|------|------|
| username | TEXT | 用户名 |
| group_id | INTEGER | 组 ID |

### group_skills

| 列 | 类型 | 说明 |
|-----|------|------|
| group_id | INTEGER | 组 ID |
| skill_name | TEXT | 技能名 |

### user_channels

| 列 | 类型 | 说明 |
|-----|------|------|
| username | TEXT | 用户名 |
| channel | TEXT | 渠道标识 |
| allowed | INTEGER | 是否允许 |
| enabled | INTEGER | 是否启用 |
| configured | INTEGER | 是否已配置 |
| config_version | INTEGER | 配置版本号 |

## 迁移系统

位置：`internal/store/migrations/`

基于时间戳的迁移文件，`YYYYMMDD_HHMMSS_description.go`。通过 `init()` + `Register()` 注册，在 `RunAll()` 中自动执行。

迁移函数必须幂等（使用 `ColumnExists` 检查列是否存在）。

当前迁移文件：

| 文件 | 说明 |
|------|------|
| `20250601..._mcp_server.go` | MCP 服务器表 |
| `20250610..._shared_folders.go` | 共享文件夹 |
| `20250615..._user_channels.go` | 用户渠道 |
| `20250620..._user_emails.go` | 用户邮箱 |
| `20250625..._ip_allocation.go` | IP 分配 |
| ... | ... |

## 配置存储

- `settings` 表以点分隔的 key-value 存储全局配置
- `flattenConfig()` 将嵌套 map 展平为 `ldap.host`、`web.listen` 等键
- `buildNested()` 反向重建嵌套结构
- 字符串/数字/布尔值直接存为字符串，数组序列化为 JSON
- `picoclaw`、`security`、`skills` 三个顶层键整体序列化为 JSON blob

## 密码哈希

- 新用户：argon2id（memory=4KB, time=1, threads=1, keyLen=32, saltLen=16）
- 兼容旧 bcrypt 密码
- 文件：`internal/store/auth.go`
