package agent

import (
  "context"
  "encoding/json"
  "testing"
)

func TestQueryServerTool_Name(t *testing.T) {
  r := NewToolRegistry()
  r.Register(&mockServerTool{name: "get_info"})
  r.SetServer("get_info", "srv")

  tool := &QueryServerTool{Registry: r}
  if tool.Name() != "query_server" {
    t.Errorf("expected query_server, got %s", tool.Name())
  }
}

func TestQueryServerTool_Execute(t *testing.T) {
  r := NewToolRegistry()
  r.Register(&mockServerTool{name: "get_info"})
  r.SetServer("get_info", "srv")

  tool := &QueryServerTool{Registry: r}
  args, _ := json.Marshal(map[string]interface{}{
    "server": "srv",
    "tool":   "get_info",
    "args":   map[string]interface{}{},
  })

  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }
}

func TestQueryServerTool_ToolNotFound(t *testing.T) {
  r := NewToolRegistry()
  tool := &QueryServerTool{Registry: r}
  args, _ := json.Marshal(map[string]interface{}{
    "server": "srv",
    "tool":   "nonexistent",
  })

  _, err := tool.Execute(context.Background(), args)
  if err == nil {
    t.Error("expected error for non-existent tool")
  }
}

func TestQueryServerTool_MissingArgs(t *testing.T) {
  r := NewToolRegistry()
  tool := &QueryServerTool{Registry: r}

  // empty server
  args, _ := json.Marshal(map[string]interface{}{"server": "", "tool": "x"})
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Error("expected failure for empty server")
  }

  // no args at all
  result, err = tool.Execute(context.Background(), json.RawMessage("{}"))
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Error("expected failure for missing params")
  }
}


