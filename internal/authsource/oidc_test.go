package authsource

import (
  "context"
  "testing"

  "github.com/picoaide/picoaide/internal/config"
)

func TestOIDCProviderDisplayName(t *testing.T) {
  p := OIDCProvider{}
  if got := p.DisplayName(); got != "OIDC" {
    t.Fatalf("DisplayName = %q, want OIDC", got)
  }
}

func TestOIDCProviderConfigFields(t *testing.T) {
  p := OIDCProvider{}
  fields := p.ConfigFields()
  if len(fields) != 1 {
    t.Fatalf("ConfigFields should have 1 section, got %d", len(fields))
  }
  if fields[0].Name != "OIDC 配置" {
    t.Fatalf("section name = %q, want OIDC 配置", fields[0].Name)
  }
}

func TestBuildOIDCConfigEmptyFields(t *testing.T) {
  cfg := &config.GlobalConfig{}
  _, _, err := buildOIDCConfig(cfg)
  if err == nil {
    t.Fatal("buildOIDCConfig with empty fields should return error")
  }
}

func TestBuildOIDCConfigMissingIssuer(t *testing.T) {
  cfg := &config.GlobalConfig{
    OIDC: config.OIDCConfig{
      IssuerURL:    "",
      ClientID:     "client-id",
      ClientSecret: "client-secret",
      RedirectURL:  "https://example.com/callback",
    },
  }
  _, _, err := buildOIDCConfig(cfg)
  if err == nil {
    t.Fatal("buildOIDCConfig with missing issuer should return error")
  }
}

func TestOIDCAuthURLEmptyConfig(t *testing.T) {
  p := OIDCProvider{}
  _, err := p.AuthURL(&config.GlobalConfig{}, "state")
  if err == nil {
    t.Fatal("AuthURL with empty config should return error")
  }
}

func TestOIDCCompleteLoginEmptyConfig(t *testing.T) {
  p := OIDCProvider{}
  _, err := p.CompleteLogin(context.Background(), &config.GlobalConfig{}, "code")
  if err == nil {
    t.Fatal("CompleteLogin with empty config should return error")
  }
}

func TestBuildOIDCConfigWithBadIssuer(t *testing.T) {
  cfg := &config.GlobalConfig{
    OIDC: config.OIDCConfig{
      IssuerURL:    "https://nx-domain-test-oidc-9999.local:1",
      ClientID:     "client-id",
      ClientSecret: "client-secret",
      RedirectURL:  "https://example.com/callback",
      Scopes:       "",
    },
  }
  _, _, err := buildOIDCConfig(cfg)
  if err == nil {
    t.Fatal("buildOIDCConfig with bad issuer should return error")
  }
}
