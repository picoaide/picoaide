package web

import (
  "net/http"
  "strings"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/auth"
)

// ============================================================
// MCP Cookie API（MCP token 认证，供容器内技能使用）
// ============================================================

// handleMCPCookiesGet 处理 GET /api/mcp/cookies
// 查询参数 domain 可选：指定则返回单个域名，否则返回全部
func (s *Server) handleMCPCookiesGet(c *gin.Context) {
  username := validateBearerOrQueryToken(c)
  if username == "" {
    return
  }

  domain := strings.TrimSpace(c.Query("domain"))

  if domain != "" {
    cookies, err := auth.GetCookie(username, domain)
    if err != nil {
      writeError(c, http.StatusInternalServerError, "读取失败")
      return
    }
    writeJSON(c, http.StatusOK, gin.H{
      "success": true,
      "domain":  domain,
      "cookies": cookies,
    })
    return
  }

  all, err := auth.GetAllCookies(username)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "读取失败")
    return
  }
  writeJSON(c, http.StatusOK, gin.H{
    "success": true,
    "cookies": all,
  })
}

// handleMCPCookiesPost 处理 POST /api/mcp/cookies
// 参数：domain, cookies
func (s *Server) handleMCPCookiesPost(c *gin.Context) {
  username := validateBearerOrQueryToken(c)
  if username == "" {
    return
  }

  domain := strings.TrimSpace(c.PostForm("domain"))
  cookieStr := strings.TrimSpace(c.PostForm("cookies"))

  if domain == "" || cookieStr == "" {
    writeError(c, http.StatusBadRequest, "域名和 Cookie 不能为空")
    return
  }

  if err := auth.SetCookie(username, domain, cookieStr); err != nil {
    writeError(c, http.StatusInternalServerError, "写入失败: "+err.Error())
    return
  }

  writeSuccess(c, "已更新 "+domain+" 的 Cookie")
}
