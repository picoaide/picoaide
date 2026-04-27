package ldap

import (
  "fmt"
  "sort"

  "github.com/go-ldap/ldap/v3"
  "github.com/picoaide/picoaide/internal/config"
)

// ============================================================
// LDAP 客户端
// ============================================================

func FetchUsers(cfg *config.GlobalConfig) ([]string, error) {
  l, err := ldap.DialURL(cfg.LDAP.Host)
  if err != nil {
    return nil, fmt.Errorf("连接 LDAP 失败: %w", err)
  }
  defer l.Close()

  err = l.Bind(cfg.LDAP.BindDN, cfg.LDAP.BindPassword)
  if err != nil {
    return nil, fmt.Errorf("LDAP 认证失败: %w", err)
  }

  searchRequest := ldap.NewSearchRequest(
    cfg.LDAP.BaseDN,
    ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
    cfg.LDAP.Filter,
    []string{cfg.LDAP.UsernameAttribute},
    nil,
  )

  sr, err := l.Search(searchRequest)
  if err != nil {
    return nil, fmt.Errorf("LDAP 搜索失败: %w", err)
  }

  var users []string
  for _, entry := range sr.Entries {
    username := entry.GetAttributeValue(cfg.LDAP.UsernameAttribute)
    if username != "" {
      users = append(users, username)
    }
  }

  sort.Strings(users)
  return users, nil
}

// TestConnection 使用指定参数测试 LDAP 连接，返回用户数量
func TestConnection(host, bindDN, bindPassword, baseDN, filter, usernameAttr string) ([]string, error) {
  l, err := ldap.DialURL(host)
  if err != nil {
    return nil, fmt.Errorf("连接 LDAP 失败: %w", err)
  }
  defer l.Close()

  err = l.Bind(bindDN, bindPassword)
  if err != nil {
    return nil, fmt.Errorf("LDAP 认证失败: %w", err)
  }

  searchRequest := ldap.NewSearchRequest(
    baseDN,
    ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
    filter,
    []string{usernameAttr},
    nil,
  )

  sr, err := l.Search(searchRequest)
  if err != nil {
    return nil, fmt.Errorf("LDAP 搜索失败: %w", err)
  }

  var users []string
  for _, entry := range sr.Entries {
    username := entry.GetAttributeValue(usernameAttr)
    if username != "" {
      users = append(users, username)
    }
  }

  sort.Strings(users)
  return users, nil
}

// Authenticate 先以管理员身份搜索用户 DN，再用用户 DN 绑定验证密码
func Authenticate(cfg *config.GlobalConfig, username, password string) bool {
  // 1. 以管理员身份连接并搜索用户 DN
  l, err := ldap.DialURL(cfg.LDAP.Host)
  if err != nil {
    return false
  }
  defer l.Close()

  err = l.Bind(cfg.LDAP.BindDN, cfg.LDAP.BindPassword)
  if err != nil {
    return false
  }

  searchRequest := ldap.NewSearchRequest(
    cfg.LDAP.BaseDN,
    ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false,
    fmt.Sprintf("(%s=%s)", cfg.LDAP.UsernameAttribute, ldap.EscapeFilter(username)),
    []string{"dn"},
    nil,
  )

  sr, err := l.Search(searchRequest)
  if err != nil || len(sr.Entries) == 0 {
    return false
  }

  userDN := sr.Entries[0].DN

  // 2. 用用户 DN 和密码验证
  l2, err := ldap.DialURL(cfg.LDAP.Host)
  if err != nil {
    return false
  }
  defer l2.Close()

  err = l2.Bind(userDN, password)
  return err == nil
}
