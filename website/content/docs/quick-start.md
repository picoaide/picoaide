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
| 权限       | root 用户                                 |
| 网络端口   | 80 端口可用（可配置）                     |
| 磁盘空间   | 至少 10GB 可用空间（用于镜像和用户数据）  |

## 下载安装

从 GitHub Releases 下载最新版本的 PicoAide 二进制文件：

```bash
# 下载最新版本（Linux amd64）
curl -L -o /usr/sbin/picoaide https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64

# 添加执行权限
chmod +x /usr/sbin/picoaide
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

## 初始化

运行 `picoaide init` 进入交互式首次运行引导：

```bash
picoaide init
```

引导过程包含 4 个步骤：

### 步骤 1/4：数据目录

```
--- 步骤 1/4: 数据目录 ---
请输入数据目录 (默认: /data/picoaide):
```

默认使用 `/data/picoaide`，该目录将存放用户数据、数据库和配置。

### 步骤 2/4：超管账户

```
--- 步骤 2/4: 超管账户 ---
管理员用户名 (默认: admin):
密码:
确认密码:
```

创建超级管理员账户，密码至少 6 位。此账户可以在管理面板中进行所有操作。

### 步骤 3/4：监听地址

```
--- 步骤 3/4: 监听地址 ---
监听地址 (默认: :80):
```

默认监听 80 端口，可根据需要修改。

### 步骤 4/4：镜像仓库

```
--- 步骤 4/4: 镜像仓库 ---
  1) GitHub (ghcr.io)
  2) 腾讯云 (hkccr.ccs.tencentyun.com)
请选择 [1]:
是否立即拉取最新镜像? [Y/n]:
```

- **GitHub**：从 `ghcr.io` 拉取，适合海外服务器
- **腾讯云**：从腾讯云镜像拉取，适合国内服务器，速度更快

初始化会自动安装 systemd 服务并启动，默认使用**本地认证模式**。

## 服务管理

初始化完成后，PicoAide 已注册为 systemd 服务并自动启动：

```bash
# 查看服务状态
systemctl status picoaide

# 停止服务
systemctl stop picoaide

# 重启服务
systemctl restart picoaide

# 查看日志
journalctl -u picoaide -f
```

服务文件位于 `/etc/systemd/system/picoaide.service`，内容大致如下：

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

[Install]
WantedBy=multi-user.target
```

## 其他命令

### 初始化指定用户

```bash
picoaide init -user zhangsan
```

用于为已存在的用户重新初始化容器目录。

### 重置密码

```bash
picoaide reset-password <username>
```

重置本地用户的密码（LDAP 用户不支持此操作）。

## 安装浏览器插件

1. 从 Chrome Web Store 安装 PicoAide 浏览器扩展
2. 点击扩展图标，输入服务器地址（如 `http://10.0.0.1`）
3. 使用超管账户登录
4. 进入管理面板完成后续配置（认证模式、用户管理、镜像拉取等）

## 下一步

- 阅读 [架构概览](/docs/architecture/) 了解系统设计
- 配置 [浏览器扩展](/docs/browser-extension/) 让 AI 控制浏览器
- 了解 [技能系统](/docs/skills/) 扩展 AI 能力
- 参考 [配置参考](/docs/configuration/) 进行高级配置
