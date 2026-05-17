# PicoAide

> 企业级 AI PaaS 工作平台 — 让每位员工拥有自己的 AI 操作助手

PicoAide 是一个私有化部署的 AI 代理管理平台。通过 Docker 容器为每位员工分配独立的 AI 助手，依托 Cookie 授权机制天然继承企业内部平台的权限控制，实现浏览器自动化、桌面控制、技能分发，同时确保企业数据不外流。

## 设计理念

> **AI 的力量应当被释放，但数据的边界必须被守护。**
>
> 企业拥抱 AI 的最大障碍，不是技术，而是信任。当企业数据流向公共 API，每一次调用都是一次不可逆的风险。真正的问题不是"要不要用 AI"，而是"如何在享受 AI 能力的同时，让数据寸步不离"。
>
> PicoAide 的答案是：让 AI 进入企业，而不是让数据离开企业。聪明的模型在内部完成思考与创造，经济的小模型在本地执行操作。数据从生成到消费，全生命周期都在企业边界之内。权限不是被重新设计的，而是从 Web 时代自然继承的 —— AI 使用员工的身份操作，自然也只能看到员工能看到的东西。
>
> 安全不应该是 AI 的对立面，而应该是 AI 的基础设施。

---

### 分层模型架构：大模型造工具，小模型用工具

```
┌──────────────────────────────────────────────────────────┐
│                      企业管理员                           │
│                                                          │
│   使用 DeepSeek / GPT-4o 等大模型                         │
│   在测试环境中分析企业内部网站（CRM、OA、ERP...）            │
│   将网站操作封装为 CLI 工具                                │
│                                                          │
│   ┌─────────────────────────────────────────────────┐    │
│   │  Skill CLI 工具示例：                             │    │
│   │                                                  │    │
│   │  crm get-orders --date 2024-01 --status shipped  │    │
│   │  oa submit-leave --type annual --days 3          │    │
│   │  erp query-inventory --warehouse BJ-01           │    │
│   │                                                  │    │
│   │  本质：模拟浏览器发送 HTTP 数据包                   │    │
│   │  测试环境开发 → 上线时切换正式域名 → 直接可用        │    │
│   └─────────────────────────────────────────────────┘    │
│                                                          │
└──────────────────────────┬───────────────────────────────┘
                           │ 通过 PicoAide 分发 Skill
                           ▼
┌──────────────────────────────────────────────────────────┐
│                      企业员工                             │
│                                                          │
│   使用 Qwen3-27B 等私有化小模型                           │
│   通过自然语言对话调用 CLI 工具                            │
│   无需了解技术细节，只需说"帮我查一下上周的订单"            │
│                                                          │
│   用户：查一下上周北京仓库的库存                           │
│   AI：  调用 erp query-inventory --warehouse BJ-01 ...   │
│   AI：  北京仓库当前库存如下：...                          │
│                                                          │
└──────────────────────────────────────────────────────────┘
```

**大模型负责「造工具」**：企业管理员使用聪明的大模型（DeepSeek、GPT-4o）分析企业内部网站的接口和页面，将其封装为 CLI 工具。大模型做的是理解网站结构、抓取接口、编写数据包模拟脚本。测试环境开发完成后，只需将域名切换为正式环境，CLI 工具即可直接使用。

**小模型负责「用工具」**：企业员工使用私有化部署的经济型小模型（Qwen3-27B 等）进行日常工具调用。小模型只需要知道有哪些 CLI 工具可用、如何传参，成本低、响应快、数据不出企业。

### 权限隔离：CLI 工具天然继承 Web 权限

```
┌──────────────────────────────────────────────────────────┐
│                      Skill CLI 工具                       │
│                                                          │
│   ┌─────────┐    ┌──────────────────────────────────┐    │
│   │  AI 模型 │───▶│  执行 CLI 命令，等待结果返回       │    │
│   │         │    │  不接触 Cookie / Token / 登录凭据  │    │
│   └─────────┘    └──────────┬───────────────────────┘    │
│                             │                             │
│                             ▼                             │
│                  ┌─────────────────────┐                  │
│                  │   CLI 工具内部       │                  │
│                  │   自动携带用户授权   │                  │
│                  │   发送 HTTP 数据包   │                  │
│                  └──────────┬──────────┘                  │
│                             │                             │
└─────────────────────────────┼─────────────────────────────┘
                              ▼
               ┌───────────────────────────┐
               │     企业内部系统            │
               │  OA / CRM / ERP / 自研    │
               │                           │
               │  CLI 使用用户自己的身份     │
               │  天然继承该用户的所有权限   │
               └───────────────────────────┘
```

- AI 的 Prompt 中 **不包含登录凭据**，CLI 工具内部自动处理认证
- 企业内部系统 **无需为 AI 重新设计权限体系**
- CLI 工具使用用户自己的 Cookie / Token 发起请求，天然继承该用户在系统中的所有权限
- 用户权限天然隔离：张三的 AI 只能操作张三有权限的数据，李四的 AI 只能操作李四的数据
- 测试环境和生产环境使用同一套 CLI 工具，只需切换域名配置

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
- **认证体系** — 本地认证 / LDAP / OIDC，AuthProvider 注册表模式，会话 Cookie + CSRF 保护
- **分组管理** — 支持层级分组，LDAP 自动同步
- **配置下发** — 全局配置合并到用户配置，支持批量队列部署
- **镜像管理** — 拉取、升级（按组/按用户选择），队列逐步重启
- **Skill 管理** — Git 仓库源、注册中心、分组绑定、自动部署、安装策略
- **MCP 中继** — SSE + WebSocket 双层代理，Browser / Computer 双通道
- **用户技能管理** — 用户可自助安装/卸载技能，支持技能来源追溯
- **团队空间（共享文件夹）** — 按组设置可见范围，容器内自动挂载
- **MCP Cookie 管理** — 用户可查看/取消已授权的 Cookie 域名
- **TLS 证书管理** — 上传和管理 TLS 证书
- **白名单** — LDAP 模式下控制哪些用户可以访问
- **文件管理** — 用户工作空间文件上传/下载/编辑

### 浏览器插件 (PicoAide Helper)

- **AI 浏览器控制** — 授权 AI 代理操作当前标签页（导航、点击、输入、截图）
- **Cookie 同步** — 将当前页面登录态同步给 AI，模拟用户身份操作
- **管理后台** — 用户管理、镜像管理、分组管理、Skill 部署、配置管理
- **19 个浏览器工具** — navigate、screenshot、click、type、get_content、execute、tabs_list、tab_new、tab_close、go_back、go_forward、reload、current_tab、tab_select、scroll、key_press、get_attribute、get_links、wait

### 桌面客户端

- **AI 桌面控制** — 远程鼠标、键盘、截图操作
- **本地 OCR** — RapidOCR 识别屏幕文字及位置坐标
- **文件操作** — 白名单目录内的文件读写、搜索
- **15 个桌面工具** — screenshot、screen_size、active_window、mouse_click、mouse_move、mouse_drag、mouse_scroll、keyboard_type、keyboard_press、screen_text(OCR)、file_read、file_write、file_list、whitelist、file_search
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

# 全自动初始化
./picoaide init
```

初始化自动完成以下操作：
1. 检测 Docker 和 systemd 可用性
2. 创建数据目录 `/data/picoaide`
3. 初始化 SQLite 数据库和默认配置
4. 创建超管 `admin`（随机密码，写入 `/data/picoaide/secret`）
5. 安装 systemd 服务
6. 打印完成信息

### 启动

```bash
# 手动启动
./picoaide serve

# 或通过 systemd
systemctl start picoaide
```

### 配置 LDAP/OIDC（可选）

通过管理后台「认证配置」页面配置认证源：
- LDAP：服务器地址、Bind DN、Base DN、用户过滤规则、分组同步模式、白名单控制
- OIDC：发行者 URL、Client ID、Client Secret、自动同步用户

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

所有路由均注册在 `/api` 和 `/api/v1` 双前缀下，`/api/version` 为单一路径。

### 基础端点（无需认证）

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/version` | GET | 服务端版本号 |
| `/api/health` | GET | 健康检查 |
| `/api/login` | POST | 登录（本地/LDAP） |
| `/api/login/auth` | GET | 浏览器认证跳转（OIDC） |
| `/api/login/callback` | GET | 浏览器认证回调 |
| `/api/login/mode` | GET | 当前认证模式 |
| `/api/logout` | POST | 登出 |
| `/api/csrf` | GET | 获取 CSRF Token |

### 普通用户端点（需 session Cookie）

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/user/info` | GET | 当前用户信息 |
| `/api/user/password` | POST | 修改密码 |
| `/api/user/skills` | GET | 已安装技能列表 |
| `/api/user/skills/install` | POST | 安装技能 |
| `/api/user/skills/uninstall` | POST | 卸载技能 |
| `/api/user/cookies` | GET | Cookie 域名列表 |
| `/api/user/cookies/delete` | POST | 取消 Cookie 授权 |
| `/api/picoclaw/channels` | GET | 渠道列表 |
| `/api/picoclaw/config-fields` | GET/POST | 渠道配置字段 |
| `/api/config` | GET/POST | Picoclaw 配置 |
| `/api/mcp/token` | GET | MCP token |
| `/api/mcp/sse/:service` | GET/POST | MCP SSE 连接 |
| `/api/mcp/cookies` | GET/POST | MCP Cookie API |
| `/api/browser/ws` | GET (WS) | 浏览器代理 WebSocket |
| `/api/computer/ws` | GET (WS) | 桌面代理 WebSocket |
| `/api/files/*` | GET/POST | 文件管理（CRUD） |
| `/api/dingtalk` | GET/POST | 钉钉配置 |
| `/api/shared-folders` | GET | 可见共享文件夹 |

### 超管端点（需 superadmin 角色）

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/admin/users` | GET/POST | 用户 CRUD |
| `/api/admin/users/batch-create` | POST | 批量导入 |
| `/api/admin/superadmins` | GET/POST | 超管 CRUD |
| `/api/admin/container/*` | POST | 容器启停/重启/调试/日志 |
| `/api/admin/images` | GET/POST | 镜像管理（列表/拉取/删除/迁移/升级） |
| `/api/admin/groups` | GET/POST | 分组 CRUD + 成员管理 |
| `/api/admin/skills` | GET/POST | 技能 CRUD + 部署/仓库/注册中心 |
| `/api/admin/skills/defaults` | GET/POST | 默认技能管理 |
| `/api/admin/auth/*` | GET/POST | 认证源列表/LDAP 测试/用户同步/分组同步 |
| `/api/admin/whitelist` | GET/POST | 白名单管理 |
| `/api/admin/config/apply` | POST | 配置下发 |
| `/api/admin/migration-rules` | GET/POST | 迁移规则管理 |
| `/api/admin/picoclaw/channels` | GET | 渠道管理列表 |
| `/api/admin/shared-folders` | GET/POST | 共享文件夹管理 |
| `/api/admin/tls/status` | GET | TLS 证书状态 |
| `/api/admin/tls/upload` | POST | TLS 证书上传 |
| `/api/admin/task/status` | GET | 异步任务状态 |
| `/api/admin/skill-install-policy` | GET/POST | 技能安装策略 |

完整 API 详情请参考 `AGENTS.md`。

## 技术栈

| 组件 | 技术 |
|------|------|
| 服务端 | Go 1.26+, Docker Engine SDK, SQLite (xorm + modernc), Gin, gorilla/websocket |
| 容器镜像 | Debian 13, PicoClaw, Node.js (NVM), Python (uv), Chromium |
| 浏览器插件 | Chrome Extension Manifest V3 |
| 桌面客户端 | Python 3.10+, PySide6, PyAutoGUI, RapidOCR |
| 网络 | Docker Bridge (100.64.0.0/16), MCP over SSE+WebSocket |

## 项目结构

```
picoaide/
├── .github/                 # GitHub Actions 工作流、PR/Issue 模板
├── assets/brand/            # 品牌资源
├── base-template/           # 容器基础模板配置
├── cmd/picoaide/            # CLI 入口（init, serve, reset-password）
├── docker/
│   ├── Dockerfile           # 容器镜像定义
│   └── entrypoint.sh        # 容器启动脚本
├── docs/                    # 设计文档和开发指南
├── images/                  # 架构图等文档资源
├── internal/
│   ├── auth/                # SQLite 用户/容器/分组/MCP Token
│   ├── authsource/          # 统一认证源抽象（LDAP/OIDC 注册表模式）
│   ├── config/              # 全局配置（DB 存储 + YAML 迁移）
│   ├── docker/              # Docker 容器/网络/镜像管理
│   ├── ldap/                # LDAP 认证和分组同步
│   ├── logger/              # 结构化日志（slog + JSON + 轮转）
│   ├── skill/               # Skill Git 仓库管理、解析、注册中心
│   ├── user/                # 用户生命周期、配置合并、Cookie 同步
│   ├── util/                # 深拷贝、文件操作、参数解析
│   └── web/                 # HTTP API、MCP 中继、WebSocket Hub
├── picoaide-desktop/        # 桌面客户端（Python/PySide6）
│   ├── core/
│   │   ├── executor.py      # 15 个 Computer Use 工具实现
│   │   ├── connection.py    # WebSocket 连接和工具调度
│   │   ├── permissions.py   # 权限分组和工具映射
│   │   └── config.py        # 客户端配置
│   ├── ui/                  # PySide6 界面（登录/主窗口/暗色主题）
│   ├── main.py              # 入口
│   └── requirements.txt
├── picoaide-extension/      # Chrome 浏览器插件
│   ├── background.js        # Service Worker + 19 个浏览器工具
│   ├── popup.html/js/css    # 弹出窗口（登录、Cookie 同步）
│   ├── offscreen.html/js    # 离屏文档处理
│   ├── icons/               # 图标资源
│   └── manifest.json
├── scripts/                 # 构建和部署脚本
├── dist/                    # 构建产物
├── Makefile                 # 构建和测试命令
├── format.sh                # 代码格式化（制表符→空格）
└── website/                 # 项目网站
```

## 构建

```bash
# 服务端
go build -o picoaide ./cmd/picoaide/

# 指定版本构建
GOOS=linux GOARCH=amd64 go build -ldflags "-X github.com/picoaide/picoaide/internal/config.Version=1.0.0" -o picoaide ./cmd/picoaide/

# 交叉编译
GOOS=linux GOARCH=arm64 go build -o picoaide ./cmd/picoaide/

# 全平台发布构建
make release PICOCLAW_VERSION=v1.0.0

# 桌面客户端（需要 Python 3.10+）
cd picoaide-desktop
pip install -r requirements.txt
pyinstaller --onefile main.py

# 运行测试
make test          # 全部测试
make test-go       # Go 单元测试 + 集成测试
make test-python   # 桌面客户端测试
make test-js       # 浏览器插件测试

# 代码检查
make lint          # golangci-lint（govet + ineffassign）
make format        # 制表符替换为空格（或 ./format.sh）
make check         # format + lint + test
```

## License

[MIT License](LICENSE)
