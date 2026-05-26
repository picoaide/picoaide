package web

import (
  "crypto/rand"
  "crypto/rsa"
  "crypto/tls"
  "crypto/x509"
  "crypto/x509/pkix"
  "encoding/pem"
  "math/big"
  "net/url"
  "testing"
  "time"
)

// ============================================================
// 测试辅助：生成测试证书
// ============================================================

// generateTestCert 生成自签名证书用于测试
func generateTestCert(t *testing.T) (certPEM, keyPEM string) {
  t.Helper()
  key, err := rsa.GenerateKey(rand.Reader, 1024)
  if err != nil {
    t.Fatalf("生成密钥失败: %v", err)
  }
  template := x509.Certificate{
    SerialNumber: big.NewInt(1),
    Subject:      pkix.Name{CommonName: "test.example.com"},
    DNSNames:     []string{"test.example.com", "www.example.com"},
    NotBefore:    time.Now().Add(-1 * time.Hour),
    NotAfter:     time.Now().Add(365 * 24 * time.Hour),
  }
  certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
  if err != nil {
    t.Fatalf("创建证书失败: %v", err)
  }
  certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
  keyPEM = string(pem.EncodeToMemory(&pem.Block{
    Type:  "RSA PRIVATE KEY",
    Bytes: x509.MarshalPKCS1PrivateKey(key),
  }))
  return
}

// generateExpiredCert 生成已过期的证书
func generateExpiredCert(t *testing.T) (certPEM, keyPEM string) {
  t.Helper()
  key, err := rsa.GenerateKey(rand.Reader, 1024)
  if err != nil {
    t.Fatalf("生成密钥失败: %v", err)
  }
  template := x509.Certificate{
    SerialNumber: big.NewInt(1),
    Subject:      pkix.Name{CommonName: "expired.example.com"},
    NotBefore:    time.Now().Add(-48 * time.Hour),
    NotAfter:     time.Now().Add(-1 * time.Hour),
  }
  certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
  if err != nil {
    t.Fatalf("创建证书失败: %v", err)
  }
  certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
  keyPEM = string(pem.EncodeToMemory(&pem.Block{
    Type:  "RSA PRIVATE KEY",
    Bytes: x509.MarshalPKCS1PrivateKey(key),
  }))
  return
}

// tlsData 从 body 中提取 data 字段的 map
func tlsData(body map[string]interface{}) map[string]interface{} {
  if d, ok := body["data"].(map[string]interface{}); ok {
    return d
  }
  return nil
}

// tlsEnabled 返回 data.enabled 字段值
func tlsEnabled(data map[string]interface{}) bool {
  if data == nil {
    return false
  }
  v, _ := data["enabled"].(bool)
  return v
}

// ============================================================
// GET /api/admin/tls/status
// ============================================================

func TestAdminTLSStatus_requiresAuth(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/tls/status", "")
  assertStatus(t, resp, 401)
}

func TestAdminTLSStatus_requiresSuperadmin(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/tls/status", "testuser")
  assertStatus(t, resp, 403)
}

func TestAdminTLSStatus_disabledByDefault(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/admin/tls/status", "testadmin")
  body := getJSON(t, resp)
  data := tlsData(body)
  if tlsEnabled(data) {
    t.Error("TLS 默认应禁用")
  }
  if data["has_cert"].(bool) {
    t.Error("TLS 默认应无证书")
  }
}

func TestAdminTLSStatus_showsEnabledAfterSave(t *testing.T) {
  env := setupTestServer(t)
  certPEM, keyPEM := generateTestCert(t)

  // 使用新的 /tls/save 端点
  form := url.Values{"cert_pem": {certPEM}, "key_pem": {keyPEM}, "enabled": {"true"}}
  resp := env.postForm(t, "/api/admin/tls/save", "testadmin", form)
  assertStatus(t, resp, 200)

  resp = env.get(t, "/api/admin/tls/status", "testadmin")
  body := getJSON(t, resp)
  data := tlsData(body)
  if !tlsEnabled(data) {
    t.Error("保存证书后 TLS 应启用")
  }
  if !data["has_cert"].(bool) {
    t.Error("保存后应有证书")
  }
  if subj, ok := data["subject"].(string); !ok || subj != "test.example.com" {
    t.Errorf("证书 subject 应为 test.example.com，得到 %v", data["subject"])
  }
  if sans, ok := data["sans"].([]interface{}); !ok || len(sans) != 2 {
    t.Errorf("SANs 应为 [test.example.com, www.example.com]，得到 %v", data["sans"])
  }
}

// ============================================================
// POST /api/admin/tls/verify
// ============================================================

func TestAdminTLSVerify_requiresAuth(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {"cert"}, "key_pem": {"key"}}
  resp := env.postForm(t, "/api/admin/tls/verify", "", form)
  assertStatus(t, resp, 401)
}

func TestAdminTLSVerify_requiresSuperadmin(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {"cert"}, "key_pem": {"key"}}
  resp := env.postForm(t, "/api/admin/tls/verify", "testuser", form)
  assertStatus(t, resp, 403)
}

func TestAdminTLSVerify_rejectsEmpty(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {""}, "key_pem": {"key"}}
  resp := env.postForm(t, "/api/admin/tls/verify", "testadmin", form)
  assertStatus(t, resp, 400)
}

func TestAdminTLSVerify_rejectsInvalidPEM(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {"not-pem"}, "key_pem": {"not-pem"}}
  resp := env.postForm(t, "/api/admin/tls/verify", "testadmin", form)
  assertStatus(t, resp, 400)
}

func TestAdminTLSVerify_returnsCertInfo(t *testing.T) {
  env := setupTestServer(t)
  certPEM, keyPEM := generateTestCert(t)

  form := url.Values{"cert_pem": {certPEM}, "key_pem": {keyPEM}}
  resp := env.postForm(t, "/api/admin/tls/verify", "testadmin", form)
  assertStatus(t, resp, 200)

  body := getJSON(t, resp)
  if !body["success"].(bool) {
    t.Fatal("验证应成功")
  }
  data := tlsData(body)
  if data == nil {
    t.Fatal("应有 data")
  }
  if data["subject"] != "test.example.com" {
    t.Errorf("subject 应为 test.example.com，得到 %v", data["subject"])
  }
  if len(data["sans"].([]interface{})) != 2 {
    t.Errorf("应有 2 个 SANs，得到 %v", data["sans"])
  }
  if !data["valid"].(bool) {
    t.Error("证书应有效")
  }
  if data["expired"].(bool) {
    t.Error("证书不应过期")
  }
}

func TestAdminTLSVerify_detectsExpired(t *testing.T) {
  env := setupTestServer(t)
  certPEM, keyPEM := generateExpiredCert(t)

  form := url.Values{"cert_pem": {certPEM}, "key_pem": {keyPEM}}
  resp := env.postForm(t, "/api/admin/tls/verify", "testadmin", form)
  assertStatus(t, resp, 200)

  body := getJSON(t, resp)
  data := tlsData(body)
  if !data["expired"].(bool) {
    t.Error("过期证书应标记为 expired")
  }
}

// ============================================================
// POST /api/admin/tls/save
// ============================================================

func TestAdminTLSSave_requiresAuth(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {"cert"}, "key_pem": {"key"}, "enabled": {"true"}}
  resp := env.postForm(t, "/api/admin/tls/save", "", form)
  assertStatus(t, resp, 401)
}

func TestAdminTLSSave_requiresSuperadmin(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {"cert"}, "key_pem": {"key"}, "enabled": {"true"}}
  resp := env.postForm(t, "/api/admin/tls/save", "testuser", form)
  assertStatus(t, resp, 403)
}

func TestAdminTLSSave_rejectsEmpty(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {""}, "key_pem": {"key"}, "enabled": {"true"}}
  resp := env.postForm(t, "/api/admin/tls/save", "testadmin", form)
  assertStatus(t, resp, 400)
}

func TestAdminTLSSave_rejectsInvalid(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {"bad"}, "key_pem": {"bad"}, "enabled": {"true"}}
  resp := env.postForm(t, "/api/admin/tls/save", "testadmin", form)
  assertStatus(t, resp, 400)
}

func TestAdminTLSSave_savesWithDisabled(t *testing.T) {
  env := setupTestServer(t)
  certPEM, keyPEM := generateTestCert(t)

  // 保存但禁用 HTTPS
  form := url.Values{"cert_pem": {certPEM}, "key_pem": {keyPEM}, "enabled": {"false"}}
  resp := env.postForm(t, "/api/admin/tls/save", "testadmin", form)
  assertStatus(t, resp, 200)

  // 验证保存后未启用
  resp = env.get(t, "/api/admin/tls/status", "testadmin")
  body := getJSON(t, resp)
  data := tlsData(body)
  if tlsEnabled(data) {
    t.Error("enabled=false 时 TLS 不应启用")
  }
  if !data["has_cert"].(bool) {
    t.Error("证书应保存")
  }
}

// ============================================================
// POST /api/admin/tls/toggle
// ============================================================

func TestAdminTLSToggle_requiresAuth(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"enabled": {"true"}}
  resp := env.postForm(t, "/api/admin/tls/toggle", "", form)
  assertStatus(t, resp, 401)
}

func TestAdminTLSToggle_requiresSuperadmin(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"enabled": {"true"}}
  resp := env.postForm(t, "/api/admin/tls/toggle", "testuser", form)
  assertStatus(t, resp, 403)
}

func TestAdminTLSToggle_failsWhenNoCert(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"enabled": {"true"}}
  resp := env.postForm(t, "/api/admin/tls/toggle", "testadmin", form)
  assertStatus(t, resp, 400)
}

func TestAdminTLSToggle_toggleOnOff(t *testing.T) {
  env := setupTestServer(t)

  // 先保存证书（但不启用）
  certPEM, keyPEM := generateTestCert(t)
  form := url.Values{"cert_pem": {certPEM}, "key_pem": {keyPEM}, "enabled": {"false"}}
  resp := env.postForm(t, "/api/admin/tls/save", "testadmin", form)
  assertStatus(t, resp, 200)

  // Toggle 启用
  form = url.Values{"enabled": {"true"}}
  resp = env.postForm(t, "/api/admin/tls/toggle", "testadmin", form)
  assertStatus(t, resp, 200)

  resp = env.get(t, "/api/admin/tls/status", "testadmin")
  body := getJSON(t, resp)
  if !tlsEnabled(tlsData(body)) {
    t.Error("Toggle 启用后 TLS 应启用")
  }

  // Toggle 关闭
  form = url.Values{"enabled": {"false"}}
  resp = env.postForm(t, "/api/admin/tls/toggle", "testadmin", form)
  assertStatus(t, resp, 200)

  resp = env.get(t, "/api/admin/tls/status", "testadmin")
  body = getJSON(t, resp)
  if tlsEnabled(tlsData(body)) {
    t.Error("Toggle 关闭后 TLS 应禁用")
  }

  // 证书应保留
  data := tlsData(body)
  if !data["has_cert"].(bool) {
    t.Error("Toggle 不应清除证书")
  }
}

// ============================================================
// POST /api/admin/tls/clear
// ============================================================

func TestAdminTLSClear_requiresAuth(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postForm(t, "/api/admin/tls/clear", "", nil)
  assertStatus(t, resp, 401)
}

func TestAdminTLSClear_requiresSuperadmin(t *testing.T) {
  env := setupTestServer(t)
  resp := env.postForm(t, "/api/admin/tls/clear", "testuser", nil)
  assertStatus(t, resp, 403)
}

func TestAdminTLSClear_clearsCertAndDisables(t *testing.T) {
  env := setupTestServer(t)

  certPEM, keyPEM := generateTestCert(t)
  form := url.Values{"cert_pem": {certPEM}, "key_pem": {keyPEM}, "enabled": {"true"}}
  env.postForm(t, "/api/admin/tls/save", "testadmin", form)

  // 清除
  resp := env.postForm(t, "/api/admin/tls/clear", "testadmin", nil)
  assertStatus(t, resp, 200)

  // 验证清除后无证书且禁用
  resp = env.get(t, "/api/admin/tls/status", "testadmin")
  body := getJSON(t, resp)
  data := tlsData(body)
  if tlsEnabled(data) {
    t.Error("清除后 TLS 应禁用")
  }
  if data["has_cert"].(bool) {
    t.Error("清除后应无证书")
  }
}

// ============================================================
// 辅助函数测试
// ============================================================

func TestGetCertNames(t *testing.T) {
  certPEM, keyPEM := generateTestCert(t)
  cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
  if err != nil {
    t.Fatal(err)
  }
  parsed, err := x509.ParseCertificate(cert.Certificate[0])
  if err != nil {
    t.Fatal(err)
  }
  cn, sans := getCertNames(parsed)
  if cn != "test.example.com" {
    t.Errorf("CN 应为 test.example.com，得到 %s", cn)
  }
  if len(sans) != 2 {
    t.Errorf("应有 2 个 SANs，得到 %d", len(sans))
  }
}

func TestHostMatchesCert(t *testing.T) {
  certPEM, keyPEM := generateTestCert(t)
  cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
  if err != nil {
    t.Fatal(err)
  }
  parsed, err := x509.ParseCertificate(cert.Certificate[0])
  if err != nil {
    t.Fatal(err)
  }

  // CN = test.example.com, SANs = [test.example.com, www.example.com]
  tests := []struct {
    host  string
    match bool
  }{
    {"test.example.com", true},
    {"www.example.com", true},
    {"other.com", false},
    {"test.example.com:443", true},
    {"", true},
  }
  for _, tc := range tests {
    result := hostMatchesCert(tc.host, parsed)
    if result != tc.match {
      t.Errorf("hostMatchesCert(%q) = %v, want %v", tc.host, result, tc.match)
    }
  }
}

func TestMatchName(t *testing.T) {
  tests := []struct {
    host    string
    pattern string
    match   bool
  }{
    {"example.com", "example.com", true},
    {"www.example.com", "*.example.com", true},
    {"sub.www.example.com", "*.example.com", false},
    {"example.com", "*.example.com", false},
    {"anything", "", false},
  }
  for _, tc := range tests {
    result := matchName(tc.host, tc.pattern)
    if result != tc.match {
      t.Errorf("matchName(%q, %q) = %v, want %v", tc.host, tc.pattern, result, tc.match)
    }
  }
}
