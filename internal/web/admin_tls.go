package web

import (
  "crypto/tls"
  "crypto/x509"
  "fmt"
  "net/http"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/config"
)

// ============================================================
// TLS 证书管理 API
// ============================================================

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

  // 解析证书获取详细信息
  if cfg.Web.TLS.CertPEM != "" {
    if cert, err := tls.X509KeyPair([]byte(cfg.Web.TLS.CertPEM), []byte(cfg.Web.TLS.KeyPEM)); err == nil {
      if len(cert.Certificate) > 0 {
        if parsed, err := x509.ParseCertificate(cert.Certificate[0]); err == nil {
          result["subject"] = parsed.Subject.CommonName
          result["issuer"] = parsed.Issuer.CommonName
          result["not_before"] = parsed.NotBefore.Format(time.RFC3339)
          result["not_after"] = parsed.NotAfter.Format(time.RFC3339)
          if time.Now().After(parsed.NotAfter) {
            result["expired"] = true
          }
        }
      }
    }
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": result})
}

// handleAdminTLSUpload 上传 TLS 证书和私钥
func (s *Server) handleAdminTLSUpload(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  certPEM := c.PostForm("cert_pem")
  keyPEM := c.PostForm("key_pem")

  if certPEM == "" || keyPEM == "" {
    writeError(c, http.StatusBadRequest, "证书和私钥不能为空")
    return
  }

  // 验证 PEM 格式
  if _, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM)); err != nil {
    writeError(c, http.StatusBadRequest, fmt.Sprintf("证书格式无效: %s", err.Error()))
    return
  }

  // 保存到配置
  raw := map[string]interface{}{
    "web": map[string]interface{}{
      "tls": map[string]interface{}{
        "enabled":  true,
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

  // 清除缓存的服务端 TLS server，下次 Serve 时自动重建
  s.tlsSrv = nil

  writeJSON(c, http.StatusOK, gin.H{"success": true, "message": "证书已保存，重启服务后生效"})
}

// handleAdminTLSClear 清除 TLS 证书配置
func (s *Server) handleAdminTLSClear(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
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
  s.tlsSrv = nil

  writeJSON(c, http.StatusOK, gin.H{"success": true, "message": "证书已清除"})
}
