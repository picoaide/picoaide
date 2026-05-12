package authsource

import "testing"

func TestProviderRegistryIncludesBuiltins(t *testing.T) {
  if _, ok := Provider("ldap"); !ok {
    t.Fatal("ldap provider should be registered")
  }
  if _, ok := Provider("oidc"); !ok {
    t.Fatal("oidc provider should be registered")
  }
}

func TestProviderCapabilityChecks(t *testing.T) {
  if _, err := passwordProvider("oidc"); err == nil {
    t.Fatal("oidc should not satisfy PasswordProvider")
  }
  if _, err := browserProvider("ldap"); err == nil {
    t.Fatal("ldap should not satisfy BrowserProvider")
  }
}
