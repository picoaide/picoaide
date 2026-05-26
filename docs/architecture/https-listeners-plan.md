# 双监听器 + HTTPS 计划

## 背景

当前架构：单一 HTTP 监听 `:80` + Unix socket。沙箱通过 bridge 网关 `100.64.0.1:80` 以及 Unix socket 与宿主通信。

## 目标架构

```
                    ┌──────────────────┐
                    │    Gin Engine     │
                    │ (externalRouter)  │
                    │  全部路由 + UI    │
                    └──┬────────────┬──┘
                       │            │
              ┌────────┘            └────────┐
              v                               v
    ┌──────────────────┐          ┌──────────────────┐
    │   :80 (redirect)  │          │  :443 (HTTPS)     │
    │  → 443 重定向     │          │  TLS 证书         │
    └──────────────────┘          └──────────────────┘
      只在外网 TLS 启用时生效        只在 TLS 启用时监听

                    ┌──────────────────┐
                    │    Gin Engine     │
                    │ (internalRouter)  │
                    │  沙箱 API 子集    │
                    └────────┬─────────┘
                             │
                             v
                    ┌──────────────────┐
                    │ 100.64.0.1:80     │
                    │ 始终 HTTP         │
                    └──────────────────┘

                    ┌──────────────────┐
                    │   Unix Socket     │
                    │  全部路由 + UI    │
                    │  /run/picoaide.sock│
                    └──────────────────┘
```

## 监听器设计

### 1. 外部监听器 (`0.0.0.0`)

| 条件 | 端口 | 行为 |
|------|------|------|
| TLS 禁用 | `:80` | 正常 HTTP，所有路由 |
| TLS 启用 | `:80` | 301 重定向到 `https://<host>$request_uri` |
| TLS 启用 | `:443` | HTTPS，所有路由 |

路由：全部（`externalRouter`，包含 admin/UI/登录等）

### 2. 内部监听器 (`100.64.0.1`)

| 条件 | 端口 | 行为 |
|------|------|------|
| 始终 | `:80` | HTTP，仅沙箱所需 API 子集 |

路由：`internalRouter`，仅包含：

```
GET    /api/health
GET    /api/picoagent/me
GET    /api/mcp/token
GET    /api/mcp/sse/:service
POST   /api/mcp/sse/:service
GET    /api/mcp/cookies
POST   /api/mcp/cookies
GET    /api/browser/ws
GET    /api/computer/ws
GET    /api/files
POST   /api/files/upload
GET    /api/files/download
POST   /api/files/delete
POST   /api/files/mkdir
GET    /api/files/edit
POST   /api/files/edit
```

### 3. Unix Socket（不变）

路径：`picoaide.sock`
路由：全部（`externalRouter`），供本机管理操作

## 配置变更

### 现有字段（已在 types.go）

```go
type TLSConfig struct {
  Enabled bool
  CertPEM string
  KeyPEM  string
}
```

无需新增配置字段，现有 TLSConfig 直接使用。

### 调试模式

`web.debug_mode` 已在配置中，`Serve()` 中改为：

```go
if cfg.Web.DebugMode {
  gin.SetMode(gin.DebugMode)
} else {
  gin.SetMode(gin.ReleaseMode)
}
```

## 实现步骤

### 第 1 步：提取内部路由注册

将 `registerAPIRoutes` 拆分为两个方法：

```go
func (s *Server) registerInternalAPIRoutes(g *gin.RouterGroup)  // 沙箱子集
func (s *Server) registerExternalAPIRoutes(g *gin.RouterGroup)  // 全部路由
```

`registerExternalAPIRoutes` 调用 `registerInternalAPIRoutes` 再追加 admin 等敏感路由。

### 第 2 步：修改 Serve() 创建双 Gin Engine

```go
externalRouter := gin.New()
externalRouter.Use(gin.Recovery(), s.secureHeaders())
s.RegisterRoutes(externalRouter)        // 全部 UI + API

internalRouter := gin.New()
internalRouter.Use(gin.Recovery(), s.secureHeaders())
s.registerInternalAPIRoutes(internalRouter.Group("/api"))
s.registerInternalAPIRoutes(internalRouter.Group("/api/v1"))
```

### 第 3 步：启动三个 listener

```go
// 内部（沙箱网桥）
internalAddr := "100.64.0.1:80"
internalSrv := &http.Server{Addr: internalAddr, Handler: internalHandler}

// 外部 :80（TLS 启用时重定向）
// 外部 :443（TLS 启用时 HTTPS）

// Unix socket（不变）
```

### 第 4 步：TLS 监听逻辑

```go
cfg := s.loadConfig()
if cfg.Web.TLS.Enabled && cfg.Web.TLS.CertPEM != "" && cfg.Web.TLS.KeyPEM != "" {
  // 启动 :443 HTTPS
  cert, err := tls.X509KeyPair([]byte(cfg.Web.TLS.CertPEM), []byte(cfg.Web.TLS.KeyPEM))
  tlsSrv := &http.Server{
    Addr:      ":443",
    Handler:   externalHandler,
    TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
  }
  // 启动 :80 重定向
  redirectSrv := &http.Server{Addr: ":80", Handler: redirectHandler}
} else {
  // 启动 :80 HTTP
  httpSrv := &http.Server{Addr: ":80", Handler: externalHandler}
}
```

重定向 handler 返回 301 `Location: https://<host>$request_uri`。

### 第 5 步：HTTPS 证书管理 API + UI

按 `docs/superpowers/specs/2026-05-14-tls-cert-management-design.md` 实现：

- `GET /api/admin/tls/status` — 查询证书状态（是否启用、过期时间）
- `POST /api/admin/tls/upload` — 上传 PEM 证书+私钥
- `POST /api/admin/tls/clear` — 清除证书配置
- UI 页面 `/admin/tls`（导航栏 + 表单）

证书存储在当前 `web.tls.cert_pem` / `web.tls.key_pem` 配置字段中（持久化到 `settings` 表）。

### 第 6 步：正式发布 + Graceful Shutdown

所有 listener 共用同一个 shutdown context，收到 SIGTERM/SIGINT 时全部关闭。

## 安全边界

| 来源 | 路由范围 | 说明 |
|------|----------|------|
| 外部网络 (0.0.0.0) | 全部 | 需要认证（session/MCP token） |
| 沙箱网络 (100.64.0.0/16) | 沙箱子集 | 需要 MCP token，无 session 要求 |
| Unix socket | 全部 | 本机进程，隐式信任 |

内部监听器不暴露 `/api/admin/*`、`/api/login`、UI 页面等敏感端点。即使沙箱被攻破，攻击者也无法通过 `100.64.0.1:80` 访问管理功能。
