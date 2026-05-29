package web

import (
  "errors"
  "io"
  "log/slog"
  "net/http"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
)

// ============================================================
// 记忆进化审计 API
// picoagent 在会话结束后通过此接口记录进化事件到数据库
// ============================================================

type auditRequest struct {
  Username       string `json:"username"`
  SessionKey     string `json:"session_key"`
  ChangesSummary string `json:"changes_summary"`
  FilesModified  string `json:"files_modified"`
}

func (s *Server) handlePicoAgentAudit(c *gin.Context) {
  // 0. 速率限制
  if s.auditLimiter != nil {
    if !s.auditLimiter.allow(clientIPFromRequest(c.Request)) {
      c.JSON(http.StatusTooManyRequests, gin.H{"error": "请求过于频繁，请稍后再试"})
      return
    }
  }

  // 1. 验证 Bearer token
  authHeader := c.GetHeader("Authorization")
  if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
    c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少 Authorization 头"})
    return
  }
  token := strings.TrimPrefix(authHeader, "Bearer ")

  username, ok := auth.ValidateMCPToken(token)
  if !ok {
    c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 token"})
    return
  }

  // 2. 限制请求体大小（最大 64KB，防止 OOM）
  c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)

  // 3. 解析请求体
  var req auditRequest
  if err := c.ShouldBindJSON(&req); err != nil {
    if err == io.EOF {
      c.JSON(http.StatusBadRequest, gin.H{"error": "请求体为空"})
      return
    }
    var maxBytesErr *http.MaxBytesError
    if errors.As(err, &maxBytesErr) {
      c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "请求体过大"})
      return
    }
    c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
    return
  }
  if req.Username == "" || req.SessionKey == "" {
    c.JSON(http.StatusBadRequest, gin.H{"error": "username 和 session_key 不能为空"})
    return
  }

  // 验证 token 与请求中的 username 一致
  if req.Username != username {
    c.JSON(http.StatusForbidden, gin.H{"error": "token 与 username 不匹配"})
    return
  }

  // 3. 写入数据库
  engine, err := auth.GetEngine()
  if err != nil {
    slog.Error("memory_evolution.audit_db_error", "error", err.Error())
    c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接失败"})
    return
  }

  log := auth.MemoryEvolutionLog{
    Username:       req.Username,
    SessionKey:     req.SessionKey,
    ChangesSummary: req.ChangesSummary,
    FilesModified:  req.FilesModified,
  }

  if _, err := engine.Insert(&log); err != nil {
    slog.Error("memory_evolution.audit_insert_error", "error", err.Error())
    c.JSON(http.StatusInternalServerError, gin.H{"error": "写入审计日志失败"})
    return
  }

  slog.Info("memory_evolution.audit",
    "username", req.Username,
    "session_key", req.SessionKey,
    "changes", req.ChangesSummary,
    "files", req.FilesModified,
  )

  c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
