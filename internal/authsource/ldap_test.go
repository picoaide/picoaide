package authsource

import (
  "testing"

  "github.com/picoaide/picoaide/internal/config"
)

const ldapBadHost = "ldap://nx-domain-test-9999.local:389"

func ldapBadCfg() *config.GlobalConfig {
  return &config.GlobalConfig{
    LDAP: config.LDAPConfig{
      Host: ldapBadHost,
    },
  }
}

func TestLDAPProviderDisplayName(t *testing.T) {
  p := LDAPProvider{}
  if got := p.DisplayName(); got != "LDAP" {
    t.Fatalf("DisplayName = %q, want LDAP", got)
  }
}

func TestLDAPProviderConfigFields(t *testing.T) {
  p := LDAPProvider{}
  fields := p.ConfigFields()
  if len(fields) != 3 {
    t.Fatalf("ConfigFields should have 3 sections, got %d", len(fields))
  }
  if fields[0].Name != "LDAP 配置" {
    t.Fatalf("first section name = %q, want LDAP 配置", fields[0].Name)
  }
}

func TestLDAPProviderActions(t *testing.T) {
  p := LDAPProvider{}
  actions := p.Actions()
  if len(actions) != 1 {
    t.Fatalf("Actions should have 1 action, got %d", len(actions))
  }
  if actions[0].ID != "test-ldap" {
    t.Fatalf("action ID = %q, want test-ldap", actions[0].ID)
  }
}

func TestLDAPProviderAuthenticate(t *testing.T) {
  p := LDAPProvider{}
  cfg := ldapBadCfg()
  if p.Authenticate(cfg, "user", "pass") {
    t.Error("Authenticate with bad host should return false")
  }
}

func TestLDAPProviderFetchUsers(t *testing.T) {
  p := LDAPProvider{}
  _, err := p.FetchUsers(ldapBadCfg())
  if err == nil {
    t.Error("FetchUsers with bad host should return error")
  }
}

func TestLDAPProviderFetchUserGroups(t *testing.T) {
  p := LDAPProvider{}
  _, err := p.FetchUserGroups(ldapBadCfg(), "user")
  if err == nil {
    t.Error("FetchUserGroups with bad host should return error")
  }
}

func TestLDAPProviderFetchGroups(t *testing.T) {
  p := LDAPProvider{}
  _, err := p.FetchGroups(ldapBadCfg())
  if err == nil {
    t.Error("FetchGroups with bad host should return error")
  }
}

func TestLDAPTestConnectionError(t *testing.T) {
  _, err := LDAPTestConnection(ldapBadHost, "cn=admin,dc=example,dc=com", "secret", "dc=example,dc=com", "(objectClass=person)", "uid")
  if err == nil {
    t.Fatal("LDAPTestConnection with bad host should return error")
  }
}

func TestLDAPTestGroupsEmptyMode(t *testing.T) {
  result, err := LDAPTestGroups(ldapBadHost, "", "", "", "", "", "", "", "")
  if err != nil {
    t.Fatalf("LDAPTestGroups with empty mode should not error: %v", err)
  }
  if len(result) != 0 {
    t.Fatalf("LDAPTestGroups with empty mode should return empty, got %d", len(result))
  }
}

func TestLDAPTestGroupsError(t *testing.T) {
  _, err := LDAPTestGroups(ldapBadHost, "cn=admin,dc=example,dc=com", "secret", "dc=example,dc=com", "member_of", "", "", "", "uid")
  if err == nil {
    t.Fatal("LDAPTestGroups with bad host should return error")
  }
}
