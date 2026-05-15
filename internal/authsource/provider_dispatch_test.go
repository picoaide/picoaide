package authsource

import (
  "context"
  "testing"

  "github.com/picoaide/picoaide/internal/config"
)

// testProviderNoDisplay is a provider that implements PasswordProvider but not Describable
type testProviderNoDisplay struct{}

func (testProviderNoDisplay) Authenticate(cfg *config.GlobalConfig, username, password string) bool {
  return false
}

func TestDirectoryProvider_ldap(t *testing.T) {
  dp, err := directoryProvider("ldap")
  if err != nil {
    t.Fatalf("directoryProvider('ldap') = %v", err)
  }
  if dp == nil {
    t.Fatal("directoryProvider('ldap') returned nil")
  }
}

func TestDirectoryProvider_local(t *testing.T) {
  _, err := directoryProvider("local")
  if err == nil {
    t.Fatal("directoryProvider('local') should return error")
  }
}

func TestDirectoryProvider_unregistered(t *testing.T) {
  _, err := directoryProvider("nonexistent")
  if err == nil {
    t.Fatal("directoryProvider('nonexistent') should return error")
  }
}

func TestDescribeProvider_nonDescribableProvider(t *testing.T) {
  Register("test-nodisplay", testProviderNoDisplay{})
  meta := DescribeProvider("test-nodisplay")
  if meta.DisplayName != "test-nodisplay" {
    t.Fatalf("DisplayName for non-Describable = %q, want 'test-nodisplay'", meta.DisplayName)
  }
  if !meta.HasPassword {
    t.Error("test-nodisplay should have password capability")
  }
}

func TestDescribeProvider_registeredLocal(t *testing.T) {
  meta := DescribeProvider("local")
  if meta.Name != "local" {
    t.Fatalf("Name = %q, want local", meta.Name)
  }
  if !meta.HasPassword {
    t.Error("local should have password")
  }
  if meta.HasBrowser {
    t.Error("local should not have browser")
  }
  if meta.HasDirectory {
    t.Error("local should not have directory")
  }
}

func TestDescribeProvider_registeredLDAP(t *testing.T) {
  meta := DescribeProvider("ldap")
  if meta.Name != "ldap" {
    t.Fatalf("Name = %q, want ldap", meta.Name)
  }
  if meta.DisplayName != "LDAP" {
    t.Fatalf("DisplayName = %q, want LDAP", meta.DisplayName)
  }
  if !meta.HasPassword {
    t.Error("ldap should have password")
  }
  if meta.HasBrowser {
    t.Error("ldap should not have browser")
  }
  if !meta.HasDirectory {
    t.Error("ldap should have directory")
  }
}

func TestDescribeProvider_registeredOIDC(t *testing.T) {
  meta := DescribeProvider("oidc")
  if meta.Name != "oidc" {
    t.Fatalf("Name = %q, want oidc", meta.Name)
  }
  if meta.DisplayName != "OIDC" {
    t.Fatalf("DisplayName = %q, want OIDC", meta.DisplayName)
  }
  if meta.HasPassword {
    t.Error("oidc should not have password")
  }
  if !meta.HasBrowser {
    t.Error("oidc should have browser")
  }
  if meta.HasDirectory {
    t.Error("oidc should not have directory")
  }
}

func TestDescribeProvider_unregistered(t *testing.T) {
  meta := DescribeProvider("nonexistent")
  if meta.Name != "nonexistent" {
    t.Fatalf("Name = %q, want nonexistent", meta.Name)
  }
  if meta.DisplayName != "" {
    t.Fatalf("DisplayName for unregistered should be empty, got %q", meta.DisplayName)
  }
  if meta.HasPassword || meta.HasBrowser || meta.HasDirectory {
    t.Error("unregistered provider should have no capabilities")
  }
}

func TestActiveProviderMeta(t *testing.T) {
  meta := ActiveProviderMeta(testConfig("local"))
  if meta.Name != "local" {
    t.Fatalf("ActiveProviderMeta Name = %q, want local", meta.Name)
  }
}

func TestActiveProviderMeta_oidc(t *testing.T) {
  meta := ActiveProviderMeta(testConfig("oidc"))
  if meta.Name != "oidc" {
    t.Fatalf("ActiveProviderMeta Name = %q, want oidc", meta.Name)
  }
}

func TestListProviders(t *testing.T) {
  providers := ListProviders()
  names := make(map[string]bool)
  for _, p := range providers {
    names[p.Name] = true
  }
  if !names["local"] {
    t.Error("ListProviders should include local")
  }
  if !names["ldap"] {
    t.Error("ListProviders should include ldap")
  }
  if !names["oidc"] {
    t.Error("ListProviders should include oidc")
  }
}

func TestRegisteredProviderNames(t *testing.T) {
  names := RegisteredProviderNames()
  nameSet := make(map[string]bool)
  for _, n := range names {
    nameSet[n] = true
  }
  if !nameSet["local"] || !nameSet["ldap"] || !nameSet["oidc"] {
    t.Error("RegisteredProviderNames should include local, ldap, oidc")
  }
}

func TestAuthenticate_dispatch_local(t *testing.T) {
  cfg := testConfig("local")
  // No DB setup, should fail gracefully
  if Authenticate(cfg, "user", "pass") {
    t.Error("Authenticate with no DB should return false")
  }
}

func TestAuthenticate_dispatch_oidc(t *testing.T) {
  cfg := testConfig("oidc")
  if Authenticate(cfg, "user", "pass") {
    t.Error("OIDC should not support password auth")
  }
}

func TestAuthURL_dispatch_local(t *testing.T) {
  cfg := testConfig("local")
  _, err := AuthURL(cfg, "state")
  if err == nil {
    t.Fatal("local provider should not support browser auth")
  }
}

func TestAuthURL_dispatch_unregistered(t *testing.T) {
  cfg := testConfig("nonexistent")
  _, err := AuthURL(cfg, "state")
  if err == nil {
    t.Fatal("unregistered provider should return error")
  }
}

func TestCompleteLogin_dispatch_local(t *testing.T) {
  cfg := testConfig("local")
  _, err := CompleteLogin(context.Background(), cfg, "code")
  if err == nil {
    t.Fatal("local provider should not support browser auth")
  }
}

func TestFetchUsers_dispatch_local(t *testing.T) {
  cfg := testConfig("local")
  _, err := FetchUsers(cfg)
  if err == nil {
    t.Fatal("local provider should not support directory sync")
  }
}

func TestFetchUserGroups_dispatch_local(t *testing.T) {
  cfg := testConfig("local")
  _, err := FetchUserGroups(cfg, "user")
  if err == nil {
    t.Fatal("local provider should not support directory sync")
  }
}

func TestAuthURL_dispatch_oidc(t *testing.T) {
  cfg := testConfig("oidc")
  // OIDC with empty config will fail in buildOIDCConfig, but the dispatch call reaches AuthURL
  _, err := AuthURL(cfg, "state")
  if err == nil {
    t.Fatal("oidc with empty config should return error")
  }
}

func TestCompleteLogin_dispatch_oidc(t *testing.T) {
  cfg := testConfig("oidc")
  _, err := CompleteLogin(context.Background(), cfg, "code")
  if err == nil {
    t.Fatal("oidc with empty config should return error")
  }
}

func TestFetchUsers_dispatch_ldap(t *testing.T) {
  cfg := testConfig("ldap")
  _, err := FetchUsers(cfg)
  if err == nil {
    t.Fatal("ldap with empty config should return error")
  }
}

func TestFetchUserGroups_dispatch_ldap(t *testing.T) {
  cfg := testConfig("ldap")
  _, err := FetchUserGroups(cfg, "user")
  if err == nil {
    t.Fatal("ldap with empty config should return error")
  }
}

func TestHasPasswordProvider(t *testing.T) {
  if !HasPasswordProvider(testConfig("local")) {
    t.Error("local should have password provider")
  }
  if HasPasswordProvider(testConfig("oidc")) {
    t.Error("oidc should not have password provider")
  }
  if HasPasswordProvider(testConfig("nonexistent")) {
    t.Error("nonexistent should not have password provider")
  }
}

func TestHasBrowserProvider(t *testing.T) {
  if !HasBrowserProvider(testConfig("oidc")) {
    t.Error("oidc should have browser provider")
  }
  if HasBrowserProvider(testConfig("local")) {
    t.Error("local should not have browser provider")
  }
  if HasBrowserProvider(testConfig("nonexistent")) {
    t.Error("nonexistent should not have browser provider")
  }
}

func TestHasDirectoryProvider(t *testing.T) {
  if !HasDirectoryProvider(testConfig("ldap")) {
    t.Error("ldap should have directory provider")
  }
  if HasDirectoryProvider(testConfig("local")) {
    t.Error("local should not have directory provider")
  }
  if HasDirectoryProvider(testConfig("nonexistent")) {
    t.Error("nonexistent should not have directory provider")
  }
}

// Test backward compatibility: AuthMode returns "ldap" by default when LDAPEnabled
func TestActiveProviderMeta_ldapFallback(t *testing.T) {
  enabled := true
  cfg := &config.GlobalConfig{
    Web: config.WebConfig{
      LDAPEnabled: &enabled,
    },
  }
  meta := ActiveProviderMeta(cfg)
  if meta.Name != "ldap" {
    t.Fatalf("ActiveProviderMeta with ldap fallback = %q, want ldap", meta.Name)
  }
}
