---
title: "API 参考"
description: "PicoAide HTTP API、MCP SSE 和管理接口参考"
weight: 12
draft: false
---

PicoAide API 主要供内置管理后台、浏览器扩展和桌面客户端使用。除健康检查和登录外，大部分接口需要登录会话或 MCP token。

## 通用约定

### 会话 Cookie

登录成功后服务端写入 Cookie：

```http
Cookie: session=<username>:<timestamp>:<signature>
```

Cookie 是 HMAC 签名的服务端会话 token，有效期 24 小时。

### CSRF

需要修改状态的表单 POST 通常要传 `csrf_token` 字段。通过以下接口获取：

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

### MCP token

MCP 和执行端 WebSocket 使用 Bearer token 或 query token：

```http
Authorization: Bearer <token>
```

或：

```text
?token=<token>
```

普通用户通过 `GET /api/mcp/token` 获取。超管不能获取 MCP token。

## 基础接口

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/api/version` | 无 | 获取服务端版本号 |
| `GET` | `/api/health` | 无 | 健康检查，返回版本 |
| `GET` | `/api/login/mode` | 无 | 获取当前认证模式 |
| `POST` | `/api/login` | 无 | 登录 |
| `POST` | `/api/logout` | 登录 | 登出 |
| `GET` | `/api/user/info` | 登录 | 当前用户、角色和认证模式 |
| `POST` | `/api/user/password` | 普通用户 | 本地模式下修改密码 |
| `GET` | `/api/csrf` | 登录 | 获取 CSRF token |

`POST /api/login` 使用表单字段：

| 字段 | 说明 |
| --- | --- |
| `username` | 用户名 |
| `password` | 密码 |

## 用户配置和文件接口

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/api/dingtalk` | 普通用户 | 获取钉钉配置 |
| `POST` | `/api/dingtalk` | 普通用户 + CSRF | 保存钉钉配置并重启容器 |
| `GET` | `/api/picoclaw/channels` | 普通用户 | 列出可用渠道 |
| `GET` | `/api/picoclaw/config-fields` | 普通用户 | 读取渠道配置字段 |
| `POST` | `/api/picoclaw/config-fields` | 普通用户 + CSRF | 保存渠道配置并重启容器 |
| `GET` | `/api/user/skills` | 普通用户 | 查看已安装技能 |
| `POST` | `/api/user/skills/install` | 普通用户 + CSRF | 安装技能 |
| `POST` | `/api/user/skills/uninstall` | 普通用户 + CSRF | 卸载技能 |
| `GET` | `/api/user/cookies` | 普通用户 | 查看已授权 Cookie 域名 |
| `POST` | `/api/user/cookies/delete` | 普通用户 + CSRF | 取消 Cookie 域名授权 |
| `GET` | `/api/shared-folders` | 普通用户 | 查看可见的团队空间文件夹 |
| `POST` | `/api/cookies` | 普通用户 + CSRF | 同步当前站点 Cookie |

文件接口限定在用户工作区：

```text
users/<username>/.picoclaw/workspace/
```

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/files?path=<dir>` | 列目录 |
| `POST` | `/api/files/upload` | 上传文件，最大 32 MB |
| `GET` | `/api/files/download?path=<file>` | 下载文件 |
| `POST` | `/api/files/delete` | 删除文件或目录 |
| `POST` | `/api/files/mkdir` | 创建目录 |
| `GET` | `/api/files/edit?path=<file>` | 读取可编辑文本文件 |
| `POST` | `/api/files/edit` | 保存可编辑文本文件 |

## MCP 接口

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/api/mcp/token` | 普通用户 | 获取 MCP token |
| `GET` | `/api/mcp/sse/:service` | MCP token | 建立 SSE 连接 |
| `POST` | `/api/mcp/sse/:service` | MCP token | 发送 JSON-RPC |
| `GET` | `/api/mcp/cookies` | MCP token | 读取 Cookie（容器内技能使用） |
| `POST` | `/api/mcp/cookies` | MCP token | 写入 Cookie |
| `GET` | `/api/browser/ws` | MCP token | 浏览器执行端 WebSocket |
| `GET` | `/api/computer/ws` | MCP token | 桌面执行端 WebSocket |

`:service` 支持：

- `browser`
- `computer`

MCP JSON-RPC 支持：

- `initialize`
- `notifications/initialized`
- `tools/list`
- `tools/call`

## 管理接口

以下接口要求超管会话。

### 全局配置

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/config` | 读取全局配置 |
| `POST` | `/api/config` | 保存全局配置 |
| `POST` | `/api/admin/config/apply` | 批量应用配置到用户 |
| `GET` | `/api/admin/task/status` | 查询任务队列状态 |
| `GET` | `/api/admin/migration-rules` | 查看 Picoclaw adapter 信息 |
| `POST` | `/api/admin/migration-rules/refresh` | 从远端刷新 adapter |
| `POST` | `/api/admin/migration-rules/upload` | 上传 adapter zip |
| `GET` | `/api/admin/picoclaw/channels` | 管理端查看可用渠道 |

### 用户和超管

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/users` | 用户和容器列表 |
| `POST` | `/api/admin/users/create` | 创建本地用户 |
| `POST` | `/api/admin/users/batch-create` | 批量导入（表单字段 usernames，逗号或换行分隔） |
| `POST` | `/api/admin/users/delete` | 删除用户 |
| `GET` | `/api/admin/superadmins` | 超管列表 |
| `POST` | `/api/admin/superadmins/create` | 创建超管并返回随机密码 |
| `POST` | `/api/admin/superadmins/delete` | 删除超管 |
| `POST` | `/api/admin/superadmins/reset` | 重置超管密码 |

统一认证模式下，不允许手动创建普通用户或组。

### 容器

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/admin/container/start` | 启动用户容器 |
| `POST` | `/api/admin/container/stop` | 停止用户容器 |
| `POST` | `/api/admin/container/restart` | 重启用户容器 |
| `POST` | `/api/admin/container/debug` | 容器调试操作 |
| `GET` | `/api/admin/container/logs` | 获取容器日志 |

### 镜像

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/images` | 本地镜像列表 |
| `POST` | `/api/admin/images/pull` | 拉取镜像，SSE 流式返回 |
| `POST` | `/api/admin/images/delete` | 删除镜像 |
| `POST` | `/api/admin/images/migrate` | 从旧镜像迁移到新镜像 |
| `POST` | `/api/admin/images/upgrade` | 拉取并升级指定用户 |
| `GET` | `/api/admin/images/registry` | 远端标签 |
| `GET` | `/api/admin/images/local-tags` | 本地标签 |
| `GET` | `/api/admin/images/upgrade-candidates` | 可升级用户和组 |
| `GET` | `/api/admin/images/users` | 镜像关联用户 |
| `GET` | `/api/admin/images/pull-status` | 镜像拉取状态查询 |

### 认证和白名单

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/admin/auth/test-ldap` | 测试 LDAP 连接和组查询 |
| `GET` | `/api/admin/auth/ldap-users` | 从 LDAP 拉取用户列表 |
| `GET` | `/api/admin/auth/providers` | 已注册的认证源列表 |
| `POST` | `/api/admin/auth/sync-groups` | 手动同步 LDAP 组 |
| `GET` | `/api/admin/whitelist` | 获取白名单 |
| `POST` | `/api/admin/whitelist` | 覆盖更新白名单 |

### 组和技能

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/groups` | 组列表 |
| `POST` | `/api/admin/groups/create` | 创建组 |
| `POST` | `/api/admin/groups/delete` | 删除组 |
| `GET` | `/api/admin/groups/members` | 查看组成员和绑定技能 |
| `POST` | `/api/admin/groups/members/add` | 添加组成员 |
| `POST` | `/api/admin/groups/members/remove` | 移除组成员 |
| `POST` | `/api/admin/groups/skills/bind` | 绑定并立即部署技能 |
| `POST` | `/api/admin/groups/skills/unbind` | 解绑技能 |
| `GET` | `/api/admin/skills` | 技能列表 |
| `POST` | `/api/admin/skills/deploy` | 部署技能到用户或组 |
| `POST` | `/api/admin/skills/remove` | 删除技能 |
| `POST` | `/api/admin/skills/upload` | 上传技能 zip |
| `POST` | `/api/admin/skills/user/bind` | 绑定技能到单个用户 |
| `POST` | `/api/admin/skills/user/unbind` | 解绑用户技能 |
| `GET` | `/api/admin/skills/user/sources` | 用户技能来源列表 |
| `GET` | `/api/admin/skills/sources` | 技能仓库来源列表 |
| `POST` | `/api/admin/skills/sources/git` | 添加 Git 技能仓库 |
| `POST` | `/api/admin/skills/sources/remove` | 移除技能仓库 |
| `POST` | `/api/admin/skills/sources/pull` | 拉取技能仓库更新 |
| `POST` | `/api/admin/skills/sources/refresh` | 刷新技能仓库 |
| `GET` | `/api/admin/skills/registry/list` | 技能注册中心列表 |
| `POST` | `/api/admin/skills/registry/install` | 从注册中心安装技能 |
| `GET` | `/api/admin/skills/defaults` | 默认技能列表 |
| `POST` | `/api/admin/skills/defaults/toggle` | 切换技能默认安装 |

### 团队空间

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/shared-folders` | 共享文件夹列表 |
| `POST` | `/api/admin/shared-folders/create` | 创建共享文件夹 |
| `POST` | `/api/admin/shared-folders/update` | 更新共享文件夹 |
| `POST` | `/api/admin/shared-folders/delete` | 删除共享文件夹 |
| `POST` | `/api/admin/shared-folders/groups/set` | 设置文件夹的组可见范围 |
| `POST` | `/api/admin/shared-folders/test` | 测试文件夹挂载 |
| `POST` | `/api/admin/shared-folders/mount` | 手动挂载文件夹 |

### TLS 证书管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/tls/status` | TLS 证书状态 |
| `POST` | `/api/admin/tls/upload` | 上传 TLS 证书 |

### 其他

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/skill-install-policy` | 技能安装策略 |
| `POST` | `/api/admin/skill-install-policy` | 设置技能安装策略 |

## 响应格式

大部分普通接口返回：

```json
{
  "success": true,
  "message": "..."
}
```

错误格式：

```json
{
  "success": false,
  "error": "..."
}
```

少数接口返回资源型 JSON，比如文件列表、任务状态、MCP JSON-RPC 响应或 SSE 流。
