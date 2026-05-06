# PicoAide

> 企业级 AI PaaS 工作平台 — 让每位员工拥有自己的 AI 操作助手

PicoAide 是一个私有化部署的 AI 代理管理平台。通过 Docker 容器为每位员工分配独立的 AI 助手，依托 Cookie 授权机制天然继承企业内部平台的权限控制，实现浏览器自动化、桌面控制、技能分发，同时确保企业数据不外流。

## 设计理念

### 分层模型架构

```
┌─────────────────────────────────────────────────────┐
│                 企业管理员                            │
│   使用 DeepSeek / GPT-4o 等大模型编写 Skill 脚本      │
│   设计业务流程、配置工具、管理权限                      │
└──────────────────────┬──────────────────────────────┘
                       │ 分发 Skill
                       ▼
┌─────────────────────────────────────────────────────┐
│                 企业员工                              │
│   使用 Qwen3-27B / 小模型执行工具调用                  │
│   只需对话即可完成操作，无需了解技术细节                │
└─────────────────────────────────────────────────────┘
```

**大模型负责「思考」**：企业管理员使用聪明的大模型（DeepSeek、GPT-4o）制作 Skill 和编写代码，设计自动化流程。

**小模型负责「执行」**：企业员工使用 Qwen3-27B 等经济型模型进行日常工具调用，成本低、响应快。

### Cookie 授权 = 天然权限隔离

```
┌──────────┐     Cookie      ┌──────────┐     请求      ┌──────────────┐
│  AI 助手  │ ──────────────▶ │ PicoAide │ ──────────▶ │  企业内部系统  │
│ (容器内)  │  携带用户Cookie  │  服务端   │  模拟登录态   │  OA/ERP/CRM  │
└──────────┘                 └──────────┘              └──────────────┘
```

- AI 的 Prompt 中 **不包含登录凭据**，只携带用户授权的 Cookie
- 员工授权哪些站点的 Cookie，AI 就只能访问哪些站点
- 企业内部系统 **无需为 AI 重新设计权限体系**
- 用户权限天然隔离，每个 AI 助手只能操作所属员工的授权范围

### 全员 AI 助手

依托 Go 语言的高效并发和 Docker 轻量容器，单台服务器可运行 **400+** 个独立 AI 代理容器（基于 PicoClaw），让企业员工人人拥有自己的操作助手。

## 系统架构

```
                          ┌───────────────────────────────────────────┐
                          │            PicoAide Server (Go)           │
                          │                                           │
                          │  ┌─────────┐  ┌──────────┐  ┌─────────┐  │
                          │  │  Web API │  │ MCP SSE  │  │ SQLite  │  │
                          │  │ (HTTP)   │  │ Proxy    │  │  DB     │  │
                          │  └────┬─────┘  └────┬─────┘  └────┬────┘  │
                          │       │             │             │        │
                          │  ┌────┴─────┐  ┌────┴─────┐       │        │
                          │  │ Session  │  │ Service  │       │        │
                          │  │ Auth     │  │ Hub (WS) │       │        │
                          │  └──────────┘  └────┬─────┘       │        │
                          │                     │              │        │
                          └─────────────────────┼──────────────┼────────┘
                 ▲            ▲                  │              │
                 │            │                  │              │
    ┌────────────┴──┐  ┌─────┴──────┐           │              │
    │ Chrome 插件    │  │ 桌面客户端  │           │              │
    │ (Browser MCP) │  │(Computer   │           │              │
    │               │  │    MCP)    │           │              │
    └───────────────┘  └────────────┘           │              │
                                                ▼              ▼
                    ┌───────────────────────────────────────────────┐
                    │              Docker Engine                     │
                    │                                               │
                    │  picoaide-net (100.64.0.0/16, ICC=false)      │
                    │                                               │
                    │  ┌──────┐ ┌──────┐ ┌──────┐       ┌──────┐   │
                    │  │用户A │ │用户B │ │用户C │ ·····  │用户N │   │
                    │  │PicoClaw PicoClaw PicoClaw       PicoClaw   │
                    │  │ AI   │ │ AI   │ │ AI   │       │ AI   │   │
                    │  └──────┘ └──────┘ └──────┘       └──────┘   │
                    │   100.64.0.2  .3      .4            .N+1     │
                    └───────────────────────────────────────────────┘
```

### MCP 三层中继

PicoClaw（AI 代理）通过 MCP 协议控制浏览器和桌面，PicoAide 服务端作为中继层：

```
PicoClaw (AI 代理)                    PicoAide Server                  执行端
┌─────────────┐  SSE/JSON-RPC  ┌───────────────────┐  WebSocket  ┌──────────┐
│             │ ─────────────▶ │                   │ ──────────▶ │ 浏览器    │
│  config.json│  /api/mcp/sse/ │  MCP SSE Proxy    │  /api/      │ 插件      │
│  MCP Client │    browser     │                   │  browser/ws │          │
│             │                │                   │             │ Chrome   │
│             │ ─────────────▶ │                   │ ──────────▶ │ 桌面     │
│             │  /api/mcp/sse/ │                   │  /api/      │ 客户端    │
│             │    computer    │                   │  computer/ws│ Python   │
└─────────────┘                └───────────────────┘             └──────────┘
```

**数据流**：AI 调用工具 → SSE POST 请求到服务端 → 服务端通过 WebSocket 转发到执行端 → 执行端在本地操作 → 结果原路返回。

### Skill 体系

```
┌─────────────────────────────────────────────────────────┐
│                    Skill 仓库 (Git)                      │
│                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐   │
│  │ 基础 Skill    │  │ 业务 Skill A │  │ 业务 Skill B │   │
│  │ (管理员维护)  │  │ (A部门维护)  │  │ (B部门维护)  │   │
│  │              │  │              │  │              │   │
│  │ • OA审批     │  │ • 财务报销    │  │ • 客户跟进   │   │
│  │ • 邮件发送   │  │ • 合同管理    │  │ • 报表生成   │   │
│  │ • 文件处理   │  │ (基于基础Skill)│  │ (基于基础Skill)│  │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘   │
│         │                 │                  │           │
└─────────┼─────────────────┼──────────────────┼───────────┘
          │                 │                  │
          ▼                 ▼                  ▼
   ┌──────────────────────────────────────────────────┐
   │              PicoAide Skill 分发引擎               │
   │                                                    │
   │   按分组绑定 Skill → 自动部署到组成员容器            │
   │   支持全员部署 / 按组部署 / 按用户部署              │
   └──────────────────────────────────────────────────┘
```

**分层 Skill 设计**：
- **基础 Skill**：企业管理员制作，包含 CLI 工具和通用流程
- **业务 Skill**：各部门基于基础 Skill，实现自己的业务逻辑
- **技能绑定**：Skill 绑定到分组，自动部署到所有组成员

## 功能特性

### 服务端

- **多用户容器管理** — Docker 容器生命周期（创建、启停、重启、删除）
- **网络隔离** — `picoaide-net` 私有网络，ICC 禁用，容器间互不可达
- **认证体系** — 本地认证 / LDAP 认证，会话 Cookie + CSRF 保护
- **分组管理** — 支持层级分组，LDAP 自动同步
- **配置下发** — 全局配置合并到用户配置，支持批量队列部署
- **镜像管理** — 拉取、升级（按组/按用户选择），队列逐步重启
- **Skill 管理** — Git 仓库源、分组绑定、自动部署
- **MCP 中继** — SSE + WebSocket 双层代理，Browser / Computer 双通道
- **白名单** — LDAP 模式下控制哪些用户可以访问
- **文件管理** — 用户工作空间文件上传/下载/编辑

### 浏览器插件 (PicoAide Helper)

- **AI 浏览器控制** — 授权 AI 代理操作当前标签页（导航、点击、输入、截图）
- **Cookie 同步** — 将当前页面登录态同步给 AI，模拟用户身份操作
- **管理后台** — 用户管理、镜像管理、分组管理、Skill 部署、配置管理
- **11 个浏览器工具** — navigate、screenshot、click、type、get_content、execute 等

### 桌面客户端

- **AI 桌面控制** — 远程鼠标、键盘、截图操作
- **本地 OCR** — RapidOCR 识别屏幕文字及位置坐标
- **文件操作** — 白名单目录内的文件读写、搜索
- **细粒度权限** — 6 大权限组（截图、鼠标、键盘、读文件、写文件、浏览目录）
- **跨平台** — Windows / macOS / Linux，PySide6 + PyInstaller 单文件打包

## 快速开始

### 前置条件

- Linux 服务器（推荐 Ubuntu 22.04+）
- Docker Engine 24+
- root 权限

### 安装

```bash
# 下载最新版本
wget https://github.com/picoaide/picoaide/releases/latest/download/picoaide-linux-amd64
chmod +x picoaide-linux-amd64
mv picoaide-linux-amd64 /usr/sbin/picoaide

# 初始化（交互式向导）
./picoaide init
```

初始化向导将引导完成：
1. 检测 Docker 环境
2. 配置监听地址
3. 创建超级管理员
4. 选择镜像源（GitHub / 腾讯云）
5. 拉取最新镜像
6. 安装 systemd 服务

### 启动

```bash
# 手动启动
./picoaide serve -listen :80

# 或通过 systemd
systemctl start picoaide
```

### 配置 LDAP（可选）

通过管理后台「认证配置」页面配置 LDAP：
- 服务器地址、Bind DN、Base DN
- 用户过滤规则
- 分组同步模式（memberOf / group_search）
- 白名单控制

### 安装浏览器插件

1. 从 Chrome 应用商店安装 PicoAide Helper
2. 点击插件图标，输入 PicoAide 服务器地址
3. 使用管理员账号登录
4. 进入管理后台进行用户和配置管理

### 安装桌面客户端

从 [Releases](https://github.com/picoaide/picoaide/releases) 下载对应平台的客户端：
- Windows: `picoaide-desktop-windows.exe`
- macOS: `picoaide-desktop-macos`
- Linux: `picoaide-desktop-linux`

## API 端点

### 认证
| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/login` | POST | 登录（本地 / LDAP） |
| `/api/logout` | POST | 登出 |
| `/api/user/info` | GET | 当前用户信息 |
| `/api/csrf` | GET | 获取 CSRF Token |

### MCP 服务
| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/mcp/sse/browser` | GET/POST | 浏览器 MCP SSE 服务 |
| `/api/mcp/sse/computer` | GET/POST | 桌面 MCP SSE 服务 |
| `/api/browser/ws` | GET (WS) | 浏览器插件 WebSocket |
| `/api/computer/ws` | GET (WS) | 桌面客户端 WebSocket |

### 管理（超管）
| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/admin/users` | GET | 用户列表 |
| `/api/admin/users/create` | POST | 创建用户 |
| `/api/admin/container/start` | POST | 启动容器 |
| `/api/admin/container/stop` | POST | 停止容器 |
| `/api/admin/container/restart` | POST | 重启容器 |
| `/api/admin/images` | GET | 镜像列表 |
| `/api/admin/images/upgrade` | POST (SSE) | 镜像升级 |
| `/api/admin/groups` | GET | 分组列表 |
| `/api/admin/skills` | GET | Skill 列表 |
| `/api/admin/config/apply` | POST | 配置下发 |

完整 API 文档请参考 `CLAUDE.md`。

## 技术栈

| 组件 | 技术 |
|------|------|
| 服务端 | Go 1.22+, Docker Engine SDK, SQLite, gorilla/websocket |
| 容器镜像 | Debian 13, PicoClaw, Node.js, Python, Chromium |
| 浏览器插件 | Chrome Extension Manifest V3 |
| 桌面客户端 | Python 3.10+, PySide6, PyAutoGUI, RapidOCR |
| 网络 | Docker Bridge (100.64.0.0/16), MCP over SSE+WebSocket |

## 项目结构

```
picoaide/
├── cmd/picoaide/           # CLI 入口（init, serve, reset-password）
├── internal/
│   ├── auth/               # SQLite 用户/容器/分组/MCP Token
│   ├── config/             # 全局配置（DB 存储 + YAML 迁移）
│   ├── docker/             # Docker 容器/网络/镜像管理
│   ├── ldap/               # LDAP 认证和分组同步
│   ├── user/               # 用户生命周期、配置合并、Cookie 同步
│   ├── util/               # 深拷贝、文件操作、参数解析
│   └── web/                # HTTP API、MCP 中继、WebSocket Hub
├── docker/
│   ├── Dockerfile          # 容器镜像定义
│   └── entrypoint.sh       # 容器启动脚本
├── picoaide-extension/     # Chrome 浏览器插件
│   ├── background.js       # Service Worker + 浏览器工具执行
│   ├── popup.html/js       # 弹出窗口（登录、Cookie 同步）
│   ├── admin/              # 管理后台（用户/镜像/分组/Skill/配置）
│   └── manifest.json
├── picoaide-desktop/       # 桌面客户端
│   ├── core/
│   │   ├── executor.py     # 15 个 Computer Use 工具实现
│   │   ├── connection.py   # WebSocket 连接和工具调度
│   │   ├── permissions.py  # 权限分组和工具映射
│   │   └── config.py       # 客户端配置
│   ├── ui/                 # PySide6 界面（登录/主窗口/暗色主题）
│   ├── main.py             # 入口
│   └── requirements.txt
└── picoaide-desktop/       # 桌面客户端
```

## 构建

```bash
# 服务端
go build -o picoaide ./cmd/picoaide/

# 交叉编译
GOOS=linux GOARCH=arm64 go build -o picoaide ./cmd/picoaide/

# 桌面客户端（需要 Python 3.10+）
cd picoaide-desktop
pip install -r requirements.txt
pyinstaller --onefile main.py
```

## License

Private - All Rights Reserved
