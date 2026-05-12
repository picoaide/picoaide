package authsource

import (
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/ldap"
)

type GroupPreview = ldap.GroupPreview
type GroupHierarchy = ldap.GroupHierarchy

type LDAPProvider struct{}

func init() {
  Register("ldap", LDAPProvider{})
}

func (LDAPProvider) Authenticate(cfg *config.GlobalConfig, username, password string) bool {
  return ldap.Authenticate(cfg, username, password)
}

func (LDAPProvider) FetchUsers(cfg *config.GlobalConfig) ([]string, error) {
  return ldap.FetchUsers(cfg)
}

func (LDAPProvider) FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error) {
  return ldap.FetchUserGroups(cfg, username)
}

func (LDAPProvider) FetchGroups(cfg *config.GlobalConfig) (map[string]GroupHierarchy, error) {
  return ldap.FetchAllGroupsWithHierarchy(cfg)
}

func LDAPAuthenticate(cfg *config.GlobalConfig, username, password string) bool {
  provider, err := passwordProvider("ldap")
  return err == nil && provider.Authenticate(cfg, username, password)
}

func LDAPTestConnection(host, bindDN, bindPassword, baseDN, filter, usernameAttr string) ([]string, error) {
  return ldap.TestConnection(host, bindDN, bindPassword, baseDN, filter, usernameAttr)
}

func LDAPTestGroups(host, bindDN, bindPassword, baseDN, groupSearchMode, groupBaseDN, groupFilter, groupMemberAttr, usernameAttr string) ([]GroupPreview, error) {
  return ldap.TestGroups(host, bindDN, bindPassword, baseDN, groupSearchMode, groupBaseDN, groupFilter, groupMemberAttr, usernameAttr)
}

func LDAPFetchUsers(cfg *config.GlobalConfig) ([]string, error) {
  provider, err := directoryProvider("ldap")
  if err != nil {
    return nil, err
  }
  return provider.FetchUsers(cfg)
}

func LDAPFetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error) {
  provider, err := directoryProvider("ldap")
  if err != nil {
    return nil, err
  }
  return provider.FetchUserGroups(cfg, username)
}
