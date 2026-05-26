---
title: "安装与部署"
description: "PicoAide 生产环境安装、目录规划、升级和迁移指南"
weight: 3
draft: false
---

本指南面向需要规划生产部署的系统管理员。在开始之前，请确保你已经完成了[快速入门](/docs/quick-start/)中的基础安装和初始化。

## 环境规划

### 操作系统

PicoAide 服务端目前支持 Linux x86_64 平台。推荐使用以下发行版：

- Ubuntu 22.04 LTS 或更新版本
- Debian 12+
- CentOS Stream 9 / RHEL 9+
- Rocky Linux 9+

### 磁盘规划

生产环境建议使用独立的持久化磁盘存储数据：

| 目录 | 用途 | 建议大小 |
|------|------|---------|
| `/data/picoaide/picoaide.db` | SQLite 数据库 | 随用户数增长，1GB 足够 1000 用户 |
| `/data/picoaide/users/` | 每个用户的工作区 | 平均每用户 50-200MB，按需规划 |
| `/data/picoaide/archive/` | 删除用户时的归档 | 与 `users/` 相当 |
| `/data/picoaide/skill/` | 已安装的技能 | 按技能数量和大小规划 |

### 网络规划

| 端口 | 用途 | 说明 |
|------|------|------|
| `:80` | HTTP 服务 | 固定端口，Web 管理 + API |
| `:443` | HTTPS 服务 | TLS 启用时自动监听 |

容器桥接网络 `picoaide-br` 使用 `100.64.0.0/16` 地址段。这是 CGNAT 保留地址，通常不会与公司内网冲突。

## 安装方式详解

### 从 GitHub Release 安装（推荐）

```bash
# 下载最新版本
curl -L -o picoaide \
  https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64

# 可选：验证下载完整性
curl -L -o picoaide.sha256 \
  https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64.sha256
sha256sum -c picoaide.sha256

# 安装
chmod +x picoaide
sudo mv picoaide /usr/sbin/picoaide
```

### 从源码构建

```bash
git clone https://github.com/picoaide/picoaide.git
cd picoaide

# 全量编译（picoagent + Alpine rootfs + picoaide）
sudo make build

# 或指定版本的生产构建
GOOS=linux GOARCH=amd64 make build

sudo mv picoaide /usr/sbin/picoaide
```

## 初始化最佳实践

### 数据目录

建议使用独立数据盘，而非系统盘：

```bash
# 挂载数据盘到 /data
sudo mkfs.ext4 /dev/sdb1
sudo mount /dev/sdb1 /data
echo '/dev/sdb1 /data ext4 defaults 0 0' | sudo tee -a /etc/fstab

# 创建 picoaide 数据目录
sudo mkdir -p /data/picoaide
```

### 超管密码

`picoaide init` 会自动创建超管账户 `admin`，并生成 **16 位随机密码**，写入 `/data/picoaide/secret`（权限 0600）。

建议：

- 初始化后立即查看密码：`cat /data/picoaide/secret`
- 将密码记录到安全密码管理器
- `secret` 文件在超管首次登录后台后自动删除
- 忘记密码后可通过 `picoaide reset-password admin` 重置

如果网络环境需要代理，请确保代理配置正确。PicoAide 的容器网络使用 `picoaide-br` 网桥，容器内通过 iptables SNAT 出站。

### 存储适配器配置

默认配置为当前的 minimal 配置。如果需要远程技能源，可以在管理后台添加 Git 仓库或注册中心源。

## 服务管理

### systemd 服务

初始化时自动生成的 systemd 服务文件：

```ini
[Unit]
Description=PicoAide Management API Server
After=network.target

[Service]
Type=simple
ExecStart=/usr/sbin/picoaide serve
WorkingDirectory=/data/picoaide
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### 常用操作

```bash
# 启动
systemctl start picoaide

# 停止
systemctl stop picoaide

# 重启
systemctl restart picoaide

# 查看状态
systemctl status picoaide

# 查看实时日志
journalctl -u picoaide -f

# 设置开机自启（初始化时已自动设置）
systemctl enable picoaide
```

### 日志管理

PicoAide 使用结构化 JSON 日志，默认写入 `logs/picoaide.log`。日志轮转由 lumberjack 管理：

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `web.log_level` | 日志级别 | `info`（可选 debug/info/warn/error） |
| `web.log_retention` | 日志保留周期 | `6m` |

## 升级流程

### 标准升级步骤

```bash
# 1. 备份数据库
sudo cp /data/picoaide/picoaide.db /data/picoaide/picoaide.db.bak.$(date +%Y%m%d)

# 2. 下载新版本二进制
curl -L -o picoaide \
  https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64
chmod +x picoaide

# 3. 替换旧二进制
sudo mv picoaide /usr/sbin/picoaide

# 4. 重启服务
systemctl restart picoaide

# 5. 验证启动
systemctl status picoaide
journalctl -u picoaide -n 50 --no-pager
```

### 数据库迁移

从旧版本升级时，系统会自动执行数据库迁移。迁移系统位于 `internal/auth/migrations/`，使用基于时间戳的文件注册：

```go
init() {
  Register(Migration{
    Timestamp: "20250601093000",
    Desc:      "添加 xxx 表的新列",
    Up: func(engine *xorm.Engine) error {
      // 幂等迁移逻辑
    },
  })
}
```

迁移在 `syncSchema()` 末尾自动执行，迁移文件使用 `ColumnExists` 检查确保幂等。升级时无需手动操作数据库。

## 数据迁移

### 迁移到新服务器

```bash
# 1. 旧服务器：停止服务并打包
systemctl stop picoaide
tar czf picoaide-backup.tar.gz \
  -C /data picoaide/picoaide.db \
  -C /data picoaide/users \
  -C /data picoaide/archive

# 2. 传输到新服务器
scp picoaide-backup.tar.gz new-server:/tmp/

# 3. 新服务器：解压恢复
sudo mkdir -p /data/picoaide
sudo tar xzf /tmp/picoaide-backup.tar.gz -C /data

# 4. 安装相同版本 picoaide 二进制并启动
sudo systemctl start picoaide
```

## 多实例注意事项

PicoAide 当前设计为单实例部署，不支持水平扩展：

- 数据库使用本地 SQLite，不支持共享存储
- 沙箱管理绑定到本机 Linux 内核（bridge + namespace）
- Bridge 创建要求节点独占 `100.64.0.0/16` 网段

如果需要故障转移，建议定期备份数据库并在备用服务器上准备相同的二进制版本。
