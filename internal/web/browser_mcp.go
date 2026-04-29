package web

import (
  "encoding/json"
  "fmt"
  "io"
  "log"
  "net/http"
  "strings"
  "time"

  "github.com/picoaide/picoaide/internal/auth"
)

// handleBrowserMCPSSE 处理 MCP SSE 连接（GET /api/browser/mcp/sse?token=xxx）
func (s *Server) handleBrowserMCPSSE(w http.ResponseWriter, r *http.Request) {
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
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

  // 发送 endpoint 事件，告诉客户端 POST 地址
  token := extractToken(r)
  postEndpoint := fmt.Sprintf("/api/browser/mcp?token=%s", token)
  fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", postEndpoint)
  flusher.Flush()

  log.Printf("[browser-mcp] %s SSE 连接建立", username)

  // 保持连接，等待 context 取消
  notify := r.Context().Done()
  ticker := time.NewTicker(30 * time.Second)
  defer ticker.Stop()

  for {
    select {
    case <-notify:
      log.Printf("[browser-mcp] %s SSE 连接关闭", username)
      return
    case <-ticker.C:
      // 发送 keepalive 注释
      fmt.Fprintf(w, ": keepalive\n\n")
      flusher.Flush()
    }
  }
}

// handleBrowserMCPMessage 处理 MCP JSON-RPC 请求（POST /api/browser/mcp?token=xxx）
func (s *Server) handleBrowserMCPMessage(w http.ResponseWriter, r *http.Request) {
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
        "name":    "picoaide-browser",
        "version": "1.0.0",
      },
    })

  case "notifications/initialized":
    w.WriteHeader(http.StatusNoContent)

  case "tools/list":
    tools := make([]map[string]interface{}, len(GetToolList()))
    for i, t := range GetToolList() {
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
    s.handleToolCall(w, r, req.ID, req.Params, username)

  default:
    writeMCPResult(w, req.ID, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": fmt.Sprintf("未知方法: %s", req.Method)},
      },
      "isError": true,
    })
  }
}

// handleToolCall 处理 tools/call 请求
func (s *Server) handleToolCall(w http.ResponseWriter, r *http.Request, id json.Number, params json.RawMessage, username string) {
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

  // 查找 Extension 连接
  conn, ok := browserHub.GetConnection(username)
  if !ok {
    writeMCPResult(w, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "浏览器扩展未连接，请先在浏览器中点击授权"},
      },
      "isError": true,
    })
    return
  }

  // 发送命令到 Extension 并等待响应
  // 发送命令到 Extension 并等待响应
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

  // 解析 Extension 返回的结果
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

  // 格式化 MCP 结果
  writeMCPResult(w, id, formatMCPResult(extResp.Result))
}

// formatMCPResult 将 Extension 返回值转为 MCP content 格式
func formatMCPResult(result interface{}) map[string]interface{} {
  if result == nil {
    return map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "执行成功"},
      },
    }
  }

  // 截图结果（包含 base64 图片）
  if m, ok := result.(map[string]interface{}); ok {
    if content, ok := m["content"].([]interface{}); ok {
      return map[string]interface{}{"content": content}
    }
    // 其他结果转为 JSON 文本
    text := fmt.Sprintf("%v", result)
    if jsonBytes, err := json.Marshal(result); err == nil {
      text = string(jsonBytes)
    }
    return map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": text},
      },
    }
  }

  return map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": fmt.Sprintf("%v", result)},
    },
  }
}

// validateBearerOrQueryToken 从 Bearer header 或 query param 验证 MCP token
func validateBearerOrQueryToken(w http.ResponseWriter, r *http.Request) string {
  token := extractToken(r)
  if token == "" {
    writeError(w, http.StatusUnauthorized, "需要 MCP token")
    return ""
  }
  username, ok := auth.ValidateMCPToken(token)
  if !ok {
    writeError(w, http.StatusForbidden, "无效的 MCP token")
    return ""
  }
  return username
}

// extractToken 从 query param 或 Authorization header 提取 token
func extractToken(r *http.Request) string {
  if token := r.URL.Query().Get("token"); token != "" {
    return token
  }
  authHeader := r.Header.Get("Authorization")
  if strings.HasPrefix(authHeader, "Bearer ") {
    return strings.TrimPrefix(authHeader, "Bearer ")
  }
  return ""
}

func writeMCPResult(w http.ResponseWriter, id json.Number, result interface{}) {
  resp := map[string]interface{}{
    "jsonrpc": "2.0",
    "id":      id,
    "result":  result,
  }
  data, _ := json.Marshal(resp)
  w.Write(data)
}

func mcpError(id json.Number, code int, message string) map[string]interface{} {
  return map[string]interface{}{
    "jsonrpc": "2.0",
    "id":      id,
    "error": map[string]interface{}{
      "code":    code,
      "message": message,
    },
  }
}
