---
title: "架构概览"
description: "PicoAide 系统架构设计"
weight: 2
draft: false
---

PicoAide 采用中心化管理、容器隔离的架构，为企业提供安全可控的 AI 代理服务。

## 系统架构

PicoAide 由以下核心组件构成：

```
┌─────────────────────────────────────────────────────┐
│                   PicoAide Server                    │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │
│  │ Web API  │  │ Docker   │  │  SQLite Database  │  │
│  │ Service  │  │ Manager  │  │  (users/containers)│  │
│  └────┬─────┘  └────┬─────┘  └──────────────────┘  │
│       │              │                               │
│  ┌────┴──────────────┴─────────────────────────┐    │
│  │              MCP Relay Layer                 │    │
│  │    (SSE + WebSocket proxy)                   │    │
│  └────┬──────────────────┬─────────────────────┘    │
│       │                  │                           │
└───────┼──────────────────┼───────────────────────────┘
        │                  │
   ┌────┴─────┐      ┌────┴─────┐
   │ Browser  │      │ Computer │
   │ Extension│      │  Client  │
   └──────────┘      └──────────┘
```

### 服务器

PicoAide Server 是整个平台的核心，负责：

- **用户认证**：支持本地认证和 LDAP 认证，管理用户会话
- **容器生命周期管理**：通过 Docker Engine SDK 创建、启动、停止、删除容器
- **网络管理**：自管 `picoaide-net` 私有网络，为每个容器分配独立 IP
- **配置下发**：统一管理模型配置、安全策略和技能配置
- **MCP 中继**：在 PicoClaw AI 容器和浏览器扩展/桌面客户端之间中继 MCP 协议消息

### 容器架构

每个用户拥有一个独立的 PicoClaw 容器：

```
users/
├── zhangsan/
│   └── .picoclaw/
│       ├── config.json      # 用户配置（JSON）
│       └── .security.yml    # 安全配置（YAML，0600）
├── lisi/
│   └── .picoclaw/
│       ├── config.json
│       └── .security.yml
```

用户目录直接挂载为容器内的 `/root`，容器内运行 PicoClaw AI 代理二进制文件。

## MCP 中继层

PicoAide 通过 MCP（Model Context Protocol）中继层连接 AI 容器与外部工具：

```
PicoClaw AI 容器
    │
    │  MCP SSE 连接
    ▼
PicoAide Server (SSE endpoint)
    │
    │  JSON-RPC over SSE
    │  (tools/list, tools/call)
    ▼
WebSocket 连接
    │
    ├──► 浏览器扩展 (/api/browser/ws)
    │    └── 标签页控制、页面操作
    │
    └──► 桌面客户端 (/api/computer/ws)
         └── 桌面控制、文件操作
```

### SSE 流程

1. AI 容器通过 SSE 连接 PicoAide Server 的 `/api/mcp/sse/browser` 或 `/api/mcp/sse/computer`
2. Server 返回 `endpoint` 事件，包含 POST 端点地址
3. AI 容器通过 POST 端点发送 JSON-RPC 请求（`tools/list`、`tools/call`）
4. Server 查找用户对应的浏览器扩展/桌面客户端 WebSocket 连接
5. 将命令转发到客户端，等待响应并返回给 AI

### 服务注册

PicoAide 注册了两个 MCP SSE 服务：

| 服务名    | 端点                        | 说明               |
| --------- | --------------------------- | ------------------ |
| browser   | `/api/mcp/sse/browser`      | 浏览器标签页控制   |
| computer  | `/api/mcp/sse/computer`     | 桌面控制与文件操作 |

每个服务提供一组 MCP 工具定义，支持 `initialize`、`tools/list`、`tools/call` 标准 JSON-RPC 方法。

## 认证流程

PicoAide 支持两种认证模式：

### 本地认证

```
用户提交用户名+密码
    │
    ▼
bcrypt 验证（本地用户表）
    │
    ▼
生成 HMAC 签名的会话 Cookie
    │
    ▼
后续请求携带 Cookie 鉴权
```

### LDAP 认证

```
用户提交用户名+密码
    │
    ▼
LDAP Bind 验证
    │
    ▼
检查白名单（whitelist.yaml）
    │
    ▼
自动创建/更新本地用户记录
    │
    ▼
同步 LDAP 组到本地组
    │
    ▼
生成 HMAC 签名的会话 Cookie
```

### 会话安全

- 会话使用 HMAC 签名的 Cookie（非 JWT），有效期 24 小时
- CSRF 令牌使用按小时滚动的时间窗口
- MCP Token 格式为 `用户名:随机hex`，用于 AI 容器与工具之间的认证

## 网络隔离

PicoAide 使用自定义 Docker 网络 `picoaide-net` 实现容器隔离：

| 属性       | 值                    | 说明                                   |
| ---------- | --------------------- | -------------------------------------- |
| 网络名称   | `picoaide-net`        | 自动创建和管理的桥接网络               |
| 子网       | `100.64.0.0/16`       | CGNAT 地址空间，不与常见网段冲突       |
| ICC        | `false`               | 禁止容器间通信，确保用户隔离           |
| IP 分配    | 从 `100.64.0.2` 递增  | 每个容器分配唯一静态 IP                |
| 网关       | `100.64.0.1`          | PicoAide Server 所在主机               |

ICC（Inter-Container Communication）设置为 `false`，确保不同用户的容器之间无法直接通信，保障数据安全。

## 配置管理

PicoAide 采用分层配置策略：

```
全局配置 (config.yaml)
    │
    │  util.MergeMap() 合并
    ▼
用户配置 (config.json)
    │
    │  用户已有键值保留
    │  缺失键从全局默认值补充
    ▼
最终生效配置
```

配置文件说明：

| 文件              | 格式   | 用途                       |
| ----------------- | ------ | -------------------------- |
| `config.yaml`     | YAML   | 全局配置（LDAP、镜像、Web）|
| `config.json`     | JSON   | 用户级 PicoClaw 配置       |
| `.security.yml`   | YAML   | 用户级安全配置（API 密钥） |
| `whitelist.yaml`  | YAML   | 用户白名单                 |
| `picoaide.db`     | SQLite | 用户账户和容器状态数据库   |
