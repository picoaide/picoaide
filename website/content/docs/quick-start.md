---
title: "快速入门"
description: "PicoAide 快速入门指南 — 安装、初始化和基础验证"
weight: 1
draft: false
---

PicoAide 是一个企业级 AI 工作平台。它为每位员工分配独立的 AI 操作助手容器，通过浏览器扩展和桌面客户端让 AI 在授权范围内执行实际工作流，同时确保企业数据不出边界。

本指南带你从零开始完成安装、初始化和基础验证，约 10 分钟可完成。

## 环境要求

服务端必须运行在 Linux 上，需要 Docker Engine 和 root 权限。

| 项目 | 要求 |
|------|------|
| 操作系统 | Linux（x86_64） |
| Docker | Docker Engine 24+，已安装并运行 |
| 权限 | root（用于 Docker 通信） |
| 端口 | 默认 `:80`，可在配置中修改 |
| 磁盘 | 持久化磁盘，用于存储数据库、用户目录和镜像 |

桌面客户端可以运行在 Windows、macOS 和 Linux 上，不受此限制。

## 下载安装

### 方式一：GitHub Release 下载（推荐）

```bash
curl -L -o picoaide \
  https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64
chmod +x picoaide
sudo mv picoaide /usr/sbin/picoaide
```

### 方式二：源码编译

```bash
git clone https://github.com/picoaide/picoaide.git
cd picoaide
go build -o picoaide ./cmd/picoaide/
sudo mv picoaide /usr/sbin/picoaide
```

### 方式三：生产环境构建（注入版本信息）

```bash
GOOS=linux GOARCH=amd64 go build \
  -ldflags "-X github.com/picoaide/picoaide/internal/config.Version=1.0.0" \
  -o picoaide ./cmd/picoaide/
```

## 初始化

运行初始化命令后，程序会创建数据目录、写入数据库默认值、生成会话密钥，并准备 systemd 服务：

```bash
sudo picoaide init
```

初始化向导依次进行四步配置：

### 第一步：数据目录

默认值是 `/data/picoaide`。该目录会保存：

- `picoaide.db` — SQLite 数据库，存储用户、容器、配置和组信息
- `users/` — 每个用户的工作目录，包含 `config.json`、`.security.yml` 和 `workspace/`
- `archive/` — 删除用户时的归档目录

### 第二步：超管账户

设置本地超级管理员用户名和密码。超管账户不依赖外部认证源，即使 LDAP 不可用也能登录管理后台。请妥善保管密码，忘记密码后可通过 `picoaide reset-password <username>` 重置。

### 第三步：监听地址

默认是 `:80`。如果启用 TLS 并将监听地址设为 `:443`，服务端会自动额外启动 `:80` 入口用于 HTTP 跳转到 HTTPS。

### 第四步：镜像仓库

选择 PicoClaw 容器的镜像来源：

- `github` — 从 `ghcr.io/picoaide/picoaide:<tag>` 拉取
- `tencent` — 从 `hkccr.ccs.tencentyun.com/picoaide/picoaide:<tag>` 拉取

初始化完成后，系统会自动拉取选定仓库的最新镜像。

### 初始化后默认配置

初始化时默认使用本地认证模式（`web.auth_mode = local`）。如果需要 LDAP 或 OIDC 认证，初始化完成后通过管理后台或 API 切换。

## 服务管理

初始化成功后，PicoAide 会自动安装 systemd 服务。你可以使用以下命令管理：

```bash
# 查看服务状态
systemctl status picoaide

# 启动服务
systemctl start picoaide

# 停止服务
systemctl stop picoaide

# 重启服务
systemctl restart picoaide

# 查看实时日志
journalctl -u picoaide -f
```

服务默认以 `picoaide serve -listen :80` 启动，工作目录为 `/data/picoaide`。

## 验证安装

完成初始化和服务启动后，通过以下步骤验证部署是否正常：

1. 浏览器访问 `http://<服务器IP>/`，应自动跳转到登录页面
2. 使用初始化时设置的超管账号登录
3. 登录后自动进入管理后台 `/admin/dashboard`
4. 在仪表盘页面可以看到容器状态、用户统计和系统信息

## 常用 CLI 命令

```bash
picoaide init                    # 初始化
picoaide init -user zhangsan     # 为已有用户准备容器目录
picoaide serve -listen :80       # 启动服务
picoaide serve -listen :443      # 启用 TLS
picoaide reset-password <user>   # 重置本地用户密码
```

## 下一步

完成基础部署后，根据你的角色选择后续阅读路径：

**如果你是管理员**
- 阅读 [安装与部署](/docs/install-deploy/) 了解生产环境深度配置
- 通过 [管理后台操作指南](/docs/admin-guide/) 学习创建用户和管理容器
- 配置 [认证与安全](/docs/auth-security/) 对接公司 LDAP

**如果你是普通用户**
- 阅读 [Web 面板操作指南](/docs/web-panel/) 了解个人配置中心
- 安装 [浏览器扩展](/docs/browser-extension/) 让 AI 控制浏览器
- 安装 [桌面客户端](/docs/desktop-client/) 让 AI 操作桌面
