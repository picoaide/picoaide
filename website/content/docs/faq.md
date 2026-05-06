---
title: "常见问题"
description: "PicoAide 使用中的常见问题与解答"
weight: 8
draft: false
---

## 安装与部署

### Q: PicoAide 支持哪些操作系统？

PicoAide 服务端仅支持 Linux 系统（推荐 Ubuntu 20.04+ 或 CentOS 7+）。服务器需要安装 Docker Engine 20.10+ 并以 root 权限运行。桌面客户端支持 Linux、macOS 和 Windows。

### Q: 如何安装 PicoAide？

```bash
# 下载二进制文件
curl -L -o /usr/sbin/picoaide https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64
chmod +x /usr/sbin/picoaide

# 运行初始化引导（自动安装 systemd 服务）
picoaide init
```

详细步骤请参考 [快速开始](/docs/quick-start/)。

### Q: 可以在没有 LDAP 的环境中使用 PicoAide 吗？

可以。`picoaide init` 默认使用本地认证模式，无需连接 LDAP。管理员通过管理面板手动创建用户。后续可以在「认证配置」页面切换为 LDAP 模式。

### Q: 如何配置 HTTPS？

在管理面板的配置页面中设置 TLS 证书，或通过 API 更新：

```json
{
  "web": {
    "tls": {
      "enabled": true,
      "cert_file": "/path/to/cert.pem",
      "key_file": "/path/to/key.pem"
    }
  }
}
```

配置完成后重启服务即可。也可以使用 Nginx 等反向代理来终止 TLS。

### Q: 数据存储在哪里？

默认数据目录为 `/data/picoaide`，可在 `picoaide init` 时自定义。包含：

- `picoaide.db` — 数据库（配置、用户、容器状态）
- `users/` — 用户容器数据
- `archive/` — 归档数据

## 容器管理

### Q: 容器之间可以互相通信吗？

不可以。PicoAide 使用 `picoaide-net` 网络，ICC（容器间通信）设置为 `false`，确保不同用户的容器完全隔离，保障数据安全。

### Q: 如何更新容器镜像？

通过浏览器插件的管理面板操作：

1. 进入「镜像管理」页面
2. 查看远程标签，拉取新版本镜像
3. 选择需要升级的用户或分组，点击「升级」
4. 系统会通过队列逐步重启用户容器

也可以通过 API 操作：

```bash
# 拉取最新镜像
curl -X POST http://localhost/api/admin/images/pull \
  -d '{"tag": "latest"}'

# 升级用户容器
curl -X POST http://localhost/api/admin/images/upgrade \
  -d '{"username": "zhangsan", "tag": "latest"}'
```

### Q: 容器数据存储在哪里？

每个用户的容器数据存储在 `/data/picoaide/users/<用户名>/` 目录下，直接挂载为容器内的 `/root`。即使容器被删除，用户数据仍然保留在宿主机上。

## 网络与连接

### Q: 容器 IP 地址是如何分配的？

PicoAide 使用 `100.64.0.0/16` CGNAT 地址空间的静态 IP 分配，从 `100.64.0.2` 开始递增。每个容器获得唯一的静态 IP，网关为 `100.64.0.1`。

### Q: 浏览器扩展无法连接服务器怎么办？

1. 确认 PicoAide 服务正在运行（`systemctl status picoaide`）
2. 检查服务器地址和端口是否正确
3. 确认网络连通性（防火墙是否放行对应端口）
4. 检查浏览器控制台是否有 CORS 相关错误

### Q: MCP Token 是什么？如何获取？

MCP Token 用于 AI 容器与浏览器扩展/桌面客户端之间的认证。格式为 `用户名:随机hex`。用户登录后通过 `GET /api/mcp/token` 获取。Token 首次请求时自动生成。

## 安全

### Q: 会话安全机制是什么？

PicoAide 使用 HMAC 签名的 Cookie 进行会话管理（非 JWT），有效期 24 小时。CSRF 令牌使用按小时滚动的时间窗口。所有 POST 请求需要验证 CSRF Token。

### Q: 如何限制哪些用户可以使用 PicoAide？

在管理面板的「认证配置」页面中启用白名单功能，只有白名单中的用户才能获得容器。白名单为空时，所有通过认证的用户均可使用。

### Q: API 密钥存储在哪里？

模型 API 密钥存储在用户级的安全配置文件 `.security.yml` 中，文件权限为 0600。全局配置中的 `security.model_list` 定义了各模型对应的 API 密钥列表。

## 性能与扩展

### Q: 一台服务器可以支持多少用户？

取决于硬件配置。每个用户容器的资源使用量（CPU、内存）可通过 `cpu_limit` 和 `memory_limit` 限制。建议根据实际负载进行容量规划。

### Q: 如何选择镜像仓库源？

- **github**（默认）：从 `ghcr.io` 拉取，适合海外服务器
- **tencent**：从腾讯云镜像拉取，适合国内服务器，速度更快

镜像仓库在 `picoaide init` 时选择，后续可在管理面板中切换。
