package web

import (
  "fmt"
  "net/http"
  "strconv"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
)

// ============================================================
// MCP 服务器管理（超管 CRUD）
// ============================================================

type mcpServerReq struct {
  Name      string
  Transport string
  Command   string
  Args      string
  URL       string
  Env       string
  Headers   string
  Enabled   bool
}

type mcpServerGrantReq struct {
  ServerID   int64
  GrantType  string
  GrantValue string
}

// parseMCPForm 从 POST 表单解析 mcpServerReq
func parseMCPForm(c *gin.Context) (mcpServerReq, error) {
  var req mcpServerReq
  req.Name = c.PostForm("name")
  req.Transport = c.PostForm("transport")
  if req.Transport == "" {
    req.Transport = "stdio"
  }
  req.Command = c.PostForm("command")
  req.Args = c.PostForm("args")
  if req.Args == "" {
    req.Args = "[]"
  }
  req.URL = c.PostForm("url")
  req.Env = c.PostForm("env")
  if req.Env == "" {
    req.Env = "{}"
  }
  req.Headers = c.PostForm("headers")
  if req.Headers == "" {
    req.Headers = "{}"
  }
  req.Enabled = c.PostForm("enabled") == "true"
  if req.Name == "" {
    return req, fmt.Errorf("name 不能为空")
  }
  return req, nil
}

// handleAdminMCPServersList 获取所有 MCP 服务器列表
func (s *Server) handleAdminMCPServersList(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }

  rows, err := engine.Query("SELECT id, name, transport, command, args, url, env, headers, enabled, created_at, updated_at FROM mcp_servers ORDER BY id")
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询失败: "+err.Error())
    return
  }

  // engine.Query 返回 map[string][]byte，JSON 序列化 []byte 默认用 base64，需转 string
  type serverRow struct {
    ID        int64  `json:"id"`
    Name      string `json:"name"`
    Transport string `json:"transport"`
    Command   string `json:"command"`
    Args      string `json:"args"`
    URL       string `json:"url"`
    Env       string `json:"env"`
    Headers   string `json:"headers"`
    Enabled   string `json:"enabled"`
    CreatedAt string `json:"created_at"`
    UpdatedAt string `json:"updated_at"`
    ToolCount int    `json:"tool_count"`
  }
  data := make([]serverRow, 0, len(rows))
  for _, r := range rows {
    id, _ := strconv.ParseInt(string(r["id"]), 10, 64)
    name := string(r["name"])
    data = append(data, serverRow{
      ID:        id,
      Name:      name,
      Transport: string(r["transport"]),
      Command:   string(r["command"]),
      Args:      string(r["args"]),
      URL:       string(r["url"]),
      Env:       string(r["env"]),
      Headers:   string(r["headers"]),
      Enabled:   string(r["enabled"]),
      CreatedAt: string(r["created_at"]),
      UpdatedAt: string(r["updated_at"]),
      ToolCount: globalMCPManager.ToolCount(name),
    })
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": data})
}

// handleAdminMCPServerCreate 创建 MCP 服务器
func (s *Server) handleAdminMCPServerCreate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  req, err := parseMCPForm(c)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }

  _, err = engine.Exec(
    `INSERT INTO mcp_servers (name, transport, command, args, url, env, headers, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
    req.Name, req.Transport, req.Command, req.Args, req.URL, req.Env, req.Headers, boolToInt(req.Enabled),
  )
  if err != nil {
    writeError(c, http.StatusInternalServerError, "创建失败: "+err.Error())
    return
  }

  // 创建后自动加载
  LoadMCPServers(c.Request.Context())

  writeJSON(c, http.StatusOK, gin.H{"success": true, "message": "MCP 服务器创建成功"})
}

// handleAdminMCPServerUpdate 更新 MCP 服务器
func (s *Server) handleAdminMCPServerUpdate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  idStr := c.Param("id")
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }

  req, err := parseMCPForm(c)
  if err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }

  _, err = engine.Exec(
    `UPDATE mcp_servers SET name=?, transport=?, command=?, args=?, url=?, env=?, headers=?, enabled=?, updated_at=datetime('now','localtime') WHERE id=?`,
    req.Name, req.Transport, req.Command, req.Args, req.URL, req.Env, req.Headers, boolToInt(req.Enabled), id,
  )
  if err != nil {
    writeError(c, http.StatusInternalServerError, "更新失败: "+err.Error())
    return
  }

  // 更新后自动加载
  LoadMCPServers(c.Request.Context())

  writeJSON(c, http.StatusOK, gin.H{"success": true, "message": "MCP 服务器更新成功"})
}

// handleAdminMCPServerDelete 删除 MCP 服务器
func (s *Server) handleAdminMCPServerDelete(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  idStr := c.Param("id")
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }

  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }

  _, err = engine.Exec("DELETE FROM mcp_servers WHERE id=?", id)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "删除失败: "+err.Error())
    return
  }

  // 删除后自动加载
  LoadMCPServers(c.Request.Context())

  writeJSON(c, http.StatusOK, gin.H{"success": true, "message": "MCP 服务器已删除"})
}

// handleAdminMCPServerGrantsList 获取指定 MCP 服务器的授权列表
func (s *Server) handleAdminMCPServerGrantsList(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  serverIDStr := c.Query("server_id")
  serverID, err := strconv.ParseInt(serverIDStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 server_id")
    return
  }

  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }

  rows, err := engine.Query("SELECT id, server_id, grant_type, grant_value FROM mcp_server_grants WHERE server_id=? ORDER BY id", serverID)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询失败: "+err.Error())
    return
  }

  type grantRow struct {
    ID         int64  `json:"id"`
    ServerID   string `json:"server_id"`
    GrantType  string `json:"grant_type"`
    GrantValue string `json:"grant_value"`
  }
  data := make([]grantRow, 0, len(rows))
  for _, r := range rows {
    id, _ := strconv.ParseInt(string(r["id"]), 10, 64)
    data = append(data, grantRow{
      ID:         id,
      ServerID:   string(r["server_id"]),
      GrantType:  string(r["grant_type"]),
      GrantValue: string(r["grant_value"]),
    })
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": data})
}

// handleAdminMCPServerGrantAdd 添加 MCP 服务器授权
func (s *Server) handleAdminMCPServerGrantAdd(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  serverIDStr := c.PostForm("server_id")
  serverID, _ := strconv.ParseInt(serverIDStr, 10, 64)
  grantType := c.PostForm("grant_type")
  grantValue := c.PostForm("grant_value")

  if grantType == "" || grantValue == "" {
    writeError(c, http.StatusBadRequest, "grant_type 和 grant_value 不能为空")
    return
  }

  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }

  _, err = engine.Exec(
    "INSERT INTO mcp_server_grants (server_id, grant_type, grant_value) VALUES (?, ?, ?)",
    serverID, grantType, grantValue,
  )
  if err != nil {
    writeError(c, http.StatusInternalServerError, "添加授权失败: "+err.Error())
    return
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true, "message": "授权添加成功"})
}

// handleAdminMCPServerGrantRemove 移除 MCP 服务器授权
func (s *Server) handleAdminMCPServerGrantRemove(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  idStr := c.Param("id")
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的 ID")
    return
  }

  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }

  _, err = engine.Exec("DELETE FROM mcp_server_grants WHERE id=?", id)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "移除授权失败: "+err.Error())
    return
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true, "message": "授权已移除"})
}

// handleAdminMCPServerTools 获取指定 MCP 服务器的工具列表
func (s *Server) handleAdminMCPServerTools(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  name := c.Query("name")
  if name == "" {
    writeError(c, http.StatusBadRequest, "name 不能为空")
    return
  }

  tools := globalMCPManager.GetServerTools(name)
  if tools == nil {
    tools = []ToolDef{}
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": tools})
}

// handleAdminMCPServersReload 重新加载所有 MCP 服务器
func (s *Server) handleAdminMCPServersReload(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  if err := LoadMCPServers(c.Request.Context()); err != nil {
    writeError(c, http.StatusInternalServerError, "重载 MCP 失败: "+err.Error())
    return
  }

  writeJSON(c, http.StatusOK, gin.H{"success": true, "message": "MCP 服务器已重新加载"})
}

// boolToInt 将 bool 转为 int（SQLite 无 bool 类型）
func boolToInt(b bool) int {
  if b {
    return 1
  }
  return 0
}
