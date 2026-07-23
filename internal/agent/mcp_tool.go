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
  "google.golang.org/adk/v2/tool"
  "google.golang.org/adk/v2/tool/mcptoolset"
)

// ============================================================
// MCPManager — mcptoolset + raw session 包装
// ============================================================

type MCPManager struct {
  mu              sync.Mutex
  serverConfigs   map[string]MCPServer
  sessions        map[string]*mcp.ClientSession // 原始 MCP 会话，供 QueryServerTool 使用
  toolsets        map[string]tool.Toolset       // mcptoolset 工具集，供 ADK 使用
  serverSummaries map[string]string
  mcpToken        string
  WorkspaceDir    string
}

func NewMCPManager() *MCPManager {
  return &MCPManager{
    serverConfigs:   make(map[string]MCPServer),
    sessions:        make(map[string]*mcp.ClientSession),
    toolsets:        make(map[string]tool.Toolset),
    serverSummaries: make(map[string]string),
  }
}

func (m *MCPManager) SetToken(token string) {
  m.mu.Lock()
  defer m.mu.Unlock()
  m.mcpToken = token
}

func (m *MCPManager) GetToken() string {
  m.mu.Lock()
  defer m.mu.Unlock()
  return m.mcpToken
}

func (m *MCPManager) Connect(ctx context.Context, name string, server *MCPServer, token string) error {
  transport := m.createTransport(server.Socket, token, name)

  // 同时连接原始 MCP 会话（供 QueryServerTool 直接调用）
  mcpClient := mcp.NewClient(&mcp.Implementation{Name: "picoagent", Version: "2.0.0"}, nil)
  session, err := mcpClient.Connect(ctx, transport, nil)
  if err != nil {
    return fmt.Errorf("MCP %s 连接失败: %w", name, err)
  }

  ts, err := mcptoolset.New(mcptoolset.Config{Transport: transport})
  if err != nil {
    session.Close()
    return fmt.Errorf("MCP %s 工具集创建失败: %w", name, err)
  }

  // 预拉取工具列表用于生成摘要
  var toolDefs []ToolDef
  for t := range session.Tools(ctx, nil) {
    if t == nil {
      continue
    }
    schema, _ := t.InputSchema.(map[string]interface{})
    toolDefs = append(toolDefs, ToolDef{Name: t.Name, Description: t.Description, InputSchema: schema})
  }

  m.mu.Lock()
  m.serverConfigs[name] = *server
  m.mcpToken = token
  if old, ok := m.sessions[name]; ok {
    old.Close()
  }
  m.sessions[name] = session
  m.toolsets[name] = ts
  m.serverSummaries[name] = generateServerSummary(name, toolDefs)
  m.mu.Unlock()

  return nil
}

func (m *MCPManager) createTransport(socket, token, serverName string) mcp.Transport {
  endpoint := fmt.Sprintf("http://localhost/api/mcp/sse/%s?token=%s", serverName, url.QueryEscape(token))
  return &mcp.StreamableClientTransport{
    Endpoint:             endpoint,
    DisableStandaloneSSE: true,
    HTTPClient: &http.Client{
      Timeout: 10 * time.Second,
      Transport: &http.Transport{
        DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
          return net.Dial("unix", socket)
        },
      },
    },
  }
}

// Toolsets 返回所有 MCP 工具集（供 ADK llmagent 使用）
func (m *MCPManager) Toolsets() []tool.Toolset {
  m.mu.Lock()
  defer m.mu.Unlock()
  var result []tool.Toolset
  for _, ts := range m.toolsets {
    result = append(result, ts)
  }
  return result
}

func (m *MCPManager) ServerNames() []string {
  m.mu.Lock()
  defer m.mu.Unlock()
  var names []string
  for name := range m.toolsets {
    names = append(names, name)
  }
  return names
}

func (m *MCPManager) Summaries() map[string]string {
  m.mu.Lock()
  defer m.mu.Unlock()
  summaries := make(map[string]string)
  for k, v := range m.serverSummaries {
    summaries[k] = v
  }
  return summaries
}

// CallTool 通过原始 MCP 会话调用工具（供 QueryServerTool 使用）
func (m *MCPManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*mcp.CallToolResult, error) {
  m.mu.Lock()
  session := m.sessions[serverName]
  m.mu.Unlock()

  if session == nil {
    return nil, fmt.Errorf("MCP 服务器 %s 未连接", serverName)
  }
  return session.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: args})
}

// ============================================================
// 服务器摘要生成
// ============================================================

func extractCapability(desc string) string {
  if desc == "" {
    return ""
  }
  if idx := strings.IndexAny(desc, "，。；,"); idx > 0 {
    desc = desc[:idx]
  }
  for _, prefix := range []string{"获取", "查询", "搜索", "调用", "读取"} {
    if strings.HasPrefix(desc, prefix) {
      desc = desc[len(prefix):]
      break
    }
  }
  desc = strings.TrimSpace(desc)
  runes := []rune(desc)
  if len(runes) > 20 {
    desc = string(runes[:20])
  }
  return desc
}

func describeToolFromName(name string) string {
  name = strings.TrimPrefix(name, "get_")
  name = strings.TrimPrefix(name, "set_")
  name = strings.TrimPrefix(name, "search_")
  name = strings.TrimPrefix(name, "list_")
  name = strings.TrimPrefix(name, "create_")
  name = strings.TrimPrefix(name, "delete_")
  name = strings.ReplaceAll(name, "_", " ")
  if len(name) > 30 {
    name = name[:30]
  }
  return strings.TrimSpace(name)
}

func generateServerSummary(serverName string, tools []ToolDef) string {
  count := len(tools)
  if count == 0 {
    return fmt.Sprintf("%s（0 个工具）", serverName)
  }

  seen := map[string]bool{}
  var caps []string
  for _, t := range tools {
    c := extractCapability(t.Description)
    if c == "" {
      c = describeToolFromName(t.Name)
    }
    if c != "" && !seen[c] {
      toolName := t.Name
      parts := strings.SplitN(toolName, "_", 3)
      if len(parts) == 3 {
        toolName = parts[2]
      }
      caps = append(caps, fmt.Sprintf("%s(%s)", c, toolName))
      seen[c] = true
    }
  }
  if len(caps) > 5 {
    caps = caps[:5]
  }
  return fmt.Sprintf("%s: %s（%d 个工具）", serverName, strings.Join(caps, "、"), count)
}

// ============================================================
// QueryServerTool — MCP 代理调用工具
// ============================================================

type QueryServerTool struct {
  Manager *MCPManager
}

func (t *QueryServerTool) Name() string { return "query_server" }

func (t *QueryServerTool) Description() string {
  return "快速调用 MCP 服务器的工具。适用于单次查询。批量任务请使用 subagent_task。参数 server 从「可用 MCP 服务器」列表中选取。"
}

func (t *QueryServerTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "server": map[string]interface{}{
        "type":        "string",
        "description": "MCP 服务器名，如 tyc-mcp、browser",
      },
      "tool": map[string]interface{}{
        "type":        "string",
        "description": "工具名（不含 mcp_servers_ 前缀），如 get_company_info",
      },
      "args": map[string]interface{}{
        "type":                 "object",
        "description":          "工具参数",
        "additionalProperties": true,
      },
    },
    "required": []string{"server", "tool", "args"},
  }
}

func (t *QueryServerTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Server string                 `json:"server"`
    Tool   string                 `json:"tool"`
    Args   map[string]interface{} `json:"args"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }
  if params.Server == "" || params.Tool == "" {
    return &ToolResult{Success: false, Data: "server 和 tool 不能为空"}, nil
  }

  result, err := t.Manager.CallTool(ctx, params.Server, params.Tool, params.Args)
  if err != nil {
    return nil, fmt.Errorf("调用 %s/%s 失败: %w", params.Server, params.Tool, err)
  }

  return mcpResultToToolResult(result, t.Manager.WorkspaceDir), nil
}

// mcpResultToToolResult converts MCP CallToolResult to ToolResult.
func mcpResultToToolResult(result *mcp.CallToolResult, workspace string) *ToolResult {
  var parts []string
  for _, c := range result.Content {
    data, _ := json.Marshal(c)
    var m map[string]interface{}
    if json.Unmarshal(data, &m) != nil {
      parts = append(parts, string(data))
      continue
    }
    if contentType, ok := m["type"].(string); ok && contentType == "image" {
      path := saveImage(m, workspace)
      if path != "" {
        parts = append(parts, fmt.Sprintf("[图片已保存到 %s]", path))
      } else {
        parts = append(parts, "(图片保存失败)")
      }
    } else if text, ok := m["text"]; ok {
      parts = append(parts, fmt.Sprint(text))
    } else {
      parts = append(parts, string(data))
    }
  }
  output := strings.Join(parts, "\n")
  if output == "" {
    output = "(工具已执行，无返回内容)"
  }
  return &ToolResult{Success: true, Data: output}
}

// ============================================================
// 图片保存
// ============================================================

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
