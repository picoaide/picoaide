package web

import (
  "encoding/json"
  "fmt"
  "io"
  "log/slog"
  "net/http"
  "time"

  "github.com/gin-gonic/gin"
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

// handleMCPSSEServiceGet 处理 MCP SSE GET 连接（建立 SSE 流）
func (s *Server) handleMCPSSEServiceGet(c *gin.Context) {
  serviceName := c.Param("service")
  _, ok := serviceRegistry[serviceName]
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
  c.Writer.Header().Set("Cache-Control", "no-cache")
  c.Writer.Header().Set("Connection", "keep-alive")
  c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

  // 发送 endpoint 事件
  token := extractToken(c.Request)
  scheme := "http"
  if c.Request.TLS != nil {
    scheme = "https"
  }
  host := c.Request.Host
  if host == "" {
    host = "100.64.0.1:80"
  }
  postEndpoint := fmt.Sprintf("%s://%s/api/mcp/sse/%s?token=%s", scheme, host, serviceName, token)
  fmt.Fprintf(c.Writer, "event: endpoint\ndata: %s\n\n", postEndpoint)
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

// handleMCPSSEServicePost 处理 MCP SSE POST 请求（JSON-RPC 消息）
func (s *Server) handleMCPSSEServicePost(c *gin.Context) {
  serviceName := c.Param("service")
  info, ok := serviceRegistry[serviceName]
  if !ok {
    writeError(c, http.StatusNotFound, "未知的 MCP 服务: "+serviceName)
    return
  }

  username := validateBearerOrQueryToken(c)
  if username == "" {
    return
  }

  body, err := io.ReadAll(c.Request.Body)
  if err != nil {
    writeError(c, http.StatusBadRequest, "读取请求体失败")
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
    writeMCPResult(c.Writer, req.ID, map[string]interface{}{
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
    c.Status(http.StatusNoContent)

  case "tools/list":
    tools := make([]map[string]interface{}, len(info.Tools))
    for i, t := range info.Tools {
      tools[i] = map[string]interface{}{
        "name":        t.Name,
        "description": t.Description,
        "inputSchema": t.InputSchema,
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
