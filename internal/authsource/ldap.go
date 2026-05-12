package authsource

import (
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/ldap"
)

// GroupPreview 用于 LDAP 配置测试页面返回组预览
type GroupPreview struct {
  Name    string   `json:"name"`
  Members []string `json:"members"`
}

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

func (LDAPProvider) FetchGroups(cfg *config.GlobalConfig) (GroupHierarchy, error) {
  ldapGroups, err := ldap.FetchAllGroupsWithHierarchy(cfg)
  if err != nil {
    return nil, err
  }
  result := make(GroupHierarchy, len(ldapGroups))
  for name, g := range ldapGroups {
    result[name] = GroupNode{
      Members:   g.Members,
      SubGroups: g.SubGroups,
    }
  }
  return result, nil
}

func (LDAPProvider) DisplayName() string {
  return "LDAP"
}

// LDAPTestConnection 测试 LDAP 连接（配置测试 UI 专用）
func LDAPTestConnection(host, bindDN, bindPassword, baseDN, filter, usernameAttr string) ([]string, error) {
  return ldap.TestConnection(host, bindDN, bindPassword, baseDN, filter, usernameAttr)
}

// LDAPTestGroups 测试 LDAP 组查询（配置测试 UI 专用）
func LDAPTestGroups(host, bindDN, bindPassword, baseDN, groupSearchMode, groupBaseDN, groupFilter, groupMemberAttr, usernameAttr string) ([]GroupPreview, error) {
  ldapGroups, err := ldap.TestGroups(host, bindDN, bindPassword, baseDN, groupSearchMode, groupBaseDN, groupFilter, groupMemberAttr, usernameAttr)
  if err != nil {
    return nil, err
  }
  result := make([]GroupPreview, 0, len(ldapGroups))
  for _, g := range ldapGroups {
    result = append(result, GroupPreview{
      Name:    g.Name,
      Members: g.Members,
    })
  }
  return result, nil
}
