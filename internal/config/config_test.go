package config

import (
	"testing"
	"time"
)

func TestImageConfigIsTencent(t *testing.T) {
	tests := []struct {
		registry string
		want     bool
	}{
		{"github", false},
		{"tencent", true},
		{"", false},
		{"other", false},
	}
	for _, tt := range tests {
		cfg := ImageConfig{Registry: tt.registry}
		if got := cfg.IsTencent(); got != tt.want {
			t.Errorf("ImageConfig{Registry:%q}.IsTencent() = %v, want %v", tt.registry, got, tt.want)
		}
	}
}

func TestImageConfigRepoName(t *testing.T) {
	cfg := ImageConfig{}
	if got := cfg.RepoName(); got != "picoaide/picoaide" {
		t.Errorf("RepoName() = %q, want %q", got, "picoaide/picoaide")
	}
}

func TestImageConfigPullRef(t *testing.T) {
	tests := []struct {
		registry string
		tag      string
		want     string
	}{
		{"github", "v1.0", "ghcr.io/picoaide/picoaide:v1.0"},
		{"tencent", "v1.0", "hkccr.ccs.tencentyun.com/picoaide/picoaide:v1.0"},
	}
	for _, tt := range tests {
		cfg := ImageConfig{Registry: tt.registry}
		if got := cfg.PullRef(tt.tag); got != tt.want {
			t.Errorf("PullRef(%q) = %q, want %q", tt.tag, got, tt.want)
		}
	}
}

func TestDefaultGlobalConfigToKV(t *testing.T) {
	cfg := DefaultGlobalConfig()
	kv, err := configToKV(cfg)
	if err != nil {
		t.Fatalf("configToKV: %v", err)
	}
	if kv["web.listen"] != ":80" {
		t.Fatalf("web.listen = %q", kv["web.listen"])
	}
	if kv["image.registry"] != "github" {
		t.Fatalf("image.registry = %q", kv["image.registry"])
	}
	if kv["picoclaw"] == "" {
		t.Fatal("picoclaw default should be stored as JSON")
	}
	if kv["security"] == "" {
		t.Fatal("security default should be stored as JSON")
	}
	if kv["skills"] == "" {
		t.Fatal("skills default should be stored as JSON")
	}
}

func TestGlobalConfigLDAPEnabled(t *testing.T) {
	t.Run("nil means enabled", func(t *testing.T) {
		cfg := GlobalConfig{}
		if !cfg.LDAPEnabled() {
			t.Error("LDAPEnabled() with nil pointer should return true")
		}
	})

	t.Run("explicit true", func(t *testing.T) {
		b := true
		cfg := GlobalConfig{Web: WebConfig{LDAPEnabled: &b}}
		if !cfg.LDAPEnabled() {
			t.Error("LDAPEnabled() with true should return true")
		}
	})

	t.Run("explicit false", func(t *testing.T) {
		b := false
		cfg := GlobalConfig{Web: WebConfig{LDAPEnabled: &b}}
		if cfg.LDAPEnabled() {
			t.Error("LDAPEnabled() with false should return false")
		}
	})
}

func TestGlobalConfigAuthMode(t *testing.T) {
	t.Run("explicit auth_mode", func(t *testing.T) {
		cfg := GlobalConfig{Web: WebConfig{AuthMode: "ldap"}}
		if got := cfg.AuthMode(); got != "ldap" {
			t.Errorf("AuthMode() = %q, want %q", got, "ldap")
		}
	})

	t.Run("local auth_mode", func(t *testing.T) {
		cfg := GlobalConfig{Web: WebConfig{AuthMode: "local"}}
		if got := cfg.AuthMode(); got != "local" {
			t.Errorf("AuthMode() = %q, want %q", got, "local")
		}
	})

	t.Run("default from LDAP enabled", func(t *testing.T) {
		cfg := GlobalConfig{}
		if got := cfg.AuthMode(); got != "ldap" {
			t.Errorf("AuthMode() = %q, want %q (default with LDAP enabled)", got, "ldap")
		}
	})

	t.Run("default from LDAP disabled", func(t *testing.T) {
		b := false
		cfg := GlobalConfig{Web: WebConfig{LDAPEnabled: &b}}
		if got := cfg.AuthMode(); got != "local" {
			t.Errorf("AuthMode() = %q, want %q", got, "local")
		}
	})
}

func TestGlobalConfigUnifiedAuthEnabled(t *testing.T) {
	t.Run("local mode", func(t *testing.T) {
		cfg := GlobalConfig{Web: WebConfig{AuthMode: "local"}}
		if cfg.UnifiedAuthEnabled() {
			t.Error("UnifiedAuthEnabled() should be false for local mode")
		}
	})

	t.Run("ldap mode", func(t *testing.T) {
		cfg := GlobalConfig{Web: WebConfig{AuthMode: "ldap"}}
		if !cfg.UnifiedAuthEnabled() {
			t.Error("UnifiedAuthEnabled() should be true for ldap mode")
		}
	})

	t.Run("default with LDAP enabled", func(t *testing.T) {
		cfg := GlobalConfig{}
		if !cfg.UnifiedAuthEnabled() {
			t.Error("UnifiedAuthEnabled() should be true by default (LDAP enabled)")
		}
	})
}

func TestGlobalConfigWhitelistEnabledForProvider(t *testing.T) {
	cfg := GlobalConfig{
		LDAP: LDAPConfig{WhitelistEnabled: true},
		OIDC: OIDCConfig{WhitelistEnabled: false},
	}
	if !cfg.WhitelistEnabledForProvider("ldap") {
		t.Fatal("LDAP whitelist should be enabled")
	}
	if cfg.WhitelistEnabledForProvider("oidc") {
		t.Fatal("OIDC whitelist should be disabled")
	}
	if cfg.WhitelistEnabledForProvider("local") {
		t.Fatal("local auth should not use whitelist")
	}
}

func TestGlobalConfigSyncIntervalDuration(t *testing.T) {
	tests := []struct {
		interval string
		want     time.Duration
	}{
		{"", 0},
		{"0", 0},
		{"1", 1 * time.Hour},
		{"24", 24 * time.Hour},
		{"30m", 30 * time.Minute},
		{"1h", 1 * time.Hour},
		{"2h30m", 2*time.Hour + 30*time.Minute},
		{"invalid", 0},
	}
	for _, tt := range tests {
		cfg := GlobalConfig{LDAP: LDAPConfig{SyncInterval: tt.interval}}
		got := cfg.SyncIntervalDuration()
		if got != tt.want {
			t.Errorf("SyncIntervalDuration(%q) = %v, want %v", tt.interval, got, tt.want)
		}
	}
}
