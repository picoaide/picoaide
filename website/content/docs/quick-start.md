---
title: "快速开始"
description: "PicoAide 快速入门指南"
weight: 1
draft: false
---

本指南将帮助你在几分钟内完成 PicoAide 的安装和基础配置。

## 环境要求

在开始之前，请确保你的服务器满足以下条件：

| 要求       | 说明                                      |
| ---------- | ----------------------------------------- |
| 操作系统   | Linux（推荐 Ubuntu 20.04+ / CentOS 7+）  |
| Docker     | Docker Engine 20.10+ 已安装并运行        |
| 权限       | root 用户或拥有 Docker 权限的用户         |
| 网络端口   | 默认监听 80 端口（可配置）                |
| 磁盘空间   | 至少 10GB 可用空间（用于镜像和用户数据）  |

## 下载安装

从 GitHub Releases 下载最新版本的 PicoAide 二进制文件：

```bash
# 下载最新版本（Linux amd64）
curl -L -o picoaide https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64

# 添加执行权限
chmod +x picoaide

# 移动到系统路径
sudo mv picoaide /usr/local/bin/
```

也可以从源码构建：

```bash
# 克隆仓库
git clone https://github.com/picoaide/picoaide.git
cd picoaide

# 构建
go build -o picoaide ./cmd/picoaide/

# 交叉编译 Linux amd64
GOOS=linux GOARCH=amd64 go build -o picoaide ./cmd/picoaide/
```

## 初始化配置

首次运行时，PicoAide 会自动生成默认配置文件 `config.yaml`：

```bash
# 创建工作目录
sudo mkdir -p /opt/picoaide
cd /opt/picoaide

# 初始化（自动生成 config.yaml）
sudo picoaide serve -listen :80
```

首次启动后，编辑 `config.yaml` 配置 LDAP 连接和管理员密码：

```yaml
ldap:
  host: "ldap://ldap.example.com:389"
  bind_dn: "cn=admin,dc=example,dc=com"
  bind_password: "your-password"
  base_dn: "ou=users,dc=example,dc=com"
  filter: "(objectClass=inetOrgPerson)"
  username_attribute: "uid"

web:
  listen: ":80"
  password: "change-me-to-a-random-secret"
  auth_mode: "ldap"    # ldap | local
```

## 启动服务

```bash
# 前台运行
sudo picoaide serve -listen :80

# 后台运行（推荐使用 systemd）
sudo picoaide serve -listen :80 &
```

使用 systemd 管理（推荐生产环境）：

```ini
# /etc/systemd/system/picoaide.service
[Unit]
Description=PicoAide Service
After=docker.service
Requires=docker.service

[Service]
Type=simple
WorkingDirectory=/opt/picoaide
ExecStart=/usr/local/bin/picoaide serve -listen :80
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable picoaide
sudo systemctl start picoaide
```

## 登录系统

服务启动后，通过浏览器扩展或 API 登录：

```bash
# API 登录获取会话
curl -X POST http://localhost/api/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "your-password"}'
```

默认管理员账号使用 `config.yaml` 中 `web.password` 字段的密码登录。

## 创建第一个用户

登录后，通过 API 创建用户容器：

```bash
# 创建用户（需要管理员权限）
curl -X POST http://localhost/api/admin/users/create \
  -H "Content-Type: application/json" \
  -b "picoaide-session=your-session-cookie" \
  -d '{"username": "zhangsan"}'

# 启动用户容器
curl -X POST http://localhost/api/admin/container/start \
  -H "Content-Type: application/json" \
  -b "picoaide-session=your-session-cookie" \
  -d '{"username": "zhangsan"}'
```

## 拉取容器镜像

首次使用需要拉取 PicoClaw 容器镜像：

```bash
# 查看本地镜像
curl http://localhost/api/admin/images \
  -b "picoaide-session=your-session-cookie"

# 从远程仓库拉取最新镜像（SSE 流式返回进度）
curl -X POST http://localhost/api/admin/images/pull \
  -b "picoaide-session=your-session-cookie" \
  -H "Content-Type: application/json" \
  -d '{"tag": "latest"}'
```

## 配置白名单

白名单控制哪些用户可以获得容器。白名单为空时，所有 LDAP 用户均可使用：

```yaml
# whitelist.yaml
users:
  - zhangsan
  - lisi
  - wangwu
```

## 下一步

- 阅读 [架构概览](/docs/architecture/) 了解系统设计
- 配置 [浏览器扩展](/docs/browser-extension/) 让 AI 控制浏览器
- 了解 [技能系统](/docs/skills/) 扩展 AI 能力
- 参考 [配置手册](/docs/configuration/) 进行高级配置
