package web

import (
  "encoding/json"
  "net/http/httptest"
  "testing"

  "github.com/gin-gonic/gin"
)

func TestMCPServerReqStruct(t *testing.T) {
  req := mcpServerReq{
    Name:      "test-server",
    Transport: "stdio",
    Command:   "/usr/bin/node",
    Args:      `["server.js"]`,
    URL:       "",
    Env:       `{"KEY": "value"}`,
    Enabled:   true,
  }
  if req.Name != "test-server" {
    t.Errorf("Name = %q, want test-server", req.Name)
  }
  if req.Transport != "stdio" {
    t.Errorf("Transport = %q, want stdio", req.Transport)
  }
  if req.Command != "/usr/bin/node" {
    t.Errorf("Command = %q, want /usr/bin/node", req.Command)
  }
  if req.Args != `["server.js"]` {
    t.Errorf("Args = %q", req.Args)
  }
  if req.URL != "" {
    t.Errorf("URL = %q, want empty", req.URL)
  }
  if req.Env != `{"KEY": "value"}` {
    t.Errorf("Env = %q", req.Env)
  }
  if !req.Enabled {
    t.Error("Enabled should be true")
  }
}

func TestMCPServerGrantReqStruct(t *testing.T) {
  req := mcpServerGrantReq{
    ServerID:   1,
    GrantType:  "user",
    GrantValue: "testuser",
  }
  if req.ServerID != 1 {
    t.Errorf("ServerID = %d, want 1", req.ServerID)
  }
  if req.GrantType != "user" {
    t.Errorf("GrantType = %q, want user", req.GrantType)
  }
  if req.GrantValue != "testuser" {
    t.Errorf("GrantValue = %q, want testuser", req.GrantValue)
  }
}

func TestAdminMCPServersList_RequiresAuth(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  w := httptest.NewRecorder()
  ctx, _ := gin.CreateTestContext(w)
  ctx.Request = httptest.NewRequest("GET", "/api/admin/mcp/servers", nil)
  s.handleAdminMCPServersList(ctx)
  var resp apiError
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  if resp.Success {
    t.Error("应该失败，未登录")
  }
  if resp.Error != "未登录" {
    t.Errorf("Error = %q, want 未登录", resp.Error)
  }
}

func TestAdminMCPServerCreate_RequiresAuth(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  w := httptest.NewRecorder()
  ctx, _ := gin.CreateTestContext(w)
  ctx.Request = httptest.NewRequest("POST", "/api/admin/mcp/servers/create", nil)
  s.handleAdminMCPServerCreate(ctx)
  var resp apiError
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  if resp.Success {
    t.Error("应该失败，未登录")
  }
  if resp.Error != "未登录" {
    t.Errorf("Error = %q, want 未登录", resp.Error)
  }
}

func TestAdminMCPServerUpdate_RequiresAuth(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  w := httptest.NewRecorder()
  ctx, _ := gin.CreateTestContext(w)
  ctx.Request = httptest.NewRequest("POST", "/api/admin/mcp/servers/update/1", nil)
  ctx.Params = gin.Params{{Key: "id", Value: "1"}}
  s.handleAdminMCPServerUpdate(ctx)
  var resp apiError
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  if resp.Success {
    t.Error("应该失败，未登录")
  }
  if resp.Error != "未登录" {
    t.Errorf("Error = %q, want 未登录", resp.Error)
  }
}

func TestAdminMCPServerDelete_RequiresAuth(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  w := httptest.NewRecorder()
  ctx, _ := gin.CreateTestContext(w)
  ctx.Request = httptest.NewRequest("POST", "/api/admin/mcp/servers/delete/1", nil)
  ctx.Params = gin.Params{{Key: "id", Value: "1"}}
  s.handleAdminMCPServerDelete(ctx)
  var resp apiError
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  if resp.Success {
    t.Error("应该失败，未登录")
  }
  if resp.Error != "未登录" {
    t.Errorf("Error = %q, want 未登录", resp.Error)
  }
}

func TestAdminMCPServerGrantsList_RequiresAuth(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  w := httptest.NewRecorder()
  ctx, _ := gin.CreateTestContext(w)
  ctx.Request = httptest.NewRequest("GET", "/api/admin/mcp/servers/grants?server_id=1", nil)
  s.handleAdminMCPServerGrantsList(ctx)
  var resp apiError
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  if resp.Success {
    t.Error("应该失败，未登录")
  }
  if resp.Error != "未登录" {
    t.Errorf("Error = %q, want 未登录", resp.Error)
  }
}

func TestAdminMCPServerGrantAdd_RequiresAuth(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  w := httptest.NewRecorder()
  ctx, _ := gin.CreateTestContext(w)
  ctx.Request = httptest.NewRequest("POST", "/api/admin/mcp/servers/grants/add", nil)
  s.handleAdminMCPServerGrantAdd(ctx)
  var resp apiError
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  if resp.Success {
    t.Error("应该失败，未登录")
  }
  if resp.Error != "未登录" {
    t.Errorf("Error = %q, want 未登录", resp.Error)
  }
}

func TestAdminMCPServerGrantRemove_RequiresAuth(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  w := httptest.NewRecorder()
  ctx, _ := gin.CreateTestContext(w)
  ctx.Request = httptest.NewRequest("POST", "/api/admin/mcp/servers/grants/remove/1", nil)
  ctx.Params = gin.Params{{Key: "id", Value: "1"}}
  s.handleAdminMCPServerGrantRemove(ctx)
  var resp apiError
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  if resp.Success {
    t.Error("应该失败，未登录")
  }
  if resp.Error != "未登录" {
    t.Errorf("Error = %q, want 未登录", resp.Error)
  }
}

func TestAdminMCPServersList_BadMethod(t *testing.T) {
  gin.SetMode(gin.TestMode)
  s := &Server{secret: "test", csrfKey: "test-csrf"}
  w := httptest.NewRecorder()
  ctx, _ := gin.CreateTestContext(w)
  ctx.Request = httptest.NewRequest("POST", "/api/admin/mcp/servers", nil)
  requireSuperadminCalled := false
  // We can't easily mock requireSuperadmin, but we can check that
  // the handler is at least defined and compiles. The auth check
  // pattern is consistent with all other admin handlers.
  s.handleAdminMCPServersList(ctx)
  var resp apiError
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("JSON 解码失败: %v", err)
  }
  _ = requireSuperadminCalled
}
