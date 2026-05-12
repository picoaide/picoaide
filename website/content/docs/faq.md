---
title: "常见问题"
description: "PicoAide 安装、认证、容器、MCP 和客户端常见问题"
weight: 13
draft: false
---

## 安装与部署

### 服务端必须运行在 Linux 吗？

是。服务端需要 Docker Engine，并通过 Docker SDK 管理用户容器。桌面客户端可以运行在 Windows、macOS 和 Linux。

### 初始化后访问哪里？

默认访问服务端根路径会跳转到 `/login`。登录后：

- 超管进入 `/admin/dashboard`
- 普通用户进入 `/manage`

### Docker 不可用时服务会启动吗？

会启动，但容器、镜像和升级等操作会返回 Docker 不可用。代码启动时会尝试初始化 Docker 客户端和 `picoaide-net`。

### 数据存在哪里？

默认是 `/data/picoaide`。主要文件：

- `picoaide.db`
- `users/`
- `archive/`

## 认证

### 没有 LDAP 可以用吗？

可以。使用 `local` 模式即可。普通用户和超管都由本地数据库管理。

### LDAP 模式下本地超管还能登录吗？

可以。本地超管账号仍然用于管理后台。

### 为什么超管不能登录浏览器扩展？

浏览器扩展是普通用户的执行端。服务端明确拒绝超管登录扩展，避免管理身份被当成普通用户工作身份使用。

### 修改密码为什么失败？

统一认证模式下普通用户不能在 PicoAide 修改密码，应去公司认证中心修改。本地模式下才支持 `/api/user/password`。

## 容器与网络

### 容器之间能互相访问吗？

默认不能。`picoaide-net` 设置了 `ICC=false`，用于阻止容器间直接通信。

### 容器 IP 怎么分配？

使用 `100.64.0.0/16`，从 `100.64.0.2` 开始分配。`100.64.0.1` 是主机侧网关。

### 删除容器会删除用户文件吗？

容器和用户目录是分离的。用户工作区保存在宿主机 `users/<username>/` 下，容器删除不等于删除用户数据。

## MCP 和客户端

### `picoaide-browser 代理未连接` 是什么原因？

浏览器扩展没有建立 WebSocket。确认：

1. 扩展用普通用户登录。
2. 用户点击了授权按钮。
3. 浏览器没有关闭。
4. AI 使用的是 `/api/mcp/sse/browser`，不是 `/api/browser/ws`。

### Cookie 同步后为什么还不能控制浏览器？

Cookie 同步只写入登录态。浏览器控制必须通过用户授权的 WebSocket 执行端。

### 桌面客户端文件访问为什么失败？

桌面文件操作受白名单限制。先调用 `computer_whitelist`，再访问返回目录内的文件。

### MCP token 从哪里来？

普通用户登录后调用：

```text
GET /api/mcp/token
```

首次请求会自动生成 token。

## 技能

### 技能实际部署到哪里？

部署到：

```text
users/<username>/.picoclaw/workspace/skills/<skill-name>
```

### 解绑组技能会删除用户工作区里的技能吗？

不会。解绑只删除组和技能的绑定关系，不会自动删除已经复制到用户工作区的目录。

### 私有 Git 仓库怎么配置？

仓库配置里 `public=false` 时必须提供 `credentials`。HTTPS token 会按 provider 设置默认用户名，例如 GitLab 默认 `oauth2`。

## 配置与升级

### 为什么升级镜像前会校验配置版本？

服务端使用 Picoclaw adapter 的 migration 规则判断旧版本到目标版本是否可升级，避免配置结构不兼容。

### 上传 Picoclaw adapter zip 有什么要求？

zip 里应直接包含 `index.json`、`hash`、`schemas/`、`ui/`、`migrations/` 等文件，不要再包一层 `picoclaw/` 顶层目录。
