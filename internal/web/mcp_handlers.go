package web

import (
  "log/slog"
  "net/http"

  "github.com/gorilla/websocket"

  "github.com/picoaide/picoaide/internal/auth"
)

var upgrader = websocket.Upgrader{
  ReadBufferSize:  4096,
  WriteBufferSize: 4096,
  CheckOrigin: func(r *http.Request) bool {
    return true
  },
}

// handleMCPToken 返回当前用户的 MCP token
func (s *Server) handleMCPToken(w http.ResponseWriter, r *http.Request) {
  username := s.requireAuth(w, r)
  if username == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  token, err := auth.GetMCPToken(username)
  if err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }
  if token == "" {
    token, err = auth.GenerateMCPToken(username)
    if err != nil {
      writeError(w, http.StatusInternalServerError, "生成 MCP token 失败: "+err.Error())
      return
    }
    slog.Info("自动生成 MCP token", "username", username)
  }

  writeJSON(w, http.StatusOK, struct {
    Success bool   `json:"success"`
    Token   string `json:"token"`
  }{
    Success: true,
    Token:   token,
  })
}
