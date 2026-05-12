# 镜像管理页面

## 页面路由

`/admin/images`

## 权限要求

超管（`superadmin`）登录

## 功能概述

管理 Docker 镜像，包括查看本地镜像列表、拉取远程镜像、删除本地镜像、将用户容器迁移到新镜像。

## 功能详细说明

### 1. 本地镜像列表

**功能**：展示所有已下载的镜像，显示每个镜像的使用用户数

**API 端点**：`GET /api/admin/images`

**响应格式**：
```json
{
  "success": true,
  "images": [
    {
      "id": "sha256:abc...",
      "repository": "ghcr.io/picoaide/picoclaw",
      "tag": "v1.0.0",
      "size": 1234567890,
      "created_at": "2024-01-01T00:00:00Z",
      "user_count": 5,
      "users": ["user1", "user2", "user3"]
    }
  ]
}
```

### 2. 拉取镜像

**触发方式**：点击"拉取镜像"按钮 → 选择远程标签 → 开始拉取

**API 端点**：`POST /api/admin/images/pull`

**请求格式**：
```json
{
  "image": "ghcr.io/picoaide/picoclaw:v1.0.0"
}
```

**响应**：SSE（Server-Sent Events）流式进度更新

```
event: progress
data: {"status": "Pulling fs layer", "progress": "..."}

event: progress
data: {"status": "Downloading", "current": 12345, "total": 67890}

event: complete
data: {"status": "completed", "message": "镜像拉取成功"}

event: error
data: {"status": "error", "message": "镜像拉取失败"}
```

前端使用 `EventSource` 或 fetch SSE 方式接收进度。

### 3. 远程仓库标签

**功能**：从远程镜像仓库获取可用标签列表，按版本号降序排列

**API 端点**：`GET /api/admin/images/registry`

**查询参数**：无

**响应格式**：
```json
{
  "success": true,
  "tags": [
    {
      "tag": "v1.0.0",
      "digest": "sha256:abc...",
      "created_at": "2024-01-01T00:00:00Z"
    },
    {
      "tag": "v0.9.0",
      "digest": "sha256:def..."
    }
  ]
}
```

### 4. 本地镜像标签

**功能**：获取本地已下载的镜像标签列表

**API 端点**：`GET /api/admin/images/local-tags`

**响应格式**：
```json
{
  "success": true,
  "tags": ["v1.0.0", "v0.9.0"]
}
```

### 5. 删除镜像

**触发方式**：点击镜像行的"删除"按钮 → 确认删除

**API 端点**：`POST /api/admin/images/delete`

**请求格式**：
```json
{
  "id": "sha256:abc..."
}
```

**响应格式（成功）**：
```json
{
  "success": true,
  "message": "镜像已删除"
}
```

**响应格式（失败）**：
```json
{
  "success": false,
  "error": "镜像正在被使用，无法删除"
}
```

**注意事项**：若有用户容器正在使用该镜像，删除操作会失败。需要先迁移用户到其他镜像。

### 6. 迁移用户到新镜像

**触发方式**：选择目标用户 → 选择目标镜像 → 确认迁移

**API 端点**：`POST /api/admin/images/migrate`

**请求格式**：
```json
{
  "from_image": "sha256:abc...",
  "to_image": "sha256:def...",
  "usernames": ["user1", "user2"]
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| from_image | string | 源镜像 ID 或名称 |
| to_image | string | 目标镜像 ID 或名称 |
| usernames | array | 要迁移的用户列表 |

**响应格式**：
```json
{
  "success": true,
  "message": "迁移完成",
  "succeeded": ["user1"],
  "failed": [
    { "username": "user2", "reason": "容器不在运行状态" }
  ]
}
```

**迁移过程**：

1. 停止用户容器
2. 使用新镜像重新创建容器
3. 启动容器
4. 更新 `containers` 表中的镜像记录

## 涉及的 API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/admin/images` | GET | 本地镜像列表 |
| `/api/admin/images/pull` | POST | 拉取镜像（SSE 流式） |
| `/api/admin/images/delete` | POST | 删除本地镜像 |
| `/api/admin/images/migrate` | POST | 迁移用户到新镜像 |
| `/api/admin/images/registry` | GET | 远程仓库标签 |
| `/api/admin/images/local-tags` | GET | 本地镜像标签 |
