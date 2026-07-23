package agent

import (
  "context"
  "encoding/json"
  "testing"
)

type testParams struct {
  Path    string `json:"path"`
  Content string `json:"content,omitempty"`
  Count   int    `json:"count"`
}

func TestNewTypedTool_Schema(t *testing.T) {
  tool := NewTypedTool[testParams]("test_tool", "a test tool",
    func(ctx context.Context, p testParams) *ToolResult {
      return &ToolResult{Success: true, Data: p.Path + ":" + p.Content}
    })

  if tool.Name() != "test_tool" {
    t.Errorf("name = %q, want test_tool", tool.Name())
  }
  if tool.Description() != "a test tool" {
    t.Errorf("desc = %q, want a test tool", tool.Description())
  }
}

func TestNewTypedTool_Execute(t *testing.T) {
  tool := NewTypedTool[testParams]("test_tool", "a test tool",
    func(ctx context.Context, p testParams) *ToolResult {
      return &ToolResult{
        Success: true,
        Data:    p.Path + ":" + p.Content,
      }
    })

  args, _ := json.Marshal(map[string]interface{}{
    "path":    "/tmp/file",
    "content": "hello",
    "count":   42,
  })
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }
  if result.Data != "/tmp/file:hello" {
    t.Errorf("result = %q, want /tmp/file:hello", result.Data)
  }
}

func TestNewTypedTool_InvalidArgs(t *testing.T) {
  tool := NewTypedTool[testParams]("test_tool", "desc",
    func(ctx context.Context, p testParams) *ToolResult {
      return &ToolResult{Success: true}
    })

  result, err := tool.Execute(context.Background(), json.RawMessage(`invalid json`))
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Error("expected failure for invalid JSON")
  }
}

func TestNewTypedTool_DefaultSchema(t *testing.T) {
  tool := NewTypedTool[string]("str_tool", "string arg tool",
    func(ctx context.Context, s string) *ToolResult {
      return &ToolResult{Success: true, Data: s}
    })

  schema := tool.Schema()
  if schema["type"] != "object" {
    t.Errorf("schema type = %v, want object", schema["type"])
  }
}
