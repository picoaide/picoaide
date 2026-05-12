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

func (LDAPProvider) ConfigFields() []FieldSection {
  return []FieldSection{
    {
      Name: "LDAP 配置",
      Fields: []FieldDefinition{
        {Key: "ldap.host", Label: "LDAP 地址", Type: FieldText, Placeholder: "ldap://ldap.example.com:389", Required: true},
        {Key: "ldap.bind_dn", Label: "Bind DN", Type: FieldText, Placeholder: "cn=admin,dc=example,dc=com", Required: true},
        {Key: "ldap.bind_password", Label: "Bind Password", Type: FieldPassword, Required: true},
        {Key: "ldap.base_dn", Label: "Base DN", Type: FieldText, Placeholder: "ou=users,dc=example,dc=com", Required: true},
        {Key: "ldap.filter", Label: "Filter", Type: FieldText, Placeholder: "(objectClass=inetOrgPerson)"},
        {Key: "ldap.username_attribute", Label: "Username Attribute", Type: FieldText, Placeholder: "uid"},
      },
    },
    {
      Name: "LDAP 组配置",
      Fields: []FieldDefinition{
        {Key: "ldap.group_search_mode", Label: "组搜索方式", Type: FieldSelect, Options: []FieldOption{
          {Value: "", Label: "不启用组同步"},
          {Value: "member_of", Label: "memberOf（AD 风格）"},
          {Value: "group_search", Label: "groupOfNames（OpenLDAP 风格）"},
        }},
        {Key: "ldap.group_base_dn", Label: "组 Base DN", Type: FieldText, Placeholder: "留空使用用户 Base DN"},
        {Key: "ldap.group_filter", Label: "组过滤条件", Type: FieldText, Placeholder: "(objectClass=groupOfNames)"},
        {Key: "ldap.group_member_attribute", Label: "组成员属性", Type: FieldText, Placeholder: "member"},
      },
    },
    {
      Name: "同步",
      Fields: []FieldDefinition{
        {Key: "ldap.sync_interval", Label: "自动同步间隔", Type: FieldSelect, Default: "30m", Options: []FieldOption{
          {Value: "0", Label: "禁用"},
          {Value: "1m", Label: "每 1 分钟"},
          {Value: "5m", Label: "每 5 分钟"},
          {Value: "15m", Label: "每 15 分钟"},
          {Value: "30m", Label: "每 30 分钟"},
          {Value: "1h", Label: "每 1 小时"},
          {Value: "24h", Label: "每天"},
        }},
      },
    },
  }
}

func (LDAPProvider) Actions() []ActionDefinition {
  return []ActionDefinition{
    {ID: "test-ldap", Label: "测试连接", Section: "LDAP 配置"},
  }
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
