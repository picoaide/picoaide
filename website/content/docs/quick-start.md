---
title: "快速入门"
description: "PicoAide 快速入门指南 — 环境要求、安装、初始化和基础验证"
weight: 1
draft: false
---

PicoAide 是一个企业级 AI 工作平台。它为每位员工分配独立的 AI 操作助手沙箱，通过浏览器扩展和桌面客户端让 AI 在授权范围内执行实际工作流，同时确保企业数据不出边界。

本指南带你从零开始完成安装、初始化和基础验证，约 10 分钟可完成。

## 环境要求

服务端必须运行在 Linux 上，需要 root 权限管理网络命名空间和 overlayfs。

| 项目 | 要求 |
|------|------|
| 操作系统 | Linux（x86_64），推荐 Ubuntu 22.04+ / Debian 12+ / CentOS Stream 9+ |
| 权限 | root（用于创建网桥、网络命名空间、overlayfs 挂载） |
| 端口 | 默认 `:80`，HTTPS 启用时额外占用 `:443` |
| 磁盘 | 持久化磁盘，用于存储 SQLite 数据库和用户目录 |
| systemd | 用于安装和管理 picoaide 服务 |

桌面客户端可以运行在 Windows、macOS 和 Linux 上，不受此限制。

## 下载安装

### 方式一：从 GitHub Release 下载（推荐）

```bash
curl -L -o picoaide \
  https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64
chmod +x picoaide
sudo mv picoaide /usr/sbin/picoaide
```

### 方式二：从源码编译

```bash
git clone https://github.com/picoaide/picoaide.git
cd picoaide

# 全量编译（必要时先安装依赖）
sudo make build

sudo mv picoaide /usr/sbin/picoaide
```

> `make build` 会依次编译 picoagent、准备 Alpine rootfs 和编译 picoaide。请勿只执行 `go build ./cmd/picoaide/`，否则沙箱功能不可用。

### 方式三：生产环境构建（注入版本信息）

```bash
GOOS=linux GOARCH=amd64 go build \
  -ldflags "-X github.com/picoaide/picoaide/internal/config.Version=v1.0.0" \
  -o picoaide ./cmd/picoaide/
```

`ldflags` 注入的 Version 会在 `/api/health` 接口和 Web 管理后台中显示，便于版本追踪。

## 初始化

`picoaide init` 是全自动静默初始化，无需交互：

```bash
sudo picoaide init
```

### 环境检查

初始化依次检查以下项目，任一不满足则退出并提示：

| 检查项 | 要求 | 说明 |
|--------|------|------|
| systemd | 已安装并可用 | 用于安装和管理 picoaide 服务 |
| root 权限 | 当前用户为 root | 网络和沙箱操作需要 |
| 端口 80/443 | 未被占用 | 服务默认监听这些端口 |
| 工作目录 `/data/picoaide` | 不存在或为空 | 防止覆盖已有数据 |

### 自动完成的操作

环境检查通过后，初始化依次执行：

1. **复制二进制** — 将自身复制到 `/usr/sbin/picoaide`（如不在此路径）
2. **创建目录结构** — `users/`、`archive/`、`rules/`、`skill/` 等
3. **初始化数据库** — 创建 `picoaide.db`、写入默认配置、生成会话密钥
4. **创建超管账户** — 用户名为 `admin`，**自动生成 16 位随机密码**
5. **保存密码文件** — 密码写入 `/data/picoaide/secret`（权限 0600，仅 root 可读）
6. **设置默认配置** — 本地认证模式（`local`）
7. **安装 systemd 服务** — 注册 `picoaide.service`，设置开机自启

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
| 工作目录 | `/data/picoaide` | 数据库、用户目录、日志均在此目录下 |

## 服务管理

```bash
# 查看服务状态
systemctl status picoaide

# 启动
systemctl start picoaide

# 停止
systemctl stop picoaide

# 重启
systemctl restart picoaide

# 查看实时日志
journalctl -u picoaide -f
```

服务默认以 `picoaide serve` 启动，固定监听 `:80`，TLS 启用时同时监听 `:443`，工作目录为 `/data/picoaide`。

## 验证安装

完成初始化和服务启动后，通过以下步骤验证部署是否正常：

1. 浏览器访问 `http://<服务器IP>/`，应自动跳转到登录页面
2. 使用超管账号 `admin` 和初始化时生成的密码登录
3. 登录后自动进入管理后台 `/admin/dashboard`
4. 在仪表盘页面可以看到用户统计和技能数量

## 常用 CLI 命令

```bash
picoaide init                    # 全自动静默初始化
picoaide serve                   # 启动服务（固定 :80，TLS 启用时同时监听 :443）
picoaide reset-password <user>   # 重置本地用户密码
```

## 下一步

完成基础部署后，根据你的角色选择后续阅读路径：

**如果你是管理员**
- 阅读 [安装与部署](/docs/install-deploy/) 了解生产环境深度配置
- 通过 [管理后台操作指南](/docs/admin-guide/) 学习创建用户和管理技能
- 配置 [认证与安全](/docs/auth-security/) 对接公司 LDAP/OIDC

**如果你是普通用户**
- 阅读 [Web 面板操作指南](/docs/web-panel/) 了解个人配置中心
- 安装 [浏览器扩展](/docs/browser-extension/) 让 AI 控制浏览器
- 安装 [桌面客户端](/docs/desktop-client/) 让 AI 操作桌面
