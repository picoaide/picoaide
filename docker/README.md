# PicoAide Deploy

[PicoClaw](https://github.com/sipeed/picoclaw) 的开箱即用开发环境容器镜像，集成完整的 Linux 工具链和多平台 AI Agent 运行时，一行命令即可启动。

镜像地址：`ghcr.io/picoaide/picoaide`

## 特性

| 特性 | 说明 |
|------|------|
| **开箱即用** | 内置 PicoClaw、Node.js LTS、Python (uv)、Git 等完整开发工具链，无需额外安装 |
| **多架构支持** | 同时支持 `linux/amd64` 和 `linux/arm64`，x86 服务器和 ARM 设备通用 |
| **数据持久化** | 配置文件和工作目录通过 volume 挂载，容器重建不丢失数据 |
| **自动更新** | 每日自动检测 PicoClaw 新版本并构建新镜像，始终保持最新 |
| **国内加速** | 预配置清华镜像源（apt、npm、pip），国内环境下载速度快 |
| **多 Shell 支持** | 内置 bash、zsh、fish，配合 fzf 提升操作效率 |

## 快速开始

### 1. 运行容器

```bash
docker run -d \
  --name picoaide-deploy \
  -v picoaide-root:/root \
  ghcr.io/picoaide/picoaide:v0.2.6
```

容器启动后会自动运行 `picoclaw gateway`，通过 Gateway 模式对外提供服务。

### 2. 进入容器

```bash
# 通过 Docker exec
docker exec -it picoaide-deploy zsh
```

## 配置说明

挂载到 `/root` 的卷会保存 PicoClaw 配置文件：

```text
/root/.picoclaw/config.json
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
| Shell | bash / zsh / fish |
| 编辑器 | vim / nano |
| 工具 | git, tmux, htop, tree, jq, ripgrep, bat, fzf, curl, wget 等 |

## 镜像标签

所有镜像使用固定版本号标签，不提供 `latest`：

| Tag | 说明 |
|-----|------|
| `vX.Y.Z` | 对应 PicoClaw release 版本 |

## 更新

拉取指定版本镜像即可更新：

```bash
docker pull ghcr.io/picoaide/picoaide:v0.2.6
docker rm -f picoaide-deploy
docker run -d --name picoaide-deploy -v picoaide-root:/root ghcr.io/picoaide/picoaide:v0.2.6
```

镜像每天自动检测 PicoClaw 新版本并构建，有新版本才会触发更新。

## 许可证

本项目基于 PicoClaw 开源项目构建，请遵循 [PicoClaw](https://github.com/sipeed/picoclaw) 的相关许可证。
