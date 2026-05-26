package web

import (
  "crypto/rand"
  "crypto/rsa"
  "crypto/x509"
  "crypto/x509/pkix"
  "encoding/pem"
  "math/big"
  "net/url"
  "testing"
  "time"
)

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
  if tlsEnabled(tlsData(body)) {
    t.Error("TLS 默认应禁用")
  }
}

func TestAdminTLSStatus_showsEnabledAfterSet(t *testing.T) {
  env := setupTestServer(t)
  certPEM, keyPEM := generateTestCert(t)

  form := url.Values{"cert_pem": {certPEM}, "key_pem": {keyPEM}}
  resp := env.postForm(t, "/api/admin/tls/upload", "testadmin", form)
  assertStatus(t, resp, 200)

  resp = env.get(t, "/api/admin/tls/status", "testadmin")
  body := getJSON(t, resp)
  if !tlsEnabled(tlsData(body)) {
    t.Error("上传证书后 TLS 应启用")
  }
}

// ============================================================
// POST /api/admin/tls/upload
// ============================================================

func TestAdminTLSUpload_requiresAuth(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {"cert"}, "key_pem": {"key"}}
  resp := env.postForm(t, "/api/admin/tls/upload", "", form)
  assertStatus(t, resp, 401)
}

func TestAdminTLSUpload_requiresSuperadmin(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {"cert"}, "key_pem": {"key"}}
  resp := env.postForm(t, "/api/admin/tls/upload", "testuser", form)
  assertStatus(t, resp, 403)
}

func TestAdminTLSUpload_rejectsEmptyCert(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {""}, "key_pem": {"key"}}
  resp := env.postForm(t, "/api/admin/tls/upload", "testadmin", form)
  assertStatus(t, resp, 400)
}

func TestAdminTLSUpload_rejectsEmptyKey(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {"cert"}, "key_pem": {""}}
  resp := env.postForm(t, "/api/admin/tls/upload", "testadmin", form)
  assertStatus(t, resp, 400)
}

func TestAdminTLSUpload_rejectsInvalidCert(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"cert_pem": {"not-a-valid-pem"}, "key_pem": {"not-a-valid-key"}}
  resp := env.postForm(t, "/api/admin/tls/upload", "testadmin", form)
  assertStatus(t, resp, 400)
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

func TestAdminTLSClear_clearsConfig(t *testing.T) {
  env := setupTestServer(t)

  certPEM, keyPEM := generateTestCert(t)
  form := url.Values{"cert_pem": {certPEM}, "key_pem": {keyPEM}}
  env.postForm(t, "/api/admin/tls/upload", "testadmin", form)

  // 验证上传成功
  resp := env.get(t, "/api/admin/tls/status", "testadmin")
  body := getJSON(t, resp)
  if !tlsEnabled(tlsData(body)) {
    t.Skip("上传失败，跳过清理测试")
  }

  // 清除
  resp = env.postForm(t, "/api/admin/tls/clear", "testadmin", nil)
  assertStatus(t, resp, 200)

  // 验证清除后禁用
  resp = env.get(t, "/api/admin/tls/status", "testadmin")
  body = getJSON(t, resp)
  if tlsEnabled(tlsData(body)) {
    t.Error("清除后 TLS 应禁用")
  }
}
