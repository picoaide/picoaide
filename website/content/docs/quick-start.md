---
title: "快速开始"
description: "PicoAide 快速入门指南"
weight: 1
draft: false
---

本指南按当前代码实现说明 PicoAide 的安装、初始化和最小可用配置。

## 环境要求

服务端运行在 Linux 上，依赖 Docker 和 root 权限。桌面客户端支持 Windows、macOS 和 Linux，但那部分不在服务端初始化范围内。

| 项目 | 要求 |
| --- | --- |
| 操作系统 | Linux |
| Docker | Docker Engine 已安装并可用 |
| 权限 | root |
| 端口 | 默认 `:80`，可在配置中修改 |
| 存储 | 用于数据库、用户目录和镜像缓存的持久化磁盘 |

## 下载安装

推荐从 GitHub Releases 下载服务端二进制文件。当前代码里的安装向导默认就是这一条路径。

```bash
curl -L -o picoaide \
  https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64
chmod +x picoaide
sudo mv picoaide /usr/sbin/picoaide
```

如果你在仓库内构建：

```bash
git clone https://github.com/picoaide/picoaide.git
cd picoaide
go build -o picoaide ./cmd/picoaide/
GOOS=linux GOARCH=amd64 go build -o picoaide ./cmd/picoaide/
```

## 初始化

运行初始化命令后，程序会创建数据目录、写入数据库默认值、生成会话密钥，并准备 systemd 服务。

```bash
picoaide init
```

初始化向导当前包含四步：

- 数据目录
- 超管账户
- 监听地址
- 镜像仓库

### 数据目录

默认值是 `/data/picoaide`。该目录会保存：

- `picoaide.db`
- `users/`
- `archive/`

### 超管账户

初始化会要求设置本地超级管理员账户。这个账户不依赖 LDAP，也不需要用户容器。

### 监听地址

默认是 `:80`。如果你启用 TLS 并把监听地址设为 `:443`，代码会额外起一个 `:80` 的跳转入口。

### 镜像仓库

初始化时可以选择：

- `github`：拉取 `ghcr.io/picoaide/picoaide:<tag>`
- `tencent`：拉取 `hkccr.ccs.tencentyun.com/picoaide/picoaide:<tag>`

初始化默认使用本地认证模式。LDAP 是否启用取决于配置中的 `web.auth_mode` 和 `web.ldap_enabled`。

## 服务管理

初始化后可以用 systemd 管理服务：

```bash
systemctl status picoaide
systemctl stop picoaide
systemctl restart picoaide
journalctl -u picoaide -f
```

服务启动命令由代码写入当前工作目录和监听地址，形式类似：

```ini
[Unit]
Description=PicoAide Management API Server
After=network.target docker.service

[Service]
Type=simple
ExecStart=/usr/sbin/picoaide serve -listen :80
WorkingDirectory=/data/picoaide
Restart=always
RestartSec=5
```

## 其他命令

### 初始化指定用户

```bash
picoaide init -user zhangsan
```

用于为已有用户重新准备容器目录和工作区。

### 重置密码

```bash
picoaide reset-password <username>
```

只对本地用户有效。LDAP 用户的密码不由 PicoAide 管理。

## 安装浏览器插件

1. 安装 `picoaide-extension`
2. 打开扩展，输入服务器地址和普通用户账号
3. 登录后获取 MCP token
4. 点击「授权 AI 控制当前标签页」后，浏览器 WebSocket 执行端才会连接到服务端

超管不能用于浏览器插件登录。代码里明确禁止超管登录扩展。

## 下一步

- 阅读 [架构概览](/docs/architecture/) 了解系统设计
- 配置 [浏览器扩展](/docs/browser-extension/)
- 了解 [技能系统](/docs/skills/)
- 参考 [配置参考](/docs/configuration/)
