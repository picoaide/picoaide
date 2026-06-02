package web

import (
  "bufio"
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "log/slog"
  "net/http"
  "os/exec"
  "strings"
  "sync"

  "github.com/picoaide/picoaide/internal/auth"
)

// ============================================================
// MCP 子进程代理管理器
// ============================================================

// MCPProxy 管理一个第三方 MCP 服务的连接
type MCPProxy struct {
  Name      string
  Transport string
  Command   string
  Args      []string
  URL       string
  Env       map[string]string
  Headers   map[string]string
  mu        sync.Mutex
  tools     []ToolDef
  running   bool
  cmd       *exec.Cmd
  stdin     io.WriteCloser
  stdout    *bufio.Reader
  cancel    context.CancelFunc
  sessionID string // Streamable HTTP 会话 ID
}

// MCPProxyManager 管理所有第三方 MCP 代理
type MCPProxyManager struct {
  mu      sync.RWMutex
  proxies map[string]*MCPProxy
}

// globalMCPManager 全局 MCP 代理管理器
var globalMCPManager = &MCPProxyManager{proxies: make(map[string]*MCPProxy)}

// LoadMCPServers 从数据库加载所有启用的 MCP 服务器并启动代理
func LoadMCPServers(ctx context.Context) error {
  engine, err := auth.GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库连接失败: %w", err)
  }

  rows, err := engine.Query("SELECT id, name, transport, command, args, url, env, headers FROM mcp_servers WHERE enabled=1")
  if err != nil {
    return fmt.Errorf("查询 MCP 服务器失败: %w", err)
  }

  m := &MCPProxyManager{proxies: make(map[string]*MCPProxy)}

  for _, row := range rows {
    name := string(row["name"])
    transport := string(row["transport"])
    command := string(row["command"])
    argsStr := string(row["args"])
    url := string(row["url"])
    envStr := string(row["env"])
    headersStr := string(row["headers"])

    proxy := &MCPProxy{
      Name:      name,
      Transport: transport,
      Command:   command,
      URL:       url,
    }

    // 解析 args JSON 数组
    if argsStr != "" && argsStr != "[]" {
      var args []string
      if err := json.Unmarshal([]byte(argsStr), &args); err == nil {
        proxy.Args = args
      }
    }

    // 解析 env JSON 对象
    if envStr != "" && envStr != "{}" {
      var env map[string]string
      if err := json.Unmarshal([]byte(envStr), &env); err == nil {
        proxy.Env = env
      }
    }

    // 解析 headers JSON 对象
    if headersStr != "" && headersStr != "{}" {
      var headers map[string]string
      if err := json.Unmarshal([]byte(headersStr), &headers); err == nil {
        proxy.Headers = headers
      }
    }

    // 获取工具列表（通过 stdio 或 HTTP 连接）
    tools, err := fetchProxyTools(ctx, proxy)
    if err != nil {
      slog.Warn("MCP 服务器连接失败", "name", name, "transport", transport, "error", err)
      proxy.tools = []ToolDef{}
    } else {
      proxy.tools = tools
    }
    // HTTP/SSE 代理无状态，标记为可用；stdio 代理由 mcpStdioHandshake 在成功时设置 running
    if proxy.Transport == "http" || proxy.Transport == "sse" {
      proxy.running = true
    }

    m.proxies[name] = proxy
  }

  // 原子替换全局管理器并清理旧代理
  globalMCPManager.mu.Lock()
  oldProxies := globalMCPManager.proxies
  globalMCPManager.proxies = m.proxies
  globalMCPManager.mu.Unlock()

  // 停止不再使用（从新配置中移除）的旧代理子进程
  for name, old := range oldProxies {
    if _, keep := m.proxies[name]; !keep {
      old.stop()
    }
  }

  return nil
}

// fetchProxyTools 通过 initialize 请求获取 MCP 服务的工具列表
func fetchProxyTools(ctx context.Context, proxy *MCPProxy) ([]ToolDef, error) {
  // stdio 传输：启动子进程，发送 initialize 和 tools/list JSON-RPC 消息
  if proxy.Transport == "stdio" && proxy.Command != "" {
    return fetchStdioTools(ctx, proxy)
  }
  // HTTP/SSE 传输
  if proxy.Transport == "http" || proxy.Transport == "sse" {
    return fetchHTTPTools(ctx, proxy)
  }
  return nil, fmt.Errorf("不支持的传输方式: %s", proxy.Transport)
}

// fetchStdioTools 通过子进程 stdio 获取工具列表
func fetchStdioTools(ctx context.Context, proxy *MCPProxy) ([]ToolDef, error) {
  tools, err := mcpStdioHandshake(ctx, proxy)
  if err != nil {
    return nil, fmt.Errorf("stdio 握手失败: %w", err)
  }
  return prefixToolNames(proxy.Name, tools), nil
}

// fetchHTTPTools 通过 HTTP 请求获取工具列表
func fetchHTTPTools(ctx context.Context, proxy *MCPProxy) ([]ToolDef, error) {
  tools, err := mcpHTTPHandshake(ctx, proxy)
  if err != nil {
    return nil, fmt.Errorf("HTTP 握手失败: %w", err)
  }
  return prefixToolNames(proxy.Name, tools), nil
}

// prefixToolNames 为工具名添加 mcp_<server_name>_ 前缀（幂等）
func prefixToolNames(serverName string, tools []ToolDef) []ToolDef {
  prefix := "mcp_" + serverName + "_"
  prefixed := make([]ToolDef, len(tools))
  for i, t := range tools {
    prefixed[i] = t
    if !strings.HasPrefix(t.Name, prefix) {
      prefixed[i].Name = prefix + t.Name
    }
  }
  return prefixed
}

// ============================================================
// 代理查询与调用路由
// ============================================================

// GetTools 返回指定用户可访问的所有第三方 MCP 工具
func (m *MCPProxyManager) GetTools(username string) []ToolDef {
  m.mu.RLock()
  defer m.mu.RUnlock()

  var tools []ToolDef
  for name, proxy := range m.proxies {
    if !hasMCPGrant(name, username) {
      continue
    }
    proxy.mu.Lock()
    tools = append(tools, proxy.tools...)
    proxy.mu.Unlock()
  }
  if tools == nil {
    tools = []ToolDef{}
  }
  return tools
}

// ToolCount 返回指定 MCP 服务器已加载的工具数量
func (m *MCPProxyManager) ToolCount(name string) int {
  m.mu.RLock()
  defer m.mu.RUnlock()
  proxy, ok := m.proxies[name]
  if !ok {
    return -1 // 未加载
  }
  proxy.mu.Lock()
  defer proxy.mu.Unlock()
  return len(proxy.tools)
}

// GetServerTools 返回指定 MCP 服务器的全部工具列表
func (m *MCPProxyManager) GetServerTools(name string) []ToolDef {
  m.mu.RLock()
  defer m.mu.RUnlock()
  proxy, ok := m.proxies[name]
  if !ok {
    return nil
  }
  proxy.mu.Lock()
  defer proxy.mu.Unlock()
  result := make([]ToolDef, len(proxy.tools))
  copy(result, proxy.tools)
  return result
}

// CallTool 路由工具调用到对应的 MCP 代理
func (m *MCPProxyManager) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
  // 解析工具名：mcp_<server_name>_<tool_name>
  if !strings.HasPrefix(toolName, "mcp_") {
    return nil, fmt.Errorf("未知工具: %s", toolName)
  }

  rest := strings.TrimPrefix(toolName, "mcp_")
  idx := strings.Index(rest, "_")
  if idx < 0 {
    return nil, fmt.Errorf("无效的工具名格式: %s", toolName)
  }

  serverName := rest[:idx]
  toolCallName := rest[idx+1:]

  m.mu.RLock()
  proxy, ok := m.proxies[serverName]
  m.mu.RUnlock()

  if !ok {
    return nil, fmt.Errorf("未知的 MCP 服务器: %s", serverName)
  }

  return proxy.call(ctx, toolCallName, args)
}

// ============================================================
// stdio 握手实现
// ============================================================

// mcpStdioHandshake 启动子进程，通过 stdio 完成 MCP 握手并获取工具列表。
// 成功后将子进程引用存入 proxy 供后续 call() 使用。
func mcpStdioHandshake(ctx context.Context, proxy *MCPProxy) ([]ToolDef, error) {
  ctx, cancel := context.WithCancel(ctx)

  cmd := exec.CommandContext(ctx, proxy.Command, proxy.Args...)

  // 设置环境变量
  if len(proxy.Env) > 0 {
    cmd.Env = append(cmd.Environ(), mapToEnvSlice(proxy.Env)...)
  }

  stdin, err := cmd.StdinPipe()
  if err != nil {
    cancel()
    return nil, fmt.Errorf("创建 stdin 管道失败: %w", err)
  }

  stdout, err := cmd.StdoutPipe()
  if err != nil {
    stdin.Close()
    cancel()
    return nil, fmt.Errorf("创建 stdout 管道失败: %w", err)
  }

  if err := cmd.Start(); err != nil {
    stdin.Close()
    cancel()
    return nil, fmt.Errorf("启动子进程失败: %w", err)
  }

  reader := bufio.NewReader(stdout)
  handshakeSuccess := false

  defer func() {
    if !handshakeSuccess {
      stdin.Close()
      if cmd.Process != nil {
        cmd.Process.Kill()
      }
      cancel()
    }
  }()

  // sendRequest 发送 JSON-RPC 请求并读取匹配 ID 的响应
  sendRequest := func(id int, method string, params map[string]interface{}) (map[string]interface{}, error) {
    req := map[string]interface{}{
      "jsonrpc": "2.0",
      "id":      id,
      "method":  method,
    }
    if params != nil {
      req["params"] = params
    }

    reqBytes, err := json.Marshal(req)
    if err != nil {
      return nil, fmt.Errorf("序列化请求失败: %w", err)
    }

    if _, err := fmt.Fprintf(stdin, "%s\n", reqBytes); err != nil {
      return nil, fmt.Errorf("写入 stdin 失败: %w", err)
    }

    for {
      line, err := reader.ReadString('\n')
      if err != nil {
        return nil, fmt.Errorf("读取 stdout 失败: %w", err)
      }
      line = strings.TrimSpace(line)
      if line == "" {
        continue
      }

      var resp struct {
        JSONRPC string          `json:"jsonrpc"`
        ID      json.Number     `json:"id"`
        Result  json.RawMessage `json:"result"`
        Error   *struct {
          Code    int    `json:"code"`
          Message string `json:"message"`
        } `json:"error"`
      }
      if err := json.Unmarshal([]byte(line), &resp); err != nil {
        continue
      }

      if resp.ID.String() != fmt.Sprintf("%d", id) {
        continue
      }

      if resp.Error != nil {
        return nil, fmt.Errorf("MCP 错误 (code=%d): %s", resp.Error.Code, resp.Error.Message)
      }

      var result map[string]interface{}
      if err := json.Unmarshal(resp.Result, &result); err != nil {
        return nil, fmt.Errorf("解析 result 失败: %w", err)
      }
      return result, nil
    }
  }

  // 发送 initialize
  initParams := map[string]interface{}{
    "protocolVersion": "2024-11-05",
    "capabilities":    map[string]interface{}{},
    "clientInfo": map[string]interface{}{
      "name":    "picoaide",
      "version": "1.0",
    },
  }
  if _, err := sendRequest(1, "initialize", initParams); err != nil {
    return nil, fmt.Errorf("initialize 失败: %w", err)
  }

  // 发送 notifications/initialized（部分 MCP 服务器需要）
  notif := map[string]interface{}{
    "jsonrpc": "2.0",
    "method":  "notifications/initialized",
  }
  notifBytes, _ := json.Marshal(notif)
  fmt.Fprintf(stdin, "%s\n", notifBytes)

  // 发送 tools/list
  result, err := sendRequest(2, "tools/list", nil)
  if err != nil {
    return nil, fmt.Errorf("tools/list 失败: %w", err)
  }

  // 解析工具列表
  toolsRaw, ok := result["tools"]
  if !ok {
    return nil, fmt.Errorf("tools/list 响应中缺少 tools 字段")
  }

  toolsJSON, err := json.Marshal(toolsRaw)
  if err != nil {
    return nil, fmt.Errorf("序列化 tools 失败: %w", err)
  }

  var tools []ToolDef
  if err := json.Unmarshal(toolsJSON, &tools); err != nil {
    return nil, fmt.Errorf("解析 ToolDef 失败: %w", err)
  }

  // 成功 — 将子进程引用存入 proxy，取消函数由 proxy.stop() 负责调用
  handshakeSuccess = true
  proxy.mu.Lock()
  proxy.cmd = cmd
  proxy.stdin = stdin
  proxy.stdout = reader
  proxy.cancel = cancel
  proxy.running = true
  proxy.mu.Unlock()

  return tools, nil
}

// ============================================================
// HTTP 握手实现
// ============================================================

// mcpHTTPHandshake 通过 HTTP POST 完成 MCP 握手并获取工具列表
// 支持 Streamable HTTP 协议（维持 Mcp-Session-Id）
func mcpHTTPHandshake(ctx context.Context, proxy *MCPProxy) ([]ToolDef, error) {
  client := &http.Client{}

  sendRequest := func(id int, method string, params map[string]interface{}, sessionID string) (map[string]interface{}, string, error) {
    req := map[string]interface{}{
      "jsonrpc": "2.0",
      "id":      id,
      "method":  method,
    }
    if params != nil {
      req["params"] = params
    }

    reqBytes, err := json.Marshal(req)
    if err != nil {
      return nil, "", fmt.Errorf("序列化请求失败: %w", err)
    }

    httpReq, err := http.NewRequestWithContext(ctx, "POST", proxy.URL, bytes.NewReader(reqBytes))
    if err != nil {
      return nil, "", fmt.Errorf("创建 HTTP 请求失败: %w", err)
    }
  httpReq.Header.Set("Content-Type", "application/json")
  httpReq.Header.Set("Mcp-Protocol-Version", "2024-11-05")
  for k, v := range proxy.Headers {
    httpReq.Header.Set(k, v)
  }
  if sessionID != "" {
    httpReq.Header.Set("Mcp-Session-Id", sessionID)
  }

  httpResp, err := client.Do(httpReq)
  if err != nil {
    return nil, "", fmt.Errorf("HTTP 请求失败: %w", err)
  }
  defer httpResp.Body.Close()

  // 检查响应中的 Session ID
  newSessionID := httpResp.Header.Get("Mcp-Session-Id")
  if newSessionID != "" {
    sessionID = newSessionID
  }

  // 读取响应体
  body, err := io.ReadAll(httpResp.Body)
  if err != nil {
    return nil, "", fmt.Errorf("读取 HTTP 响应失败: %w", err)
  }

  // 尝试解析为 JSON-RPC（普通 HTTP 或 SSE data 行内嵌 JSON）
  // 对于 SSE 响应，body 是 "data: {...}\n\ndata: {...}" 格式
  parseBody := body
  if httpResp.Header.Get("Content-Type") == "text/event-stream" || bytes.HasPrefix(bytes.TrimSpace(body), []byte("data: ")) {
    parseBody = extractSSEJSON(body)
  }

  var resp struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.Number     `json:"id"`
    Result  json.RawMessage `json:"result"`
    Error   *struct {
      Code    int    `json:"code"`
      Message string `json:"message"`
    } `json:"error"`
  }
  if err := json.Unmarshal(parseBody, &resp); err != nil {
    return nil, "", fmt.Errorf("解析 HTTP 响应 JSON 失败: %w (body: %s)", err, string(parseBody))
  }

  if resp.Error != nil {
    return nil, "", fmt.Errorf("MCP 错误 (code=%d): %s", resp.Error.Code, resp.Error.Message)
  }

  var result map[string]interface{}
  if err := json.Unmarshal(resp.Result, &result); err != nil {
    return nil, "", fmt.Errorf("解析 result 失败: %w", err)
  }
  return result, sessionID, nil
  }

  // 发送 initialize
  initParams := map[string]interface{}{
    "protocolVersion": "2024-11-05",
    "capabilities":    map[string]interface{}{},
    "clientInfo": map[string]interface{}{
      "name":    "picoaide",
      "version": "1.0",
    },
  }
  sid := ""
  if _, newSid, err := sendRequest(1, "initialize", initParams, ""); err != nil {
    return nil, fmt.Errorf("initialize 失败: %w", err)
  } else {
    sid = newSid
  }

  // 发送 tools/list
  result, newSid, err := sendRequest(2, "tools/list", nil, sid)
  if err != nil {
    return nil, fmt.Errorf("tools/list 失败: %w", err)
  }
  if newSid != "" {
    sid = newSid
  }

  // 保存 session ID 供后续 call 使用
  proxy.mu.Lock()
  proxy.sessionID = sid
  proxy.mu.Unlock()

  toolsRaw, ok := result["tools"]
  if !ok {
    return nil, fmt.Errorf("tools/list 响应中缺少 tools 字段")
  }

  toolsJSON, err := json.Marshal(toolsRaw)
  if err != nil {
    return nil, fmt.Errorf("序列化 tools 失败: %w", err)
  }

  var tools []ToolDef
  if err := json.Unmarshal(toolsJSON, &tools); err != nil {
    return nil, fmt.Errorf("解析 ToolDef 失败: %w", err)
  }

  return tools, nil
}

// ============================================================
// 工具调用
// ============================================================

// call 在代理上执行工具调用
func (p *MCPProxy) call(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
  p.mu.Lock()
  defer p.mu.Unlock()

  if !p.running {
    return nil, fmt.Errorf("MCP 代理 %s 未运行", p.Name)
  }

  switch p.Transport {
  case "stdio":
    return p.callStdio(ctx, toolName, args)
  case "http", "sse":
    return p.callHTTP(ctx, toolName, args)
  default:
    return nil, fmt.Errorf("不支持的传输方式: %s", p.Transport)
  }
}

// callStdio 通过子进程 stdin/stdout 发送 tools/call 并读取结果
func (p *MCPProxy) callStdio(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
  req := map[string]interface{}{
    "jsonrpc": "2.0",
    "id":      3,
    "method":  "tools/call",
    "params": map[string]interface{}{
      "name":      toolName,
      "arguments": args,
    },
  }

  reqBytes, err := json.Marshal(req)
  if err != nil {
    return nil, fmt.Errorf("序列化请求失败: %w", err)
  }

  if _, err := fmt.Fprintf(p.stdin, "%s\n", reqBytes); err != nil {
    return nil, fmt.Errorf("写入 stdin 失败: %w", err)
  }

  for {
    line, err := p.stdout.ReadString('\n')
    if err != nil {
      return nil, fmt.Errorf("读取 stdout 失败: %w", err)
    }
    line = strings.TrimSpace(line)
    if line == "" {
      continue
    }

    var resp struct {
      JSONRPC string          `json:"jsonrpc"`
      ID      json.Number     `json:"id"`
      Result  json.RawMessage `json:"result"`
      Error   *struct {
        Code    int    `json:"code"`
        Message string `json:"message"`
      } `json:"error"`
    }
    if err := json.Unmarshal([]byte(line), &resp); err != nil {
      continue
    }

    if resp.ID.String() != "3" {
      continue
    }

    if resp.Error != nil {
      return nil, fmt.Errorf("MCP 错误 (code=%d): %s", resp.Error.Code, resp.Error.Message)
    }

    var result map[string]interface{}
    if err := json.Unmarshal(resp.Result, &result); err != nil {
      return nil, fmt.Errorf("解析 result 失败: %w", err)
    }
    return result, nil
  }
}

// callHTTP 通过 HTTP POST 发送 tools/call 并读取结果
func (p *MCPProxy) callHTTP(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
  req := map[string]interface{}{
    "jsonrpc": "2.0",
    "id":      3,
    "method":  "tools/call",
    "params": map[string]interface{}{
      "name":      toolName,
      "arguments": args,
    },
  }

  reqBytes, err := json.Marshal(req)
  if err != nil {
    return nil, fmt.Errorf("序列化请求失败: %w", err)
  }

  httpReq, err := http.NewRequestWithContext(ctx, "POST", p.URL, bytes.NewReader(reqBytes))
  if err != nil {
    return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
  }
  httpReq.Header.Set("Content-Type", "application/json")
  httpReq.Header.Set("Mcp-Protocol-Version", "2024-11-05")
  for k, v := range p.Headers {
    httpReq.Header.Set(k, v)
  }
  if p.sessionID != "" {
    httpReq.Header.Set("Mcp-Session-Id", p.sessionID)
  }

  client := &http.Client{}
  httpResp, err := client.Do(httpReq)
  if err != nil {
    return nil, fmt.Errorf("HTTP 请求失败: %w", err)
  }
  defer httpResp.Body.Close()

  // 更新 session ID（某些服务器可能每次请求都更新）
  newSessionID := httpResp.Header.Get("Mcp-Session-Id")
  if newSessionID != "" && newSessionID != p.sessionID {
    p.mu.Lock()
    p.sessionID = newSessionID
    p.mu.Unlock()
  }

  body, err := io.ReadAll(httpResp.Body)
  if err != nil {
    return nil, fmt.Errorf("读取 HTTP 响应失败: %w", err)
  }

  // 处理 SSE 响应
  parseBody := body
  if httpResp.Header.Get("Content-Type") == "text/event-stream" || bytes.HasPrefix(bytes.TrimSpace(body), []byte("data: ")) {
    parseBody = extractSSEJSON(body)
  }

  var resp struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.Number     `json:"id"`
    Result  json.RawMessage `json:"result"`
    Error   *struct {
      Code    int    `json:"code"`
      Message string `json:"message"`
    } `json:"error"`
  }
  if err := json.Unmarshal(parseBody, &resp); err != nil {
    return nil, fmt.Errorf("解析 HTTP 响应 JSON 失败: %w (body: %s)", err, string(parseBody))
  }

  if resp.Error != nil {
    return nil, fmt.Errorf("MCP 错误 (code=%d): %s", resp.Error.Code, resp.Error.Message)
  }

  var result map[string]interface{}
  if err := json.Unmarshal(resp.Result, &result); err != nil {
    return nil, fmt.Errorf("解析 result 失败: %w", err)
  }
  return result, nil
}

// ============================================================
// 辅助函数
// ============================================================

// stop 停止代理的子进程并清理资源
func (p *MCPProxy) stop() {
  p.mu.Lock()
  defer p.mu.Unlock()

  if p.cancel != nil {
    p.cancel()
  }
  if p.stdin != nil {
    p.stdin.Close()
  }
  if p.cmd != nil && p.cmd.Process != nil {
    p.cmd.Process.Kill()
  }
  p.running = false
  p.tools = nil
}

// extractSSEJSON 从 SSE 响应体中提取第一个 data: 行的 JSON
func extractSSEJSON(body []byte) []byte {
  scanner := bufio.NewScanner(bytes.NewReader(body))
  for scanner.Scan() {
    line := scanner.Text()
    if strings.HasPrefix(line, "data: ") {
      return []byte(line[6:])
    }
  }
  return body
}

// mapToEnvSlice 将 map[string]string 转换为 ["KEY=VAL", ...] 格式
func mapToEnvSlice(env map[string]string) []string {
  result := make([]string, 0, len(env))
  for k, v := range env {
    result = append(result, k+"="+v)
  }
  return result
}

// ============================================================
// 授权查询
// ============================================================

// hasMCPGrant 检查用户是否有权访问指定的 MCP 服务器
func hasMCPGrant(serverName, username string) bool {
  engine, err := auth.GetEngine()
  if err != nil {
    return false
  }

  // 检查是否有针对该用户的授权
  count, err := engine.Where("server_id IN (SELECT id FROM mcp_servers WHERE name=?) AND ((grant_type='user' AND grant_value=?) OR grant_type='*')",
    serverName, username).Count(&MCPServerGrant{})
  if err != nil {
    return false
  }
  return count > 0
}

// MCPServerGrant xorm 模型（用于查询）
type MCPServerGrant struct {
  ID         int64  `xorm:"pk autoincr 'id'"`
  ServerID   int64  `xorm:"notnull 'server_id'"`
  GrantType  string `xorm:"notnull 'grant_type'"`
  GrantValue string `xorm:"notnull 'grant_value'"`
}

func (MCPServerGrant) TableName() string {
  return "mcp_server_grants"
}
