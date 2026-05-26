---
title: "故障排查"
description: "PicoAide 常见问题的诊断步骤和解决方案 — 容器、认证、代理、配置和客户端问题"
weight: 8
draft: false
---

本文档按问题类型分类，提供诊断思路和解决方案。如果以下方法无法解决问题，请到 [GitHub Issues](https://github.com/picoaide/picoaide/issues) 提交反馈。

## 服务启动问题

### 服务无法启动

**现象**：`systemctl start picoaide` 后服务启动失败。

**排查步骤**：

```bash
# 查看服务状态和错误信息
systemctl status picoaide

# 查看最近日志
journalctl -u picoaide -n 50 --no-pager

# 检查是否有 root 权限
whoami
```

**常见原因**：
- 未使用 root 用户运行
- 端口 80/443 被占用
- 工作目录 `/data/picoaide` 权限不正确
- SQLite 数据库文件损坏（系统会自动备份为 `picoaide.db.broken.<timestamp>`）

### 网桥创建失败

**现象**：日志显示无法创建 `picoaide-br` 网桥。

**排查步骤**：

```bash
# 检查是否已存在
ip link show picoaide-br

# 检查 iptables 规则
iptables -L FORWARD -n

# 检查内核模块
lsmod | grep br_netfilter
```

**解决方案**：
- 安装 bridge 和 netfilter 内核模块
- 确保系统没有其他进程占用了 100.64.0.0/16 网段

## 认证问题

### LDAP 连接失败

**现象**：配置 LDAP 后无法登录。

**排查步骤**：

1. 在管理后台的认证配置页面点击「测试连接」，检查 Bind 是否成功
2. 测试用户搜索：输入一个已知用户名，检查是否能找到用户
3. 如果使用了 SSL/TLS，确认端口正确（LDAPS 默认 636）
4. 检查 LDAP 服务器防火墙是否允许连接

### LDAP 白名单拒登

**现象**：LDAP 认证成功但显示"未在白名单中"。

**解决方案**：
- 在管理后台的白名单页面添加该用户名
- 或关闭白名单功能（设置 `ldap.whitelist_enabled = false`）

### 超管无法登录

**排查步骤**：
1. 确认使用的用户名正确
2. 如果忘记密码，使用 `picoaide reset-password <username>` 重置
3. 确认系统至少有一个超管（查看数据库 `local_users` 表）

### CSRF Token 过期

**现象**：操作时提示 CSRF 验证失败。

**解决方案**：刷新页面重新获取 CSRF token，然后重试操作。

## 代理连接问题

### Browser 代理未连接

**现象**：AI 调用 browser 工具时返回"代理未连接"。

**排查步骤**：

1. 确认浏览器扩展已用普通用户登录
2. 确认用户已点击扩展的「授权 AI 控制当前标签页」按钮
3. 确认扩展的 WebSocket 连接保持在线（不要关闭扩展弹窗）
4. 确认用户沙箱配置中包含 browser MCP server 的连接信息

常见原因：
- 扩展使用了超管账号登录（服务端会拒绝）
- 扩展与服务端网络不通
- 浏览器进入了省电模式

### Computer 代理未连接

**现象**：AI 调用 computer 工具时返回"代理未连接"。

**排查步骤**：
1. 确认桌面客户端已启动并登录
2. 确认客户端已通过 WebSocket 连接到服务端
3. 检查客户端日志是否有错误信息

### WebSocket 频繁断开

**可能原因**：
- 网络不稳定或有防火墙中断长连接
- 负载均衡器对 WebSocket 连接设置了超时
- 浏览器进入了休眠或省电模式

**解决方案**：
- 检查网络稳定性
- 如果通过反向代理，确认配置了 WebSocket 支持
- 保持扩展或客户端窗口活跃

## 配置问题

### 配置更新不生效

**排查步骤**：
1. 确认配置已保存到数据库（settings 表）
2. 某些配置修改后需要重启沙箱才能生效（如渠道配置）
3. 重启服务：`systemctl restart picoaide`

## 沙箱问题

### 沙箱启动失败

**现象**：用户沙箱无法启动。

**排查步骤**：

```bash
# 查看服务端日志
journalctl -u picoaide -n 50 --no-pager | grep -i error

# 检查 overlayfs 是否可用
mount | grep overlay
```

**常见原因**：
- overlayfs 内核模块未加载
- Alpine rootfs 文件不存在或损坏
- 磁盘空间不足
- 100.64.0.0/16 网段被占用

### 网络隔离失效

**现象**：容器间可以互相通信。

**排查**：

```bash
# 检查 iptables 规则
iptables -L FORWARD -n | grep picoaide

# 确保有 DROP 规则
iptables -I FORWARD -i picoaide-br -o picoaide-br -j DROP
```

正常情况下，所有通过 `picoaide-br` 网桥的容器间通信都会被 DROP。

## 扩展和客户端问题

### 扩展登录失败

**排查步骤**：
1. 确认使用普通用户账号（超管不能登录扩展）
2. 确认服务器地址填写正确（协议 + IP + 端口）
3. 如果使用 HTTPS，确认证书有效

### Cookie 同步无效

**说明**：Cookie 同步只把当前页面的登录态写入用户的 `.security.yml`，让 AI Agent 可以在配置中复用。它**不**等同于打开浏览器控制连接。如果需要 AI 操作浏览器页面，必须在扩展中完成授权流程。

### 桌面文件访问失败

**排查步骤**：
1. 在桌面客户端设置中配置文件白名单目录
2. AI 应先调用 `computer_whitelist` 获取可访问目录
3. 确认 AI 访问的文件路径在白名单范围内
4. 检查客户端侧的权限组设置是否允许文件操作

## 日志与调试

### 访问日志

PicoAide 使用结构化 JSON 日志，写入 `logs/picoaide.log`：

```json
{"time":"2026-05-12T10:30:00.123+08:00","level":"INFO","msg":"请求","method":"POST","path":"/api/login","status":200,"duration":"42ms"}
```

查看日志：

```bash
# 实时追踪
journalctl -u picoaide -f

# 查看最近 100 条
journalctl -u picoaide -n 100

# 查看指定时间的日志
journalctl -u picoaide --since "2026-05-12 10:00" --until "2026-05-12 11:00"
```

### 调试端点

```text
GET /api/health
```

返回服务端版本号和运行状态，用于基本健康检查。
