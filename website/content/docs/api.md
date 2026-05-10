---
title: "API 参考"
description: "PicoAide HTTP API 接口文档"
weight: 7
draft: false
---

PicoAide 提供完整的 RESTful JSON API，所有端点返回 JSON 格式数据。API 专为浏览器扩展和桌面客户端集成而设计。

## 通用说明

### 认证方式

除登录接口外，所有 API 请求需携带会话 Cookie：

```
Cookie: picoaide-session=<session-value>
```

MCP 相关接口使用 Bearer Token 或 Query Parameter 认证：

```
# Bearer Token 方式
Authorization: Bearer <mcp-token>

# Query Parameter 方式
?token=<mcp-token>
```

### CSRF 保护

所有 POST 请求需要携带 CSRF Token：

```
X-CSRF-Token: <csrf-token>
```

通过 `GET /api/csrf` 获取 CSRF Token。

### 响应格式

所有接口返回统一的 JSON 格式：

```json
{
  "success": true,
  "message": "操作成功",
  "data": {}
}
```

错误响应：

```json
{
  "success": false,
  "error": "错误描述"
}
```

## 认证接口

| 方法   | 端点                  | 说明           |
| ------ | --------------------- | -------------- |
| POST   | `/api/login`          | 用户登录       |
| POST   | `/api/logout`         | 用户登出       |
| GET    | `/api/user/info`      | 获取当前用户信息 |
| POST   | `/api/user/password`  | 修改密码（仅本地模式） |
| GET    | `/api/csrf`           | 获取 CSRF Token |

## MCP SSE 接口

| 方法   | 端点                        | 说明                          |
| ------ | --------------------------- | ----------------------------- |
| GET    | `/api/mcp/token`            | 获取当前用户的 MCP Token      |
| GET    | `/api/mcp/sse/{service}`    | 建立 MCP SSE 连接             |
| POST   | `/api/mcp/sse/{service}`    | 发送 MCP JSON-RPC 消息        |
| GET    | `/api/browser/ws`           | 浏览器扩展 WebSocket 连接     |
| GET    | `/api/computer/ws`          | 桌面客户端 WebSocket 连接     |

MCP SSE 服务支持 `{service}` 值为 `browser` 或 `computer`。

浏览器控制只能通过 `browser` MCP 服务调用 `browser_*` 工具完成。`/api/browser/ws` 是浏览器扩展连接 PicoAide Server 的执行端 WebSocket，不是 AI 容器直接调用的接口。AI 容器应使用下发到 `config.json` 的 `tools.mcp.servers.browser.url`。

## 用户接口

| 方法   | 端点                  | 说明           |
| ------ | --------------------- | -------------- |
| GET    | `/api/config`         | 读取全局配置   |
| POST   | `/api/config`         | 保存全局配置   |
| GET    | `/api/dingtalk`       | 读取钉钉配置   |
| POST   | `/api/dingtalk`       | 保存钉钉配置并重启容器 |
| GET    | `/api/cookies`        | 获取 Cookie 数据 |

## 文件管理接口

| 方法   | 端点                    | 说明               |
| ------ | ----------------------- | ------------------ |
| GET    | `/api/files`            | 列出文件（JSON）   |
| POST   | `/api/files/upload`     | 上传文件           |
| GET    | `/api/files/download`   | 下载文件           |
| POST   | `/api/files/delete`     | 删除文件或目录     |
| POST   | `/api/files/mkdir`      | 创建目录           |
| GET    | `/api/files/edit`       | 读取文件内容       |
| POST   | `/api/files/edit`       | 保存文件内容       |

## 管理接口

以下接口需要管理员权限。

### 用户管理

| 方法   | 端点                            | 说明         |
| ------ | ------------------------------- | ------------ |
| GET    | `/api/admin/users`              | 用户列表     |
| POST   | `/api/admin/users/create`       | 创建用户     |
| POST   | `/api/admin/users/delete`       | 删除用户     |

### 超级管理员

| 方法   | 端点                                | 说明               |
| ------ | ----------------------------------- | ------------------ |
| GET    | `/api/admin/superadmins`            | 超管列表           |
| POST   | `/api/admin/superadmins/create`     | 创建超管           |
| POST   | `/api/admin/superadmins/delete`     | 删除超管           |
| POST   | `/api/admin/superadmins/reset`      | 重置超管密码       |

### 容器管理

| 方法   | 端点                              | 说明         |
| ------ | --------------------------------- | ------------ |
| POST   | `/api/admin/container/start`      | 启动容器     |
| POST   | `/api/admin/container/stop`       | 停止容器     |
| POST   | `/api/admin/container/restart`    | 重启容器     |
| GET    | `/api/admin/container/logs`       | 获取容器日志 |

### 镜像管理

| 方法   | 端点                                  | 说明                       |
| ------ | ------------------------------------- | -------------------------- |
| GET    | `/api/admin/images`                   | 本地镜像列表               |
| POST   | `/api/admin/images/pull`              | 拉取镜像（SSE 流式）       |
| POST   | `/api/admin/images/delete`            | 删除镜像                   |
| POST   | `/api/admin/images/migrate`           | 迁移镜像                   |
| POST   | `/api/admin/images/upgrade`           | 升级镜像                   |
| GET    | `/api/admin/images/registry`          | 远程仓库标签列表           |
| GET    | `/api/admin/images/local-tags`        | 本地镜像标签列表           |
| GET    | `/api/admin/images/upgrade-candidates`| 可升级镜像列表             |
| GET    | `/api/admin/images/users`             | 镜像关联用户列表           |

### 组管理

| 方法   | 端点                                    | 说明           |
| ------ | --------------------------------------- | -------------- |
| GET    | `/api/admin/groups`                     | 组列表         |
| POST   | `/api/admin/groups/create`              | 创建组         |
| POST   | `/api/admin/groups/delete`              | 删除组         |
| GET    | `/api/admin/groups/members`             | 组成员列表     |
| POST   | `/api/admin/groups/members/add`         | 添加组成员     |
| POST   | `/api/admin/groups/members/remove`      | 移除组成员     |
| POST   | `/api/admin/groups/skills/bind`         | 绑定技能到组   |
| POST   | `/api/admin/groups/skills/unbind`       | 解绑组技能     |

### 技能管理

| 方法   | 端点                                    | 说明               |
| ------ | --------------------------------------- | ------------------ |
| GET    | `/api/admin/skills`                     | 技能列表           |
| POST   | `/api/admin/skills/deploy`              | 部署技能           |
| GET    | `/api/admin/skills/download`            | 下载技能           |
| POST   | `/api/admin/skills/remove`              | 删除技能           |
| POST   | `/api/admin/skills/install`             | 安装技能           |
| GET    | `/api/admin/skills/repos/list`          | 仓库列表           |
| POST   | `/api/admin/skills/repos/add`           | 添加仓库           |
| POST   | `/api/admin/skills/repos/pull`          | 拉取仓库           |
| POST   | `/api/admin/skills/repos/remove`        | 删除仓库           |

### 白名单管理

| 方法   | 端点                        | 说明         |
| ------ | --------------------------- | ------------ |
| GET    | `/api/admin/whitelist`      | 白名单列表   |
| POST   | `/api/admin/whitelist`      | 更新白名单   |

### 认证配置

| 方法   | 端点                                | 说明               |
| ------ | ----------------------------------- | ------------------ |
| POST   | `/api/admin/auth/test-ldap`         | 测试 LDAP 连接     |
| GET    | `/api/admin/auth/ldap-users`        | 获取 LDAP 用户列表 |
| POST   | `/api/admin/auth/sync-groups`       | 同步 LDAP 组       |

### 系统管理

| 方法   | 端点                            | 说明               |
| ------ | ------------------------------- | ------------------ |
| POST   | `/api/admin/config/apply`       | 应用配置并重启     |
| GET    | `/api/admin/task/status`        | 异步任务状态查询   |
