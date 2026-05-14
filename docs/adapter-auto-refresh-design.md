# Picoclaw 适配器自动拉取方案

## 背景

当前问题：
1. `init` 和首次 `serve` 都不会自动拉取 Picoclaw 适配器，只有手动点 API `POST /api/admin/migration-rules/refresh` 才拉取
2. 默认远程源 `raw.githubusercontent.com` 在国内经常无法访问
3. 没有定时检查更新机制

## 改动概述

### 1. 去重：规则文件统一放在 internal/user/picoclaw_rules/

当前有两份副本：
- `rules/picoclaw/`（顶层，Hugo 用）
- `internal/user/picoclaw_rules/`（Go embed 用）

改为只保留 `internal/user/picoclaw_rules/`，删除顶层 `rules/picoclaw/`。

| 用途 | 路径 |
|------|------|
| 单一源文件 | `internal/user/picoclaw_rules/` |
| Go embed | `//go:embed all:picoclaw_rules`（不变） |
| Hugo 构建复制 | `cp -r ../internal/user/picoclaw_rules static/rules/picoclaw` |
| 文件系统回退 | 加一条到 `findBundledPicoClawAdapterRoot` 中 |

发布后 `https://www.picoaide.com/rules/picoclaw/hash`。

> Hugo 模块挂载的 `source` 路径不能超出项目目录（`website/`），所以改用 CI 中 `cp -r` 复制的方式。

### 2. 改默认适配器 URL

`internal/config/config.go` 中 `PicoClawAdapterRemoteBaseURLs()` 的默认值改为：

```
https://www.picoaide.com/rules/picoclaw
```

保留覆盖顺序：数据库配置 > `PICOAIDE_PICOCLAW_ADAPTER_URLS` 环境变量 > `PICOAIDE_PICOCLAW_ADAPTER_URL` 环境变量 > 默认值。数据库字段 `picoclaw_adapter_remote_base_url` 支持逗号分隔。

### 3. 定时刷新 + 更新检测 + 重试

新增 `AutoRefreshPicoClawAdapter` 函数：

```
1. 读取本地 hash 文件
2. Fetch 远程 hash 文件
3. bytes.Equal(localHash, remoteHash) → 相同则跳过
4. 不同则走完整下载 + 原子替换
5. 失败时指数退避：30s → 1m → 2m → 4m → 8m → cap 1h
6. 成功后退避复原，每 24h 检查一次
```

在 `web.Serve()` 中启动后台 goroutine。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `rules/picoclaw/` | 删除 |
| `.github/workflows/website.yml` | 加 `cp -r` 复制适配器文件步骤 |
| `internal/config/config.go` | 加 `PicoClawAdapterRemoteBaseURLs()`，改默认值 |
| `internal/user/picoclaw_adapter.go` | 加 `RefreshIfChanged` + `AutoRefresh` 重试循环；更新 `findBundledPicoClawAdapterRoot` |
| `internal/user/picoclaw_migration.go` | 加 `AutoRefreshPicoClawMigrationRules` 包装函数 |
| `internal/web/server.go` | 启动时调用 `AutoRefresh` goroutine |
| `internal/web/admin_config.go` | 手动刷新 API 使用多 URL 和更新检测 |
