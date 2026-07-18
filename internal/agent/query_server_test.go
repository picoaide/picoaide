package agent

import (
  "context"
  "encoding/json"
  "testing"
)

func TestQueryServerTool_Name(t *testing.T) {
  tool := &QueryServerTool{Manager: NewMCPManager()}
  if tool.Name() != "query_server" {
    t.Errorf("expected query_server, got %s", tool.Name())
  }
}

func TestQueryServerTool_MissingArgs(t *testing.T) {
  tool := &QueryServerTool{Manager: NewMCPManager()}

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

func TestQueryServerTool_ServerNotConnected(t *testing.T) {
  tool := &QueryServerTool{Manager: NewMCPManager()}
  args, _ := json.Marshal(map[string]interface{}{
    "server": "nonexistent",
    "tool":   "some_tool",
    "args":   map[string]interface{}{},
  })

  _, err := tool.Execute(context.Background(), args)
  if err == nil {
    t.Error("expected error for non-existent server")
  }
}
