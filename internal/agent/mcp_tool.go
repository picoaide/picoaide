package agent

import (
  "context"
  "encoding/base64"
  "encoding/json"
  "fmt"
  "log/slog"
  "net"
  "net/http"
  "net/url"
  "os"
  "path/filepath"
  "strings"
  "sync"
  "time"

  "github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPToolManager struct {
  mu           sync.Mutex
  sessions     map[string]*mcp.ClientSession
  tools        map[string]*mcpToolEntry
  WorkspaceDir string // 沙箱工作区路径，用于保存文件等
}

type mcpToolEntry struct {
  serverName string
  tool       *mcp.Tool
}

func NewMCPToolManager() *MCPToolManager {
  return &MCPToolManager{
    sessions: make(map[string]*mcp.ClientSession),
    tools:    make(map[string]*mcpToolEntry),
  }
}

func (m *MCPToolManager) Connect(ctx context.Context, name string, server *MCPServer, token string) error {
  mcpClient := mcp.NewClient(&mcp.Implementation{
    Name:    "picoagent",
    Version: "2.0.0",
  }, nil)

  endpoint := fmt.Sprintf("http://localhost/api/mcp/sse/%s?token=%s", name, url.QueryEscape(token))

  transport := &mcp.StreamableClientTransport{
    Endpoint:      endpoint,
    DisableStandaloneSSE: true,
  }

  // Unix socket 覆盖 HTTP Transport 的 Dialer
  transport.HTTPClient = &http.Client{
    Timeout: 10 * time.Second,
    Transport: &http.Transport{
      DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
        return net.Dial("unix", server.Socket)
      },
    },
  }

  session, err := mcpClient.Connect(ctx, transport, nil)
  if err != nil {
    return fmt.Errorf("连接 MCP %s 失败: %w", name, err)
  }

  m.mu.Lock()
  defer m.mu.Unlock()

  if old, ok := m.sessions[name]; ok {
    old.Close()
  }
  m.sessions[name] = session

  for tool, err := range session.Tools(ctx, nil) {
    if err != nil {
      continue
    }
    entry := tool
    for existingName, e := range m.tools {
      if e.serverName == name && existingName == entry.Name {
        delete(m.tools, existingName)
      }
    }
    m.tools[entry.Name] = &mcpToolEntry{serverName: name, tool: entry}
  }

  return nil
}

func (m *MCPToolManager) RegisterAll(registry *ToolRegistry) {
  m.mu.Lock()
  defer m.mu.Unlock()
  for _, entry := range m.tools {
    registry.Register(&mcpToolExecutor{
      name:         entry.tool.Name,
      desc:         entry.tool.Description,
      schema:       entry.tool.InputSchema,
      serverName:   entry.serverName,
      manager:      m,
      workspaceDir: m.WorkspaceDir,
    })
  }
}

func (m *MCPToolManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*mcp.CallToolResult, error) {
  m.mu.Lock()
  session, ok := m.sessions[serverName]
  m.mu.Unlock()
  if !ok {
    return nil, fmt.Errorf("MCP 服务器 %s 未连接", serverName)
  }
  return session.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: args})
}

func (m *MCPToolManager) Close() {
  m.mu.Lock()
  defer m.mu.Unlock()
  for name, session := range m.sessions {
    session.Close()
    delete(m.sessions, name)
  }
}

type mcpToolExecutor struct {
  name        string
  desc        string
  schema      interface{}
  serverName  string
  manager     *MCPToolManager
  workspaceDir string
}

func (e *mcpToolExecutor) Name() string                    { return e.name }
func (e *mcpToolExecutor) Description() string              { return e.desc }
func (e *mcpToolExecutor) Schema() map[string]interface{}   { return toMap(e.schema) }

func (e *mcpToolExecutor) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var arguments map[string]interface{}
  if err := json.Unmarshal(args, &arguments); err != nil {
    arguments = map[string]interface{}{}
  }
  result, err := e.manager.CallTool(ctx, e.serverName, e.name, arguments)
  if err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("MCP 工具调用失败: %v", err)}, nil
  }
  var parts []string
  for _, c := range result.Content {
    data, err := json.Marshal(c)
    if err != nil {
      continue
    }
    var m map[string]interface{}
    if json.Unmarshal(data, &m) == nil {
      if contentType, ok := m["type"].(string); ok && contentType == "image" {
        path := saveImage(m, e.workspaceDir)
        if path != "" {
          parts = append(parts, fmt.Sprintf("[图片已保存到 %s]", path))
        } else {
          slog.Debug("mcp.save_image_failed", "workspace", e.workspaceDir, "data_len", len(fmt.Sprint(m["data"])))
          parts = append(parts, "(图片保存失败)")
        }
      } else if text, ok := m["text"]; ok {
        parts = append(parts, fmt.Sprint(text))
      } else {
        slog.Debug("mcp.unknown_content_type", "type", m["type"], "keys", fmt.Sprintf("%v", keysOfMap(m)))
        parts = append(parts, string(data))
      }
    }
  }
  output := strings.Join(parts, "\n")
  if output == "" {
    output = "(工具已执行，无返回内容)"
  }
  return &ToolResult{Success: true, Data: output}, nil
}

// saveImage 将 MCP image content 保存为 PNG 文件，返回保存路径
func saveImage(m map[string]interface{}, workspace string) string {
  dataStr, _ := m["data"].(string)
  if dataStr == "" {
    return ""
  }
  decoded, err := base64.StdEncoding.DecodeString(dataStr)
  if err != nil {
    slog.Debug("mcp.save_image_decode_failed", "error", err.Error())
    return ""
  }

  dir := filepath.Join(workspace, "screenshots")
  os.MkdirAll(dir, 0755)
  now := time.Now()
  name := fmt.Sprintf("screenshot_%s.png", now.Format("20060102_150405"))
  path := filepath.Join(dir, name)
  if err := os.WriteFile(path, decoded, 0644); err != nil {
    slog.Debug("mcp.save_image_write_failed", "error", err.Error(), "path", path)
    return ""
  }
  return path
}

func keysOfMap(m map[string]interface{}) []string {
  keys := make([]string, 0, len(m))
  for k := range m {
    keys = append(keys, k)
  }
  return keys
}

func toMap(v interface{}) map[string]interface{} {
  if m, ok := v.(map[string]interface{}); ok {
    return m
  }
  data, _ := json.Marshal(v)
  var m map[string]interface{}
  json.Unmarshal(data, &m)
  return m
}
