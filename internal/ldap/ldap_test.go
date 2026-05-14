package ldap

import (
  "reflect"
  "testing"
)

// ============================================================
// extractCN 测试
// ============================================================

func TestExtractCN_Simple(t *testing.T) {
  dn := "CN=John Doe,OU=Users,DC=example,DC=com"
  want := "John Doe"
  got := extractCN(dn)
  if got != want {
    t.Errorf("extractCN(%q) = %q, want %q", dn, got, want)
  }
}

func TestExtractCN_WithSpaces(t *testing.T) {
  dn := "CN=Developers,OU=Groups,DC=example,DC=com"
  want := "Developers"
  got := extractCN(dn)
  if got != want {
    t.Errorf("extractCN(%q) = %q, want %q", dn, got, want)
  }
}

func TestExtractCN_EmptyDN(t *testing.T) {
  got := extractCN("")
  if got != "" {
    t.Errorf("extractCN(\"\") = %q, want \"\"", got)
  }
}

func TestExtractCN_InvalidDN(t *testing.T) {
  got := extractCN("not-a-valid-dn")
  if got != "" {
    t.Errorf("extractCN(%q) = %q, want \"\"", "not-a-valid-dn", got)
  }
}

func TestExtractCN_CNNotFirst(t *testing.T) {
  // extractCN 只检查第一个 RDN 中的 CN 属性
  dn := "OU=Groups,CN=MyTeam,DC=example"
  want := "" // CN 在第二个 RDN 中，函数不遍历所有 RDN
  got := extractCN(dn)
  if got != want {
    t.Errorf("extractCN(%q) = %q, want %q", dn, got, want)
  }
}

func TestExtractCN_NoCN(t *testing.T) {
  dn := "OU=Users,DC=example,DC=com"
  got := extractCN(dn)
  if got != "" {
    t.Errorf("extractCN(%q) = %q, want \"\"", dn, got)
  }
}

func TestExtractCN_CaseInsensitive(t *testing.T) {
  dn := "cn=admin,dc=example,dc=com"
  want := "admin"
  got := extractCN(dn)
  if got != want {
    t.Errorf("extractCN(%q) = %q, want %q", dn, got, want)
  }
}

// ============================================================
// extractAttrValue 测试
// ============================================================

func TestExtractAttrValue_UID(t *testing.T) {
  dn := "uid=jdoe,ou=users,dc=example,dc=com"
  want := "jdoe"
  got := extractAttrValue(dn, "uid")
  if got != want {
    t.Errorf("extractAttrValue(%q, \"uid\") = %q, want %q", dn, got, want)
  }
}

func TestExtractAttrValue_CN(t *testing.T) {
  dn := "CN=test-group,OU=Groups,DC=example"
  want := "test-group"
  got := extractAttrValue(dn, "cn")
  if got != want {
    t.Errorf("extractAttrValue(%q, \"cn\") = %q, want %q", dn, got, want)
  }
}

func TestExtractAttrValue_MultipleRDNs(t *testing.T) {
  dn := "uid=jdoe+cn=John,ou=users,dc=example"
  // should find uid in first RDN which has multi-valued attributes
  want := "jdoe"
  got := extractAttrValue(dn, "uid")
  if got != want {
    t.Errorf("extractAttrValue(%q, \"uid\") = %q, want %q", dn, got, want)
  }
}

func TestExtractAttrValue_NotFound(t *testing.T) {
  dn := "CN=admin,DC=example"
  got := extractAttrValue(dn, "mail")
  if got != "" {
    t.Errorf("extractAttrValue(%q, \"mail\") = %q, want \"\"", dn, got)
  }
}

func TestExtractAttrValue_EmptyDN(t *testing.T) {
  got := extractAttrValue("", "uid")
  if got != "" {
    t.Errorf("extractAttrValue(\"\", \"uid\") = %q, want \"\"", got)
  }
}

func TestExtractAttrValue_InvalidDN(t *testing.T) {
  got := extractAttrValue("invalid", "cn")
  if got != "" {
    t.Errorf("extractAttrValue(\"invalid\", \"cn\") = %q, want \"\"", got)
  }
}

// ============================================================
// firstN 测试
// ============================================================

func TestFirstN_LessThanN(t *testing.T) {
  input := []string{"a", "b"}
  got := firstN(input, 5)
  if !reflect.DeepEqual(got, input) {
    t.Errorf("firstN(%v, 5) = %v, want %v", input, got, input)
  }
}

func TestFirstN_ExactlyN(t *testing.T) {
  input := []string{"a", "b", "c"}
  got := firstN(input, 3)
  if !reflect.DeepEqual(got, input) {
    t.Errorf("firstN(%v, 3) = %v, want %v", input, got, input)
  }
}

func TestFirstN_MoreThanN(t *testing.T) {
  input := []string{"a", "b", "c", "d", "e", "f"}
  want := []string{"a", "b", "c"}
  got := firstN(input, 3)
  if !reflect.DeepEqual(got, want) {
    t.Errorf("firstN(%v, 3) = %v, want %v", input, got, want)
  }
}

func TestFirstN_Empty(t *testing.T) {
  got := firstN([]string{}, 5)
  if got == nil || len(got) != 0 {
    t.Errorf("firstN([], 5) = %v, want empty slice", got)
  }
}

func TestFirstN_NIsZero(t *testing.T) {
  input := []string{"a", "b"}
  got := firstN(input, 0)
  if len(got) != 0 {
    t.Errorf("firstN(%v, 0) = %v, want empty", input, got)
  }
}

// ============================================================
// GroupHierarchy 类型检查（编译时验证）
// ============================================================

func TestGroupHierarchyStruct(t *testing.T) {
  gh := GroupHierarchy{
    Members:   []string{"user1", "user2"},
    SubGroups: []string{"child-group"},
  }
  if len(gh.Members) != 2 {
    t.Errorf("expected 2 members, got %d", len(gh.Members))
  }
  if len(gh.SubGroups) != 1 {
    t.Errorf("expected 1 subgroup, got %d", len(gh.SubGroups))
  }
}

// ============================================================
// GroupPreview 类型检查
// ============================================================

func TestGroupPreviewStruct(t *testing.T) {
  gp := GroupPreview{
    Name:        "test-group",
    MemberCount: 3,
    Members:     []string{"u1", "u2", "u3"},
    SubGroups:   []string{},
  }
  if gp.Name != "test-group" {
    t.Errorf("Name = %q, want %q", gp.Name, "test-group")
  }
  if gp.MemberCount != 3 {
    t.Errorf("MemberCount = %d, want 3", gp.MemberCount)
  }
}
