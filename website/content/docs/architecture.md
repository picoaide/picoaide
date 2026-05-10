---
title: "架构概览"
description: "PicoAide 系统架构、认证、容器隔离和 MCP 中继说明"
weight: 2
draft: false
---

PicoAide 由一个 Go 服务端、多个用户级 PicoClaw 容器、浏览器扩展和桌面客户端组成。服务端不直接执行浏览器或桌面动作，而是作为认证、配置、容器和 MCP 消息的控制平面。

## 组件边界

| 组件 | 代码位置 | 职责 |
| --- | --- | --- |
| 服务端 | `cmd/picoaide`, `internal/web` | HTTP API、管理后台、认证、MCP 中继、WebSocket 中继 |
| 配置层 | `internal/config`, `internal/auth` | SQLite 配置、用户、组、白名单、会话密钥 |
| 容器层 | `internal/docker`, `internal/user` | Docker 网络、容器生命周期、用户工作区 |
| 浏览器执行端 | `picoaide-extension` | 当前浏览器标签页控制、Cookie 同步 |
| 桌面执行端 | `picoaide-desktop` | 截图、鼠标、键盘、OCR、白名单文件访问 |
| 官网 | `website` | Hugo 静态站点和文档 |

整体调用路径：

```text
用户 / 管理员
  -> PicoAide Server
     -> SQLite 配置和状态
     -> Docker Engine
     -> 用户 PicoClaw 容器
     -> MCP SSE
        -> WebSocket 执行端
           -> 浏览器扩展 / 桌面客户端
```

## 认证模型

服务端支持本地认证和 LDAP 认证。登录接口先尝试本地认证，再按配置尝试 LDAP。

本地认证流程：

```text
POST /api/login
  -> auth.AuthenticateLocal
  -> 生成 HMAC session cookie
  -> 首次普通用户登录时异步初始化用户容器目录
```

LDAP 认证流程：

```text
POST /api/login
  -> ldap.Authenticate
  -> 白名单检查
  -> 首次登录异步初始化用户
  -> 异步同步用户 LDAP 组
  -> 生成 HMAC session cookie
```

会话 Cookie 名称是 `session`，有效期为 24 小时。CSRF token 按登录用户和小时窗口生成，表单 POST 请求通过 `csrf_token` 字段校验。

超管和普通用户的边界不同：

- 超管可以访问 `/admin/*` 和管理 API。
- 普通用户访问 `/manage`，管理自己的渠道配置、文件和本地密码。
- 超管不能登录浏览器扩展，也不能获取普通用户 MCP token。

## 容器隔离

PicoAide 为每个普通用户分配独立 PicoClaw 容器。用户目录位于：

```text
<data-dir>/users/<username>/
  .picoclaw/
    config.json
    .security.yml
    workspace/
      skills/
```

Docker 网络使用 `picoaide-net`：

| 属性 | 值 |
| --- | --- |
| 子网 | `100.64.0.0/16` |
| 网关 | `100.64.0.1` |
| 容器 IP | 从 `100.64.0.2` 开始分配 |
| 容器间通信 | ICC=false |

服务端会在启动时初始化 Docker 客户端并确保网络存在。如果 Docker 不可用，管理后台仍可启动，但容器相关操作会返回服务不可用。

## MCP 中继

PicoAide 注册两个 MCP SSE 服务：

| 服务 | SSE 端点 | 执行端 WebSocket |
| --- | --- | --- |
| browser | `/api/mcp/sse/browser` | `/api/browser/ws` |
| computer | `/api/mcp/sse/computer` | `/api/computer/ws` |

MCP 调用流程：

```text
PicoClaw 容器
  -> GET /api/mcp/sse/{service}?token=...
  -> POST /api/mcp/sse/{service}?token=...
  -> tools/list
  -> tools/call
  -> PicoAide 查找同用户 WebSocket 执行端
  -> 转发命令并等待结果
```

`/api/browser/ws` 和 `/api/computer/ws` 只给执行端连接使用。AI 容器不应该直接调用 WebSocket 地址。

如果执行端未连接，工具调用返回类似：

```text
picoaide-browser 代理未连接
picoaide-computer 代理未连接
```

## 配置下发

全局配置在 SQLite 中，用户级配置写入：

- `config.json`
- `.security.yml`

保存渠道配置或应用全局配置后，服务端会重启对应容器。升级镜像时，代码还会通过 Picoclaw adapter 的 migration 规则检查配置版本是否可升级。

Picoclaw adapter 的默认来源：

- 本地 bundled `rules/picoclaw`
- `PICOAIDE_RULE_CACHE_DIR`
- `~/.picoaide-config.yaml`
- `PICOAIDE_PICOCLAW_ADAPTER_URL`
- 默认远端 `https://raw.githubusercontent.com/picoaide/picoaide/main/rules/picoclaw`
