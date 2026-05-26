package web

import (
  "context"
  "crypto/tls"
  "crypto/x509"
  "fmt"
  "log/slog"
  "net/http"
  "strings"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/config"
)

// ============================================================
// TLS 证书管理 API
// ============================================================

// getCertNames 从 tls.Certificate 中提取 CommonName 和 SANs
func getCertNames(cert *x509.Certificate) (string, []string) {
  cn := cert.Subject.CommonName
  sans := make([]string, 0, len(cert.DNSNames)+1)
  seen := make(map[string]bool)
  if cn != "" {
    seen[cn] = true
    sans = append(sans, cn)
  }
  for _, dns := range cert.DNSNames {
    if !seen[dns] {
      seen[dns] = true
      sans = append(sans, dns)
    }
  }
  return cn, sans
}

// hostMatchesCert 检查请求 Host 是否匹配证书的 CN 或任一 SAN
func hostMatchesCert(host string, cert *x509.Certificate) bool {
  h := strings.ToLower(strings.Split(host, ":")[0])
  if h == "" {
    return true
  }
  cn := strings.ToLower(cert.Subject.CommonName)
  if matchName(h, cn) {
    return true
  }
  for _, dns := range cert.DNSNames {
    if matchName(h, strings.ToLower(dns)) {
      return true
    }
  }
  return false
}

// matchName 支持通配符匹配（如 *.example.com 匹配 a.example.com）
func matchName(host, pattern string) bool {
  if pattern == "" {
    return false
  }
  if pattern == host {
    return true
  }
  if strings.HasPrefix(pattern, "*.") {
    suffix := pattern[1:]
    return strings.HasSuffix(host, suffix) && host != suffix && !strings.Contains(host[:len(host)-len(pattern)+1], ".")
  }
  return false
}

// parseCertPEMs 从 PEM 文本解析出 x509.Certificate 和证书信息 map
func parseCertInfo(certPEM, keyPEM string) (map[string]interface{}, error) {
  cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
  if err != nil {
    return nil, fmt.Errorf("证书格式无效: %w", err)
  }
  if len(cert.Certificate) == 0 {
    return nil, fmt.Errorf("证书内容为空")
  }
  parsed, err := x509.ParseCertificate(cert.Certificate[0])
  if err != nil {
    return nil, fmt.Errorf("解析证书失败: %w", err)
  }
  cn, sans := getCertNames(parsed)
  info := map[string]interface{}{
    "valid":      true,
    "subject":    cn,
    "sans":       sans,
    "issuer":     parsed.Issuer.CommonName,
    "not_before": parsed.NotBefore.Format(time.RFC3339),
    "not_after":  parsed.NotAfter.Format(time.RFC3339),
    "expired":    time.Now().After(parsed.NotAfter),
  }
  return info, nil
}

// handleAdminTLSStatus 查询当前 TLS 证书状态
func (s *Server) handleAdminTLSStatus(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  cfg := s.loadConfig()
  result := map[string]interface{}{
    "enabled":  cfg.Web.TLS.Enabled,
    "has_cert": cfg.Web.TLS.CertPEM != "" && cfg.Web.TLS.KeyPEM != "",
  }

  if cfg.Web.TLS.CertPEM != "" {
    if info, err := parseCertInfo(cfg.Web.TLS.CertPEM, cfg.Web.TLS.KeyPEM); err == nil {
      for k, v := range info {
        result[k] = v
      }
      // host_match 检查请求 Host 是否匹配证书域名
      if parsed, err := tls.X509KeyPair([]byte(cfg.Web.TLS.CertPEM), []byte(cfg.Web.TLS.KeyPEM)); err == nil {
        if len(parsed.Certificate) > 0 {
          if x509Cert, err := x509.ParseCertificate(parsed.Certificate[0]); err == nil {
            result["host_match"] = hostMatchesCert(c.Request.Host, x509Cert)
          }
        }
      }
    }
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": result})
}

// handleAdminTLSVerify 验证证书和私钥，返回证书信息（不保存）
func (s *Server) handleAdminTLSVerify(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusBadRequest, "无效的 CSRF 令牌")
    return
  }

  certPEM := c.PostForm("cert_pem")
  keyPEM := c.PostForm("key_pem")

  if certPEM == "" || keyPEM == "" {
    writeError(c, http.StatusBadRequest, "证书和私钥不能为空")
    return
  }

  info, err := parseCertInfo(certPEM, keyPEM)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // host_match 检查请求 Host 是否匹配证书域名
  if cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM)); err == nil {
    if len(cert.Certificate) > 0 {
      if x509Cert, err := x509.ParseCertificate(cert.Certificate[0]); err == nil {
        info["host_match"] = hostMatchesCert(c.Request.Host, x509Cert)
      }
    }
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": info})
}

// handleAdminTLSSave 保存证书和私钥，并控制 HTTPS 开关
func (s *Server) handleAdminTLSSave(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusBadRequest, "无效的 CSRF 令牌")
    return
  }

  certPEM := c.PostForm("cert_pem")
  keyPEM := c.PostForm("key_pem")
  enabledStr := c.PostForm("enabled")

  if certPEM == "" || keyPEM == "" {
    writeError(c, http.StatusBadRequest, "证书和私钥不能为空")
    return
  }

  // 验证 PEM 对
  if _, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM)); err != nil {
    writeError(c, http.StatusBadRequest, fmt.Sprintf("证书格式无效: %s", err.Error()))
    return
  }

  enabled := enabledStr == "true"

  // 保存到配置
  raw := map[string]interface{}{
    "web": map[string]interface{}{
      "tls": map[string]interface{}{
        "enabled":  enabled,
        "cert_pem": certPEM,
        "key_pem":  keyPEM,
      },
    },
  }
  if err := config.SaveRawToDB(raw, "system"); err != nil {
    writeError(c, http.StatusInternalServerError, "保存证书失败")
    return
  }

  // 重载配置
  if newCfg, err := config.LoadFromDB(); err == nil {
    s.cfg.Store(newCfg)
  }

  // 热加载 TLS 监听
  if err := s.reloadTLS(); err != nil {
    slog.Warn("TLS 热加载失败", "error", err)
  }

  msg := "证书已保存"
  if enabled {
    msg += "，HTTPS 已启用"
  } else {
    msg += "，HTTPS 已关闭"
  }

  writeJSON(c, http.StatusOK, gin.H{
    "success": true,
    "message": msg,
    "data": gin.H{
      "enabled":  enabled,
      "has_cert": true,
    },
  })
}

// handleAdminTLSToggle 仅切换 HTTPS 开关，不影响证书内容
func (s *Server) handleAdminTLSToggle(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusBadRequest, "无效的 CSRF 令牌")
    return
  }

  enabledStr := c.PostForm("enabled")
  enabled := enabledStr == "true"

  // 如果要启用但无证书，报错
  if enabled {
    cfg := s.loadConfig()
    if cfg.Web.TLS.CertPEM == "" || cfg.Web.TLS.KeyPEM == "" {
      writeError(c, http.StatusBadRequest, "请先上传证书后再启用 HTTPS")
      return
    }
  }

  raw := map[string]interface{}{
    "web": map[string]interface{}{
      "tls": map[string]interface{}{
        "enabled": enabled,
      },
    },
  }
  if err := config.SaveRawToDB(raw, "system"); err != nil {
    writeError(c, http.StatusInternalServerError, "保存配置失败")
    return
  }

  if newCfg, err := config.LoadFromDB(); err == nil {
    s.cfg.Store(newCfg)
  }

  // 热加载 TLS 监听
  if err := s.reloadTLS(); err != nil {
    slog.Warn("TLS 热加载失败", "error", err)
  }

  msg := "HTTPS 已关闭"
  if enabled {
    msg = "HTTPS 已启用"
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true, "message": msg})
}

// handleAdminTLSClear 清除 TLS 证书并禁用 HTTPS
func (s *Server) handleAdminTLSClear(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusBadRequest, "无效的 CSRF 令牌")
    return
  }

  raw := map[string]interface{}{
    "web": map[string]interface{}{
      "tls": map[string]interface{}{
        "enabled":  false,
        "cert_pem": "",
        "key_pem":  "",
      },
    },
  }
  if err := config.SaveRawToDB(raw, "system"); err != nil {
    writeError(c, http.StatusInternalServerError, "清除证书失败")
    return
  }

  if newCfg, err := config.LoadFromDB(); err == nil {
    s.cfg.Store(newCfg)
  }

  // 热加载 TLS 监听（关闭 TLS）
  if err := s.reloadTLS(); err != nil {
    slog.Warn("TLS 热加载失败", "error", err)
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true, "message": "证书已清除"})
}

// reloadTLS 根据当前配置热加载/卸载 TLS 监听
func (s *Server) reloadTLS() error {
  // 停止已有的 TLS 服务器
  if s.tlsSrv != nil {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    oldSrv := s.tlsSrv
    s.tlsSrv = nil
    if err := oldSrv.Shutdown(ctx); err != nil {
      slog.Warn("关闭旧 HTTPS 服务器失败", "error", err)
    }
    cancel()
  }

  cfg := s.loadConfig()
  if !cfg.Web.TLS.Enabled || cfg.Web.TLS.CertPEM == "" || cfg.Web.TLS.KeyPEM == "" {
    return nil
  }

  tlsSrv, err := s.buildTLSServer()
  if err != nil {
    return fmt.Errorf("创建 HTTPS 服务器失败: %w", err)
  }
  if tlsSrv == nil {
    return nil
  }

  s.tlsSrv = tlsSrv
  go func() {
    slog.Info("HTTPS 服务器已启动", "addr", ":443")
    if err := tlsSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
      slog.Error("HTTPS 服务失败", "error", err)
    }
  }()
  return nil
}
