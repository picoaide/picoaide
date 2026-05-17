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

`picoaide init` 是全自动静默初始化，无需交互。它会运行环境检查、创建数据目录、初始化数据库和 systemd 服务：

```bash
sudo picoaide init
```

### 环境检查（自动执行）

初始化依次检查以下项目，任一不满足则退出并提示：

| 检查项 | 要求 | 说明 |
|--------|------|------|
| systemd | 已安装并可用 | 用于安装和管理 picoaide 服务 |
| Docker Engine | 24+，守护进程运行中 | 容器管理依赖 Docker SDK |
| 端口 80 | 未被占用 | 服务固定监听 `:80` |
| 数据目录 `/data/picoaide` | 不存在或为空 | 防止覆盖已有数据 |

### 自动完成的操作

环境检查通过后，初始化依次执行：

1. **复制二进制** — 将自身复制到 `/usr/sbin/picoaide`（如不在此路径）
2. **创建目录结构** — `users/`、`archive/`、`rules/`
3. **初始化数据库** — 创建 `picoaide.db`、写入默认配置、生成会话密钥
4. **创建超管账户** — 用户名为 `admin`，**自动生成 16 位随机密码**
5. **保存密码文件** — 密码写入 `/data/picoaide/secret`（权限 0600，仅 root 可读）
6. **设置默认配置** — 本地认证（`local`）、镜像源为腾讯云
7. **安装 systemd 服务** — 注册 `picoaide.service`，开机自启

### 初始化完成后

```bash
# 查看超管密码
cat /data/picoaide/secret

# 启动服务
systemctl start picoaide

# 查看状态
systemctl status picoaide
```

> **首次登录后，`/data/picoaide/secret` 文件会被自动删除**，请及时记录密码。忘记密码后可通过 `picoaide reset-password admin` 重置。

### 默认配置

| 项目 | 默认值 | 说明 |
|------|--------|------|
| 认证模式 | `local` | 本地账户认证，可在后台切换 LDAP/OIDC |
| 服务端口 | `:80`（TLS 启用时同时监听 `:443`） | 固定端口，不支持修改 |
| 镜像源 | 腾讯云（`tencent`） | 国内部署速度快，可在配置中切换 |
| 工作目录 | `/data/picoaide` | 数据库、用户目录、日志均在此目录下 |

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

服务默认以 `picoaide serve` 启动，固定监听 `:80`（TLS 启用时同时监听 `:443`），工作目录为 `/data/picoaide`。

## 验证安装

完成初始化和服务启动后，通过以下步骤验证部署是否正常：

1. 浏览器访问 `http://<服务器IP>/`，应自动跳转到登录页面
2. 使用初始化时设置的超管账号登录
3. 登录后自动进入管理后台 `/admin/dashboard`
4. 在仪表盘页面可以看到容器状态、用户统计和系统信息

## 常用 CLI 命令

```bash
picoaide init                    # 全自动初始化
picoaide serve                   # 启动服务（固定 :80，TLS 启用时同时监听 :443）
picoaide reset-password <user>   # 重置本地用户密码

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
