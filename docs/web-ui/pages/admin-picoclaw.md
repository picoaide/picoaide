# 渠道策略页面

## 页面路由

`/admin/picoclaw`

## 权限要求

超管（`superadmin`）登录

## 功能概述

管理全局渠道策略（允许/禁用渠道）、查看用户的 Picoclaw 配置、下发配置到用户。

## 功能详细说明

### 1. 渠道管理

**功能**：定义哪些渠道允许使用，以及各渠道的启用状态

**API 端点**：

- `GET /api/admin/channels` — 获取渠道列表
- `POST /api/admin/channels` — 更新渠道允许状态

**获取渠道列表**：`GET /api/admin/channels`

**响应格式**：
```json
{
  "success": true,
  "channels": [
    {
      "name": "wechat",
      "display_name": "微信",
      "allowed": true,
      "description": "微信渠道"
    },
    {
      "name": "dingtalk",
      "display_name": "钉钉",
      "allowed": true,
      "description": "钉钉渠道"
    },
    {
      "name": "slack",
      "display_name": "Slack",
      "allowed": false,
      "description": "Slack 渠道"
    }
  ]
}
```

**更新渠道状态**：`POST /api/admin/channels`
```json
{
  "channel": "slack",
  "allowed": true
}
```

### 2. 用户 Picoclaw 配置列表

**功能**：查看所有用户的 Picoclaw 配置概览

**API 端点**：`GET /api/admin/picoclaw/users`

**响应格式**：
```json
{
  "success": true,
  "users": [
    {
      "username": "user1",
      "config_version": 3,
      "channels": ["wechat", "dingtalk"],
      "container_status": "running",
      "last_config_applied": "2024-01-01T00:00:00Z"
    }
  ]
}
```

### 3. 查看/编辑用户配置

**功能**：查看或编辑指定用户的 Picoclaw 配置

**API 端点**：

- `GET /api/admin/picoclaw/user` — 获取指定用户配置
- `POST /api/admin/picoclaw/user` — 保存指定用户配置

**获取用户配置**：`GET /api/admin/picoclaw/user?username=user1`

**响应格式**：
```json
{
  "success": true,
  "username": "user1",
  "config": {
    "channels": {
      "wechat": {
        "enabled": true,
        "config": {
          "app_id": "wx123456"
        }
      }
    },
    "skills": ["code-review"],
    "picoclaw": {
      "model": "gpt-4"
    }
  },
  "security": {
    "cors_origins": ["https://example.com"]
  }
}
```

**保存用户配置**：`POST /api/admin/picoclaw/user`
```json
{
  "username": "user1",
  "config": {
    "channels": {
      "wechat": {
        "enabled": true
      }
    }
  }
}
```

### 4. 配置下发

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

- `GET /api/admin/picoclaw/adapter/info` — 获取 Adapter 信息
- `POST /api/admin/picoclaw/adapter/refresh` — 刷新 Adapter

**获取 Adapter 信息**：`GET /api/admin/picoclaw/adapter/info`

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

**刷新 Adapter**：`POST /api/admin/picoclaw/adapter/refresh`

支持从远程 URL 拉取（通过配置 `picoclaw_adapter_remote_base_url` 设置）或上传 ZIP 包。

## 涉及的 API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/admin/channels` | GET | 渠道列表 |
| `/api/admin/channels` | POST | 更新渠道允许状态 |
| `/api/admin/picoclaw/users` | GET | 用户配置列表 |
| `/api/admin/picoclaw/user` | GET | 获取用户配置 |
| `/api/admin/picoclaw/user` | POST | 保存用户配置 |
| `/api/admin/config/apply` | POST | 下发配置到用户 |
| `/api/admin/picoclaw/adapter/info` | GET | Adapter 信息 |
| `/api/admin/picoclaw/adapter/refresh` | POST | 刷新 Adapter |
