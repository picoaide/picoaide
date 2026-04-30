package web

import (
  "encoding/json"
  "fmt"
  "io"
  "log"
  "net/http"
  "time"
)

// ServiceInfo 描述一个 MCP SSE 服务的完整配置
type ServiceInfo struct {
  Hub        *ServiceHub
  Tools      []ToolDef
  ServerName string
  Version    string
}

// serviceRegistry 已注册的 MCP 服务
var serviceRegistry = map[string]*ServiceInfo{}

func init() {
  RegisterService("browser", browserSvc, browserToolDefs, "picoaide-browser")
  RegisterService("computer", computerSvc, computerToolDefs, "picoaide-computer")
}

// RegisterService 注册一个 MCP SSE 服务
func RegisterService(name string, hub *ServiceHub, tools []ToolDef, serverName string) {
  serviceRegistry[name] = &ServiceInfo{
    Hub:        hub,
    Tools:      tools,
    ServerName: serverName,
    Version:    "1.0.0",
  }
}

// handleMCPSSEService 处理 MCP SSE 连接和 JSON-RPC 消息（通用）
// GET: 建立 SSE 流，发送 endpoint 事件
// POST: 处理 JSON-RPC 消息
func (s *Server) handleMCPSSEService(w http.ResponseWriter, r *http.Request, serviceName string) {
  info, ok := serviceRegistry[serviceName]
  if !ok {
    writeError(w, http.StatusNotFound, "未知的 MCP 服务: "+serviceName)
    return
  }

  if r.Method == "OPTIONS" {
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
    w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
    w.WriteHeader(http.StatusOK)
    return
  }

  if r.Method == "POST" {
    s.handleMCPMessageService(w, r, info)
    return
  }

  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 和 POST 方法")
    return
  }

  username := validateBearerOrQueryToken(w, r)
  if username == "" {
    return
  }

  // SSE 响应头
  w.Header().Set("Content-Type", "text/event-stream")
  w.Header().Set("Cache-Control", "no-cache")
  w.Header().Set("Connection", "keep-alive")
  w.Header().Set("Access-Control-Allow-Origin", "*")

  flusher, ok := w.(http.Flusher)
  if !ok {
    writeError(w, http.StatusInternalServerError, "不支持 SSE")
    return
  }

  // 发送 endpoint 事件
  token := extractToken(r)
  scheme := "http"
  if r.TLS != nil {
    scheme = "https"
  }
  host := r.Host
  if host == "" {
    host = "100.64.0.1:80"
  }
  postEndpoint := fmt.Sprintf("%s://%s/api/mcp/sse/%s?token=%s", scheme, host, serviceName, token)
  fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", postEndpoint)
  flusher.Flush()

  log.Printf("[mcp-%s] %s SSE 连接建立", serviceName, username)

  // 保持连接
  notify := r.Context().Done()
  ticker := time.NewTicker(30 * time.Second)
  defer ticker.Stop()

  for {
    select {
    case <-notify:
      log.Printf("[mcp-%s] %s SSE 连接关闭", serviceName, username)
      return
    case <-ticker.C:
      fmt.Fprintf(w, ": keepalive\n\n")
      flusher.Flush()
    }
  }
}

// handleMCPMessageService 处理 MCP JSON-RPC 请求（通用）
func (s *Server) handleMCPMessageService(w http.ResponseWriter, r *http.Request, info *ServiceInfo) {
  if r.Method == "OPTIONS" {
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
    w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
    w.WriteHeader(http.StatusOK)
    return
  }

  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }

  username := validateBearerOrQueryToken(w, r)
  if username == "" {
    return
  }

  body, err := io.ReadAll(r.Body)
  if err != nil {
    writeError(w, http.StatusBadRequest, "读取请求体失败")
    return
  }
  defer r.Body.Close()

  var req struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.Number     `json:"id"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params"`
  }
  if err := json.Unmarshal(body, &req); err != nil {
    writeJSON(w, http.StatusOK, mcpError(json.Number("null"), -32700, "Parse error"))
    return
  }

  w.Header().Set("Content-Type", "application/json")
  w.Header().Set("Access-Control-Allow-Origin", "*")

  switch req.Method {
  case "initialize":
    writeMCPResult(w, req.ID, map[string]interface{}{
      "protocolVersion": "2024-11-05",
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
    w.WriteHeader(http.StatusNoContent)

  case "tools/list":
    tools := make([]map[string]interface{}, len(info.Tools))
    for i, t := range info.Tools {
      tools[i] = map[string]interface{}{
        "name":        t.Name,
        "description": t.Description,
        "inputSchema": t.InputSchema,
      }
    }
    writeMCPResult(w, req.ID, map[string]interface{}{
      "tools": tools,
    })

  case "tools/call":
    s.handleMCPToolCall(w, r, req.ID, req.Params, username, info)

  default:
    writeMCPResult(w, req.ID, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": fmt.Sprintf("未知方法: %s", req.Method)},
      },
      "isError": true,
    })
  }
}

// handleMCPToolCall 处理 tools/call 请求（通用）
func (s *Server) handleMCPToolCall(w http.ResponseWriter, r *http.Request, id json.Number, params json.RawMessage, username string, info *ServiceInfo) {
  var p struct {
    Name      string                 `json:"name"`
    Arguments map[string]interface{} `json:"arguments"`
  }
  if err := json.Unmarshal(params, &p); err != nil {
    writeMCPResult(w, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "参数解析失败: " + err.Error()},
      },
      "isError": true,
    })
    return
  }

  // 查找代理连接
  conn, ok := info.Hub.GetConnection(username)
  if !ok {
    writeMCPResult(w, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": info.ServerName + " 代理未连接"},
      },
      "isError": true,
    })
    return
  }

  // 发送命令到代理并等待响应
  result, err := conn.SendCommand(r.Context(), p.Name, p.Arguments)
  if err != nil {
    writeMCPResult(w, id, map[string]interface{}{
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
    writeMCPResult(w, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": errMsg},
      },
      "isError": true,
    })
    return
  }

  writeMCPResult(w, id, formatMCPResult(extResp.Result))
}
