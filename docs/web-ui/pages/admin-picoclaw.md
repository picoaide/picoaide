# 渠道策略页面

## 页面路由

`/admin/picoclaw`

## 权限要求

超管（`superadmin`）登录

## 功能概述

管理全局渠道策略（允许/禁用渠道）、查看用户的 Picoclaw 配置、下发配置到用户。

## 功能详细说明

### 1. 渠道管理

**功能**：查看各渠道的基本信息（名称、显示名、描述）。渠道的允许/禁用状态通过全局配置 `POST /api/config` 管理（配置键 `picoclaw.channels.<渠道名>.allowed`）。

**API 端点**：

- `GET /api/admin/picoclaw/channels` — 获取渠道列表

**获取渠道列表**：`GET /api/admin/picoclaw/channels`

**响应格式**：
```json
{
  "success": true,
  "channels": [
    {
      "name": "wechat",
      "display_name": "微信",
      "icon": "wechat",
      "description": "微信渠道",
      "allowed": true
    },
    {
      "name": "dingtalk",
      "display_name": "钉钉",
      "icon": "dingtalk",
      "description": "钉钉渠道",
      "allowed": true
    },
    {
      "name": "slack",
      "display_name": "Slack",
      "icon": "slack",
      "description": "Slack 渠道",
      "allowed": false
    }
  ]
}
```

### 2. 配置下发

**功能**：将全局配置和应用设置下发到指定用户的容器配置文件

**API 端点**：`POST /api/admin/config/apply`

**请求格式**：
```json
{
  "username": "user1"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| username | string | 目标用户名，或传 `"all"` 下发到所有用户 |

**响应格式**：
```json
{
  "success": true,
  "message": "配置下发成功",
  "config_path": "users/user1/.picoclaw/config.json",
  "security_path": "users/user1/.picoclaw/.security.yml"
}
```

**配置下发**仅下发全局配置，不涉及逐个用户的配置查看和编辑。各用户的渠道启用状态由用户自行在 `/manage` 页面管理。

**配置合并规则**：

1. 读取用户的当前配置
2. 合并全局 Picoclaw 配置（`picoclaw` 键下的配置）
3. 合并用户所在组的配置
4. 合并渠道安全配置
5. 写入用户的 `config.json` 和 `.security.yml`
6. 如果配置版本低于当前支持版本，执行迁移链升级

## Picoclaw Adapter

**功能**：管理 Picoclaw 配置版本适配器，支持不同版本的配置格式

**API 端点**：

- `GET /api/admin/migration-rules` — 查看迁移规则信息
- `POST /api/admin/migration-rules/refresh` — 刷新迁移规则（远程拉取）
- `POST /api/admin/migration-rules/upload` — 上传 Adapter ZIP

**获取迁移规则信息**：`GET /api/admin/migration-rules`

**响应格式**：
```json
{
  "success": true,
  "current_version": 3,
  "supported_versions": [1, 2, 3],
  "cached_at": "2024-01-01T00:00:00Z",
  "cache_ttl": 3600
}
```

**刷新迁移规则**：`POST /api/admin/migration-rules/refresh`

支持从远程 URL 拉取（通过配置 `picoclaw_adapter_remote_base_url` 设置）或上传 ZIP 包。

## 涉及的 API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/admin/picoclaw/channels` | GET | 渠道列表 |
| `/api/admin/config/apply` | POST | 下发配置到用户 |
| `/api/admin/migration-rules` | GET | 迁移规则信息 |
| `/api/admin/migration-rules/refresh` | POST | 刷新迁移规则 |
| `/api/admin/migration-rules/upload` | POST | 上传 Adapter ZIP |
