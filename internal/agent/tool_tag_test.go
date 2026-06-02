package agent

import (
  "context"
  "encoding/json"
  "testing"
)

type mockServerTool struct {
  name       string
  serverName string
}

func (m *mockServerTool) Name() string        { return m.name }
func (m *mockServerTool) Description() string  { return "desc " + m.name }
func (m *mockServerTool) Schema() map[string]interface{} {
  return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}
func (m *mockServerTool) Execute(_ context.Context, _ json.RawMessage) (*ToolResult, error) {
  return &ToolResult{Success: true, Data: m.name}, nil
}

// ============================================================
// ListBasic — 无 server 的基础工具
// ============================================================

func TestListBasic_ReturnsOnlyBasicTools(t *testing.T) {
  r := NewToolRegistry()
  r.Register(&mockServerTool{name: "cmd"})                 // basic
  r.Register(&mockServerTool{name: "read"})                // basic

  r.Register(&mockServerTool{name: "mcp_tyc_get_info"})    // will be set as server tool
  r.SetServer("mcp_tyc_get_info", "tyc-mcp")

  basic := r.ListBasic()
  if len(basic) != 2 {
    t.Fatalf("expected 2 basic tools, got %d", len(basic))
  }
}

func TestListBasic_ToolCount(t *testing.T) {
  r := NewToolRegistry()
  r.Register(&mockServerTool{name: "a"})
  r.Register(&mockServerTool{name: "b"})
  r.Register(&mockServerTool{name: "c"})

  r.SetServer("c", "data-srv")

  basic := r.ListBasic()
  if len(basic) != 2 {
    t.Fatalf("expected 2 basic, got %d (c should be excluded)", len(basic))
  }
}

// ============================================================
// ListByServer — 按服务器查询工具
// ============================================================

func TestListByServer_ReturnsServerTools(t *testing.T) {
  r := NewToolRegistry()
  r.Register(&mockServerTool{name: "mcp_tyc_get_info"})
  r.Register(&mockServerTool{name: "mcp_tyc_search_news"})
  r.SetServer("mcp_tyc_get_info", "tyc-mcp")
  r.SetServer("mcp_tyc_search_news", "tyc-mcp")

  tools := r.ListByServer("tyc-mcp")
  if len(tools) != 2 {
    t.Fatalf("expected 2 tools for tyc-mcp, got %d", len(tools))
  }
}

func TestListByServer_MultipleServers(t *testing.T) {
  r := NewToolRegistry()
  r.Register(&mockServerTool{name: "t1"})
  r.Register(&mockServerTool{name: "t2"})
  r.Register(&mockServerTool{name: "t3"})
  r.SetServer("t1", "srv-a")
  r.SetServer("t2", "srv-b")
  r.SetServer("t3", "srv-a")

  a := r.ListByServer("srv-a")
  if len(a) != 2 {
    t.Fatalf("expected 2 for srv-a, got %d", len(a))
  }
  b := r.ListByServer("srv-b")
  if len(b) != 1 {
    t.Fatalf("expected 1 for srv-b, got %d", len(b))
  }
}

func TestListByServer_UnknownServer(t *testing.T) {
  r := NewToolRegistry()
  tools := r.ListByServer("nonexistent")
  if len(tools) != 0 {
    t.Errorf("expected empty list for unknown server, got %v", tools)
  }
}

// ============================================================
// ListServers — 获取所有 MCP 服务器名
// ============================================================

func TestListServers_ReturnsUniqueServers(t *testing.T) {
  r := NewToolRegistry()
  r.Register(&mockServerTool{name: "a"})
  r.Register(&mockServerTool{name: "b"})
  r.SetServer("a", "srv1")
  r.SetServer("b", "srv2")

  servers := r.ListServers()
  if len(servers) != 2 {
    t.Fatalf("expected 2 servers, got %d: %v", len(servers), servers)
  }
}

func TestListServers_NoDuplicates(t *testing.T) {
  r := NewToolRegistry()
  r.Register(&mockServerTool{name: "a"})
  r.Register(&mockServerTool{name: "b"})
  r.Register(&mockServerTool{name: "c"})
  r.SetServer("a", "srv1")
  r.SetServer("b", "srv1")
  r.SetServer("c", "srv2")

  servers := r.ListServers()
  if len(servers) != 2 {
    t.Fatalf("expected 2 unique servers, got %d: %v", len(servers), servers)
  }
}

func TestListServers_Empty(t *testing.T) {
  r := NewToolRegistry()
  servers := r.ListServers()
  if len(servers) != 0 {
    t.Errorf("expected 0 servers, got %d", len(servers))
  }
}

// ============================================================
// SetServer — 设置工具的 MCP 服务器归属
// ============================================================

func TestSetServer_ToolNotFound(t *testing.T) {
  r := NewToolRegistry()
  // should not panic
  r.SetServer("nonexistent", "srv")
}

func TestSetServer_Overwrite(t *testing.T) {
  r := NewToolRegistry()
  r.Register(&mockServerTool{name: "t"})
  r.SetServer("t", "srv1")
  r.SetServer("t", "srv2")

  tools := r.ListByServer("srv1")
  if len(tools) != 0 {
    t.Error("tool should no longer be under srv1 after overwrite")
  }
  tools = r.ListByServer("srv2")
  if len(tools) != 1 {
    t.Errorf("expected 1 tool under srv2, got %d", len(tools))
  }
}

// ============================================================
// Resolve 向后兼容
// ============================================================

func TestResolve_ReturnsAllTools(t *testing.T) {
  r := NewToolRegistry()
  r.Register(&mockServerTool{name: "basic"})
  r.Register(&mockServerTool{name: "mcp_t"})
  r.SetServer("mcp_t", "srv")

  all := r.Resolve(context.Background())
  if len(all) != 2 {
    t.Fatalf("Resolve should return all tools, got %d", len(all))
  }
}

// ============================================================
// 并发安全
// ============================================================

func TestToolRegistry_ConcurrentSafe(t *testing.T) {
  r := NewToolRegistry()

  done := make(chan struct{}, 2)
  go func() {
    for i := 0; i < 100; i++ {
      r.Register(&mockServerTool{name: "t"})
      r.SetServer("t", "srv")
      r.ListBasic()
    }
    done <- struct{}{}
  }()

  go func() {
    for i := 0; i < 100; i++ {
      r.Register(&mockServerTool{name: "t2"})
      r.ListByServer("srv")
      r.ListServers()
    }
    done <- struct{}{}
  }()

  <-done
  <-done
}
