# PicoAide Deploy

[PicoClaw](https://github.com/sipeed/picoclaw) 的开箱即用开发环境容器镜像，集成完整的 Linux 工具链和多平台 AI Agent 运行时，一行命令即可启动。

提供两个镜像变体：

| 镜像 | 说明 |
|------|------|
| `ghcr.io/picoaide/picoaide-browser` | 含 Chromium 浏览器（默认推荐） |
| `ghcr.io/picoaide/picoaide` | 精简版，无浏览器 |

## 特性

| 特性 | 说明 |
|------|------|
| **开箱即用** | 内置 PicoClaw、Node.js LTS、Python (uv)、Git 等完整开发工具链，无需额外安装 |
| **多架构支持** | 同时支持 `linux/amd64` 和 `linux/arm64`，x86 服务器和 ARM 设备通用 |
| **数据持久化** | 配置文件和工作目录通过 volume 挂载，容器重建不丢失数据 |
| **自动更新** | 每日自动检测 PicoClaw 新版本并构建新镜像，始终保持最新 |
| **安全访问** | SSH 仅允许密钥登录，禁用密码认证 |
| **国内加速** | 预配置清华镜像源（apt、npm、pip），国内环境下载速度快 |
| **多 Shell 支持** | 内置 bash、zsh、fish，配合 fzf 提升操作效率 |

## 快速开始

### 1. 创建目录并下载配置

```bash
mkdir picoaide-deploy && cd picoaide-deploy
mkdir -p root data

# 下载 docker-compose 文件
curl -O https://raw.githubusercontent.com/PicoAide/PicoAide/main/docker/docker-compose.yaml
```

### 2. 配置 SSH 公钥（可选）

```bash
echo "你的SSH公钥内容" > root/.ssh/authorized_keys
```

### 3. 启动容器

```bash
docker compose up -d
```

容器启动后会自动运行 `picoclaw gateway`，通过 Gateway 模式对外提供服务。

### 4. 进入容器

```bash
# 通过 Docker exec
docker exec -it picoaide-deploy zsh

# 或通过 SSH（需要先配置公钥）
ssh -p 2222 root@<地址>
```

## 配置说明

### 目录结构

```
picoaide-deploy/
├── docker-compose.yaml
├── root/                    # 挂载到容器 /root（持久化用户数据）
│   ├── .ssh/
│   │   └── authorized_keys  # SSH 公钥
│   └── .picoclaw/
│       └── config.json      # PicoClaw 配置文件
└── data/                    # 挂载到容器 /data（持久化工作数据）
```

### 使用无浏览器版本

编辑 `docker-compose.yaml`，将镜像替换为精简版：

```yaml
image: ghcr.io/picoaide/picoaide:latest
```

### 自定义端口映射

```yaml
services:
  picoaide-deploy:
    image: ghcr.io/picoaide/picoaide-browser:latest
    container_name: picoaide-deploy
    volumes:
      - ./root:/root
      - ./data:/data
    ports:
      - "2222:22"       # SSH
      - "18790:18790"   # PicoClaw Gateway
    restart: unless-stopped
```

### PicoClaw 配置

配置文件位于 `root/.picoclaw/config.json`，首次启动会自动生成默认配置。主要配置项：

- **Channels** — 启用消息平台（Telegram / Discord / WhatsApp / 飞书 / 企业微信 / QQ 等）
- **Model List** — 添加 AI 模型 API（支持 OpenAI / Anthropic / DeepSeek / GLM / Qwen 等 20+ 模型）
- **Tools** — 工具开关（网页搜索、文件操作、定时任务、MCP 等）

详细配置请参考 [PicoClaw 官方文档](https://github.com/sipeed/picoclaw)。

## 内置环境

| 组件 | 版本/说明 |
|------|----------|
| 操作系统 | Debian 13 (slim) |
| PicoClaw | 最新 release 版本 |
| Node.js | v22 LTS（通过 NVM 管理） |
| Python | 通过 uv 管理 |
| 浏览器 | Chromium（仅 picoaide-browser 镜像） |
| Shell | bash / zsh / fish |
| 编辑器 | vim / nano |
| 工具 | git, tmux, htop, tree, jq, ripgrep, bat, fzf, curl, wget 等 |

## 镜像标签

| Tag | 说明 |
|-----|------|
| `latest` | 最新构建 |
| `vX.Y.Z` | 对应 PicoClaw release 版本 |
| `sha-xxxxxx` | 对应构建的 commit SHA |

## 更新

拉取最新镜像即可更新：

```bash
docker compose pull && docker compose up -d
```

镜像每天自动检测 PicoClaw 新版本并构建，有新版本才会触发更新。

## 许可证

本项目基于 PicoClaw 开源项目构建，请遵循 [PicoClaw](https://github.com/sipeed/picoclaw) 的相关许可证。
