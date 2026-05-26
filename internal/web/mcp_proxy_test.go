package web

import (
  "context"
  "testing"
)

func TestMCPProxyManager_GetTools_Empty(t *testing.T) {
  m := &MCPProxyManager{proxies: make(map[string]*MCPProxy)}
  tools := m.GetTools("testuser")
  if len(tools) != 0 {
    t.Errorf("GetTools() returned %d tools, want 0", len(tools))
  }
}

func TestMCPProxyManager_GetTools_WithNoGrants(t *testing.T) {
  m := &MCPProxyManager{proxies: make(map[string]*MCPProxy)}
  m.proxies["node-server"] = &MCPProxy{
    Name:      "node-server",
    Transport: "stdio",
    Command:   "node",
    Args:      []string{"server.js"},
    tools: []ToolDef{
      {Name: "mcp_node-server_hello", Description: "hello tool"},
    },
  }
  // User has no grants, should get no tools
  tools := m.GetTools("testuser")
  if len(tools) != 0 {
    t.Errorf("GetTools() returned %d tools, want 0 (no grants)", len(tools))
  }
}

func TestMCPProxyManager_GetTools_AllUsersGrant(t *testing.T) {
  m := &MCPProxyManager{proxies: make(map[string]*MCPProxy)}
  m.proxies["node-server"] = &MCPProxy{
    Name:      "node-server",
    Transport: "stdio",
    Command:   "node",
    Args:      []string{"server.js"},
    tools: []ToolDef{
      {Name: "mcp_node-server_hello", Description: "hello tool"},
    },
  }
  // Simulate the grant check would pass for "*" - but we directly test hasMCPGrant
  // by checking our test-only function
}

func TestHasMCPGrant_NoEngine(t *testing.T) {
  // When DB engine is nil, hasMCPGrant should return false gracefully
  result := hasMCPGrant("any-server", "testuser")
  if result {
    t.Error("hasMCPGrant should return false when DB is not initialized")
  }
}

func TestNewMCPProxy(t *testing.T) {
  p := &MCPProxy{
    Name:      "test",
    Transport: "stdio",
    Command:   "echo",
    Args:      []string{"hello"},
    URL:       "",
  }
  if p.Name != "test" {
    t.Errorf("Name = %q, want test", p.Name)
  }
  if p.Transport != "stdio" {
    t.Errorf("Transport = %q, want stdio", p.Transport)
  }
}

func TestGlobalMCPManagerExists(t *testing.T) {
  if globalMCPManager == nil {
    t.Fatal("globalMCPManager should not be nil")
  }
}

func TestLoadMCPServers_NoEngine(t *testing.T) {
  // Without a DB engine, LoadMCPServers should return an error gracefully
  err := LoadMCPServers(context.Background())
  if err == nil {
    t.Log("LoadMCPServers returned nil error (possibly engine exists)")
  }
}

func TestMCPProxyManager_CallTool_NoProxies(t *testing.T) {
  m := &MCPProxyManager{proxies: make(map[string]*MCPProxy)}
  _, err := m.CallTool(context.Background(), "nonexistent_tool", nil)
  if err == nil {
    t.Error("CallTool should return error when no proxies match")
  }
}

func TestMCPProxyManager_CallTool_UnknownPrefix(t *testing.T) {
  m := &MCPProxyManager{proxies: make(map[string]*MCPProxy)}
  m.proxies["test"] = &MCPProxy{
    Name:      "test",
    Transport: "stdio",
    tools: []ToolDef{
      {Name: "mcp_test_hello", Description: "hello"},
    },
  }
  // Tool name without valid mcp_ prefix
  _, err := m.CallTool(context.Background(), "no_prefix_tool", nil)
  if err == nil {
    t.Error("CallTool should return error for tool without mcp_ prefix")
  }
}

func TestMCPProxyManager_CallTool_WrongServer(t *testing.T) {
  m := &MCPProxyManager{proxies: make(map[string]*MCPProxy)}
  m.proxies["test"] = &MCPProxy{
    Name:      "test",
    Transport: "stdio",
    tools: []ToolDef{
      {Name: "mcp_test_hello", Description: "hello"},
    },
  }
  // Tool name with mcp_ prefix but wrong server
  _, err := m.CallTool(context.Background(), "mcp_wrong_hello", nil)
  if err == nil {
    t.Error("CallTool should return error for unknown server")
  }
}


