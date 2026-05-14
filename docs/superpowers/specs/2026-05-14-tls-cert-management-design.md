# TLS 证书管理设计文档

## 概述

重构 PicoAide 的 HTTPS 配置方式：
- 删除可配置的监听地址，固定监听 `:80`（HTTP）/ `:443`（HTTPS）
- HTTPS 改为简单开关
- 支持通过 Web UI 上传 cert.pem 和 key.pem
- 上传时自动校验证书域名与浏览器 Host 头是否匹配
- 校验通过后自动启用 HTTPS 重定向并重启服务

## 修改清单

### 文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `internal/config/config.go` | 修改 | TLSConfig 增加 CertPEM/KeyPEM 字段，删除 CertFile/KeyFile 引用 |
| `internal/web/server.go` | 修改 | 固定 :80 监听，HTTPS 从内存加载证书监听 :443，Docker 内网兼容 |
| `internal/web/handlers.go` | 修改 | setSessionCookie 的 Secure 标记逻辑不变 |
| `internal/web/admin_tls.go` | **新增** | TLS 管理 handler：upload/status/clear |
| `internal/web/server.go` | 修改 | 注册新路由 |
| `internal/user/picoclaw_fixups.go` | 修改 | containerBaseURL 写死 http://100.64.0.1:80 |
| `cmd/picoaide/main.go` | 修改 | 删除 init 中的 `-listen` 相关逻辑 |
| `internal/config/config.go` | 修改 | systemd 模板删除 `-listen` 参数 |
| `internal/web/ui/admin/modules/tls.js` | **新增** | TLS 配置页面 JS |
| `internal/web/ui/admin/templates/tls.html` | **新增** | TLS 配置页面 HTML |
| `internal/web/ui/admin/nav.html` | 修改 | 添加 /admin/tls 导航入口 |

## 数据模型

### TLSConfig (internal/config/config.go)

```go
type TLSConfig struct {
  Enabled bool
  CertPEM string  // PEM 编码的证书内容
  KeyPEM  string  // PEM 编码的私钥内容
}
```

### DB settings 表

| 键 | 类型 | 说明 |
|----|------|------|
| `web.tls.enabled` | bool | HTTPS 启用/禁用 |
| `web.tls.cert_pem` | string (text) | PEM 格式证书全文 |
| `web.tls.key_pem` | string (text) | PEM 格式私钥全文 |
| ~~`web.tls.cert_file`~~ | ~~string~~ | **删除** |
| ~~`web.tls.key_file`~~ | ~~string~~ | **删除** |
| ~~`web.listen`~~ | ~~string~~ | 不再通过 UI 编辑，内部默认 `:80` |

### LoadFromDB 和 configToKV 更新

```go
// LoadFromDB 新增
cfg.Web.TLS.CertPEM = kv["web.tls.cert_pem"]
cfg.Web.TLS.KeyPEM = kv["web.tls.key_pem"]

// configToKV 新增
kv["web.tls.cert_pem"] = cfg.Web.TLS.CertPEM
kv["web.tls.key_pem"] = cfg.Web.TLS.KeyPEM
```

### buildNested/flattenConfig

`web.tls.cert_pem` 和 `web.tls.key_pem` 作为普通字符串键处理（含换行符的 PEM 文本）。它们不会被 `boolKeys` 或 `jsonBlobKeys` 匹配，所以自然作为字符串存储。

## API 设计

### POST /api/admin/tls/upload

上传/更新 TLS 证书，并同时控制 HTTPS 开关。

**请求**: `multipart/form-data`
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `cert` | file | 启用时必填 | PEM 编码的证书文件 |
| `key` | file | 启用时必填 | PEM 编码的私钥文件 |
| `enabled` | string | 是 | `"true"` 或 `"false"` |
| `csrf_token` | string | 是 | CSRF 令牌 |

**逻辑**:

当 `enabled=true`:
1. 读取 cert 和 key 文件内容
2. PEM 解码 → `x509.ParseCertificate`
3. 提取 Subject.CommonName 和 DNSNames（SANs）
4. 从请求 Host 头获取当前域名（去端口）
5. 域名匹配校验：Host 与 CN/SANs 任一匹配（大小写不敏感）
6. 不匹配 → 返回 `400 { success:false, error:"证书域名不匹配..." }`
7. 匹配 → 写入 DB（cert_pem, key_pem, enabled=true）
8. 更新内存配置（s.cfg）
9. 启动 goroutine: sleep(1s) → `systemctl restart picoaide`
10. 返回成功

当 `enabled=false`:
1. 设置 enabled=false 写入 DB
2. 证书数据保留（不清除）
3. 启动 goroutine 重启
4. 返回成功

**响应**:
```json
{
  "success": true,
  "message": "证书已保存，服务重启中，请稍后通过 https://example.com 访问",
  "cert_info": {
    "subject": "CN=example.com",
    "sans": ["example.com", "www.example.com"],
    "issuer": "CN=R3, O=Let's Encrypt",
    "not_before": "2025-05-14T00:00:00Z",
    "not_after": "2027-05-14T00:00:00Z"
  }
}
```

### GET /api/admin/tls/status

查询当前 TLS 配置状态。

**响应**:
```json
{
  "success": true,
  "enabled": true,
  "configured": true,
  "cert_info": {
    "subject": "CN=example.com",
    "sans": ["example.com", "www.example.com"],
    "issuer": "CN=R3, O=Let's Encrypt",
    "not_before": "2025-05-14T00:00:00Z",
    "not_after": "2027-05-14T00:00:00Z"
  }
}
```

未配置时:
```json
{
  "success": true,
  "enabled": false,
  "configured": false
}
```

### POST /api/admin/tls/clear

清空已存储的证书数据（不改变 enabled 状态）。

## 服务启动变更

`web.Serve()` 逻辑简化（`server.go`）：

```
inputs: cfg.GlobalConfig

always:
  listenAddr = ":80"

if cfg.Web.TLS.Enabled && CertPEM != "" && KeyPEM != "":
  1. tls.X509KeyPair([]byte(CertPEM), []byte(KeyPEM)) → tls.Certificate
  2. 启动 :80 redirectServer:
     - Docker 内网请求 → appHandler（放行 HTTP）
     - 外部请求 → 301 重定向到 HTTPS
  3. 启动 :443 HTTPS server:
     - TLSConfig.Certificates = [cert]
     - ListenAndServeTLS("", "")  // 从内存加载
else:
  启动 :80 HTTP server
```

## 前端设计

### 页面路径

`/admin/tls` — 独立 TLS 配置页面（导航名称："证书配置"）

### 页面结构

- **状态卡片**: 显示 HTTPS 启用状态、证书域名、颁发者、过期时间、SANs
- **HTTPS 开关**: Select 组件（开启/关闭）
- **证书上传区**（HTTPS 开启时显示）:
  - 已配置: 显示当前证书摘要 + "替换证书" 按钮
  - 未配置: 显示 cert.pem + key.pem 文件选择器
- **操作按钮**: 重置、保存配置

### 前端文件

- `internal/web/ui/admin/templates/tls.html` — 页面模板
- `internal/web/ui/admin/modules/tls.js` — 页面逻辑（export init 函数）
  - loadConfig(): GET /api/admin/tls/status
  - saveConfig(): POST /api/admin/tls/upload (multipart)
  - renderStatus(): 根据状态渲染页面

### 导航

在 `nav.html` 中添加 `/admin/tls` 入口，名称为"证书配置"，位于"系统配置"附近。

## 影响分析

### 向下兼容

- `web.listen` 仍可从 DB 读取，但 UI 不再展示，始终为 `:80`
- 旧系统升级：已配置的 `web.tls.cert_file` 和 `web.tls.key_file` 路径值被忽略
- 已有 cert_pem/key_pem 数据的用户不受影响

### containerBaseURL

`internal/user/picoclaw_fixups.go` 中的 `containerBaseURL()` 不再引用 `cfg.Web.Listen`，直接返回 `http://100.64.0.1:80`。Docker 内网容器始终通过 HTTP 访问 picoaide API。

### systemd 服务

模板中删除 `-listen` 参数：
```
ExecStart=/usr/sbin/picoaide
```

`init` 子命令中的 `cfg.Web.Listen = ":80"` 保留（默认值），但不再写入 systemd 模板。

## 安全考虑

- 私钥以明文 PEM 存储在 SQLite 数据库中
- DB 文件权限应限制为仅 root 可读（程序以 root 运行）
- 证书上传 API 需要超管权限和 CSRF 校验
- 域名匹配校验防止证书被替换为其他域名的证书
- TLS 最低版本限制为 `tls.VersionTLS12`
- HTTP→HTTPS 重定向使用 301 Moved Permanently

## 测试策略

- **单元测试**: 证书解析、域名匹配逻辑（`x509.ParseCertificate` + CN/SANs 提取）
- **集成测试**: upload API 端点（模拟 multipart 上传）、status API、enable/disable 流程
- **不测试**: 实际的 `systemctl restart`（集成环境覆盖）
- **测试文件**: `internal/web/admin_tls_test.go`
