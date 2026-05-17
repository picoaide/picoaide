---
title: "安装与部署"
description: "PicoAide 生产环境安装、配置、升级和迁移指南"
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

### Docker 要求

服务端通过 Docker Engine SDK 管理容器，要求：

- Docker Engine 24.0 或更新版本
- Docker 守护进程正常运行
- 当前用户有权限访问 Docker socket（通过 root 运行）

验证 Docker 可用性：

```bash
docker info
```

### 磁盘规划

生产环境建议使用独立的持久化磁盘存储数据：

| 目录 | 用途 | 建议大小 |
|------|------|---------|
| `/data/picoaide/picoaide.db` | SQLite 数据库 | 随用户数增长，1GB 足够 1000 用户 |
| `/data/picoaide/users/` | 每个用户的工作区 | 平均每用户 50-200MB，按需规划 |
| `/data/picoaide/archive/` | 删除用户时的归档 | 与 users/ 相当 |
| `/var/lib/docker/` | Docker 镜像和容器 | 镜像约 2-5GB，按更新频率规划 |

### 网络规划

| 端口 | 用途 | 说明 |
|------|------|------|
| `:80` | HTTP 服务 | 默认监听端口 |
| `:443` | HTTPS 服务 | 需配置 TLS |

容器网络 `picoaide-net` 使用 `100.64.0.0/16` 地址段。确保该网段不与公司内网冲突。

## 安装方式详解

### 从 GitHub Release 安装（推荐）

```bash
# 下载最新版本
curl -L -o picoaide \
  https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64

# 验证下载完整性（可选）
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

# 开发构建
go build -o picoaide ./cmd/picoaide/

# 生产构建（推荐，含版本信息）
GOOS=linux GOARCH=amd64 go build \
  -ldflags "-X github.com/picoaide/picoaide/internal/config.Version=v1.0.0" \
  -o picoaide ./cmd/picoaide/

sudo mv picoaide /usr/sbin/picoaide
```

`ldflags` 注入的 Version 会在 `/api/health` 接口和 Web 管理后台中显示，便于版本追踪。

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

### 超管账户

初始化时创建的超管是系统的"逃生账户"，建议：

- 使用强密码（至少 16 位，含大小写字母、数字和特殊字符）
- 记录密码到安全密码管理器
- 创建完成后立即登录验证

### 镜像仓库

选择镜像仓库的考虑因素：

- **github**：适合海外部署或可访问 GitHub Container Registry 的环境。使用默认官方镜像，更新及时
- **tencent**：适合中国大陆部署，从腾讯云容器镜像服务拉取，速度更快

初始化完成后，系统会立即拉取所选仓库的最新镜像。如果网络环境需要代理，请确保 Docker 已配置代理：

```bash
# /etc/systemd/system/docker.service.d/proxy.conf
[Service]
Environment="HTTP_PROXY=http://proxy:port"
Environment="HTTPS_PROXY=http://proxy:port"
Environment="NO_PROXY=localhost,127.0.0.1,docker"
```

## 服务管理

### systemd 服务

初始化时自动生成的 systemd 服务：

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

# 设置开机自启
systemctl enable picoaide
```

### 日志管理

PicoAide 使用结构化 JSON 日志，默认写入 `logs/picoaide.log`。日志轮转由 lumberjack 管理：

```yaml
# 日志配置
web.log_level: info       # debug / info / warn / error
web.log_retention: 6m     # 日志保留周期
```

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

### 镜像升级

在管理后台的"镜像管理"页面可以：

1. 查看远程仓库最新标签
2. 拉取新版本镜像
3. 查看可升级用户（运行着旧版本镜像的用户）
4. 执行升级操作

升级时系统会自动检查 Picoclaw Adapter 的迁移规则，确保配置结构兼容。如果迁移链不支持直接升级，系统会提示需要先升级到中间版本。

## 数据迁移

### 迁移数据目录

```bash
# 停止服务
systemctl stop picoaide

# 复制数据到新位置
cp -a /data/picoaide /data2/picoaide

# 启动服务
systemctl start picoaide
```

### 迁移到新服务器

```bash
# 1. 旧服务器：停止服务并导出
systemctl stop picoaide
tar czf picoaide-backup.tar.gz \
  -C /data picoaide/picoaide.db \
  -C /data picoaide/users \
  -C /data picoaide/archive \
  -C /data picoaide/picoaide.log

# 2. 传输到新服务器
scp picoaide-backup.tar.gz new-server:/tmp/

# 3. 新服务器：解压并恢复
cd /
tar xzf /tmp/picoaide-backup.tar.gz
# 根据实际情况调整路径

# 4. 安装相同版本 picoaide 二进制
# 5. 启动服务
systemctl start picoaide
```

## 多实例注意事项

PicoAide 当前设计为单实例部署。如果需要多节点：

- 数据库使用本地 SQLite，不支持共享存储
- 容器管理绑定到本机 Docker
- 前端负载均衡请确保会话粘性（session affinity）

如果需要高可用，建议定期备份数据库并准备备用服务器。
