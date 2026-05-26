---
title: "架构设计"
description: "PicoAide 系统架构、设计哲学、组件关系和关键架构决策详解"
weight: 2
draft: false
---

## 设计哲学

PicoAide 的核心理念可以概括为一句话：

> **AI 的力量应当被释放，但数据的边界必须被守护。**

这决定了 PicoAide 的架构走向——它不是一个直接把 AI 能力暴露给用户的管理工具，而是一个**控制平面**。所有 AI 操作都在明确定义的边界内执行：容器隔离了执行环境，认证继承了组织权限，MCP 中继控制了工具访问范围，文件系统通过沙盒路径限制了数据读写。

PicoAide 不试图替代大模型或 Agent 框架。它解决的是企业在引入 AI 操作助手时面临的三个核心矛盾：

1. **能力 vs 安全** — AI 需要操作真实环境才有价值，但操作权限必须精确可控
2. **统一 vs 个性化** — 管理需要统一策略，但每位员工的工作场景和技能需求各不相同
3. **集成 vs 独立** — AI 需要接入浏览器、桌面、文件系统、IM 平台，但不能拥有不受限的访问权

## 系统总览

PicoAide 由一个 Go 服务端、多个用户级容器沙箱、浏览器扩展和桌面客户端组成。服务端不直接执行浏览器或桌面动作，而是作为认证、配置、容器和 MCP 消息的控制平面。

```text
┌──────────────┐     ┌──────────────────────┐     ┌──────────────────────┐
│  管理员/用户   │────▶│  PicoAide Server      │────▶│   SQLite (xorm ORM)   │
│  (浏览器)     │     │  (Go + Gin)          │     │  用户/组/配置/技能    │
└──────────────┘     └─────────┬────────────┘     └──────────────────────┘
                               │
          ┌────────────────────┼──────────────────────┐
          ▼                    ▼                      ▼
   ┌────────────┐     ┌───────────────┐     ┌──────────────────┐
   │ OverlayFS  │     │  MCP SSE 端点  │     │  WebSocket 中继   │
   │ Sandbox×N  │     │  agent/browser │     │  (ServiceHub)    │
   │ (picoagent)│     │  /computer     │     │                   │
   └────────────┘     └───────┬───────┘     └────────┬─────────┘
                              │                      │
                              ▼                      ▼
                       ┌──────────────┐      ┌──────────────────┐
                       │ 浏览器扩展    │      │ 桌面客户端        │
                       │ (Chrome MV3) │      │ (Python PyQt)    │
                       └──────────────┘      └──────────────────┘
```

## 五大设计决策

### 一、控制平面模式

PicoAide 不做 Agent 框架，不做大模型推理。它做一个清晰的**控制平面**：

- **认证**：统一管理用户身份，通过 Provider 注册表模式支持 Local / LDAP / OIDC 等多种认证源，`init()` 自动注册，新增认证源无需改动核心代码
- **配置**：在 SQLite 中以展平键值对存储全局配置，支持实时修改和认证源切换自动清理
- **沙箱**：通过 overlayfs + network namespace 管理用户沙箱生命周期，不依赖 Docker，使用原生 Linux 内核能力
- **中继**：MCP SSE + WebSocket 负责消息转发，不参与执行；支持第三方 MCP 服务器代理和平台内置工具

这个边界带来的好处：PicoAide 不绑定任何大模型，不限定任何 Agent 框架。用户容器内跑什么 AI 软件由管理员决定，PicoAide 只负责供给和连接。

### 二、原生 Linux 沙箱隔离

每位员工拥有独立的 AI 沙箱容器。这不是为了性能，而是为了**安全边界**：

- 容器使用独立网络 `picoaide-br`（100.64.0.0/16），iptables DROP 规则禁止容器间通信
- OverlayFS 架构：下层是 Alpine rootfs（只读），上层是 tmpfs（临时），用户工作目录通过 bind mount 挂载
- 一个用户的 AI Agent 无法访问其他用户的容器、网络或数据
- 删除用户时容器和目录会归档到 `archive/`，不会留下残留
- 网络使用 veth pair 连接沙箱网络命名空间和宿主机网桥
- 通过 iptables SNAT 实现容器出站访问能力

容器使用 `picoagent` 自定义二进制（Go 语言编写）作为沙箱内的 AI Agent，而非通用 Docker 镜像。沙箱通过 Unix socket（`picoaide.sock`，权限 0700）与主服务通信，获取配置和 MCP 工具。

### 三、认证可插拔（Provider 注册表模式）

PicoAide 不创造新的身份体系，而是继承已有的。通过 `internal/authsource` 包的 Provider 注册表模式，系统将认证源抽象为三个能力接口：

| 接口 | 能力 | 实现认证源 |
|------|------|-----------|
| `PasswordProvider` | 用户名密码认证 | Local、LDAP |
| `BrowserProvider` | 浏览器跳转登录 | OIDC |
| `DirectoryProvider` | 可枚举目录用户和组 | LDAP |

设计要点：

- **`init()` 自动注册**：每种认证源在文件内通过 `init()` 调用 `Register("name", ProviderType{})` 注册，无需手动导入
- **超管逃生通道**：无论哪种认证模式，超管账户始终在本地存储并优先走本地认证。即使 LDAP 服务器宕机或 OIDC Provider 不可用，管理员也能用本地超管密码登录系统
- **切换清理**：切换认证模式时系统自动执行 `purgeOrdinaryAuthProviderStateForConfig()`，清理旧模式下的所有普通用户、组、容器记录、用户目录和 IM 连接

### 四、双层 HTTP 架构（沙箱感知）

PicoAide 使用**双层 Gin 引擎**架构，实现沙箱内外差异化的 API 暴露：

- **外部处理器**（`buildExternalHandler`）：注册全部路由（UI + 认证 + 所有 API），面向浏览器管理员和普通用户
- **内部处理器**（`buildInternalHandler`）：仅注册沙箱内部需要的最小 API 子集（健康检查、picoagent 配置、文件管理、MCP/WebSocket），供沙箱内 `picoagent` 使用
- **沙箱感知分发**（`sandboxAwareHandler`）：通过检查请求源 IP 是否来自 `100.64.0.0/16` 自动分发到不同处理器
- **Unix Socket**：额外在 `picoaide.sock`（权限 0700）监听内部处理器，供宿主机上 picoagent 进程本地通信

所有 API 路由均注册在 `/api` 和 `/api/v1` 双前缀下，确保兼容性。

### 五、MCP 三层中继 + 工具聚合

PicoAide 使用 Model Context Protocol（MCP）作为 AI 与外部工具的通信协议。架构分为三层，并通过 `init()` 注册表统一管理服务：

```text
用户沙箱容器 (picoagent)
        │
        ▼  SSE 连接 (MCP 协议)
┌───────────────────────────────┐
│  MCP Service 层               │
│  ─ initialize / tools/list    │
│  ─ tools/call 路由到执行端    │
│  服务注册表：browser/computer  │
│  → WebSocket 代理；agent → 服务端处理 │
├───────────────────────────────┤
│  ServiceHub 层                │
│  ─ 用户级 WebSocket 连接管理   │
│  ─ 命令转发 / 30s 超时         │
│  ─ Ping 保活 / 断线清理        │
├───────────────────────────────┤
│  执行端层                      │
│  ─ 浏览器扩展 (browser)        │
│  ─ 桌面客户端 (computer)       │
│  ─ 平台工具 (agent, 服务端)    │
│  ─ 第三方 MCP 服务器 (stdio/SSE/HTTP) │
└───────────────────────────────┘
```

**工具聚合**：`agent` 服务独有地聚合了所有工具来源——当 AI 调用 `tools/list` 时，返回列表包含：
1. 平台工具（`picoaide_*`，6 个）
2. 浏览器工具（`browser_*`，19 个，需扩展在线）
3. 桌面工具（`computer_*`，15 个，需客户端在线）
4. 第三方 MCP 工具（`mcp_<name>_*`，需服务器授权）

## 组件与代码组织

| 组件 | 代码位置 | 职责 |
|------|---------|------|
| CLI 入口 | `cmd/picoaide/` | 命令路由（init/serve/reset-password），初始化引导，超管设置 |
| HTTP 服务 | `internal/web/` | Gin 路由、所有 API handler、MCP SSE、WebSocket 代理、任务队列 |
| 配置管理 | `internal/config/` | GlobalConfig 结构体、YAML/JSON 加载保存、展平 KV 存储 |
| 数据库 | `internal/auth/` | xorm ORM、9 张表、密码哈希、迁移系统 |
| 认证源 | `internal/authsource/` | Provider 注册表、LDAP/OIDC 实现、sync 编排 |
| 用户管理 | `internal/user/` | 目录创建、IP 分配、白名单、配置合并下发 |
| 沙箱管理 | `internal/sandbox/` | OverlayFS + network namespace 容器管理 |
| 日志 | `internal/logger/` | 结构化 slog + JSON + lumberjack 轮转 |
| 浏览器扩展 | `picoaide-extension/` | Chrome Manifest V3 扩展 |
| 桌面客户端 | `picoaide-desktop/` | Python PyQt 桌面应用 |
| 官网 | `website/` | Hugo 静态站点和文档 |

## 安全边界总结

| 层面 | 机制 | 说明 |
|------|------|------|
| 容器 | OverlayFS + network namespace | 用户间完全隔离，iptables DROP 阻止容器间通信 |
| 网络 | `picoaide-br` 桥接 + SNAT | 100.64.0.0/16 CGNAT 地址，不与企业内网冲突 |
| API | 双层 Gin 引擎 | 沙箱内部只能访问最小 API 子集 |
| 会话 | HMAC-SHA256 签名 Cookie | 24 小时有效期，HttpOnly + SameSite=Lax |
| CSRF | 滚动时间窗口 HMAC 令牌 | 基于会话密钥 + 用户 + 小时窗口派生 |
| 速率 | 内存限流器 | 登录接口 10 次/5 分钟 |
| 文件 | `os.Root` 沙盒路径 | 限制在用户工作区内，防止路径遍历 |
| MCP | Bearer Token 认证 | 每用户独立 Token，超管不能获取 |
