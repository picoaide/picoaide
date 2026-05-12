# 普通用户管理页面

## 页面路由

`/manage`

## 权限要求

普通用户登录后访问，超管不可用（超管跳转 `/admin/dashboard`）

## 功能概述

为用户提供自我管理界面，包含通讯渠道配置、文件管理、修改密码、钉钉配置、Cookie 同步和 MCP Token 查看。

## 功能标签

页面使用标签切换不同功能模块：

- 通讯渠道
- 文件管理
- 修改密码
- 文本编辑器

## 功能详细说明

### 1. 通讯渠道

**功能**：查看当前用户的渠道策略、启用/禁用渠道、配置渠道参数

**API 端点**：

- `GET /api/user/channels` — 读取用户渠道列表及启用状态
- `POST /api/user/channels` — 更新渠道启用状态
- `GET /api/user/config/fields` — 获取渠道配置字段定义
- `POST /api/user/config/fields` — 保存渠道配置字段

**获取渠道列表请求**：`GET /api/user/channels`

**响应格式**：
```json
{
  "success": true,
  "channels": [
    {
      "name": "wechat",
      "allowed": true,
      "enabled": true,
      "configured": true
    }
  ]
}
```

**启用/禁用渠道请求**：`POST /api/user/channels`
```json
{
  "channel": "wechat",
  "enabled": true
}
```

**配置渠道字段请求**：`POST /api/user/config/fields`
```json
{
  "channel": "wechat",
  "fields": {
    "app_id": "wx123456",
    "app_secret": "secret"
  }
}
```

**保存后自动重启容器**：配置保存成功后，调用 `POST /api/admin/container/restart`（单用户）重启该用户的代理容器。

### 2. 文件管理

**功能**：在 `.picoclaw/workspace/` 沙盒目录中 CRUD 文件

**限制**：

- 上传文件最大 32MB
- 路径限制在 `.picoclaw/workspace/` 目录内
- 通过 `util.SafeRelPath` 防止目录遍历

**API 端点**：

| 操作 | 端点 | 方法 |
|------|------|------|
| 列出文件 | `/api/files` | GET |
| 上传文件 | `/api/files/upload` | POST |
| 下载文件 | `/api/files/download` | GET |
| 删除文件 | `/api/files/delete` | POST |
| 创建目录 | `/api/files/mkdir` | POST |
| 读取文件内容 | `/api/files/edit` | GET |
| 保存文件内容 | `/api/files/edit` | POST |

**列出文件请求**：`GET /api/files?path=/`

**响应格式**：
```json
{
  "success": true,
  "entries": [
    { "name": "config.json", "type": "file", "size": 1024 },
    { "name": "data", "type": "dir" }
  ],
  "breadcrumb": [
    { "name": "workspace", "path": "/" }
  ]
}
```

**上传文件**：`POST /api/files/upload`（multipart/form-data）

| 字段 | 说明 |
|------|------|
| file | 文件内容 |
| path | 目标目录路径 |

**下载文件**：`GET /api/files/download?path=/config.json`

返回文件二进制流。

**删除文件/目录**：`POST /api/files/delete`
```json
{
  "path": "/old_file.txt"
}
```

**创建目录**：`POST /api/files/mkdir`
```json
{
  "path": "/new_folder"
}
```

**读取文本文件**：`GET /api/files/edit?path=/config.json`

**响应格式**：
```json
{
  "success": true,
  "content": "文件内容...",
  "path": "/config.json"
}
```

**保存文本文件**：`POST /api/files/edit`
```json
{
  "path": "/config.json",
  "content": "新的文件内容..."
}
```

### 3. 修改密码

**功能**：修改当前用户的登录密码

**API 端点**：`POST /api/user/password`

**注意**：仅本地认证模式可用。统一认证模式（LDAP/OIDC）下不可用，界面提示"请联系管理员"。

**请求格式**：
```json
{
  "old_password": "旧密码",
  "new_password": "新密码"
}
```

**响应格式（成功）**：
```json
{
  "success": true,
  "message": "密码修改成功"
}
```

**响应格式（失败）**：
```json
{
  "success": false,
  "error": "旧密码错误"
}
```

### 4. 钉钉配置

**功能**：读取和保存钉钉机器人配置（client_id / client_secret）

**API 端点**：

- `GET /api/dingtalk` — 读取钉钉配置
- `POST /api/dingtalk` — 保存钉钉配置

**读取请求**：`GET /api/dingtalk`

**响应格式**：
```json
{
  "success": true,
  "client_id": "dingxxx",
  "client_secret": "secretxxx"
}
```

**保存请求**：`POST /api/dingtalk`
```json
{
  "client_id": "dingxxx",
  "client_secret": "secretxxx"
}
```

保存成功后自动调用容器重启。

### 5. Cookie 同步

**功能**：将浏览器 Cookie 同步到用户的 `.security.yml` 文件，使代理容器中的浏览器自动化功能可使用已登录的会话

**API 端点**：`POST /api/cookies`

**请求格式**：
```json
{
  "cookies": [
    {
      "domain": ".example.com",
      "name": "session",
      "value": "abc123",
      "path": "/"
    }
  ]
}
```

### 6. MCP Token

**功能**：获取当前用户的 MCP Token，用于浏览器扩展/桌面代理的认证

**API 端点**：`GET /api/mcp/token`

**响应格式**：
```json
{
  "success": true,
  "token": "username:a1b2c3d4e5f6..."
}
```

**注意**：MCP Token 格式为 `用户名:随机hex`。超管账号不可用此功能。
