---
title: "API 参考"
description: "PicoAide HTTP API 端点完整参考 — 认证、用户、管理、MCP 和 WebSocket 接口"
weight: 13
draft: false
---

PicoAide API 主要供内置管理后台、浏览器扩展和桌面客户端使用。所有路由均注册在 `/api` 和 `/api/v1` 双前缀下，如 `/api/health` 和 `/api/v1/health` 均可访问。`/api/version` 为单一路径。

## 通用约定

### 会话 Cookie

登录成功后服务端写入 `session` Cookie：

```http
Cookie: session=<username>:<timestamp>:<signature>
```

Cookie 是 HMAC-SHA256 签名的服务端会话 token，有效期 24 小时，HttpOnly + SameSite=Lax。

### CSRF 保护

需要修改状态的 POST/PUT/PATCH 请求通常需要传 `csrf_token` 字段。通过以下接口获取：

```text
GET /api/csrf
```

返回：

```json
{
  "success": true,
  "csrf_token": "..."
}
```

文件上传端点免 CSRF 检查。

### MCP Token 认证

MCP 和执行端 WebSocket 使用 Bearer token 或 query token：

```http
Authorization: Bearer <token>
```

或：

```text
?token=<token>
```

普通用户通过 `GET /api/mcp/token` 获取。超管不能获取 MCP token。

## 响应格式

成功响应：

```json
{
  "success": true,
  "message": "..."
}
```

错误响应：

```json
{
  "success": false,
  "error": "..."
}
```

资源型接口（如文件列表、用户信息等）返回对应的数据结构。

## 基础接口（无需认证）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/version` | 获取服务端版本号 |
| `GET` | `/api/health` | 健康检查，返回版本信息 |
| `GET` | `/api/login/mode` | 获取当前认证模式及活跃认证源元数据 |

## 认证接口

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| `POST` | `/api/login` | 限流 | 用户名密码登录，表单：`username`、`password`。设置 `session` Cookie |
| `GET` | `/api/login/auth` | 无 | 启动浏览器 SSO 流程，重定向到 OIDC 授权 URL |
| `GET` | `/api/login/callback` | 无 | SSO 回调处理器，验证 state、兑换 code、创建会话 |
| `POST` | `/api/logout` | 登录 | 登出，清除 session Cookie |
| `GET` | `/api/csrf` | 登录 | 获取 CSRF token |

登录接口有限流器保护（10 次/5 分钟/IP）。

## 用户信息接口（需 Session Cookie）

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/api/user/info` | 登录 | 当前用户信息（username、role、source） |
| `GET` | `/api/user/init-status` | 登录 | 用户目录初始化状态 |
| `POST` | `/api/user/password` | 普通用户 | 修改密码（仅本地模式）。表单：`old_password`、`new_password` |

## 对话接口（需 Session Cookie）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/user/chat/history` | 获取最新对话历史 |
| `POST` | `/api/user/chat/send` | 发送消息。表单：`message`。返回 `run_id` |
| `GET` | `/api/user/chat/stream` | SSE 流式输出。参数：`run_id`。支持断线重连 |
| `POST` | `/api/user/chat/stop` | 停止当前正在运行的对话 |

## 渠道配置接口（需 Session Cookie）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/channels` | 用户可用的通讯渠道列表及启用/配置状态 |
| `GET` | `/api/channels/config-fields` | 获取指定渠道的配置字段定义和当前值。参数：`section` |
| `POST` | `/api/channels/config-fields` | 保存渠道配置。表单：`section`、`values`（JSON）。实时更新 IM 连接 |

## 定时任务接口（需登录）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/cron` | 列出当前用户的定时任务 |
| `POST` | `/api/cron/create` | 创建定时任务。表单：`schedule`、`prompt`、`channel_id` |
| `POST` | `/api/cron/update` | 更新定时任务。表单：`id`、`schedule`、`prompt`、`channel_id` |
| `POST` | `/api/cron/delete` | 删除定时任务。表单：`id` |
| `POST` | `/api/cron/toggle` | 切换定时任务启用/禁用。表单：`id` |

## Cookie 同步接口（需 Session Cookie）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/cookies` | 同步网页 Cookie 到用户配置。表单：`domain`、`cookies` |
| `GET` | `/api/user/cookies` | 列出已授权的 Cookie 域名 |
| `POST` | `/api/user/cookies/delete` | 撤销 Cookie 域名授权。表单：`domain` |

## 文件管理接口（需 Session Cookie + 普通用户）

工作区路径为 `users/<username>/`（沙箱内的 `/root`）。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/files` | 列目录（含面包屑导航）。参数：`path`。返回 `entries[]`、`breadcrumb[]` |
| `POST` | `/api/files/upload` | 上传文件（最大 32MB）。表单：`path`、`file`（multipart） |
| `GET` | `/api/files/download` | 下载文件。参数：`path` |
| `POST` | `/api/files/delete` | 删除文件或目录。表单：`path` |
| `POST` | `/api/files/mkdir` | 创建目录。表单：`path`、`name` |
| `GET` | `/api/files/edit` | 读取文本文件内容。参数：`path`。返回 `filename`、`content` |
| `POST` | `/api/files/edit` | 保存文本文件内容。表单：`path`、`content` |

## 共享文件夹接口（需登录）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/shared-folders` | 列出当前用户可访问的共享文件夹 |

## 用户技能接口（需 Session Cookie + 普通用户）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/user/skills` | 列出所有技能及安装状态（installed/group/available） |
| `POST` | `/api/user/skills/install` | 安装技能。表单：`skill_name` |
| `POST` | `/api/user/skills/uninstall` | 卸载技能。表单：`skill_name` |

## 全局配置接口（需超管 Session Cookie）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/config` | 以 JSON 格式读取全部配置 |
| `POST` | `/api/config` | 保存配置 JSON。自动处理认证源切换清理 |

## MCP 接口

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/api/mcp/token` | Session Cookie | 获取/生成当前用户的 MCP token |
| `GET` | `/api/mcp/sse/:service` | MCP Token | 建立 MCP SSE 流（browser/computer/agent） |
| `POST` | `/api/mcp/sse/:service` | MCP Token | 发送 JSON-RPC 消息（initialize/tools/list/tools/call） |
| `GET` | `/api/mcp/cookies` | MCP Token | 获取已存储的 Cookie。参数：`domain`（可选） |
| `POST` | `/api/mcp/cookies` | MCP Token | 存储 Cookie。表单：`domain`、`cookies` |
| `GET` | `/api/mcp/cookies` | MCP Token | 获取已存储的 cookie。参数：`domain`（可选） |
| `POST` | `/api/mcp/cookies` | MCP Token | 存储 cookie。表单：`domain`、`cookies` |

## WebSocket 代理接口

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/api/browser/ws` | MCP Token | 浏览器扩展 WebSocket |
| `GET` | `/api/computer/ws` | MCP Token | 桌面客户端 WebSocket |

## 沙箱内部接口（仅沙箱内可访问）

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/api/picoagent/me` | Bearer Token | 获取 picoagent 运行配置（模型、工具、MCP socket 路径） |

## 管理接口（需超管 Session Cookie）

### 用户管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/users` | 用户列表（分页+搜索）。参数：`page`、`page_size`、`search` |
| `POST` | `/api/admin/users/create` | 创建本地用户。返回生成的密码 |
| `POST` | `/api/admin/users/batch-create` | 批量创建用户。表单：`usernames`（换行分隔） |
| `POST` | `/api/admin/users/delete` | 删除用户（归档目录）。表单：`username` |

### 超管账户管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/superadmins` | 超管列表 |
| `POST` | `/api/admin/superadmins/create` | 创建超管，返回随机密码 |
| `POST` | `/api/admin/superadmins/delete` | 删除超管（至少保留一个） |
| `POST` | `/api/admin/superadmins/reset` | 重置超管密码 |
| `POST` | `/api/admin/password` | 超管修改自己的密码 |

### 认证与白名单

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/whitelist` | 白名单列表（分页+搜索） |
| `POST` | `/api/admin/whitelist` | 更新白名单（替换或增量） |
| `POST` | `/api/admin/auth/test-ldap` | 测试 LDAP 连接和组查询 |
| `GET` | `/api/admin/auth/ldap-users` | 获取目录用户列表 |
| `POST` | `/api/admin/auth/sync-users` | 手动同步目录用户 |
| `POST` | `/api/admin/auth/sync-groups` | 手动同步组 |
| `GET` | `/api/admin/auth/providers` | 已注册的认证源列表 |

### 用户组管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/groups` | 组列表（分页+搜索） |
| `POST` | `/api/admin/groups/create` | 创建组。表单：`name`、`description`、`parent_id` |
| `POST` | `/api/admin/groups/delete` | 删除组。表单：`name` |
| `GET` | `/api/admin/groups/members` | 查看组成员。参数：`name` |
| `POST` | `/api/admin/groups/members/add` | 添加组成员。表单：`group_name`、`usernames` |
| `POST` | `/api/admin/groups/members/remove` | 移除组成员。表单：`group_name`、`username` |
| `POST` | `/api/admin/groups/skills/bind` | 绑定技能到组。表单：`group_name`、`skill_name` |
| `POST` | `/api/admin/groups/skills/unbind` | 解绑组技能。表单：`group_name`、`skill_name` |

### 技能管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/skills` | 技能列表（分页+搜索+来源过滤） |
| `POST` | `/api/admin/skills/deploy` | 部署技能到用户/组 |
| `POST` | `/api/admin/skills/remove` | 删除技能 |
| `POST` | `/api/admin/skills/user/bind` | 绑定技能到单个用户 |
| `POST` | `/api/admin/skills/user/unbind` | 解绑用户技能 |
| `GET` | `/api/admin/skills/user/sources` | 用户技能来源列表 |
| `GET` | `/api/admin/skills/defaults` | 默认技能列表 |
| `POST` | `/api/admin/skills/defaults/toggle` | 切换技能默认安装 |

### 技能仓库管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/skills/sources` | 技能源列表 |
| `POST` | `/api/admin/skills/sources/git` | 添加 Git 技能仓库 |
| `POST` | `/api/admin/skills/sources/remove` | 移除技能仓库 |
| `POST` | `/api/admin/skills/sources/pull` | 拉取仓库更新 |
| `POST` | `/api/admin/skills/sources/refresh` | 刷新注册源索引 |
| `GET` | `/api/admin/skills/registry/list` | 注册中心技能列表 |
| `POST` | `/api/admin/skills/registry/install` | 从注册中心安装技能 |

### 共享文件夹管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/shared-folders` | 共享文件夹列表 |
| `POST` | `/api/admin/shared-folders/create` | 创建共享文件夹 |
| `POST` | `/api/admin/shared-folders/update` | 更新共享文件夹 |
| `POST` | `/api/admin/shared-folders/delete` | 删除共享文件夹 |
| `POST` | `/api/admin/shared-folders/groups/set` | 设置文件夹组可见范围 |
| `POST` | `/api/admin/shared-folders/test` | 测试用户挂载状态 |
| `POST` | `/api/admin/shared-folders/mount` | 挂载到所有关联用户 |

### MCP 服务器管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/mcp/servers` | MCP 服务器列表 |
| `POST` | `/api/admin/mcp/servers/create` | 创建 MCP 服务器 |
| `POST` | `/api/admin/mcp/servers/update/:id` | 更新 MCP 服务器 |
| `POST` | `/api/admin/mcp/servers/delete/:id` | 删除 MCP 服务器 |
| `GET` | `/api/admin/mcp/servers/grants` | 服务器授权列表 |
| `POST` | `/api/admin/mcp/servers/grants/add` | 添加授权 |
| `POST` | `/api/admin/mcp/servers/grants/remove/:id` | 移除授权 |
| `POST` | `/api/admin/mcp/servers/reload` | 重新加载所有 MCP 服务器 |
| `GET` | `/api/admin/mcp/servers/tools` | 获取服务器工具列表 |

### 其他管理接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/task/status` | 异步任务队列状态 |
| `GET` | `/api/admin/skill-install-policy` | 技能安装策略 |
| `POST` | `/api/admin/skill-install-policy` | 设置技能安装策略 |
| `GET` | `/api/admin/tls/status` | TLS 证书状态 |
| `POST` | `/api/admin/tls/upload` | 上传 TLS 证书 |
| `POST` | `/api/admin/tls/clear` | 清除 TLS 证书 |
| `GET` | `/api/admin/channels` | 渠道定义列表 |
| `POST` | `/api/admin/model/test` | 测试模型连接 |
