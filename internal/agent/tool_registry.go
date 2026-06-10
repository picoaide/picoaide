package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "log/slog"
  "path/filepath"
  "strings"
  "sync"
  "time"
)

var sandboxWorkspace = "/workspace"

// safePath 验证路径在沙箱工作区内，防止路径遍历攻击
func safePath(path string) string {
  if path == "" {
    return ""
  }
  cleaned := filepath.Clean(path)
  if filepath.IsAbs(cleaned) {
    if !strings.HasPrefix(cleaned, sandboxWorkspace+"/") && cleaned != sandboxWorkspace {
      return ""
    }
    return cleaned
  }
  return filepath.Join(sandboxWorkspace, cleaned)
}

// ============================================================
// 工具注册和执行
// ============================================================

type ToolResult struct {
  Success bool
  Data    string
}

type ToolExecutor interface {
  Name() string
  Description() string
  Schema() map[string]interface{}
  Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error)
}

type ToolEntry struct {
  executor   ToolExecutor
  serverName string
}

type ToolRegistry struct {
  mu      sync.RWMutex
  entries map[string]*ToolEntry
}

func NewToolRegistry() *ToolRegistry {
  return &ToolRegistry{
    entries: make(map[string]*ToolEntry),
  }
}

func (r *ToolRegistry) Register(executor ToolExecutor) {
  r.mu.Lock()
  defer r.mu.Unlock()
  r.entries[executor.Name()] = &ToolEntry{executor: executor}
}

func (r *ToolRegistry) SetServer(toolName, serverName string) {
  r.mu.Lock()
  defer r.mu.Unlock()
  if e, ok := r.entries[toolName]; ok {
    e.serverName = serverName
  }
}

func (r *ToolRegistry) Resolve(ctx context.Context) []ToolDef {
  r.mu.RLock()
  defer r.mu.RUnlock()
  var defs []ToolDef
  for _, entry := range r.entries {
    defs = append(defs, ToolDef{
      Name:        entry.executor.Name(),
      Description: entry.executor.Description(),
      InputSchema: entry.executor.Schema(),
    })
  }
  return defs
}

func (r *ToolRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (result *ToolResult, err error) {
  defer func() {
    if r := recover(); r != nil {
      slog.Error("tool.panic_recovered", "tool", name, "panic", r)
      result = &ToolResult{Success: false, Data: fmt.Sprintf("工具执行异常: %v", r)}
      err = nil
    }
  }()

  r.mu.RLock()
  entry, ok := r.entries[name]
  r.mu.RUnlock()

  if !ok {
    slog.Debug("tool.execute_not_found", "tool", name)
    return nil, fmt.Errorf("工具未找到: %s", name)
  }

  start := time.Now()
  result, err = entry.executor.Execute(ctx, args)
  if result == nil && err == nil {
    result = &ToolResult{Success: false, Data: fmt.Sprintf("工具 %s 执行返回空结果（可能发生 panic 已被捕获）", name)}
  }
  duration := time.Since(start)
  slog.Debug("tool.execute_complete",
    "tool", name,
    "success", result != nil && result.Success,
    "duration_ms", duration.Milliseconds(),
  )
  return result, err
}

func (r *ToolRegistry) LookupByServer(serverName, toolName string) ToolExecutor {
  r.mu.RLock()
  defer r.mu.RUnlock()
  for _, entry := range r.entries {
    if entry.serverName == serverName && entry.executor.Name() == toolName {
      return entry.executor
    }
  }
  return nil
}

func (r *ToolRegistry) ListBasic() []ToolDef {
  r.mu.RLock()
  defer r.mu.RUnlock()
  var defs []ToolDef
  for _, entry := range r.entries {
    if entry.serverName == "" {
      defs = append(defs, ToolDef{
        Name:        entry.executor.Name(),
        Description: entry.executor.Description(),
        InputSchema: entry.executor.Schema(),
      })
    }
  }
  return defs
}

func (r *ToolRegistry) ListByServer(serverName string) []ToolDef {
  r.mu.RLock()
  defer r.mu.RUnlock()
  var defs []ToolDef
  for _, entry := range r.entries {
    if entry.serverName == serverName {
      defs = append(defs, ToolDef{
        Name:        entry.executor.Name(),
        Description: entry.executor.Description(),
        InputSchema: entry.executor.Schema(),
      })
    }
  }
  return defs
}

func (r *ToolRegistry) ListServers() []string {
  r.mu.RLock()
  defer r.mu.RUnlock()
  seen := make(map[string]bool)
  var servers []string
  for _, entry := range r.entries {
    if entry.serverName != "" && !seen[entry.serverName] {
      seen[entry.serverName] = true
      servers = append(servers, entry.serverName)
    }
  }
  return servers
}
