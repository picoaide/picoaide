package web

import (
  "bufio"
  "context"
  "io"
  "net/http"
  "net/url"
  "strings"
  "testing"
  "time"

  "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToken_GenerateAndRetrieve(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/mcp/token", "testuser")
  assertStatus(t, resp, 200)
  var result struct {
    Success bool   `json:"success"`
    Token   string `json:"token"`
  }
  parseJSON(t, resp, &result)
  if result.Token == "" {
    t.Error("should have token")
  }

  // 再次获取应为同一个 token
  resp2 := env.get(t, "/api/mcp/token", "testuser")
  assertStatus(t, resp2, 200)
  var result2 struct {
    Token string `json:"token"`
  }
  parseJSON(t, resp2, &result2)
  if result.Token != result2.Token {
    t.Error("token should be stable")
  }
}

func TestMCPSSEGetLegacySendsEndpointEvent(t *testing.T) {
  env := setupTestServer(t)
  token := getMCPTokenForTest(t, env)

  ctx, cancel := context.WithTimeout(context.Background(), time.Second)
  defer cancel()
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, env.HTTP.URL+"/api/mcp/sse/browser?token="+token, nil)
  if err != nil {
    t.Fatalf("NewRequest: %v", err)
  }
  req.Header.Set("Accept", "text/event-stream")
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("GET MCP SSE: %v", err)
  }
  defer resp.Body.Close()
  assertStatus(t, resp, 200)

  line, err := bufio.NewReader(resp.Body).ReadString('\n')
  if err != nil {
    t.Fatalf("read SSE line: %v", err)
  }
  if strings.TrimSpace(line) != "event: endpoint" {
    t.Fatalf("first SSE line = %q, want endpoint event", line)
  }
}

func TestMCPSSEGetStreamableDoesNotSendLegacyEndpoint(t *testing.T) {
  env := setupTestServer(t)
  token := getMCPTokenForTest(t, env)

  ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
  defer cancel()
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, env.HTTP.URL+"/api/mcp/sse/browser?token="+token, nil)
  if err != nil {
    t.Fatalf("NewRequest: %v", err)
  }
  req.Header.Set("Accept", "text/event-stream")
  req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
  req.Header.Set("Mcp-Session-Id", "test-session")
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("GET streamable MCP SSE: %v", err)
  }
  defer resp.Body.Close()
  assertStatus(t, resp, 200)

  line, err := bufio.NewReader(resp.Body).ReadString('\n')
  if err != nil && ctx.Err() == nil && err != io.EOF {
    t.Fatalf("read streamable SSE line: %v", err)
  }
  if strings.TrimSpace(line) == "event: endpoint" {
    t.Fatal("streamable standalone SSE should not emit legacy endpoint event")
  }
}

func TestMCPStreamableInitializeReturnsSessionAndNegotiatedVersion(t *testing.T) {
  env := setupTestServer(t)
  token := getMCPTokenForTest(t, env)

  body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
  req, err := http.NewRequest(http.MethodPost, env.HTTP.URL+"/api/mcp/sse/browser?token="+token, body)
  if err != nil {
    t.Fatalf("NewRequest: %v", err)
  }
  req.Header.Set("Content-Type", "application/json")
  req.Header.Set("Accept", "application/json, text/event-stream")
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("POST initialize: %v", err)
  }
  assertStatus(t, resp, 200)
  if resp.Header.Get("Mcp-Session-Id") == "" {
    t.Fatal("initialize response should include Mcp-Session-Id")
  }
  var result struct {
    Result struct {
      ProtocolVersion string `json:"protocolVersion"`
    } `json:"result"`
  }
  parseJSON(t, resp, &result)
  if result.Result.ProtocolVersion != "2025-11-25" {
    t.Fatalf("protocolVersion = %q, want 2025-11-25", result.Result.ProtocolVersion)
  }
}

func TestMCPStreamableClientTransportConnectsAndListsTools(t *testing.T) {
  env := setupTestServer(t)
  token := getMCPTokenForTest(t, env)

  ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
  defer cancel()

  transport := &mcp.StreamableClientTransport{
    Endpoint:   env.HTTP.URL + "/api/mcp/sse/browser?token=" + token,
    MaxRetries: -1,
  }
  client := mcp.NewClient(&mcp.Implementation{
    Name:    "picoclaw",
    Version: "0.2.8-test",
  }, nil)

  session, err := client.Connect(ctx, transport, nil)
  if err != nil {
    t.Fatalf("Connect: %v", err)
  }
  defer session.Close()

  tools, err := session.ListTools(ctx, nil)
  if err != nil {
    t.Fatalf("ListTools: %v", err)
  }
  if len(tools.Tools) == 0 {
    t.Fatal("ListTools returned no tools")
  }
}

func getMCPTokenForTest(t *testing.T, env *testEnv) string {
  t.Helper()
  resp := env.get(t, "/api/mcp/token", "testuser")
  assertStatus(t, resp, 200)
  var result struct {
    Token string `json:"token"`
  }
  parseJSON(t, resp, &result)
  if result.Token == "" {
    t.Fatal("empty MCP token")
  }
  return result.Token
}

func TestFiles_ListRoot(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/files", "testuser")
  assertStatus(t, resp, 200)
}

func TestFiles_Mkdir(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "path": {""},
    "name": {"testdir"},
  }
  resp := env.postForm(t, "/api/files/mkdir", "testuser", form)
  assertStatus(t, resp, 200)
}

func TestFiles_Mkdir_InvalidName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "path": {""},
    "name": {".."},
  }
  resp := env.postForm(t, "/api/files/mkdir", "testuser", form)
  assertStatus(t, resp, 400)
}

func TestFiles_EditReadAndSave(t *testing.T) {
  env := setupTestServer(t)
  // 创建文本文件
  form := url.Values{
    "path":    {"hello.txt"},
    "content": {"Hello World"},
  }
  resp := env.postForm(t, "/api/files/edit", "testuser", form)
  assertStatus(t, resp, 200)

  // 读取回来
  resp = env.get(t, "/api/files/edit?path=hello.txt", "testuser")
  assertStatus(t, resp, 200)
  var result struct {
    Success bool   `json:"success"`
    Content string `json:"content"`
  }
  parseJSON(t, resp, &result)
  if result.Content != "Hello World" {
    t.Errorf("content=%q, want %q", result.Content, "Hello World")
  }
}

func TestFiles_Download(t *testing.T) {
  env := setupTestServer(t)
  // 先创建文件
  form := url.Values{
    "path":    {"download.txt"},
    "content": {"download me"},
  }
  env.postForm(t, "/api/files/edit", "testuser", form)

  resp := env.get(t, "/api/files/download?path=download.txt", "testuser")
  assertStatus(t, resp, 200)
  ct := resp.Header.Get("Content-Disposition")
  if !strings.Contains(ct, "download.txt") {
    t.Errorf("Content-Disposition=%q", ct)
  }
}

func TestFiles_Delete(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "path":    {"to-delete.txt"},
    "content": {"temp"},
  }
  env.postForm(t, "/api/files/edit", "testuser", form)

  delForm := url.Values{"path": {"to-delete.txt"}}
  resp := env.postForm(t, "/api/files/delete", "testuser", delForm)
  assertStatus(t, resp, 200)
}

func TestFiles_DeleteRoot_Prevented(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"path": {""}}
  resp := env.postForm(t, "/api/files/delete", "testuser", form)
  assertStatus(t, resp, 400)
}

func TestDingTalk_GetEmpty(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/dingtalk", "testuser")
  assertStatus(t, resp, 200)
}

func TestDingTalk_Save(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "client_id":     {"test-id"},
    "client_secret": {"test-secret"},
  }
  resp := env.postForm(t, "/api/dingtalk", "testuser", form)
  assertStatus(t, resp, 200)
}

func TestCookies_Success(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "domain":  {"example.com"},
    "cookies": {"session=abc123"},
  }
  resp := env.postForm(t, "/api/cookies", "testuser", form)
  assertStatus(t, resp, 200)
}

func TestCookies_MissingFields(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "domain":  {""},
    "cookies": {""},
  }
  resp := env.postForm(t, "/api/cookies", "testuser", form)
  assertStatus(t, resp, 400)
}
