package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/picoaide/picoaide/internal/auth"
)

// validateBearerOrQueryToken 从 Bearer header 或 query param 验证 MCP token
func validateBearerOrQueryToken(c *gin.Context) string {
	token := extractToken(c.Request)
	if token == "" {
		writeError(c, http.StatusUnauthorized, "需要 MCP token")
		return ""
	}
	username, ok := auth.ValidateMCPToken(token)
	if !ok {
		writeError(c, http.StatusForbidden, "无效的 MCP token")
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

// formatMCPResult 将代理返回值转为 MCP content 格式
func formatMCPResult(result interface{}) map[string]interface{} {
	if result == nil {
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "执行成功"},
			},
		}
	}

	if m, ok := result.(map[string]interface{}); ok {
		if content, ok := m["content"].([]interface{}); ok {
			return map[string]interface{}{"content": content}
		}
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
