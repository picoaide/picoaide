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
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"

  "log/slog"
)

// ============================================================
// TLS 证书管理
// ============================================================

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
    "success":    true,
    "enabled":    s.cfg.Web.TLS.Enabled,
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

    certInfo, _ := parseCertInfo(certBytes)

    // 保存到 DB
    s.cfg.Web.TLS.CertPEM = string(certBytes)
    s.cfg.Web.TLS.KeyPEM = string(keyBytes)
    s.cfg.Web.TLS.Enabled = true
    if err := config.SaveToDB(s.cfg, username); err != nil {
      writeError(c, http.StatusInternalServerError, "保存配置失败: "+err.Error())
      return
    }

    // 异步重启
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

  domains := []string{cert.Subject.CommonName}
  domains = append(domains, cert.DNSNames...)

  var validDomains []string
  for _, d := range domains {
    if strings.TrimSpace(d) != "" {
      validDomains = append(validDomains, d)
    }
  }

  for _, d := range validDomains {
    if matchDomain(d, host) {
      return nil
    }
  }

  return fmt.Errorf("证书域名 (%v) 与当前访问域名 (%s) 不匹配", validDomains, host)
}

// matchDomain 支持通配符域名匹配（如 *.example.com 匹配 sub.example.com，不匹配 sub.sub.example.com）
func matchDomain(pattern, host string) bool {
  if strings.EqualFold(pattern, host) {
    return true
  }
  if strings.HasPrefix(pattern, "*.") {
    suffix := pattern[1:]
    lowerHost := strings.ToLower(host)
    lowerSuffix := strings.ToLower(suffix)
    if !strings.HasSuffix(lowerHost, lowerSuffix) {
      return false
    }
    if len(lowerHost) == len(lowerSuffix) {
      return false // host == suffix means no label before wildcard
    }
    label := lowerHost[:len(lowerHost)-len(lowerSuffix)]
    return !strings.Contains(label, ".")
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
