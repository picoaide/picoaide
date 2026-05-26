# PicoAide

> 企业级 AI 工作平台 — 让每位员工拥有自己的 AI 操作助手

PicoAide 是一个私有化部署的 AI 代理管理平台。通过 overlayfs + network namespace 为每位员工分配独立的 AI 沙箱，配合浏览器扩展和桌面客户端让 AI 在授权范围内操作真实环境。认证可插拔（Local / LDAP / OIDC），技能可分发，MCP 中继聚合平台、浏览器、桌面和第三方工具。

## 设计理念

> **AI 的力量应当被释放，但数据的边界必须被守护。**

企业拥抱 AI 的最大障碍，不是技术，而是信任。当企业数据流向公共 API，每一次调用都是一次不可逆的风险。真正的问题不是"要不要用 AI"，而是"如何在享受 AI 能力的同时，让数据寸步不离"。

PicoAide 的答案是：**让 AI 进入企业，而不是让数据离开企业**。AI 在原生 Linux 沙箱中运行，通过继承用户身份操作企业系统，天然只能看到用户能看到的内容。沙箱之间 iptables DROP 隔离，文件系统通过 `os.Root` 沙盒路径限制。安全不是 AI 的对立面，而是 AI 的基础设施。

PicoAide 不绑定任何大模型，不限定任何 Agent 框架。用户沙箱内跑什么 AI 软件由管理员决定，PicoAide 只负责供给和连接。

## 核心功能

### 服务端

- **原生 Linux 沙箱隔离** — overlayfs + network namespace，不依赖 Docker。`picoaide-br` 网桥（100.64.0.0/16），iptables DROP 禁止容器间通信
- **认证可插拔** — Provider 注册表模式，`init()` 自动注册认证源。内置 Local / LDAP / OIDC，新增认证源无需修改核心代码
- **MCP 三层中继** — browser（19 个工具，通过浏览器扩展执行）、computer（15 个工具，通过桌面客户端执行）、agent（6 个平台工具 + 聚合所有工具 + 第三方 MCP 代理）
- **分组管理** — 树形层级分组，LDAP 自动同步，按组绑定技能
- **技能管理** — Git 仓库源、注册中心、按组部署、默认技能、安装策略
- **配置 KV 展平存储** — 点分隔键值对存入 SQLite，实时修改即时生效，认证源切换自动清理
- **对话流式输出** — SSE 流式推送 AI 响应，支持断线重连和上下文记忆
- **定时任务** — Cron 调度器，支持按用户配置定时 AI 任务
- **文件管理** — `os.Root` 沙盒路径，所有文件操作在用户工作区中
- **团队空间** — 按组设置可见范围的共享文件夹，沙箱内只读挂载
- **TLS 证书管理** — 上传和管理 HTTPS 证书，自动重启生效
- **双层 API 架构** — 外部处理器（完整路由）和内部处理器（沙箱最小 API），通过源 IP 自动分发

### 浏览器扩展 (PicoAide Helper)

- Chrome Manifest V3，通过 WebSocket 连接服务端
- 授权 AI 控制当前标签页（导航、点击、输入、截图等 19 个工具）
- Cookie 同步：将当前页面登录态写入 AI 配置
- 超管不能登录，确保管理身份与工作身份分离

### 桌面客户端

- Python PyQt，支持 Windows / macOS / Linux
- AI 桌面控制：截图、鼠标、键盘、OCR（15 个桌面工具）
- 6 大权限组（截图、屏幕信息、OCR、鼠标、键盘、文件）
- 文件白名单目录机制，限制 AI 文件访问范围

## 系统架构

```
                           ┌──────────────────────────────────────────────┐
                           │           PicoAide Server (Go + Gin)         │
                           │                                              │
                           │  ┌──────────┐  ┌──────────┐  ┌────────────┐  │
                           │  │ 外部 API  │  │ 内部 API  │  │ SQLite(xorm)│  │
                           │  │ (全路由)  │  │ (最小集)  │  │ 用户/组/配置 │  │
                           │  └────┬─────┘  └────┬─────┘  └──────┬─────┘  │
                           │       │             │               │         │
                           │  ┌────┴─────┐  ┌────┴──────┐       │         │
                           │  │ Session  │  │ MCP SSE   │       │         │
                           │  │ Auth     │  │ ServiceHub│       │         │
                           │  └──────────┘  └────┬───────┘       │         │
                           │                     │                │         │
                           └─────────────────────┼────────────────┼─────────┘
                  ▲            ▲                  │                │
                  │            │                  │                │
     ┌────────────┴──┐  ┌─────┴──────┐           │                │
     │ 浏览器扩展     │  │ 桌面客户端  │           │                │
     │ (Browser MCP) │  │(Computer   │           │                │
     │ 19 tools      │  │ MCP 15)    │           │                │
     └───────────────┘  └────────────┘           │                │
                                                 ▼                ▼
                     ┌──────────────────────────────────────────────────┐
                     │         原生 Linux 沙箱 (overlayfs + netns)       │
                     │                                                  │
                     │   picoaide-br (100.64.0.0/16, iptables DROP)    │
                     │                                                  │
                     │  ┌──────┐ ┌──────┐ ┌──────┐       ┌──────┐      │
                     │  │用户A │ │用户B │ │用户C │ ·····  │用户N │      │
                     │  │picoagent picoagent picoagent     picoagent    │
                     │  └──────┘ └──────┘ └──────┘       └──────┘      │
                     │   100.64.0.2  .3      .4            .N+1         │
                     └──────────────────────────────────────────────────┘
```

### MCP 三层架构

```
沙箱 (picoagent)                 PicoAide Server                  执行端
┌──────────────┐  SSE/JSON-RPC ┌────────────────────┐  WebSocket ┌──────────┐
│              │ ────────────▶ │                    │ ─────────▶ │ 浏览器    │
│  agent 服务   │  /api/mcp/sse │  ServiceHub 中继   │  /api/     │ 扩展      │
│  (工具聚合)   │  /browser    │  (连接管理/路由)    │  browser/ws│ (Chrome) │
│              │ ────────────▶ │                    │ ─────────▶ │ 桌面      │
│              │  /api/mcp/sse │  MCPProxyManager   │  /api/     │ 客户端    │
│              │  /computer    │  (第三方 MCP 代理)   │  computer/ws│(Python) │
│              │ ────────────▶ │                    │            │          │
│              │  /api/mcp/sse │  Picoaide 平台工具  │            │          │
│              │  /agent       │  (服务端直接处理)    │            │          │
└──────────────┘               └────────────────────┘            └──────────┘
```

## 快速开始

### 前置条件

- Linux 服务器（推荐 Ubuntu 22.04+ / Debian 12+）
- root 权限
- systemd

### 安装

```bash
# 下载最新版本
curl -L -o picoaide \
  https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64
chmod +x picoaide
sudo mv picoaide /usr/sbin/picoaide

# 全自动初始化
sudo picoaide init
```

初始化自动完成：环境检查 → 创建数据目录 → 初始化 SQLite 数据库 → 创建超管 `admin`（随机密码写入 `/data/picoaide/secret`）→ 安装 systemd 服务。

### 启动

```bash
# 查看超管密码
cat /data/picoaide/secret

# 启动服务
systemctl start picoaide

# 查看状态
systemctl status picoaide
```

首次超管登录后 `secret` 文件自动删除。忘记密码用 `picoaide reset-password admin` 重置。

## 构建

```bash
# ⚠️ 必须全量编译（picoagent + Alpine rootfs + picoaide）
make build

# 运行全部测试
make test

# 全平台发布构建
make release PICOCLAW_VERSION=v1.0.0

# 部署到测试服务器
go build -o picoaide ./cmd/picoaide/
scp picoaide root@server:/usr/sbin/picoaide
ssh root@server systemctl restart picoaide
```

## 技术栈

| 组件 | 技术 |
|------|------|
| 服务端 | Go 1.24+, Gin, xorm + modernc/sqlite, gorilla/websocket |
| 沙箱 | overlayfs, network namespace, veth pair, iptables |
| 认证 | argon2id, go-ldap, coreos/go-oidc |
| 浏览器插件 | Chrome Extension Manifest V3 |
| 桌面客户端 | Python 3.10+, PyQt (PySide6), PyAutoGUI |
| 官网 | Hugo + Cloudflare Pages |

## 项目结构

```
picoaide/
├── cmd/picoaide/             # CLI 入口（init, serve, reset-password）
├── cmd/picoagent/            # 沙箱内 AI Agent 二进制
├── internal/
│   ├── auth/                 # SQLite ORM（xorm）、用户/组/容器 CRUD、密码哈希
│   ├── authsource/           # 认证源 Provider 注册表（LDAP/OIDC/Local）
│   ├── config/               # 全局配置展平存储、YAML/JSON 加载
│   ├── sandbox/              # overlayfs + network namespace 沙箱管理
│   ├── user/                 # 用户生命周期、IP 分配、配置合并
│   ├── web/                  # Gin HTTP 服务、所有 handler、MCP SSE、WebSocket Hub
│   │   └── ui/               # 嵌入的 Web UI（SPA + JS + CSS）
│   ├── ldap/                 # LDAP 客户端（认证、用户/组查询）
│   ├── logger/               # 结构化日志（slog + lumberjack）
│   ├── im/                   # IM 网关（钉钉、飞书、企业微信）
│   ├── scheduler/            # Cron 定时任务调度器
│   ├── skill/                # 技能解析、Git 仓库、注册中心
│   └── util/                 # 工具函数（深拷贝、文件操作、路径安全）
├── picoaide-desktop/         # 桌面客户端（Python/PyQt）
├── picoaide-extension/       # Chrome 浏览器扩展
├── website/                  # Hugo 官网站点 + 文档
├── bundle/                   # Alpine rootfs + picoagent 分发
├── Makefile                  # 构建和测试命令
└── format.sh                 # 代码格式化
```

## API 端点一览

所有路由均注册在 `/api` 和 `/api/v1` 双前缀下。认证方式：Session Cookie（Web）/ Bearer Token（MCP）。

| 类别 | 端点 | 说明 |
|------|------|------|
| 公开 | `GET /api/version`, `/api/health`, `/api/login/mode` | 版本、健康检查、认证模式 |
| 认证 | `POST /api/login`, `GET /api/login/auth`, `GET /api/login/callback`, `POST /api/logout`, `GET /api/csrf` | 登录/登出/SSO/CSRF |
| 用户 | `GET /api/user/info`, `POST /api/user/password` | 个人信息、改密 |
| 对话 | `GET/POST /api/user/chat/*` | SSE 流式 AI 对话 |
| 文件 | `GET/POST /api/files/*` | 文件 CRUD（list/upload/download/delete/mkdir/edit） |
| 渠道 | `GET/POST /api/channels/*` | 通讯渠道配置 |
| 定时任务 | `GET/POST /api/cron/*` | Cron 任务 CRUD |
| Cookie | `POST /api/cookies`, `GET/POST /api/user/cookies/*` | Cookie 同步/授权 |
| 共享文件夹 | `GET /api/shared-folders` | 可见共享文件夹 |
| 用户技能 | `GET/POST /api/user/skills/*` | 技能安装/卸载/列表 |
| MCP | `GET /api/mcp/token`, `GET/POST /api/mcp/sse/:service`, `GET/POST /api/mcp/cookies` | MCP Token、SSE、Cookie |
| WebSocket | `GET /api/browser/ws`, `GET /api/computer/ws` | 浏览器/桌面代理 WS |
| 内部 | `GET /api/picoagent/me` | 沙箱 picoagent 配置（仅沙箱内访问） |
| 配置 | `GET/POST /api/config` | 全局配置读写（超管） |
| 管理-用户 | `GET/POST /api/admin/users/*`, `/api/admin/superadmins/*` | 用户/超管 CRUD |
| 管理-认证 | `GET/POST /api/admin/auth/*`, `/api/admin/whitelist` | LDAP 测试/同步/白名单 |
| 管理-组 | `GET/POST /api/admin/groups/*` | 组 CRUD + 成员/技能管理 |
| 管理-技能 | `GET/POST /api/admin/skills/*` | 技能/源/仓库/默认技能管理 |
| 管理-共享 | `GET/POST /api/admin/shared-folders/*` | 共享文件夹 CRUD + 挂载 |
| 管理-MCP | `GET/POST /api/admin/mcp/servers/*` | 第三方 MCP 服务器管理 |
| 管理-其他 | `/api/admin/tls/*`, `/api/admin/model/test`, `/api/admin/skill-install-policy`, `/api/admin/task/status` | TLS/模型测试/策略/任务 |

## License

[MIT License](LICENSE)
