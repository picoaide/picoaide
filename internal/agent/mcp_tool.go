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
  mu              sync.Mutex
  sessions        map[string]*mcp.ClientSession
  tools           map[string]*mcpToolEntry
  serverConfigs   map[string]MCPServer
  serverSummaries map[string]string // 服务器名 → 一行摘要
  mcpToken        string
  WorkspaceDir    string // 沙箱工作区路径，用于保存文件等
}

type mcpToolEntry struct {
  serverName string
  tool       *mcp.Tool
}

func NewMCPToolManager() *MCPToolManager {
  return &MCPToolManager{
    sessions:        make(map[string]*mcp.ClientSession),
    tools:           make(map[string]*mcpToolEntry),
    serverConfigs:   make(map[string]MCPServer),
    serverSummaries: make(map[string]string),
  }
}

func (m *MCPToolManager) SetToken(token string) {
  m.mu.Lock()
  defer m.mu.Unlock()
  m.mcpToken = token
}

func (m *MCPToolManager) Connect(ctx context.Context, name string, server *MCPServer, token string) error {
  m.mu.Lock()
  m.serverConfigs[name] = *server
  m.mcpToken = token
  m.mu.Unlock()

  var lastErr error
  for attempt := 0; attempt < 2; attempt++ {
    if attempt > 0 {
      slog.Debug("mcp.reconnect", "server", name, "attempt", attempt+1)
      select {
      case <-time.After(time.Second):
      case <-ctx.Done():
        return ctx.Err()
      }
    }
    if err := m.connectOnce(ctx, name, server, token); err != nil {
      lastErr = err
      continue
    }
    return nil
  }
  return fmt.Errorf("MCP %s 连接失败(重试2次): %w", name, lastErr)
}

// connectOnce 单次 MCP 连接尝试（未持锁，内部会获取锁）
func (m *MCPToolManager) connectOnce(ctx context.Context, name string, server *MCPServer, token string) error {
  mcpClient := mcp.NewClient(&mcp.Implementation{
    Name:    "picoagent",
    Version: "2.0.0",
  }, nil)

  endpoint := fmt.Sprintf("http://localhost/api/mcp/sse/%s?token=%s", name, url.QueryEscape(token))

  transport := &mcp.StreamableClientTransport{
    Endpoint:             endpoint,
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

  // 连接成功后自动生成服务器摘要
  serverTools := m.collectServerTools(name)
  m.serverSummaries[name] = generateServerSummary(name, serverTools)

  return nil
}

// collectServerTools 收集指定服务器的工具定义列表
func (m *MCPToolManager) collectServerTools(serverName string) []ToolDef {
  var defs []ToolDef
  for _, entry := range m.tools {
    if entry.serverName == serverName {
      schema, _ := entry.tool.InputSchema.(map[string]interface{})
      defs = append(defs, ToolDef{
        Name:        entry.tool.Name,
        Description: entry.tool.Description,
        InputSchema: schema,
      })
    }
  }
  return defs
}

// tryReconnect 尝试重新连接指定 MCP 服务器
func (m *MCPToolManager) tryReconnect(ctx context.Context, name string) error {
  m.mu.Lock()
  config, ok := m.serverConfigs[name]
  token := m.mcpToken
  m.mu.Unlock()

  if !ok {
    return fmt.Errorf("MCP 服务器 %s 配置不存在", name)
  }

  return m.Connect(ctx, name, &config, token)
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
    registry.SetServer(entry.tool.Name, entry.serverName)
  }
}

func (m *MCPToolManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*mcp.CallToolResult, error) {
  // 持锁获取 session 并保持锁在 CallTool 期间不被释放，
  // 防止并发 connectOnce 关闭正在使用的 session
  m.mu.Lock()
  session := m.sessions[serverName]
  if session == nil {
    m.mu.Unlock()
    if err := m.tryReconnect(ctx, serverName); err != nil {
      return nil, err
    }
    m.mu.Lock()
    session = m.sessions[serverName]
    if session == nil {
      m.mu.Unlock()
      return nil, fmt.Errorf("MCP 服务器 %s 重连后仍然不可用", serverName)
    }
  }
  result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: args})
  m.mu.Unlock()
  if err != nil && isConnError(err) {
    slog.Debug("mcp.calltool_reconnecting", "server", serverName, "tool", toolName)
    // 先解锁再重连：tryReconnect 内部自行管理锁
    if reconnectErr := m.tryReconnect(ctx, serverName); reconnectErr == nil {
      m.mu.Lock()
      if newSession := m.sessions[serverName]; newSession != nil {
        result, err = newSession.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: args})
        m.mu.Unlock()
        return result, err
      }
      m.mu.Unlock()
    }
  }
  return result, nil
}

// getSession 线程安全地获取 session
func (m *MCPToolManager) getSession(name string) *mcp.ClientSession {
  m.mu.Lock()
  defer m.mu.Unlock()
  return m.sessions[name]
}

// isConnError 判断是否为连接类错误（可重连）
func isConnError(err error) bool {
  if err == nil {
    return false
  }
  msg := err.Error()
  return strings.Contains(msg, "unexpected EOF") ||
    strings.Contains(msg, "connection") ||
    strings.Contains(msg, "reset") ||
    strings.Contains(msg, "stream error") ||
    strings.Contains(msg, "transport") ||
    strings.Contains(msg, "broken pipe")
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

// Summary 返回指定 MCP 服务器的一行摘要
func (m *MCPToolManager) Summary(serverName string) string {
  m.mu.Lock()
  defer m.mu.Unlock()
  return m.serverSummaries[serverName]
}

// ============================================================
// 服务器摘要生成
// ============================================================

// extractCapability 从工具描述中提取核心能力短语
func extractCapability(desc string) string {
  if desc == "" {
    return ""
  }
  // 取第一个分句
  if idx := strings.IndexAny(desc, "，。；,"); idx > 0 {
    desc = desc[:idx]
  }
  // 去掉中文动词前缀
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

// describeToolFromName 从工具名生成可读描述（英文 fallback）
func describeToolFromName(name string) string {
  // 取最后一个 _ 后面的部分（动词），去掉 get_/search_/list_ 前缀
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

// generateServerSummary 为 MCP 服务器生成一行摘要，含工具名供 AI 调用 query_server 时使用
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
      // 提取工具名（去掉 mcp_<server>_ 前缀）
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
  Registry *ToolRegistry
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

  toolName := fmt.Sprintf("mcp_%s_%s", params.Server, params.Tool)
  rawArgs, _ := json.Marshal(params.Args)
  return t.Registry.Execute(ctx, toolName, rawArgs)
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
