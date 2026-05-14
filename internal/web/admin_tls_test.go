package web

import (
  "crypto/ecdsa"
  "crypto/elliptic"
  "crypto/rand"
  "crypto/x509"
  "crypto/x509/pkix"
  "encoding/pem"
  "math/big"
  "testing"
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
  certPEM, keyPEM := generateTestCertPair("example.com")
  if err := validateCertKeyPair(certPEM, keyPEM); err != nil {
    t.Fatalf("validateCertKeyPair failed: %v", err)
  }
  // 用错误的 key 应该失败
  _, wrongKey := generateTestCertPair("other.com")
  if err := validateCertKeyPair(certPEM, wrongKey); err == nil {
    t.Fatal("expected error for mismatched key")
  }
  // 无效的 PEM 应该失败
  if err := validateCertKeyPair([]byte("invalid"), []byte("invalid")); err == nil {
    t.Fatal("expected error for invalid PEM")
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

func TestParseCertInfo(t *testing.T) {
  certPEM, _ := generateTestCertPair("test.example.com")
  info, err := parseCertInfo(certPEM)
  if err != nil {
    t.Fatalf("parseCertInfo failed: %v", err)
  }
  if info["subject"] == "" {
    t.Error("subject should not be empty")
  }
  sans, ok := info["sans"].([]string)
  if !ok || len(sans) == 0 || sans[0] != "test.example.com" {
    t.Errorf("sans should contain test.example.com, got %v", sans)
  }
  if info["not_after"] == "" {
    t.Error("not_after should not be empty")
  }
  if info["issuer"] == "" {
    t.Error("issuer should not be empty")
  }
}

func TestParseCertInfoInvalidPEM(t *testing.T) {
  if _, err := parseCertInfo([]byte("invalid")); err == nil {
    t.Fatal("expected error for invalid PEM")
  }
  if _, err := parseCertInfo([]byte{}); err == nil {
    t.Fatal("expected error for empty PEM")
  }
}
