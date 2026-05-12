---
title: "故障排查"
description: "PicoAide 常见问题的诊断步骤和解决方案"
weight: 7
draft: false
---

本文档按问题类型分类，提供诊断思路和解决方案。如果以下方法无法解决问题，请到 [GitHub Issues](https://github.com/picoaide/picoaide/issues) 提交反馈。

## 容器问题

### 容器启动失败

**现象**：用户容器无法启动，管理后台显示容器状态异常。

**排查步骤**：

1. 检查 Docker 是否正常运行：
   ```bash
   docker info
   systemctl status docker
   ```

2. 检查 picoaide-net 网络是否存在：
   ```bash
   docker network ls | grep picoaide-net
   ```
   如果不存在，重启 picoaide 服务会自动创建。

3. 查看具体错误信息：
   ```bash
   journalctl -u picoaide -n 50 --no-pager | grep -i error
   ```

**常见原因**：
- Docker 未安装或未运行
- 镜像未拉取（检查 `docker images` 是否有对应镜像）
- 网络 `picoaide-net` 被误删除
- 磁盘空间不足

### Docker 不可用

**现象**：管理后台显示"服务不可用"。

**说明**：PicoAide 在 Docker 不可用时仍可启动，管理后台和 API 可以正常使用，但所有容器、镜像相关操作不可用。

**解决方案**：安装并启动 Docker，然后重启 picoaide。

### 容器 IP 冲突

**现象**：容器创建失败或启动后无法访问网络。

**原因**：`picoaide-net` 使用 `100.64.0.0/16` 地址段。如果这个网段与公司内网或其他 Docker 网络冲突，会导致 IP 分配失败。

**解决方案**：`picoaide-net` 使用 CGNAT 地址空间，通常不会与内网冲突。如果确实冲突，需要调整网络规划。

### 删除用户后容器未清理

**现象**：删除用户后 Docker 中仍存在该用户的容器。

**排查步骤**：
```bash
docker ps -a | grep picoaide-
docker rm -f picoaide-<username>
```

正常情况下删除用户时会自动停止并删除容器。如果手动删除数据库记录可能导致残留，需要手动清理。

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

**现象**：超管账户登录被拒绝。

**排查步骤**：
1. 确认使用的用户名正确
2. 如果忘记密码，使用 `picoaide reset-password <username>` 重置
3. 确认系统至少有一个超管（查看数据库 `local_users` 表中 role 为 `superadmin` 的记录）

### CSRF Token 过期

**现象**：操作时提示 CSRF 验证失败。

**解决方案**：刷新页面重新获取 CSRF token，然后重试操作。

## 代理连接问题

### Browser 代理未连接

**现象**：AI 调用 browser 工具时返回 `picoaide-browser 代理未连接`。

**排查步骤**：
1. 确认浏览器扩展已用普通用户登录
2. 确认用户已点击扩展的「授权 AI 控制当前标签页」按钮
3. 确认扩展的 WebSocket 连接保持在线（不要关闭扩展弹窗）
4. 确认用户容器配置中包含 browser MCP server：
   ```json
   {
     "tools": {
       "mcp": {
         "servers": {
           "browser": {
             "enabled": true,
             "type": "sse",
             "url": "http://100.64.0.1:80/api/mcp/sse/browser?token=<mcp-token>"
           }
         }
       }
     }
   }
   ```

### Computer 代理未连接

**现象**：AI 调用 computer 工具时返回 `picoaide-computer 代理未连接`。

**排查步骤**：
1. 确认桌面客户端已启动并登录
2. 确认客户端已经通过 WebSocket 连接到服务端
3. 检查客户端关闭或网络断开会导致连接中断

### WebSocket 频繁断开

**可能原因**：
- 网络不稳定或有防火墙中断长连接
- 负载均衡器对 WebSocket 连接设置了超时
- 浏览器进入了休眠或省电模式

**解决方案**：
- 检查网络稳定性
- 如果通过反向代理，确认配置了 WebSocket 支持
- 教育用户保持扩展或客户端窗口活跃

## 配置问题

### 配置更新不生效

**排查步骤**：
1. 确认配置已保存到数据库（settings 表）
2. 某些配置修改后需要重启容器才能生效（如渠道配置）
3. 重启服务：`systemctl restart picoaide`

### Picoclaw Adapter 迁移失败

**现象**：升级镜像或应用配置时提示配置版本不兼容。

**原因**：当前配置版本到目标版本的迁移链不存在或不完整。

**解决方案**：
1. 刷新 Adapter 包到最新版本
2. 如果需要中间版本迁移，先升级到中间版本再升级到目标版本
3. 检查 Adapter 的 `migrations/` 目录是否包含所需的迁移规则

### Adapter ZIP 上传失败

**现象**：上传 Adapter zip 包时提示格式错误。

**解决方案**：zip 内应直接包含以下文件，不要包含 `picoclaw/` 顶层目录：

```text
index.json
hash
schemas/config-v1.json
ui/ui-v1.json
migrations/v1-to-v2.json
```

## 扩展和客户端问题

### 扩展登录失败

**现象**：浏览器扩展输入账号密码后登录失败。

**排查步骤**：
1. 确认使用普通用户账号（超管不能登录扩展）
2. 确认服务器地址填写正确（协议 + IP + 端口）
3. 如果使用 HTTPS，确认证书有效

### Cookie 同步无效

**现象**：Cookie 同步成功，但 AI 仍无法访问目标网站。

**说明**：Cookie 同步只把当前页面的登录态写入用户的 `.security.yml`，让 AI Agent 可以在配置中复用。它**不**等同于打开浏览器控制连接。如果需要 AI 操作浏览器页面，必须启用 browser MCP 连接。

### 桌面文件访问失败

**现象**：AI 调用 `computer_file_read` 或 `computer_file_write` 时失败。

**排查步骤**：
1. 先在管理后台配置桌面客户端的文件白名单目录
2. AI 应先调用 `computer_whitelist` 获取可访问目录
3. 确认 AI 访问的文件路径在白名单范围内
4. 检查客户端侧的权限设置是否允许文件操作

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

### 容器日志

查看特定用户的容器日志：
```bash
docker logs picoaide-<username>
docker logs -f picoaide-<username>  # 实时追踪
```

### 调试端点

```text
GET /api/health
```

返回服务端版本号和运行状态，用于基本健康检查。
