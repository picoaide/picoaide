package agent

import (
  "context"
  "encoding/json"
  "fmt"

  "google.golang.org/adk/v2/agent"
  "google.golang.org/adk/v2/tool"
  "google.golang.org/adk/v2/tool/functiontool"
)

// ============================================================
// TypedTool — 泛型工具包装器
// ============================================================

type ToolFunc[T any] func(context.Context, T) *ToolResult

type typedTool[T any] struct {
  name, desc string
  fn         ToolFunc[T]
}

func newTypedTool[T any](name, description string, fn ToolFunc[T]) *typedTool[T] {
  return &typedTool[T]{
    name: name,
    desc: description,
    fn:   fn,
  }
}

func NewTypedTool[T any](name, description string, fn ToolFunc[T]) ToolExecutor {
  return newTypedTool[T](name, description, fn)
}

func (t *typedTool[T]) Name() string                       { return t.name }
func (t *typedTool[T]) Description() string                 { return t.desc }

func (t *typedTool[T]) Schema() map[string]interface{} {
  return map[string]interface{}{"type": "object"}
}

func (t *typedTool[T]) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params T
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("参数解析失败: %v", err)}, nil
  }
  return t.fn(ctx, params), nil
}

// AsADKTool 创建 ADK v2 兼容的工具
func (t *typedTool[T]) AsADKTool() (tool.Tool, error) {
  cfg := functiontool.Config{
    Name:        t.name,
    Description: t.desc,
  }
  return functiontool.New[T, *ToolResult](cfg, func(ctx agent.Context, args T) (*ToolResult, error) {
    return t.fn(ctx, args), nil
  })
}

// ADKToolProvider 由可生成 ADK 工具的执行器实现
type ADKToolProvider interface {
  AsADKTool() (tool.Tool, error)
}
