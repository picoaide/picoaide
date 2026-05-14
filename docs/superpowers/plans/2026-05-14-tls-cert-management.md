# TLS 证书管理 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 移除监听地址配置，改为固定 `:80`/`:443`，HTTPS 改为开关 + 上传证书，自动校验域名并重启服务

**Architecture:** 
- Config 层：`TLSConfig` 新增 `CertPEM`/`KeyPEM` 字段，DB 存储 PEM 全文
- Server 层：固定 `:80` 监听，HTTPS 时从内存加载证书到 `tls.Config`，`:443` 提供 HTTPS，`:80` 提供 Docker 内网兼容 + 外部重定向
- API 层：新增 `admin_tls.go` 处理证书上传/状态查询，上传时校验域名匹配 Host 头
- UI 层：独立页面 `/admin/tls`（导航名"证书配置"），文件上传 + 开关

**Tech Stack:** Go 1.22+, Gin, crypto/x509, crypto/tls, xorm

---

### Task 1: TLSConfig 数据模型变更

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: 修改 TLSConfig 结构体**

```go
type TLSConfig struct {
  Enabled bool
  CertPEM string // PEM 编码的证书内容
  KeyPEM  string // PEM 编码的私钥内容
}
```

删除 `CertFile` 和 `KeyFile` 字段。

- [ ] **Step 2: 更新 LoadFromDB**

在 `internal/config/config.go` 的 `LoadFromDB` 函数中，将 `cert_file`/`key_file` 替换为 `cert_pem`/`key_pem`：

```go
// 删除这两行:
// cfg.Web.TLS.CertFile = kv["web.tls.cert_file"]
// cfg.Web.TLS.KeyFile = kv["web.tls.key_file"]

// 新增这两行:
cfg.Web.TLS.CertPEM = kv["web.tls.cert_pem"]
cfg.Web.TLS.KeyPEM = kv["web.tls.key_pem"]
```

- [ ] **Step 3: 更新 configToKV**

```go
// 删除:
// kv["web.tls.cert_file"] = cfg.Web.TLS.CertFile
// kv["web.tls.key_file"] = cfg.Web.TLS.KeyFile

// 新增:
kv["web.tls.cert_pem"] = cfg.Web.TLS.CertPEM
kv["web.tls.key_pem"] = cfg.Web.TLS.KeyPEM
```

- [ ] **Step 4: 更新 buildNested（boolKeys map）**

`web.tls.enabled` 已在 `boolKeys` 中，无需修改。`cert_pem`/`key_pem` 作为普通字符串处理，不需要特殊处理。

- [ ] **Step 5: 更新 DefaultGlobalConfig**

删除 `WebConfig` 中的 `Listen: ":80"`（保留但不强制），删除 TLS 相关默认值。实际上 `DefaultGlobalConfig` 中的 `Web.Listen` 可以保留默认值 `:80`，但不再展示到 UI。

- [ ] **Step 6: 运行测试确认编译通过**

Run: `go build -o /dev/null ./cmd/picoaide/`
Expected: 编译成功

---

### Task 2: 服务启动逻辑重构

**Files:**
- Modify: `internal/web/server.go`

- [ ] **Step 1: 固定监听地址为 :80**

在 `Serve` 函数开头，删除对 `cfg.Web.Listen` 的读取，直接写死：

```go
func Serve(cfg *config.GlobalConfig) error {
  listenAddr := ":80"  // 固定 :80
  // 删除: if listenAddr == "" { listenAddr = ":80" }
```

- [ ] **Step 2: 重构 TLS 启动逻辑，从内存加载证书**

找到 `if cfg.Web.TLS.Enabled && cfg.Web.TLS.CertFile != "" && cfg.Web.TLS.KeyFile != "" {` 这段，替换为：

```go
if cfg.Web.TLS.Enabled && cfg.Web.TLS.CertPEM != "" && cfg.Web.TLS.KeyPEM != "" {
  // 从内存解析证书
  cert, err := tls.X509KeyPair([]byte(cfg.Web.TLS.CertPEM), []byte(cfg.Web.TLS.KeyPEM))
  if err != nil {
    return fmt.Errorf("解析证书失败: %w", err)
  }

  // :80 重定向服务器（Docker 内网放行 HTTP）
  redirectMux := http.NewServeMux()
  redirectMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    if isDockerNetworkRequest(r) {
      appHandler.ServeHTTP(w, r)
      return
    }
    http.Redirect(w, r, httpsRedirectTarget(r), http.StatusMovedPermanently)
  })
  redirectServer = &http.Server{Addr: ":80", Handler: redirectMux}
  go func() {
    slog.Info("HTTP 入口已启动", "listen", ":80", "internal", dockerpkg.NetworkSubnet, "external", "redirect-to-https")
    if err := redirectServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
      slog.Error("HTTP 入口服务错误", "error", err)
    }
  }()

  // :443 HTTPS 服务器
  srv := &http.Server{
    Addr:    ":443",
    Handler: appHandler,
    TLSConfig: &tls.Config{
      Certificates: []tls.Certificate{cert},
      MinVersion:   tls.VersionTLS12,
    },
  }
  go func() {
    slog.Info("管理面板启动", "url", "https://:443")
    if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
      slog.Error("服务启动失败", "error", err)
    }
  }()

  <-sigCh
  slog.Info("收到终止信号，开始优雅关闭...")
  return gracefulShutdown(srv, redirectServer, dockerOK, s.syncCancel)
}
```

注意：
- `listenAddr` 不再用于判断 `strings.HasSuffix(listenAddr, ":443")`，因为现在 HTTPS 固定 :443
- 删除 `os.Stat` 检查 cert_file/key_file 的逻辑
- 保留 HTTP-only 分支（else 分支）

- [ ] **Step 3: 简化 HTTP-only 分支**

```go
// else 分支（TLS 未启用）
srv := &http.Server{
  Addr:    ":80",
  Handler: appHandler,
}
go func() {
  slog.Info("管理面板启动", "url", "http://:80")
  if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
    slog.Error("服务启动失败", "error", err)
  }
}()
```

- [ ] **Step 4: 运行测试确认编译通过**

Run: `go build -o /dev/null ./cmd/picoaide/`
Expected: 编译成功

---

### Task 3: containerBaseURL 简化

**Files:**
- Modify: `internal/user/picoclaw_fixups.go`

- [ ] **Step 1: 简化 containerBaseURL 函数**

找到 `containerBaseURL` 函数（约第 296 行），简化为固定返回：

```go
func containerBaseURL(cfg *config.GlobalConfig) string {
  return "http://100.64.0.1:80"
}
```

删除涉及 `cfg.Web.Listen`、`cfg.Web.TLS.Enabled`、端口解析的全部逻辑。

- [ ] **Step 2: 运行测试确认通过**

Run: `go test ./internal/user/ -run TestContainerBaseURL -v`
Expected: 所有 containerBaseURL 测试通过（可能需要更新测试期望值）

---

### Task 4: admin_tls handler（核心业务逻辑）

**Files:**
- Create: `internal/web/admin_tls.go`

- [ ] **Step 1: 写入完整 handler 文件**

```go
package web

import (
  "crypto/tls"
  "crypto/x509"
  "encoding/pem"
  "fmt"
  "net"
  "net/http"
  "os/exec"
  "strings"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/config"
  "log/slog"
)

// handleAdminTLSStatus 返回当前 TLS 配置状态
func (s *Server) handleAdminTLSStatus(c *gin.Context) {
  username := s.requireAuth(c)
  if username == "" {
    return
  }
  if !auth.IsSuperadmin(username) {
    writeError(c, http.StatusForbidden, "仅超级管理员可访问")
    return
  }

  resp := map[string]interface{}{
    "success":   true,
    "enabled":   s.cfg.Web.TLS.Enabled,
    "configured": s.cfg.Web.TLS.CertPEM != "" && s.cfg.Web.TLS.KeyPEM != "",
  }

  if resp["configured"].(bool) {
    if info, err := parseCertInfo([]byte(s.cfg.Web.TLS.CertPEM)); err == nil {
      resp["cert_info"] = info
    }
  }

  writeJSON(c, http.StatusOK, resp)
}

// handleAdminTLSUpload 上传证书/启禁 HTTPS
func (s *Server) handleAdminTLSUpload(c *gin.Context) {
  username := s.requireAuth(c)
  if username == "" {
    return
  }
  if !auth.IsSuperadmin(username) {
    writeError(c, http.StatusForbidden, "仅超级管理员可访问")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  enabled := c.PostForm("enabled") == "true"

  if enabled {
    // 读取上传文件
    certFile, err := c.FormFile("cert")
    if err != nil {
      writeError(c, http.StatusBadRequest, "请上传证书文件 (cert.pem)")
      return
    }
    keyFile, err := c.FormFile("key")
    if err != nil {
      writeError(c, http.StatusBadRequest, "请上传私钥文件 (key.pem)")
      return
    }

    // 读取文件内容
    certData, err := certFile.Open()
    if err != nil {
      writeError(c, http.StatusInternalServerError, "读取证书文件失败")
      return
    }
    defer certData.Close()
    keyData, err := keyFile.Open()
    if err != nil {
      writeError(c, http.StatusInternalServerError, "读取私钥文件失败")
      return
    }
    defer keyData.Close()

    certBytes := make([]byte, certFile.Size)
    if _, err := certData.Read(certBytes); err != nil {
      writeError(c, http.StatusInternalServerError, "读取证书文件失败")
      return
    }
    keyBytes := make([]byte, keyFile.Size)
    if _, err := keyData.Read(keyBytes); err != nil {
      writeError(c, http.StatusInternalServerError, "读取私钥文件失败")
      return
    }

    // 验证 PEM 和证书
    if err := validateCertKeyPair(certBytes, keyBytes); err != nil {
      writeError(c, http.StatusBadRequest, "证书校验失败: "+err.Error())
      return
    }

    // 域名匹配校验
    host := c.Request.Host
    if h, _, err := net.SplitHostPort(host); err == nil {
      host = h
    }
    if err := validateCertDomain(certBytes, host); err != nil {
      writeError(c, http.StatusBadRequest, err.Error())
      return
    }

    // 提取证书信息用于响应
    certInfo, _ := parseCertInfo(certBytes)

    // 保存到 DB
    s.cfg.Web.TLS.CertPEM = string(certBytes)
    s.cfg.Web.TLS.KeyPEM = string(keyBytes)
    s.cfg.Web.TLS.Enabled = true
    if err := config.SaveToDB(s.cfg, username); err != nil {
      writeError(c, http.StatusInternalServerError, "保存配置失败: "+err.Error())
      return
    }

    // 异步重启服务
    go func() {
      time.Sleep(1 * time.Second)
      if err := exec.Command("systemctl", "restart", "picoaide").Run(); err != nil {
        slog.Error("重启服务失败", "error", err)
      }
    }()

    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success":   true,
      "message":   fmt.Sprintf("证书已保存，服务重启中，请稍后通过 https://%s 访问", host),
      "cert_info": certInfo,
    })
    return
  }

  // 关闭 HTTPS
  s.cfg.Web.TLS.Enabled = false
  if err := config.SaveToDB(s.cfg, username); err != nil {
    writeError(c, http.StatusInternalServerError, "保存配置失败: "+err.Error())
    return
  }

  go func() {
    time.Sleep(1 * time.Second)
    if err := exec.Command("systemctl", "restart", "picoaide").Run(); err != nil {
      slog.Error("重启服务失败", "error", err)
    }
  }()

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "HTTPS 已关闭，服务重启中...",
  })
}

// validateCertKeyPair 验证证书和私钥是否匹配
func validateCertKeyPair(certPEM, keyPEM []byte) error {
  if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
    return fmt.Errorf("证书和私钥不匹配: %w", err)
  }
  return nil
}

// validateCertDomain 验证证书域名是否匹配请求域名
func validateCertDomain(certPEM []byte, host string) error {
  block, _ := pem.Decode(certPEM)
  if block == nil {
    return fmt.Errorf("无法解析 PEM 格式证书")
  }
  cert, err := x509.ParseCertificate(block.Bytes)
  if err != nil {
    return fmt.Errorf("解析证书失败: %w", err)
  }

  // 收集所有域名
  domains := []string{cert.Subject.CommonName}
  domains = append(domains, cert.DNSNames...)

  // 去空
  var validDomains []string
  for _, d := range domains {
    if strings.TrimSpace(d) != "" {
      validDomains = append(validDomains, d)
    }
  }

  // 匹配（支持通配符）
  for _, d := range validDomains {
    if matchDomain(d, host) {
      return nil
    }
  }

  return fmt.Errorf("证书域名 (%v) 与当前访问域名 (%s) 不匹配", validDomains, host)
}

// matchDomain 支持通配符域名匹配（如 *.example.com 匹配 sub.example.com）
func matchDomain(pattern, host string) bool {
  if strings.EqualFold(pattern, host) {
    return true
  }
  if strings.HasPrefix(pattern, "*.") {
    suffix := pattern[1:] // .example.com
    return strings.HasSuffix(strings.ToLower(host), strings.ToLower(suffix))
  }
  return false
}

// parseCertInfo 从 PEM 证书提取展示信息
func parseCertInfo(certPEM []byte) (map[string]interface{}, error) {
  block, _ := pem.Decode(certPEM)
  if block == nil {
    return nil, fmt.Errorf("无法解析 PEM 格式")
  }
  cert, err := x509.ParseCertificate(block.Bytes)
  if err != nil {
    return nil, err
  }
  return map[string]interface{}{
    "subject":    cert.Subject.String(),
    "sans":       cert.DNSNames,
    "issuer":     cert.Issuer.String(),
    "not_before": cert.NotBefore.Format(time.RFC3339),
    "not_after":  cert.NotAfter.Format(time.RFC3339),
  }, nil
}
```

注意：需要在文件顶部 import `crypto/tls`, `crypto/x509`, `encoding/pem`。

- [ ] **Step 2: 编译确认**

Run: `go build -o /dev/null ./cmd/picoaide/`
Expected: 编译成功

---

### Task 5: 路由注册

**Files:**
- Modify: `internal/web/server.go`

- [ ] **Step 1: 在 RegisterRoutes 中添加 TLS 路由**

在 admin 路由组内添加（在 `admin.GET("/task/status", ...)` 附近）：

```go
admin.GET("/tls/status", s.handleAdminTLSStatus)
admin.POST("/tls/upload", s.handleAdminTLSUpload)
```

- [ ] **Step 2: 编译确认**

Run: `go build -o /dev/null ./cmd/picoaide/`
Expected: 编译成功

---

### Task 6: Systemd 模板更新

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: 删除 systemd 模板中的 -listen 参数**

```go
// 修改前:
ExecStart=/usr/sbin/picoaide serve -listen {{.ListenAddr}}

// 修改后:
ExecStart=/usr/sbin/picoaide
```

- [ ] **Step 2: 更新 ServiceTemplateData**

```go
type ServiceTemplateData struct {
  WorkingDir string
  // 删除 ListenAddr 字段
}
```

- [ ] **Step 3: 更新 InstallService 函数**

```go
func InstallService(cfg *GlobalConfig) error {
  workDir, _ := os.Getwd()
  if workDir == "" {
    workDir = "/data/picoaide"
  }
  // 删除 listenAddr 相关逻辑
  data := ServiceTemplateData{
    WorkingDir: workDir,
  }
  // ...其余不变
}
```

- [ ] **Step 4: 编译确认**

Run: `go build -o /dev/null ./cmd/picoaide/`
Expected: 编译成功

---

### Task 7: 前端页面 — tls.html

**Files:**
- Create: `internal/web/ui/admin/templates/tls.html`

- [ ] **Step 1: 写入页面模板**

```html
<section class="page-header">
  <div>
    <div class="page-kicker">平台配置</div>
    <h2>证书配置</h2>
    <p>管理 HTTPS 加密访问和 SSL 证书。</p>
  </div>
</section>
<div id="tls-msg" class="msg"></div>

<div class="card">
  <div class="card-header">当前状态</div>
  <div id="tls-status">
    <div class="text-sm text-muted">加载中...</div>
  </div>
</div>

<div class="card">
  <div class="card-header">HTTPS 开关</div>
  <div class="field">
    <select id="tls-enabled">
      <option value="false">关闭</option>
      <option value="true">开启</option>
    </select>
  </div>
</div>

<div class="card" id="cert-upload-card" style="display:none">
  <div class="card-header">证书文件</div>
  <div id="current-cert-info" class="text-sm text-muted mb-1"></div>
  <div class="field"><label>证书文件 (PEM)</label><input type="file" id="cert-file" accept=".pem,.crt,.cert"></div>
  <div class="field"><label>私钥文件 (PEM)</label><input type="file" id="key-file" accept=".pem,.key"></div>
  <p class="text-sm text-muted">上传后将校验证书域名与当前浏览器访问的域名是否一致。</p>
</div>

<div class="btn-group mt-2" style="justify-content:flex-end">
  <button class="btn btn-ghost" id="reset-btn">重置</button>
  <button class="btn btn-primary" id="save-btn">保存配置</button>
</div>
```

---

### Task 8: 前端页面 — tls.js

**Files:**
- Create: `internal/web/ui/admin/modules/tls.js`

- [ ] **Step 1: 写入 JS 逻辑**

```javascript
export async function init(ctx) {
  const { Api, showMsg, $ } = ctx;
  var currentStatus = {};

  await loadStatus();

  $('#tls-enabled').addEventListener('change', function() {
    $('#cert-upload-card').style.display = this.value === 'true' ? 'block' : 'none';
  });

  $('#save-btn').addEventListener('click', saveConfig);
  $('#reset-btn').addEventListener('click', async () => {
    if (await confirmModal('重新加载？未保存的修改将丢失。')) loadStatus();
  });

  async function loadStatus() {
    showMsg('#tls-msg', '加载中...', true);
    try {
      var res = await Api.get('/api/admin/tls/status');
      if (!res.success) { showMsg('#tls-msg', res.error, false); return; }
      currentStatus = res;
      renderStatus();
      showMsg('#tls-msg', '');
    } catch (e) { showMsg('#tls-msg', e.message, false); }
  }

  function renderStatus() {
    var el = $('#tls-status');
    if (currentStatus.configured && currentStatus.cert_info) {
      var info = currentStatus.cert_info;
      el.innerHTML =
        '<div class="grid-2">' +
        '  <div>HTTPS: ' + (currentStatus.enabled ? '已启用' : '未启用') + '</div>' +
        '  <div>域名: ' + esc(info.subject) + '</div>' +
        '  <div>颁发者: ' + esc(info.issuer) + '</div>' +
        '  <div>过期: ' + esc(formatDate(info.not_after)) + '</div>' +
        '  <div>SANs: ' + esc((info.sans || []).join(', ')) + '</div>' +
        '</div>';
      $('#current-cert-info').textContent = '当前证书: ' + info.subject;
    } else {
      el.innerHTML = '<div class="text-sm text-muted">尚未配置证书</div>';
      $('#current-cert-info').textContent = '';
    }
    $('#tls-enabled').value = currentStatus.enabled ? 'true' : 'false';
    $('#cert-upload-card').style.display = currentStatus.enabled ? 'block' : 'none';
  }

  async function saveConfig() {
    var enabled = $('#tls-enabled').value === 'true';

    if (enabled) {
      var certFile = $('#cert-file').files && $('#cert-file').files[0];
      var keyFile = $('#key-file').files && $('#key-file').files[0];

      if (!currentStatus.configured && !certFile) {
        showMsg('#tls-msg', '请选择证书文件', false);
        return;
      }
      if (!currentStatus.configured && !keyFile) {
        showMsg('#tls-msg', '请选择私钥文件', false);
        return;
      }
    }

    showMsg('#tls-msg', '保存中...', true);

    try {
      var csrf = await getCSRF();
      var form = new FormData();
      form.append('enabled', enabled ? 'true' : 'false');
      form.append('csrf_token', csrf);
      if (enabled && $('#cert-file').files[0]) form.append('cert', $('#cert-file').files[0]);
      if (enabled && $('#key-file').files[0]) form.append('key', $('#key-file').files[0]);

      var base = await getServerUrl();
      var resp = await fetch(base + '/api/admin/tls/upload', {
        method: 'POST',
        credentials: 'include',
        body: form,
      });
      var res = await resp.json();
      showMsg('#tls-msg', res.message || res.error, !!res.success);
      if (res.success) {
        currentStatus.enabled = enabled;
        if (res.cert_info) {
          currentStatus.configured = true;
          currentStatus.cert_info = res.cert_info;
        }
        renderStatus();
      }
    } catch (e) { showMsg('#tls-msg', e.message, false); }
  }

  function esc(s) {
    if (s == null) return '';
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function formatDate(iso) {
    if (!iso) return '';
    return new Date(iso).toLocaleString('zh-CN');
  }

  async function getCSRF() {
    var res = await Api.get('/api/csrf');
    return res.csrf_token || '';
  }

  async function getServerUrl() {
    return window.location.origin.replace(/\/+$/, '');
  }
}
```

---

### Task 9: 导航与路由注册

**Files:**
- Modify: `internal/web/ui/admin/nav.html`
- Modify: `internal/web/ui.go`

- [ ] **Step 1: 在 nav.html 添加"证书配置"入口**

找到"系统配置"附近的导航项，添加：
```html
<a href="/admin/tls" class="nav-item" data-page="tls">证书配置</a>
```

- [ ] **Step 2: 在 ui.go 注册路由**

找到 `ui.go` 中 admin 前端路由注册部分，添加 `tls` 页面路由：
```go
r.GET("/admin/tls", s.handleAdminPage("tls"))
```

- [ ] **Step 3: 编译确认**

Run: `go build -o /dev/null ./cmd/picoaide/`
Expected: 编译成功

---

### Task 10: 测试

**Files:**
- Create: `internal/web/admin_tls_test.go`
- Modify: `internal/user/picoclaw_fixups_test.go`（如果 containerBaseURL 测试需要更新）

- [ ] **Step 1: 编写单元测试 — 域名匹配**

```go
package web

import "testing"

func TestMatchDomain(t *testing.T) {
  tests := []struct {
    pattern string
    host    string
    want    bool
  }{
    {"example.com", "example.com", true},
    {"example.com", "EXAMPLE.COM", true},
    {"example.com", "other.com", false},
    {"*.example.com", "sub.example.com", true},
    {"*.example.com", "sub.sub.example.com", false},
    {"*.example.com", "example.com", false},
    {"*.example.com", "other.com", false},
  }
  for _, tt := range tests {
    if got := matchDomain(tt.pattern, tt.host); got != tt.want {
      t.Errorf("matchDomain(%q, %q) = %v, want %v", tt.pattern, tt.host, got, tt.want)
    }
  }
}

func TestValidateCertKeyPair(t *testing.T) {
  // 使用自签名证书测试
  certPEM, keyPEM := generateTestCertPair("example.com")
  if err := validateCertKeyPair(certPEM, keyPEM); err != nil {
    t.Fatalf("validateCertKeyPair failed: %v", err)
  }
  // 用错误的 key 应该失败
  _, wrongKey := generateTestCertPair("other.com")
  if err := validateCertKeyPair(certPEM, wrongKey); err == nil {
    t.Fatal("expected error for mismatched key")
  }
}

func TestValidateCertDomain(t *testing.T) {
  certPEM, _ := generateTestCertPair("example.com")
  if err := validateCertDomain(certPEM, "example.com"); err != nil {
    t.Fatalf("expected match, got: %v", err)
  }
  if err := validateCertDomain(certPEM, "EXAMPLE.COM"); err != nil {
    t.Fatalf("expected case-insensitive match, got: %v", err)
  }
  if err := validateCertDomain(certPEM, "other.com"); err == nil {
    t.Fatal("expected mismatch error")
  }
}
```

- [ ] **Step 2: 编写测试辅助函数（生成自签名测试证书）**

```go
import (
  "crypto/ecdsa"
  "crypto/elliptic"
  "crypto/rand"
  "crypto/x509"
  "crypto/x509/pkix"
  "encoding/pem"
  "math/big"
  "time"
)

func generateTestCertPair(domain string) (certPEM, keyPEM []byte) {
  priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
  if err != nil {
    panic(err)
  }
  template := &x509.Certificate{
    SerialNumber: big.NewInt(1),
    Subject: pkix.Name{
      CommonName: domain,
    },
    DNSNames:              []string{domain},
    NotBefore:             time.Now().Add(-1 * time.Hour),
    NotAfter:              time.Now().Add(365 * 24 * time.Hour),
    KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
    ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
    BasicConstraintsValid: true,
  }
  certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
  if err != nil {
    panic(err)
  }
  certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
  keyBytes, err := x509.MarshalECPrivateKey(priv)
  if err != nil {
    panic(err)
  }
  keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
  return certPEM, keyPEM
}
```

- [ ] **Step 3: 运行测试**

Run: `go test ./internal/web/ -run "TestMatchDomain|TestValidateCert|TestParseCertInfo" -v`
Expected: PASS

- [ ] **Step 4: 集成测试（可选）**

需要 DB 集成，参照 `integration_auth_test.go` 的模式，但可以不涵盖 restart（因为 restart 需要 systemd）。

- [ ] **Step 5: 运行全部测试**

Run: `go test ./internal/... -count=1`
Expected: PASS

---

### Task 11: 清理与编译验证

- [ ] **Step 1: 删除旧设置页面中的监听地址和证书配置**

修改 `internal/web/ui/admin/templates/settings.html`：
- 删除"监听地址"输入框（第 66 行）
- 删除 HTTPS 开关（第 67-71 行）
- 删除证书文件路径输入框（第 73-76 行）
- 保留描述文字？或替换为指向证书配置页面的提示

- [ ] **Step 2: 完整编译验证**

Run: `go build -o picoaide ./cmd/picoaide/`
Expected: 编译成功

- [ ] **Step 3: 运行 format 和 lint**

Run: `make check`
Expected: 全部通过
