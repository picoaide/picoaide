---
title: "常见问题"
description: "PicoAide 安装、认证、沙箱、MCP 和客户端常见问题汇总"
weight: 14
draft: false
---

## 安装与部署

### 服务端必须运行在 Linux 吗？

是。服务端需要 Linux 内核能力（overlayfs、network namespace、bridge、iptables）来管理沙箱容器。桌面客户端可以运行在 Windows、macOS 和 Linux。

### 需要 Docker 吗？

不需要。PicoAide 使用原生 Linux 内核能力隔离沙箱，不依赖 Docker。底层通过 overlayfs + network namespace + iptables 实现容器级隔离。

### 初始化后访问哪里？

默认访问服务端根路径会跳转到 `/login`。登录后：
- 超管进入 `/admin/dashboard`
- 普通用户进入 `/user/welcome`

### 数据存在哪里？

默认是 `/data/picoaide`。主要文件：
- `picoaide.db` — SQLite 数据库
- `users/` — 用户工作目录
- `archive/` — 删除用户时的归档

## 认证

### 没有 LDAP 可以用吗？

可以。使用 `local` 模式即可。普通用户和超管都由本地数据库管理。

### LDAP 模式下本地超管还能登录吗？

可以。本地超管账号仍然可以登录管理后台。超管优先走本地认证，即使在 LDAP/OIDC 模式下也不受影响。

### 为什么超管不能登录浏览器扩展？

浏览器扩展是普通用户的执行端。服务端明确拒绝超管登录扩展，避免管理身份被当成普通用户工作身份使用。

### 修改密码为什么失败？

统一认证模式下（LDAP/OIDC）普通用户不能在 PicoAide 修改密码，应去公司认证中心修改。本地模式下才支持修改密码。

## 沙箱与网络

### 沙箱之间能互相访问吗？

不能。`picoaide-br` 网桥设置了 iptables DROP 规则，所有容器间通信都会被阻止。

### 沙箱 IP 怎么分配？

使用 `100.64.0.0/16` CGNAT 地址段，从 `100.64.0.2` 开始顺序分配。`100.64.0.1` 是宿主机侧网关。同一用户的 IP 不会变（数据库持久化）。

### 删除容器会删除用户文件吗？

不会。容器和用户目录是分离的。用户工作区保存在宿主机 `users/<username>/` 下，容器删除不等于删除用户数据。

## MCP 和客户端

### "代理未连接"是什么原因？

浏览器扩展或桌面客户端没有建立 WebSocket 连接。确认：
1. 扩展/客户端已用普通用户登录
2. 扩展已点击授权按钮/客户端已保持运行
3. 浏览器没有关闭或进入休眠

### Cookie 同步后为什么还不能控制浏览器？

Cookie 同步只写入登录态到配置文件。浏览器控制必须通过用户授权的 WebSocket 执行端。两者是独立的。

### 桌面客户端文件访问为什么失败？

桌面文件操作受白名单限制。先调用 `computer_whitelist`，再访问返回目录内的文件。同时确认该权限组已开启。

### MCP token 从哪里来？

普通用户登录后调用 `GET /api/mcp/token`。首次请求会自动生成并持久化。

### 超管为什么不能获取 MCP Token？

超管身份是管理身份，不应该用于工具调用场景。浏览器扩展和桌面客户端也明确拒绝超管登录。

## 技能

### 技能实际部署到哪里？

部署到 `users/<username>/skills/<skill-name>` 目录，在沙箱中以只读方式挂载。

### 解绑组技能会删除用户工作区里的技能吗？

不会。解绑只删除组和技能的绑定关系，不会自动删除已经部署到用户工作区的目录。如果需要清理，需要手动删除。

### 私有 Git 仓库怎么配置？

仓库配置里 `public=false` 时必须提供 `credentials`。HTTPS token 会按 provider 设置默认用户名，例如 GitLab 默认 `oauth2`。

## 配置与升级

### 系统升级后需要手动迁移数据库吗？

不需要。升级到新版本后首次启动会自动执行数据库迁移。迁移系统在 `syncSchema()` 末尾自动运行，所有迁移都是幂等的。

### 升级过程中会影响运行中的用户吗？

升级需要重启服务（`systemctl restart picoaide`），所有运行中的沙箱容器会停止。升级完成后用户需要重新登录并启动沙箱。

### 如何备份整个系统？

```bash
systemctl stop picoaide
tar czf picoaide-backup-$(date +%Y%m%d).tar.gz -C /data picoaide/
```
