package ldap

import (
  "net"
  "sort"
  "sync"
  "testing"
  "time"

  ber "github.com/go-asn1-ber/asn1-ber"
  ldap "github.com/go-ldap/ldap/v3"
  "github.com/picoaide/picoaide/internal/config"
)

// ============================================================
// 覆盖 ldap.DefaultTimeout 使连接错误测试快速失败
// ============================================================

func init() {
  ldap.DefaultTimeout = 100 * time.Millisecond
}

// ============================================================
// LDAP 协议响应构造辅助
// ============================================================

func readLDAPMessage(conn net.Conn) (messageID int64, opTag ber.Tag, err error) {
  packet, err := ber.ReadPacket(conn)
  if err != nil {
    return 0, 0, err
  }
  if len(packet.Children) < 2 {
    return 0, 0, nil
  }
  messageID = packet.Children[0].Value.(int64)
  opTag = packet.Children[1].Tag
  return
}

func writeEnvelope(conn net.Conn, messageID int64, protocolOp *ber.Packet) error {
  msg := ber.NewSequence("LDAP Message")
  msg.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, messageID, "Message ID"))
  msg.AppendChild(protocolOp)
  _, err := conn.Write(msg.Bytes())
  return err
}

func makeBindResponseOp(resultCode int64) *ber.Packet {
  op := ber.NewSequence("Bind Response")
  op.ClassType = ber.ClassApplication
  op.Tag = 1
  op.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, resultCode, "Result Code"))
  op.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "Matched DN"))
  op.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "Diagnostic Message"))
  return op
}

func sendBindResponse(conn net.Conn, messageID int64, resultCode int64) error {
  return writeEnvelope(conn, messageID, makeBindResponseOp(resultCode))
}

func makeSearchResultEntryOp(dn string, attrs map[string][]string) *ber.Packet {
  entry := ber.NewSequence("Search Result Entry")
  entry.ClassType = ber.ClassApplication
  entry.Tag = 4
  entry.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, dn, "Object Name"))

  attrList := ber.NewSequence("Attributes")
  var attrNames []string
  for name := range attrs {
    attrNames = append(attrNames, name)
  }
  sort.Strings(attrNames)
  for _, attrName := range attrNames {
    vals := attrs[attrName]
    pa := ber.NewSequence("Partial Attribute")
    pa.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, attrName, "Attribute Type"))

    valueSet := ber.NewSequence("Attribute Values")
    valueSet.Tag = ber.TagSet
    for _, v := range vals {
      valueSet.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, v, "Attribute Value"))
    }
    pa.AppendChild(valueSet)
    attrList.AppendChild(pa)
  }
  entry.AppendChild(attrList)
  return entry
}

func sendSearchResultEntry(conn net.Conn, messageID int64, dn string, attrs map[string][]string) error {
  return writeEnvelope(conn, messageID, makeSearchResultEntryOp(dn, attrs))
}

func makeSearchResultDoneOp(resultCode int64, matchedDN, diagMsg string) *ber.Packet {
  op := ber.NewSequence("Search Result Done")
  op.ClassType = ber.ClassApplication
  op.Tag = 5
  op.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, resultCode, "Result Code"))
  op.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, matchedDN, "Matched DN"))
  op.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, diagMsg, "Diagnostic Message"))
  return op
}

func sendSearchResultDone(conn net.Conn, messageID int64, resultCode int64, matchedDN, diagMsg string) error {
  return writeEnvelope(conn, messageID, makeSearchResultDoneOp(resultCode, matchedDN, diagMsg))
}

// ============================================================
// TCP 服务器辅助
// ============================================================

// testLDAPServer 启动一个 TCP 服务器，接受一个连接后调用 handler 处理。
// handler 必须处理所有请求（Bind + Search），否则客户端会阻塞。
func testLDAPServer(t *testing.T, handler func(net.Conn)) (string, func()) {
  t.Helper()
  ln, err := net.Listen("tcp", "127.0.0.1:0")
  if err != nil {
    t.Fatalf("listen failed: %v", err)
  }
  addr := ln.Addr().String()
  var wg sync.WaitGroup
  wg.Add(1)
  go func() {
    conn, err := ln.Accept()
    if err != nil {
      wg.Done()
      return
    }
    handler(conn)
    conn.Close()
    ln.Close()
    wg.Done()
  }()
  return addr, func() { ln.Close(); wg.Wait() }
}

// startMultiConnectServer 启动一个可接受多个连接的 TCP 服务器。
func startMultiConnectServer(t *testing.T, handler func(net.Conn, int)) (string, func()) {
  t.Helper()
  ln, err := net.Listen("tcp", "127.0.0.1:0")
  if err != nil {
    t.Fatalf("listen failed: %v", err)
  }
  addr := ln.Addr().String()
  var wg sync.WaitGroup
  go func() {
    idx := 0
    for {
      conn, err := ln.Accept()
      if err != nil {
        return
      }
      wg.Add(1)
      go func(c net.Conn, i int) {
        handler(c, i)
        c.Close()
        wg.Done()
      }(conn, idx)
      idx++
    }
  }()
  return addr, func() { ln.Close(); wg.Wait() }
}

// ============================================================
// 测试配置辅助
// ============================================================

func testConfig() *config.GlobalConfig {
  return &config.GlobalConfig{
    LDAP: config.LDAPConfig{
      Host:                 "ldap://127.0.0.1:1",
      BindDN:               "cn=admin,dc=example,dc=com",
      BindPassword:         "secret",
      BaseDN:               "dc=example,dc=com",
      Filter:               "(objectClass=person)",
      UsernameAttribute:    "uid",
      GroupSearchMode:      "member_of",
      GroupBaseDN:          "ou=groups,dc=example,dc=com",
      GroupFilter:          "(objectClass=group)",
      GroupMemberAttribute: "member",
    },
  }
}

func testConfigForConn(addr string, opts ...func(*config.GlobalConfig)) *config.GlobalConfig {
  cfg := testConfig()
  cfg.LDAP.Host = "ldap://" + addr
  for _, opt := range opts {
    opt(cfg)
  }
  return cfg
}

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
  dn := "OU=Groups,CN=MyTeam,DC=example"
  want := ""
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
  if len(got) != 2 || got[0] != "a" || got[1] != "b" {
    t.Errorf("firstN(%v, 5) = %v, want %v", input, got, input)
  }
}

func TestFirstN_ExactlyN(t *testing.T) {
  input := []string{"a", "b", "c"}
  got := firstN(input, 3)
  if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
    t.Errorf("firstN(%v, 3) = %v, want %v", input, got, input)
  }
}

func TestFirstN_MoreThanN(t *testing.T) {
  input := []string{"a", "b", "c", "d", "e", "f"}
  want := []string{"a", "b", "c"}
  got := firstN(input, 3)
  if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
    t.Errorf("firstN(%v, 3) = %v, want %v", input, got, want)
  }
}

func TestFirstN_Empty(t *testing.T) {
  got := firstN([]string{}, 5)
  if len(got) != 0 {
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
// 服务器 handler 工厂
// ============================================================

// handleBindError: 读取 BindRequest → 发送 BindResponse 失败 → 关闭
func handleBindError(conn net.Conn) {
  msgID, tag, err := readLDAPMessage(conn)
  if err != nil || tag != 0 {
    return
  }
  sendBindResponse(conn, msgID, 49) // Invalid Credentials
}

// handleSearchError: 读取 BindRequest → 成功 → 读取 SearchRequest → 发送错误 → 关闭
func handleSearchError(conn net.Conn) {
  msgID, tag, err := readLDAPMessage(conn)
  if err != nil || tag != 0 {
    return
  }
  sendBindResponse(conn, msgID, 0)
  msgID, tag, err = readLDAPMessage(conn)
  if err != nil || tag != 3 {
    return
  }
  sendSearchResultDone(conn, msgID, 32, "", "No Such Object")
}

// handleSearchEmpty: 读取 BindRequest → 成功 → 读取 SearchRequest → 返回空结果 → 关闭
func handleSearchEmpty(conn net.Conn) {
  msgID, tag, err := readLDAPMessage(conn)
  if err != nil || tag != 0 {
    return
  }
  sendBindResponse(conn, msgID, 0)
  msgID, tag, err = readLDAPMessage(conn)
  if err != nil || tag != 3 {
    return
  }
  sendSearchResultDone(conn, msgID, 0, "", "")
}

// ============================================================
// FetchUsers 测试
// ============================================================

func TestFetchUsers_ConnectionError(t *testing.T) {
  cfg := testConfig()
  _, err := FetchUsers(cfg)
  if err == nil {
    t.Fatal("expected connection error")
  }
}

func TestFetchUsers_BindError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleBindError)
  defer cleanup()
  cfg := testConfigForConn(addr)
  _, err := FetchUsers(cfg)
  if err == nil {
    t.Fatal("expected bind error")
  }
}

func TestFetchUsers_SearchError_NoSuchObject(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchError)
  defer cleanup()
  cfg := testConfigForConn(addr)
  _, err := FetchUsers(cfg)
  if err == nil {
    t.Fatal("expected search error for No Such Object")
  }
}

func TestFetchUsers_SearchError_Generic(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultDone(conn, msgID, 1, "", "Operations Error")
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  _, err := FetchUsers(cfg)
  if err == nil {
    t.Fatal("expected search error")
  }
}

func TestFetchUsers_Success(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=alice,dc=example,dc=com",
      map[string][]string{"uid": {"alice"}})
    sendSearchResultEntry(conn, msgID, "uid=bob,dc=example,dc=com",
      map[string][]string{"uid": {"bob"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  users, err := FetchUsers(cfg)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(users) != 2 {
    t.Fatalf("expected 2 users, got %d: %v", len(users), users)
  }
  if users[0] != "alice" || users[1] != "bob" {
    t.Errorf("expected [alice bob], got %v", users)
  }
}

func TestFetchUsers_EmptyEntries(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchEmpty)
  defer cleanup()
  cfg := testConfigForConn(addr)
  users, err := FetchUsers(cfg)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(users) != 0 {
    t.Errorf("expected empty users, got %v", users)
  }
}

func TestFetchUsers_EmptyUsername(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=,dc=example,dc=com",
      map[string][]string{"uid": {""}})
    sendSearchResultEntry(conn, msgID, "uid=alice,dc=example,dc=com",
      map[string][]string{"uid": {"alice"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  users, err := FetchUsers(cfg)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(users) != 1 || users[0] != "alice" {
    t.Errorf("expected [alice], got %v", users)
  }
}

// ============================================================
// fetchGroupsByMemberOf 测试
// ============================================================

func TestFetchGroupsByMemberOf_ConnectionError(t *testing.T) {
  cfg := testConfig()
  _, err := fetchGroupsByMemberOf(cfg, "testuser")
  if err == nil {
    t.Fatal("expected connection error")
  }
}

func TestFetchGroupsByMemberOf_BindError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleBindError)
  defer cleanup()
  cfg := testConfigForConn(addr)
  _, err := fetchGroupsByMemberOf(cfg, "testuser")
  if err == nil {
    t.Fatal("expected bind error")
  }
}

func TestFetchGroupsByMemberOf_SearchError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchError)
  defer cleanup()
  cfg := testConfigForConn(addr)
  _, err := fetchGroupsByMemberOf(cfg, "testuser")
  if err == nil {
    t.Fatal("expected search error")
  }
}

func TestFetchGroupsByMemberOf_NoUserFound(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchEmpty)
  defer cleanup()
  cfg := testConfigForConn(addr)
  _, err := fetchGroupsByMemberOf(cfg, "testuser")
  if err == nil {
    t.Fatal("expected '搜索用户失败' error for empty results")
  }
}

func TestFetchGroupsByMemberOf_Success(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=jdoe,dc=example,dc=com",
      map[string][]string{
        "uid":      {"jdoe"},
        "memberOf": {"CN=Developers,OU=Groups,DC=example,DC=com", "CN=Admins,OU=Groups,DC=example,DC=com"},
      })
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  groups, err := fetchGroupsByMemberOf(cfg, "jdoe")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(groups) != 2 {
    t.Fatalf("expected 2 groups, got %d: %v", len(groups), groups)
  }
  // memberOf 的顺序由 LDAP 服务端决定，与 map 迭代顺序一致
  if groups[0] != "Developers" || groups[1] != "Admins" {
    t.Errorf("expected [Developers Admins], got %v", groups)
  }
}

func TestFetchGroupsByMemberOf_EmptyMemberOf(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=jdoe,dc=example,dc=com",
      map[string][]string{"uid": {"jdoe"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  groups, err := fetchGroupsByMemberOf(cfg, "jdoe")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(groups) != 0 {
    t.Errorf("expected 0 groups, got %d: %v", len(groups), groups)
  }
}

// ============================================================
// fetchGroupsBySearch 测试
// ============================================================

func TestFetchGroupsBySearch_ConnectionError(t *testing.T) {
  cfg := testConfig()
  cfg.LDAP.GroupSearchMode = "group_search"
  _, err := fetchGroupsBySearch(cfg, "testuser")
  if err == nil {
    t.Fatal("expected connection error")
  }
}

func TestFetchGroupsBySearch_BindError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleBindError)
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "group_search"
  _, err := fetchGroupsBySearch(cfg, "testuser")
  if err == nil {
    t.Fatal("expected bind error")
  }
}

func TestFetchGroupsBySearch_UserSearchFail(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultDone(conn, msgID, 32, "", "No Such Object")
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "group_search"
  _, err := fetchGroupsBySearch(cfg, "testuser")
  if err == nil {
    t.Fatal("expected user search error")
  }
}

func TestFetchGroupsBySearch_UserSearchEmpty(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchEmpty)
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "group_search"
  _, err := fetchGroupsBySearch(cfg, "testuser")
  if err == nil {
    t.Fatal("expected user search error for empty results")
  }
}

func TestFetchGroupsBySearch_GroupSearchFail(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    // Bind
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    // 用户搜索（第一次 Search）
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=testuser,dc=example,dc=com",
      map[string][]string{"dn": {"uid=testuser,dc=example,dc=com"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
    // 组搜索（第二次 Search）——不响应，连接会被关闭
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "group_search"
  _, err := fetchGroupsBySearch(cfg, "testuser")
  if err == nil {
    t.Fatal("expected group search error")
  }
}

func TestFetchGroupsBySearch_Success(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    // 用户搜索：返回 DN
    sendSearchResultEntry(conn, msgID, "uid=jdoe,dc=example,dc=com",
      map[string][]string{"dn": {"uid=jdoe,dc=example,dc=com"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    // 组搜索：返回组
    sendSearchResultEntry(conn, msgID, "CN=Developers,OU=Groups,DC=example,DC=com",
      map[string][]string{"cn": {"Developers"}})
    sendSearchResultEntry(conn, msgID, "CN=Admins,OU=Groups,DC=example,DC=com",
      map[string][]string{"cn": {"Admins"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "group_search"
  groups, err := fetchGroupsBySearch(cfg, "jdoe")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(groups) != 2 {
    t.Fatalf("expected 2 groups, got %d: %v", len(groups), groups)
  }
  // 组的顺序由搜索返回条目顺序确定
  if groups[0] != "Developers" || groups[1] != "Admins" {
    t.Errorf("expected [Developers Admins], got %v", groups)
  }
}

func TestFetchGroupsBySearch_Defaults(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=jdoe,dc=example,dc=com",
      map[string][]string{"dn": {"uid=jdoe,dc=example,dc=com"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "CN=Team,OU=Groups,DC=example,DC=com",
      map[string][]string{"cn": {"Team"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  cfg := testConfigForConn(addr, func(cfg *config.GlobalConfig) {
    cfg.LDAP.GroupSearchMode = "group_search"
    cfg.LDAP.GroupBaseDN = ""
    cfg.LDAP.GroupFilter = ""
    cfg.LDAP.GroupMemberAttribute = ""
  })
  groups, err := fetchGroupsBySearch(cfg, "jdoe")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(groups) != 1 || groups[0] != "Team" {
    t.Errorf("expected [Team], got %v", groups)
  }
}

// ============================================================
// FetchUserGroups 测试（分发逻辑）
// ============================================================

func TestFetchUserGroups_MemberOfMode(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=jdoe,dc=example,dc=com",
      map[string][]string{
        "uid":      {"jdoe"},
        "memberOf": {"CN=TeamA,OU=Groups,DC=example,DC=com"},
      })
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "member_of"
  groups, err := FetchUserGroups(cfg, "jdoe")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(groups) != 1 || groups[0] != "TeamA" {
    t.Errorf("expected [TeamA], got %v", groups)
  }
}

func TestFetchUserGroups_GroupSearchMode(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=jdoe,dc=example,dc=com",
      map[string][]string{"dn": {"uid=jdoe,dc=example,dc=com"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "CN=TeamB,OU=Groups,DC=example,DC=com",
      map[string][]string{"cn": {"TeamB"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "group_search"
  groups, err := FetchUserGroups(cfg, "jdoe")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(groups) != 1 || groups[0] != "TeamB" {
    t.Errorf("expected [TeamB], got %v", groups)
  }
}

// ============================================================
// FetchAllGroupsWithHierarchy 测试
// ============================================================

func TestFetchAllGroupsWithHierarchy_MemberOf_Error(t *testing.T) {
  cfg := testConfig()
  cfg.LDAP.GroupSearchMode = "member_of"
  _, err := FetchAllGroupsWithHierarchy(cfg)
  if err == nil {
    t.Fatal("expected error (FetchUsers fails)")
  }
}

func TestFetchAllGroupsWithHierarchy_GroupSearch_Error(t *testing.T) {
  cfg := testConfig()
  cfg.LDAP.GroupSearchMode = "group_search"
  _, err := FetchAllGroupsWithHierarchy(cfg)
  if err == nil {
    t.Fatal("expected error (connection fails)")
  }
}

// ============================================================
// fetchAllGroupsByMemberOf 测试
// ============================================================

func TestFetchAllGroupsByMemberOf_ContinueOnUserError(t *testing.T) {
  cfg := testConfig()
  _, err := fetchAllGroupsByMemberOf(cfg)
  if err == nil {
    t.Fatal("expected error")
  }
}

// ============================================================
// fetchAllGroupsHierarchyBySearch 测试
// ============================================================

func TestFetchAllGroupsHierarchyBySearch_ConnectionError(t *testing.T) {
  cfg := testConfig()
  cfg.LDAP.GroupSearchMode = "group_search"
  _, err := fetchAllGroupsHierarchyBySearch(cfg)
  if err == nil {
    t.Fatal("expected connection error")
  }
}

func TestFetchAllGroupsHierarchyBySearch_BindError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleBindError)
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "group_search"
  _, err := fetchAllGroupsHierarchyBySearch(cfg)
  if err == nil {
    t.Fatal("expected bind error")
  }
}

func TestFetchAllGroupsHierarchyBySearch_SearchError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchError)
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "group_search"
  _, err := fetchAllGroupsHierarchyBySearch(cfg)
  if err == nil {
    t.Fatal("expected search error")
  }
}

func TestFetchAllGroupsHierarchyBySearch_Success(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "CN=Developers,OU=Groups,DC=example,DC=com",
      map[string][]string{
        "cn":     {"Developers"},
        "member": {"uid=alice,dc=example,dc=com", "uid=bob,dc=example,dc=com", "CN=SubGroup,OU=Groups,DC=example,DC=com"},
      })
    sendSearchResultEntry(conn, msgID, "CN=Admins,OU=Groups,DC=example,DC=com",
      map[string][]string{
        "cn":     {"Admins"},
        "member": {"CN=DevOps,OU=Groups,DC=example,DC=com"},
      })
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "group_search"
  result, err := fetchAllGroupsHierarchyBySearch(cfg)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(result) != 2 {
    t.Fatalf("expected 2 groups, got %d", len(result))
  }
  dev, ok := result["Developers"]
  if !ok {
    t.Fatal("missing 'Developers'")
  }
  if len(dev.Members) != 2 || dev.Members[0] != "alice" || dev.Members[1] != "bob" {
    t.Errorf("Developers members = %v, want [alice bob]", dev.Members)
  }
  if len(dev.SubGroups) != 1 || dev.SubGroups[0] != "SubGroup" {
    t.Errorf("Developers subgroups = %v, want [SubGroup]", dev.SubGroups)
  }
  admins, ok := result["Admins"]
  if !ok {
    t.Fatal("missing 'Admins'")
  }
  if len(admins.Members) != 0 {
    t.Errorf("Admins members = %v, want []", admins.Members)
  }
  if len(admins.SubGroups) != 1 || admins.SubGroups[0] != "DevOps" {
    t.Errorf("Admins subgroups = %v, want [DevOps]", admins.SubGroups)
  }
}

func TestFetchAllGroupsHierarchyBySearch_EmptyGroupName(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=no-cn,dc=example,dc=com",
      map[string][]string{"uid": {"no-cn"}})
    sendSearchResultEntry(conn, msgID, "CN=Valid,OU=Groups,DC=example,DC=com",
      map[string][]string{"cn": {"Valid"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "group_search"
  result, err := fetchAllGroupsHierarchyBySearch(cfg)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(result) != 1 {
    t.Fatalf("expected 1 group, got %d", len(result))
  }
  if _, ok := result["Valid"]; !ok {
    t.Errorf("expected 'Valid', got %v", result)
  }
}

func TestFetchAllGroupsHierarchyBySearch_Defaults(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "CN=DefaultGroup,OU=Groups,DC=example,DC=com",
      map[string][]string{"cn": {"DefaultGroup"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  cfg := testConfigForConn(addr, func(cfg *config.GlobalConfig) {
    cfg.LDAP.GroupSearchMode = "group_search"
    cfg.LDAP.GroupBaseDN = ""
    cfg.LDAP.GroupFilter = ""
    cfg.LDAP.GroupMemberAttribute = ""
  })
  result, err := fetchAllGroupsHierarchyBySearch(cfg)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(result) != 1 {
    t.Fatalf("expected 1 group, got %d", len(result))
  }
}

// ============================================================
// TestConnection 测试
// ============================================================

func TestTestConnection_ConnectionError(t *testing.T) {
  _, err := TestConnection("ldap://127.0.0.1:1", "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "(objectClass=person)", "uid")
  if err == nil {
    t.Fatal("expected connection error")
  }
}

func TestTestConnection_BindError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleBindError)
  defer cleanup()
  _, err := TestConnection("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "(objectClass=person)", "uid")
  if err == nil {
    t.Fatal("expected bind error")
  }
}

func TestTestConnection_SearchError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchError)
  defer cleanup()
  _, err := TestConnection("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "(objectClass=person)", "uid")
  if err == nil {
    t.Fatal("expected search error")
  }
}

func TestTestConnection_BaseDNNotFound(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultDone(conn, msgID, 32, "", "No Such Object")
  })
  defer cleanup()
  _, err := TestConnection("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "(objectClass=person)", "uid")
  if err == nil {
    t.Fatal("expected Base DN error")
  }
}

func TestTestConnection_Success(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=alice,dc=ex,dc=com",
      map[string][]string{"uid": {"alice"}})
    sendSearchResultEntry(conn, msgID, "uid=bob,dc=ex,dc=com",
      map[string][]string{"uid": {"bob"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  users, err := TestConnection("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "(objectClass=person)", "uid")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(users) != 2 || users[0] != "alice" || users[1] != "bob" {
    t.Errorf("expected [alice bob], got %v", users)
  }
}

func TestTestConnection_EmptyEntries(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchEmpty)
  defer cleanup()
  users, err := TestConnection("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "(objectClass=person)", "uid")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(users) != 0 {
    t.Errorf("expected 0 users, got %d", len(users))
  }
}

func TestTestConnection_EmptyUsername(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=,dc=ex,dc=com",
      map[string][]string{"uid": {""}})
    sendSearchResultEntry(conn, msgID, "uid=alice,dc=ex,dc=com",
      map[string][]string{"uid": {"alice"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  users, err := TestConnection("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "(objectClass=person)", "uid")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(users) != 1 || users[0] != "alice" {
    t.Errorf("expected [alice], got %v", users)
  }
}

// ============================================================
// Authenticate 测试
// ============================================================

func TestAuthenticate_FirstDialError(t *testing.T) {
  cfg := testConfig()
  result := Authenticate(cfg, "testuser", "pwd")
  if result {
    t.Fatal("expected false")
  }
}

func TestAuthenticate_BindError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleBindError)
  defer cleanup()
  cfg := testConfigForConn(addr)
  result := Authenticate(cfg, "testuser", "pwd")
  if result {
    t.Fatal("expected false")
  }
}

func TestAuthenticate_SearchError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchError)
  defer cleanup()
  cfg := testConfigForConn(addr)
  result := Authenticate(cfg, "testuser", "pwd")
  if result {
    t.Fatal("expected false (search error)")
  }
}

func TestAuthenticate_SearchEmpty(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchEmpty)
  defer cleanup()
  cfg := testConfigForConn(addr)
  result := Authenticate(cfg, "testuser", "pwd")
  if result {
    t.Fatal("expected false (empty search results)")
  }
}

func TestAuthenticate_SecondBindFail(t *testing.T) {
  addr, cleanup := startMultiConnectServer(t, func(conn net.Conn, idx int) {
    if idx == 0 {
      msgID, tag, err := readLDAPMessage(conn)
      if err != nil || tag != 0 {
        return
      }
      sendBindResponse(conn, msgID, 0)
      msgID, tag, err = readLDAPMessage(conn)
      if err != nil || tag != 3 {
        return
      }
      sendSearchResultEntry(conn, msgID, "uid=testuser,dc=example,dc=com",
        map[string][]string{"dn": {"uid=testuser,dc=example,dc=com"}})
      sendSearchResultDone(conn, msgID, 0, "", "")
      return
    }
    if idx == 1 {
      msgID, tag, err := readLDAPMessage(conn)
      if err != nil || tag != 0 {
        return
      }
      sendBindResponse(conn, msgID, 49)
    }
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  result := Authenticate(cfg, "testuser", "wrong_password")
  if result {
    t.Fatal("expected false (second bind fails with invalid credentials)")
  }
}

func TestAuthenticate_SecondDialFail(t *testing.T) {
  ln, err := net.Listen("tcp", "127.0.0.1:0")
  if err != nil {
    t.Fatal(err)
  }
  addr := ln.Addr().String()

  done := make(chan struct{})
  go func() {
    conn, err := ln.Accept()
    if err != nil {
      return
    }
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      conn.Close()
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      conn.Close()
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=testuser,dc=example,dc=com",
      map[string][]string{"dn": {"uid=testuser,dc=example,dc=com"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
    conn.Close()
    ln.Close()
    close(done)
  }()

  cfg := testConfigForConn(addr)
  result := Authenticate(cfg, "testuser", "pwd")
  if result {
    t.Fatal("expected false (second DialURL fails)")
  }
  <-done
}

func TestAuthenticate_Success(t *testing.T) {
  addr, cleanup := startMultiConnectServer(t, func(conn net.Conn, idx int) {
    if idx == 0 {
      msgID, tag, err := readLDAPMessage(conn)
      if err != nil || tag != 0 {
        return
      }
      sendBindResponse(conn, msgID, 0)
      msgID, tag, err = readLDAPMessage(conn)
      if err != nil || tag != 3 {
        return
      }
      sendSearchResultEntry(conn, msgID, "uid=testuser,dc=example,dc=com",
        map[string][]string{"dn": {"uid=testuser,dc=example,dc=com"}})
      sendSearchResultDone(conn, msgID, 0, "", "")
      return
    }
    if idx == 1 {
      msgID, tag, err := readLDAPMessage(conn)
      if err != nil || tag != 0 {
        return
      }
      sendBindResponse(conn, msgID, 0)
    }
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  result := Authenticate(cfg, "testuser", "correct_password")
  if !result {
    t.Fatal("expected true")
  }
}

// ============================================================
// TestGroups 测试
// ============================================================

func TestTestGroups_EmptyMode(t *testing.T) {
  result, err := TestGroups("ldap://127.0.0.1:1", "", "", "", "", "", "", "", "")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if result != nil {
    t.Fatalf("expected nil result, got %v", result)
  }
}

func TestTestGroups_ConnectionError(t *testing.T) {
  _, err := TestGroups("ldap://127.0.0.1:1", "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "member_of", "", "", "", "uid")
  if err == nil {
    t.Fatal("expected connection error")
  }
}

func TestTestGroups_BindError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleBindError)
  defer cleanup()
  _, err := TestGroups("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "member_of", "", "", "", "uid")
  if err == nil {
    t.Fatal("expected bind error")
  }
}

func TestTestGroups_MemberOf_SearchError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchError)
  defer cleanup()
  _, err := TestGroups("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "member_of", "", "", "", "uid")
  if err == nil {
    t.Fatal("expected search error")
  }
}

func TestTestGroups_GroupSearch_SearchError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, handleSearchError)
  defer cleanup()
  _, err := TestGroups("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "group_search", "", "", "", "uid")
  if err == nil {
    t.Fatal("expected search error")
  }
}

func TestTestGroups_MemberOf_Defaults(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=alice,dc=example,dc=com",
      map[string][]string{
        "uid":      {"alice"},
        "memberOf": {"CN=TeamA,OU=Groups,DC=example,DC=com"},
      })
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  result, err := TestGroups("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "member_of", "", "", "", "uid")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(result) != 1 || result[0].Name != "TeamA" {
    t.Errorf("expected [TeamA], got %v", result)
  }
}

func TestTestGroups_MemberOf_Success(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=alice,dc=example,dc=com",
      map[string][]string{
        "uid":      {"alice"},
        "memberOf": {"CN=TeamA,OU=Groups,DC=example,DC=com"},
      })
    sendSearchResultEntry(conn, msgID, "uid=bob,dc=example,dc=com",
      map[string][]string{
        "uid":      {"bob"},
        "memberOf": {"CN=TeamA,OU=Groups,DC=example,DC=com", "CN=TeamB,OU=Groups,DC=example,DC=com"},
      })
    sendSearchResultEntry(conn, msgID, "uid=charlie,dc=example,dc=com",
      map[string][]string{"uid": {"charlie"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  result, err := TestGroups("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "member_of", "", "", "", "uid")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(result) != 2 {
    t.Fatalf("expected 2 groups, got %d", len(result))
  }
  if result[0].Name != "TeamA" || result[1].Name != "TeamB" {
    t.Errorf("expected [TeamA TeamB], got %v", result)
  }
  if result[0].MemberCount != 2 {
    t.Errorf("TeamA MemberCount = %d, want 2", result[0].MemberCount)
  }
  if result[1].MemberCount != 1 {
    t.Errorf("TeamB MemberCount = %d, want 1", result[1].MemberCount)
  }
  if result[0].SubGroups == nil {
    t.Errorf("TeamA.SubGroups is nil, want []string{}")
  }
}

func TestTestGroups_GroupSearch_Success(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "CN=DevTeam,OU=Groups,DC=example,DC=com",
      map[string][]string{
        "cn":     {"DevTeam"},
        "member": {"uid=alice,dc=example,dc=com", "uid=bob,dc=example,dc=com", "CN=SubTeam,OU=Groups,DC=example,DC=com"},
      })
    sendSearchResultEntry(conn, msgID, "CN=Ops,OU=Groups,DC=example,DC=com",
      map[string][]string{
        "cn":     {"Ops"},
        "member": {"CN=DevOps,OU=Groups,DC=example,DC=com"},
      })
    sendSearchResultEntry(conn, msgID, "CN=Empty,OU=Groups,DC=example,DC=com",
      map[string][]string{"cn": {"Empty"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  result, err := TestGroups("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "group_search", "", "", "", "uid")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(result) != 3 {
    t.Fatalf("expected 3 groups, got %d", len(result))
  }
  if result[0].Name != "DevTeam" || result[1].Name != "Empty" || result[2].Name != "Ops" {
    t.Errorf("expected [DevTeam Empty Ops], got names: %v", result)
  }
  if result[0].MemberCount != 2 {
    t.Errorf("DevTeam MemberCount = %d, want 2", result[0].MemberCount)
  }
  if len(result[0].Members) != 2 {
    t.Errorf("DevTeam Members = %v, want [alice bob]", result[0].Members)
  }
  if len(result[0].SubGroups) != 1 || result[0].SubGroups[0] != "SubTeam" {
    t.Errorf("DevTeam SubGroups = %v, want [SubTeam]", result[0].SubGroups)
  }
  if result[2].MemberCount != 0 {
    t.Errorf("Ops MemberCount = %d, want 0", result[2].MemberCount)
  }
  if result[1].Members == nil {
    t.Errorf("Empty.Members is nil, want []string{}")
  }
  if result[1].SubGroups == nil {
    t.Errorf("Empty.SubGroups is nil, want []string{}")
  }
  if len(result[1].Members) != 0 || len(result[1].SubGroups) != 0 {
    t.Errorf("Empty members/subgroups should be empty, got members=%v, subgroups=%v",
      result[1].Members, result[1].SubGroups)
  }
}

func TestTestGroups_GroupSearch_SkipEmptyCN(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "uid=nocn,dc=example,dc=com",
      map[string][]string{"uid": {"nocn"}})
    sendSearchResultEntry(conn, msgID, "CN=Valid,OU=Groups,DC=example,DC=com",
      map[string][]string{"cn": {"Valid"}})
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  result, err := TestGroups("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "group_search", "", "", "", "uid")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(result) != 1 || result[0].Name != "Valid" {
    t.Errorf("expected [Valid], got %v", result)
  }
}

func TestTestGroups_MemberOf_SkipEmptyUsername(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultEntry(conn, msgID, "cn=no-uid,dc=example,dc=com",
      map[string][]string{"cn": {"no-uid"}})
    sendSearchResultEntry(conn, msgID, "uid=alice,dc=example,dc=com",
      map[string][]string{
        "uid":      {"alice"},
        "memberOf": {"CN=TeamA,OU=Groups,DC=example,DC=com"},
      })
    sendSearchResultDone(conn, msgID, 0, "", "")
  })
  defer cleanup()
  result, err := TestGroups("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "member_of", "", "", "", "uid")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(result) != 1 || result[0].Name != "TeamA" {
    t.Errorf("expected [TeamA], got %v", result)
  }
}

// ============================================================
// fetchAllGroupsByMemberOf 成功路径
// ============================================================

func TestFetchAllGroupsByMemberOf_Success(t *testing.T) {
  // Simulate 2 users, each in different groups, one fetch fails
  var mu sync.Mutex
  callCount := 0
  addr, cleanup := startMultiConnectServer(t, func(conn net.Conn, idx int) {
    mu.Lock()
    callCount++
    cnt := callCount
    mu.Unlock()
    if cnt == 1 {
      // FetchUsers: first search returns all users
      msgID, tag, err := readLDAPMessage(conn)
      if err != nil || tag != 0 {
        return
      }
      sendBindResponse(conn, msgID, 0)
      msgID, tag, err = readLDAPMessage(conn)
      if err != nil || tag != 3 {
        return
      }
      sendSearchResultEntry(conn, msgID, "uid=alice,dc=example,dc=com",
        map[string][]string{"uid": {"alice"}})
      sendSearchResultEntry(conn, msgID, "uid=bob,dc=example,dc=com",
        map[string][]string{"uid": {"bob"}})
      sendSearchResultDone(conn, msgID, 0, "", "")
      return
    }
    if cnt == 2 {
      // extractGroupsByMemberOf for alice
      msgID, tag, err := readLDAPMessage(conn)
      if err != nil || tag != 0 {
        return
      }
      sendBindResponse(conn, msgID, 0)
      msgID, tag, err = readLDAPMessage(conn)
      if err != nil || tag != 3 {
        return
      }
      sendSearchResultEntry(conn, msgID, "uid=alice,dc=example,dc=com",
        map[string][]string{
          "uid":      {"alice"},
          "memberOf": {"CN=TeamA,OU=Groups,DC=example,DC=com"},
        })
      sendSearchResultDone(conn, msgID, 0, "", "")
      return
    }
    if cnt == 3 {
      // extractGroupsByMemberOf for bob — 返回错误，验证 continue
      msgID, tag, err := readLDAPMessage(conn)
      if err != nil || tag != 0 {
        return
      }
      sendBindResponse(conn, msgID, 0)
      msgID, tag, err = readLDAPMessage(conn)
      if err != nil || tag != 3 {
        return
      }
      sendSearchResultDone(conn, msgID, 1, "", "Operations Error")
      return
    }
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "member_of"
  result, err := fetchAllGroupsByMemberOf(cfg)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(result) != 1 {
    t.Fatalf("expected 1 group (alice's), got %d: %v", len(result), result)
  }
  if result["TeamA"] == nil || len(result["TeamA"]) != 1 || result["TeamA"][0] != "alice" {
    t.Errorf("expected TeamA -> [alice], got %v", result)
  }
}

// ============================================================
// FetchAllGroupsWithHierarchy 成功路径
// ============================================================

func TestFetchAllGroupsWithHierarchy_MemberOf_Success(t *testing.T) {
  var mu sync.Mutex
  callCount := 0
  addr, cleanup := startMultiConnectServer(t, func(conn net.Conn, idx int) {
    mu.Lock()
    callCount++
    cnt := callCount
    mu.Unlock()

    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }

    if cnt == 1 {
      // FetchUsers
      sendSearchResultEntry(conn, msgID, "uid=alice,dc=example,dc=com",
        map[string][]string{"uid": {"alice"}})
      sendSearchResultDone(conn, msgID, 0, "", "")
      return
    }
    if cnt == 2 {
      // fetchGroupsByMemberOf for alice
      sendSearchResultEntry(conn, msgID, "uid=alice,dc=example,dc=com",
        map[string][]string{
          "uid":      {"alice"},
          "memberOf": {"CN=TeamA,OU=Groups,DC=example,DC=com"},
        })
      sendSearchResultDone(conn, msgID, 0, "", "")
    }
  })
  defer cleanup()
  cfg := testConfigForConn(addr)
  cfg.LDAP.GroupSearchMode = "member_of"
  result, err := FetchAllGroupsWithHierarchy(cfg)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(result) != 1 || result["TeamA"].Members[0] != "alice" {
    t.Errorf("expected TeamA -> [alice], got %v", result)
  }
}

// ============================================================
// TestConnection 通用搜索错误（非 32 代码）
// ============================================================

func TestTestConnection_GenericSearchError(t *testing.T) {
  addr, cleanup := testLDAPServer(t, func(conn net.Conn) {
    msgID, tag, err := readLDAPMessage(conn)
    if err != nil || tag != 0 {
      return
    }
    sendBindResponse(conn, msgID, 0)
    msgID, tag, err = readLDAPMessage(conn)
    if err != nil || tag != 3 {
      return
    }
    sendSearchResultDone(conn, msgID, 50, "", "Insufficient Access Rights")
  })
  defer cleanup()
  _, err := TestConnection("ldap://"+addr, "cn=admin,dc=ex,dc=com", "pwd", "dc=ex,dc=com", "(objectClass=person)", "uid")
  if err == nil {
    t.Fatal("expected search error")
  }
}
