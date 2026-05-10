package web

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/picoaide/picoaide/internal/auth"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		if strings.HasPrefix(origin, "chrome-extension://") || strings.HasPrefix(origin, "moz-extension://") {
			return true
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		return origin == scheme+"://"+r.Host
	},
}

// handleMCPToken 返回当前用户的 MCP token
func (s *Server) handleMCPToken(c *gin.Context) {
	username := s.requireNonSuperadmin(c)
	if username == "" {
		return
	}

	token, err := auth.GetMCPToken(username)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if token == "" {
		token, err = auth.GenerateMCPToken(username)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "生成 MCP token 失败: "+err.Error())
			return
		}
		slog.Info("自动生成 MCP token", "username", username)
	}

	writeJSON(c, http.StatusOK, struct {
		Success bool   `json:"success"`
		Token   string `json:"token"`
	}{
		Success: true,
		Token:   token,
	})
}
