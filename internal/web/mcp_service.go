package web

import (
  "crypto/rand"
  "encoding/hex"
  "encoding/json"
  "fmt"
  "io"
  "log/slog"
  "net/http"
  "strings"
  "sync"
  "time"

  "github.com/gin-gonic/gin"
)

// toolToMap 将 ToolDef 转为 MCP 响应格式的 map
func toolToMap(t ToolDef) map[string]interface{} {
  return map[string]interface{}{
    "name":        t.Name,
    "description": t.Description,
    "inputSchema": t.InputSchema,
  }
}

// ServiceInfo 描述一个 MCP SSE 服务的完整配置
type ServiceInfo struct {
  Hub        *ServiceHub
  Tools      []ToolDef
  ServerName string
  Version    string
}

// serviceRegistry 已注册的 MCP 服务
var (
  serviceRegistry   = map[string]*ServiceInfo{}
  serviceRegistryMu sync.RWMutex
)

func init() {
  RegisterService("browser", browserSvc, browserToolDefs, "picoaide-browser")
  RegisterService("computer", computerSvc, computerToolDefs, "picoaide-computer")
  RegisterPicoaideService("agent", picoaideToolDefs, "picoaide-agent")
}

// getService 并发安全地获取已注册的 MCP 服务
func getService(name string) (*ServiceInfo, bool) {
  serviceRegistryMu.RLock()
  info, ok := serviceRegistry[name]
  serviceRegistryMu.RUnlock()
  return info, ok
}

// RegisterPicoaideService 注册一个 MCP SSE 服务（服务端 handler，无需代理 Hub）
func RegisterPicoaideService(name string, tools []ToolDef, serverName string) {
  serviceRegistryMu.Lock()
  serviceRegistry[name] = &ServiceInfo{
    Hub:        nil,
    Tools:      tools,
    ServerName: serverName,
    Version:    "1.0.0",
  }
  serviceRegistryMu.Unlock()
}

// unregisterService 注销一个 MCP SSE 服务
func unregisterService(name string) {
  serviceRegistryMu.Lock()
  delete(serviceRegistry, name)
  serviceRegistryMu.Unlock()
}

// RegisterService 注册一个 MCP SSE 服务
func RegisterService(name string, hub *ServiceHub, tools []ToolDef, serverName string) {
  serviceRegistryMu.Lock()
  serviceRegistry[name] = &ServiceInfo{
    Hub:        hub,
    Tools:      tools,
    ServerName: serverName,
    Version:    "1.0.0",
  }
  serviceRegistryMu.Unlock()
}

// handleMCPSSEServiceGet 处理 MCP SSE GET 连接（建立 SSE 流）
func (s *Server) handleMCPSSEServiceGet(c *gin.Context) {
  serviceName := c.Param("service")
  _, ok := getService(serviceName)
  if !ok {
    writeError(c, http.StatusNotFound, "未知的 MCP 服务: "+serviceName)
    return
  }

  username := validateBearerOrQueryToken(c)
  if username == "" {
    return
  }

  // SSE 响应头
  c.Writer.Header().Set("Content-Type", "text/event-stream")
  c.Writer.Header().Set("Cache-Control", "no-cache, no-transform")
  c.Writer.Header().Set("Connection", "keep-alive")
  c.Writer.Header().Set("X-Accel-Buffering", "no")
  c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
  c.Status(http.StatusOK)

  // 发送 endpoint 事件
  // PicoClaw 0.2.8 uses the MCP Streamable HTTP transport for type=sse. Its
  // standalone GET stream must be empty until server-initiated messages exist.
  // Older SSE clients do not send Mcp-Protocol-Version and still expect the
  // first event to be "endpoint".
  if !isStreamableMCPRequest(c.Request) {
    scheme := "http"
    if c.Request.TLS != nil {
      scheme = "https"
    }
    host := c.Request.Host
    if host == "" {
      host = "100.64.0.1:80"
    }
    // 动态提取路径前缀，兼容 /api/mcp/sse 和 /api/v1/mcp/sse 等任意前缀
    ssePath := "/api/mcp/sse/" + serviceName
    pathPrefix := c.Request.URL.Path
    if idx := strings.Index(pathPrefix, ssePath); idx >= 0 {
      pathPrefix = pathPrefix[:idx]
    } else {
      pathPrefix = "/api"
    }
    postEndpoint := fmt.Sprintf("%s://%s%s/mcp/sse/%s", scheme, host, pathPrefix, serviceName)
    fmt.Fprintf(c.Writer, "event: endpoint\ndata: %s\n\n", postEndpoint)
  } else {
    // Flush a comment frame so Streamable HTTP clients finish the standalone
    // SSE handshake immediately even behind buffering reverse proxies.
    fmt.Fprintf(c.Writer, ": connected\n\n")
  }
  c.Writer.WriteHeaderNow()
  c.Writer.Flush()

  slog.Info("MCP SSE 连接建立", "service", serviceName, "username", username)

  // 保持连接
  notify := c.Request.Context().Done()
  ticker := time.NewTicker(30 * time.Second)
  defer ticker.Stop()

  for {
    select {
    case <-notify:
      slog.Info("MCP SSE 连接关闭", "service", serviceName, "username", username)
      return
    case <-ticker.C:
      fmt.Fprintf(c.Writer, ": keepalive\n\n")
      c.Writer.Flush()
    }
  }
}

func isStreamableMCPRequest(r *http.Request) bool {
  if r.Header.Get("Mcp-Protocol-Version") != "" {
    return true
  }
  return r.Header.Get("Mcp-Session-Id") != ""
}

func isStreamableMCPPost(r *http.Request) bool {
  return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func streamableSessionID(username, serviceName string) string {
  var b [16]byte
  if _, err := rand.Read(b[:]); err == nil {
    return hex.EncodeToString(b[:])
  }
  return username + "-" + serviceName
}

func negotiatedMCPProtocolVersion(params json.RawMessage) string {
  var p struct {
    ProtocolVersion string `json:"protocolVersion"`
  }
  if err := json.Unmarshal(params, &p); err == nil && p.ProtocolVersion != "" {
    return p.ProtocolVersion
  }
  return "2024-11-05"
}

// handleMCPSSEServicePost 处理 MCP SSE POST 请求（JSON-RPC 消息）
func (s *Server) handleMCPSSEServicePost(c *gin.Context) {
  serviceName := c.Param("service")
  info, ok := getService(serviceName)
  if !ok {
    writeError(c, http.StatusNotFound, "未知的 MCP 服务: "+serviceName)
    return
  }

  username := validateBearerOrQueryToken(c)
  if username == "" {
    return
  }

  c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20) // 1 MB
  body, err := io.ReadAll(c.Request.Body)
  if err != nil {
    writeError(c, http.StatusBadRequest, "请求体过大或读取失败")
    return
  }
  defer c.Request.Body.Close()

  var req struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.Number     `json:"id"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params"`
  }
  if err := json.Unmarshal(body, &req); err != nil {
    writeJSON(c, http.StatusOK, mcpError(json.Number("null"), -32700, "Parse error"))
    return
  }

  c.Writer.Header().Set("Content-Type", "application/json")
  c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

  switch req.Method {
  case "initialize":
    if isStreamableMCPPost(c.Request) {
      c.Writer.Header().Set("Mcp-Session-Id", streamableSessionID(username, serviceName))
    }
    writeMCPResult(c.Writer, req.ID, map[string]interface{}{
      "protocolVersion": negotiatedMCPProtocolVersion(req.Params),
      "capabilities": map[string]interface{}{
        "tools": map[string]interface{}{
          "listChanged": false,
        },
      },
      "serverInfo": map[string]interface{}{
        "name":    info.ServerName,
        "version": info.Version,
      },
    })

  case "notifications/initialized":
    c.Status(http.StatusAccepted)

  case "tools/list":
    tools := make([]map[string]interface{}, 0)

    if info.Hub == nil {
      // 服务端服务：聚合所有来源的工具
      // 1. PicoAide 平台工具
      for _, t := range picoaideToolDefs {
        tools = append(tools, toolToMap(t))
      }
      // 2. 浏览器工具（如果已连接）
      if conn, ok := browserSvc.GetConnection(username); ok && conn != nil {
        for _, t := range browserToolDefs {
          tools = append(tools, toolToMap(t))
        }
      }
      // 3. 桌面代理工具（如果已连接）
      if conn, ok := computerSvc.GetConnection(username); ok && conn != nil {
        for _, t := range computerToolDefs {
          tools = append(tools, toolToMap(t))
        }
      }
    } else {
      for _, t := range info.Tools {
        tools = append(tools, toolToMap(t))
      }
    }

    writeMCPResult(c.Writer, req.ID, map[string]interface{}{
      "tools": tools,
    })

  case "tools/call":
    s.handleMCPToolCall(c, req.ID, req.Params, username, info)

  default:
    writeMCPResult(c.Writer, req.ID, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": fmt.Sprintf("未知方法: %s", req.Method)},
      },
      "isError": true,
    })
  }
}

// handleMCPToolCall 处理 tools/call 请求（通用）
func (s *Server) handleMCPToolCall(c *gin.Context, id json.Number, params json.RawMessage, username string, info *ServiceInfo) {
  var p struct {
    Name      string                 `json:"name"`
    Arguments map[string]interface{} `json:"arguments"`
  }
  if err := json.Unmarshal(params, &p); err != nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "参数解析失败: " + err.Error()},
      },
      "isError": true,
    })
    return
  }

  // 服务端 handler（无 Hub 的服务）
  if info.Hub == nil {
    // 先检查 PicoAide 平台工具 handler
    if _, ok := picoaideHandlers[p.Name]; ok {
      picoaideHandleMCPToolCall(s, c, id, p.Name, p.Arguments, username)
      return
    }
    // 第三方 MCP 代理服务：按服务名查找代理，直接转发
    serviceName := c.Param("service")
    if proxy, ok := globalMCPManager.GetProxy(serviceName); ok {
      result, err := proxy.call(c.Request.Context(), p.Name, p.Arguments)
      if err == nil {
        writeMCPResult(c.Writer, id, formatMCPResult(result))
        return
      }
    }
    // 转发到浏览器 Hub
    if strings.HasPrefix(p.Name, "browser_") {
      s.forwardToHub(c, id, p.Name, p.Arguments, username, browserSvc, "浏览器")
      return
    }
    // 转发到桌面代理 Hub
    if strings.HasPrefix(p.Name, "computer_") {
      s.forwardToHub(c, id, p.Name, p.Arguments, username, computerSvc, "桌面代理")
      return
    }
    // 都没找到，返回错误
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "未知工具: " + p.Name},
      },
      "isError": true,
    })
    return
  }

  // 查找代理连接
  conn, ok := info.Hub.GetConnection(username)
  if !ok {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": info.ServerName + " 代理未连接"},
      },
      "isError": true,
    })
    return
  }

  // 发送命令到代理并等待响应
  result, err := conn.SendCommand(c.Request.Context(), p.Name, p.Arguments)
  if err != nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "执行失败: " + err.Error()},
      },
      "isError": true,
    })
    return
  }

  // 解析代理返回的结果
  var extResp struct {
    Result interface{} `json:"result"`
    Error  interface{} `json:"error"`
  }
  json.Unmarshal(result, &extResp)

  if extResp.Error != nil {
    errMsg := fmt.Sprintf("%v", extResp.Error)
    if m, ok := extResp.Error.(map[string]interface{}); ok {
      if msg, ok := m["message"].(string); ok {
        errMsg = msg
      }
    }
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": errMsg},
      },
      "isError": true,
    })
    return
  }

  writeMCPResult(c.Writer, id, formatMCPResult(extResp.Result))
}

// forwardToHub 将 agent 服务收到的工具调用转发到对应 Hub（浏览器/桌面代理）
func (s *Server) forwardToHub(c *gin.Context, id json.Number, toolName string, args map[string]interface{}, username string, hub *ServiceHub, displayName string) {
  conn, ok := hub.GetConnection(username)
  if !ok {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": displayName + " 代理未连接"},
      },
      "isError": true,
    })
    return
  }

  result, err := conn.SendCommand(c.Request.Context(), toolName, args)
  if err != nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "执行失败: " + err.Error()},
      },
      "isError": true,
    })
    return
  }

  var extResp struct {
    Result interface{} `json:"result"`
    Error  interface{} `json:"error"`
  }
  json.Unmarshal(result, &extResp)

  if extResp.Error != nil {
    errMsg := fmt.Sprintf("%v", extResp.Error)
    if m, ok := extResp.Error.(map[string]interface{}); ok {
      if msg, ok := m["message"].(string); ok {
        errMsg = msg
      }
    }
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": errMsg},
      },
      "isError": true,
    })
    return
  }

  writeMCPResult(c.Writer, id, formatMCPResult(extResp.Result))
}
