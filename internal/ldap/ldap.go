package ldap

import (
	"fmt"
	"sort"
	"strings"

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
		errStr := err.Error()
		if strings.Contains(errStr, "32") || strings.Contains(errStr, "No Such Object") {
			return nil, fmt.Errorf("Base DN '%s' 不存在或无权访问，请检查 LDAP 配置", cfg.LDAP.BaseDN)
		}
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

// FetchUserGroups 获取单个用户所属的 LDAP 组名列表
func FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error) {
	if cfg.LDAP.GroupSearchMode == "group_search" {
		return fetchGroupsBySearch(cfg, username)
	}
	return fetchGroupsByMemberOf(cfg, username)
}

// fetchGroupsByMemberOf 从用户条目的 memberOf 属性读取组（AD 风格）
func fetchGroupsByMemberOf(cfg *config.GlobalConfig, username string) ([]string, error) {
	l, err := ldap.DialURL(cfg.LDAP.Host)
	if err != nil {
		return nil, fmt.Errorf("连接 LDAP 失败: %w", err)
	}
	defer l.Close()

	if err := l.Bind(cfg.LDAP.BindDN, cfg.LDAP.BindPassword); err != nil {
		return nil, fmt.Errorf("LDAP 认证失败: %w", err)
	}

	searchRequest := ldap.NewSearchRequest(
		cfg.LDAP.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false,
		fmt.Sprintf("(%s=%s)", cfg.LDAP.UsernameAttribute, ldap.EscapeFilter(username)),
		[]string{"memberOf"},
		nil,
	)

	sr, err := l.Search(searchRequest)
	if err != nil || len(sr.Entries) == 0 {
		return nil, fmt.Errorf("搜索用户失败")
	}

	var groups []string
	for _, dn := range sr.Entries[0].GetAttributeValues("memberOf") {
		name := extractCN(dn)
		if name != "" {
			groups = append(groups, name)
		}
	}
	return groups, nil
}

// fetchGroupsBySearch 搜索 groupOfNames 条目查找包含用户的组（OpenLDAP 风格）
func fetchGroupsBySearch(cfg *config.GlobalConfig, username string) ([]string, error) {
	l, err := ldap.DialURL(cfg.LDAP.Host)
	if err != nil {
		return nil, fmt.Errorf("连接 LDAP 失败: %w", err)
	}
	defer l.Close()

	if err := l.Bind(cfg.LDAP.BindDN, cfg.LDAP.BindPassword); err != nil {
		return nil, fmt.Errorf("LDAP 认证失败: %w", err)
	}

	// 先获取用户 DN
	userSearch := ldap.NewSearchRequest(
		cfg.LDAP.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false,
		fmt.Sprintf("(%s=%s)", cfg.LDAP.UsernameAttribute, ldap.EscapeFilter(username)),
		[]string{"dn"},
		nil,
	)
	userSR, err := l.Search(userSearch)
	if err != nil || len(userSR.Entries) == 0 {
		return nil, fmt.Errorf("搜索用户 DN 失败")
	}
	userDN := userSR.Entries[0].DN

	// 搜索包含该用户 DN 的组
	groupBaseDN := cfg.LDAP.GroupBaseDN
	if groupBaseDN == "" {
		groupBaseDN = cfg.LDAP.BaseDN
	}
	groupFilter := cfg.LDAP.GroupFilter
	if groupFilter == "" {
		groupFilter = "(objectClass=groupOfNames)"
	}
	memberAttr := cfg.LDAP.GroupMemberAttribute
	if memberAttr == "" {
		memberAttr = "member"
	}

	combinedFilter := fmt.Sprintf("(&%s(%s=%s))", groupFilter, memberAttr, ldap.EscapeFilter(userDN))
	groupSearch := ldap.NewSearchRequest(
		groupBaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		combinedFilter,
		[]string{"cn"},
		nil,
	)
	groupSR, err := l.Search(groupSearch)
	if err != nil {
		return nil, fmt.Errorf("搜索组失败: %w", err)
	}

	var groups []string
	for _, entry := range groupSR.Entries {
		name := entry.GetAttributeValue("cn")
		if name != "" {
			groups = append(groups, name)
		}
	}
	return groups, nil
}

// FetchAllGroupsWithMembers 获取所有 LDAP 组及其成员（用于手动全量同步）
func FetchAllGroupsWithMembers(cfg *config.GlobalConfig) (map[string][]string, error) {
	groups, err := FetchAllGroupsWithHierarchy(cfg)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]string, len(groups))
	for name, group := range groups {
		result[name] = group.Members
	}
	return result, nil
}

// GroupHierarchy LDAP 组同步结果，包含直接成员和直接子组。
type GroupHierarchy struct {
	Members   []string
	SubGroups []string
}

// FetchAllGroupsWithHierarchy 获取所有 LDAP 组、成员和组嵌套关系。
func FetchAllGroupsWithHierarchy(cfg *config.GlobalConfig) (map[string]GroupHierarchy, error) {
	if cfg.LDAP.GroupSearchMode == "group_search" {
		return fetchAllGroupsHierarchyBySearch(cfg)
	}
	members, err := fetchAllGroupsByMemberOf(cfg)
	if err != nil {
		return nil, err
	}
	result := make(map[string]GroupHierarchy, len(members))
	for name, groupMembers := range members {
		result[name] = GroupHierarchy{Members: groupMembers}
	}
	return result, nil
}

// fetchAllGroupsByMemberOf 遍历用户读取 memberOf
func fetchAllGroupsByMemberOf(cfg *config.GlobalConfig) (map[string][]string, error) {
	users, err := FetchUsers(cfg)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]string)
	for _, u := range users {
		groups, err := fetchGroupsByMemberOf(cfg, u)
		if err != nil {
			continue
		}
		for _, g := range groups {
			result[g] = append(result[g], u)
		}
	}
	return result, nil
}

// fetchAllGroupsHierarchyBySearch 搜索所有组条目并读取成员和嵌套组。
func fetchAllGroupsHierarchyBySearch(cfg *config.GlobalConfig) (map[string]GroupHierarchy, error) {
	l, err := ldap.DialURL(cfg.LDAP.Host)
	if err != nil {
		return nil, fmt.Errorf("连接 LDAP 失败: %w", err)
	}
	defer l.Close()

	if err := l.Bind(cfg.LDAP.BindDN, cfg.LDAP.BindPassword); err != nil {
		return nil, fmt.Errorf("LDAP 认证失败: %w", err)
	}

	groupBaseDN := cfg.LDAP.GroupBaseDN
	if groupBaseDN == "" {
		groupBaseDN = cfg.LDAP.BaseDN
	}
	groupFilter := cfg.LDAP.GroupFilter
	if groupFilter == "" {
		groupFilter = "(objectClass=groupOfNames)"
	}
	memberAttr := cfg.LDAP.GroupMemberAttribute
	if memberAttr == "" {
		memberAttr = "member"
	}

	searchRequest := ldap.NewSearchRequest(
		groupBaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		groupFilter,
		[]string{"cn", memberAttr},
		nil,
	)
	sr, err := l.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("搜索组失败: %w", err)
	}

	result := make(map[string]GroupHierarchy)
	for _, entry := range sr.Entries {
		groupName := entry.GetAttributeValue("cn")
		if groupName == "" {
			continue
		}
		group := result[groupName]
		for _, memberDN := range entry.GetAttributeValues(memberAttr) {
			uid := extractAttrValue(memberDN, cfg.LDAP.UsernameAttribute)
			if uid != "" {
				group.Members = append(group.Members, uid)
				continue
			}
			subGroupName := extractCN(memberDN)
			if subGroupName != "" {
				group.SubGroups = append(group.SubGroups, subGroupName)
			}
		}
		result[groupName] = group
	}
	return result, nil
}

// extractCN 从 DN 中提取 CN 值
func extractCN(dn string) string {
	parsed, err := ldap.ParseDN(dn)
	if err != nil || len(parsed.RDNs) == 0 {
		return ""
	}
	for _, attr := range parsed.RDNs[0].Attributes {
		if strings.EqualFold(attr.Type, "cn") {
			return attr.Value
		}
	}
	return ""
}

// extractAttrValue 从 DN 中提取指定属性的值
func extractAttrValue(dn, attrType string) string {
	parsed, err := ldap.ParseDN(dn)
	if err != nil {
		return ""
	}
	for _, rdn := range parsed.RDNs {
		for _, attr := range rdn.Attributes {
			if strings.EqualFold(attr.Type, attrType) {
				return attr.Value
			}
		}
	}
	return ""
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
		errStr := err.Error()
		if strings.Contains(errStr, "32") || strings.Contains(errStr, "No Such Object") {
			return nil, fmt.Errorf("Base DN '%s' 不存在或无权访问，请检查 Base DN 配置", baseDN)
		}
		return nil, fmt.Errorf("LDAP 搜索失败（Base DN: %s, Filter: %s）: %w", baseDN, filter, err)
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

// GroupPreview LDAP 组预览信息
type GroupPreview struct {
	Name        string   `json:"name"`
	MemberCount int      `json:"member_count"`
	Members     []string `json:"members"`
	SubGroups   []string `json:"sub_groups"`
}

// TestGroups 测试 LDAP 组查询，返回组预览信息
// groupSearchMode 为空时返回空切片不报错
func TestGroups(host, bindDN, bindPassword, baseDN, groupSearchMode, groupBaseDN, groupFilter, groupMemberAttr, usernameAttr string) ([]GroupPreview, error) {
	if groupSearchMode == "" {
		return nil, nil
	}

	l, err := ldap.DialURL(host)
	if err != nil {
		return nil, fmt.Errorf("连接 LDAP 失败: %w", err)
	}
	defer l.Close()

	if err := l.Bind(bindDN, bindPassword); err != nil {
		return nil, fmt.Errorf("LDAP 认证失败: %w", err)
	}

	if groupBaseDN == "" {
		groupBaseDN = baseDN
	}
	if groupFilter == "" {
		groupFilter = "(objectClass=groupOfNames)"
	}
	if groupMemberAttr == "" {
		groupMemberAttr = "member"
	}

	if groupSearchMode == "group_search" {
		return testGroupsBySearch(l, groupBaseDN, groupFilter, groupMemberAttr, usernameAttr)
	}
	return testGroupsByMemberOf(l, baseDN, groupFilter, usernameAttr)
}

// testGroupsBySearch 搜索 groupOfNames 条目
func testGroupsBySearch(l *ldap.Conn, groupBaseDN, groupFilter, memberAttr, usernameAttr string) ([]GroupPreview, error) {
	searchRequest := ldap.NewSearchRequest(
		groupBaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		groupFilter,
		[]string{"cn", memberAttr},
		nil,
	)
	sr, err := l.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("搜索组失败: %w", err)
	}

	var result []GroupPreview
	for _, entry := range sr.Entries {
		groupName := entry.GetAttributeValue("cn")
		if groupName == "" {
			continue
		}
		var members []string
		var subGroups []string
		for _, dn := range entry.GetAttributeValues(memberAttr) {
			uid := extractAttrValue(dn, usernameAttr)
			if uid != "" {
				members = append(members, uid)
				continue
			}
			cn := extractCN(dn)
			if cn != "" {
				subGroups = append(subGroups, cn)
			}
		}
		preview := GroupPreview{
			Name:        groupName,
			MemberCount: len(members),
			Members:     firstN(members, 5),
			SubGroups:   firstN(subGroups, 5),
		}
		if preview.Members == nil {
			preview.Members = []string{}
		}
		if preview.SubGroups == nil {
			preview.SubGroups = []string{}
		}
		result = append(result, preview)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// testGroupsByMemberOf 从用户的 memberOf 属性聚合组
func testGroupsByMemberOf(l *ldap.Conn, baseDN, filter, usernameAttr string) ([]GroupPreview, error) {
	// 获取所有用户及其 memberOf
	searchRequest := ldap.NewSearchRequest(
		baseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		filter,
		[]string{usernameAttr, "memberOf"},
		nil,
	)
	sr, err := l.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("搜索用户失败: %w", err)
	}

	groupMembers := make(map[string][]string)
	for _, entry := range sr.Entries {
		username := entry.GetAttributeValue(usernameAttr)
		if username == "" {
			continue
		}
		for _, dn := range entry.GetAttributeValues("memberOf") {
			groupName := extractCN(dn)
			if groupName != "" {
				groupMembers[groupName] = append(groupMembers[groupName], username)
			}
		}
	}

	var result []GroupPreview
	for name, members := range groupMembers {
		preview := GroupPreview{
			Name:        name,
			MemberCount: len(members),
			Members:     firstN(members, 5),
			SubGroups:   []string{},
		}
		result = append(result, preview)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func firstN(slice []string, n int) []string {
	if len(slice) <= n {
		return slice
	}
	return slice[:n]
}
