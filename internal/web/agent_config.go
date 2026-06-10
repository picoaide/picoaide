package web

import (
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "net/http"
  "net/url"
  "strconv"
  "strings"
  "time"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/store"
)

// ============================================================
// PicoAgent 配置 API
// 沙箱内 PicoAgent 启动时通过此接口获取配置
// ============================================================

type agentConfigResponse struct {
  UserID    string                `json:"user_id"`
  Workspace string                `json:"workspace"`
  Model     agentModelConfig      `json:"model"`
  Tools     map[string]toolConfig `json:"tools"`
  MCPServers map[string]mcpServer `json:"mcp_servers"`
  MaxIter   int                   `json:"max_iter"`
  RequestTimeout int              `json:"request_timeout"`
}

type agentModelConfig struct {
  Provider       string  `json:"provider"`
  ModelID        string  `json:"model_id"`
  BaseURL        string  `json:"base_url,omitempty"`
  MaxTokens      int     `json:"max_tokens,omitempty"`
  MaxIter        int     `json:"max_iter,omitempty"`
  Temperature    float64 `json:"temperature,omitempty"`
  ContextWindow  int     `json:"context_window,omitempty"`
  RequestTimeout int     `json:"request_timeout,omitempty"`
}

type toolConfig struct {
  Enabled bool `json:"enabled"`
}

type mcpServer struct {
  Socket string `json:"socket"`
}

func (s *Server) handlePicoAgentConfig(c *gin.Context) {
  // 1. 提取 Bearer token
  authHeader := c.GetHeader("Authorization")
  if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
    c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少 Authorization 头"})
    return
  }
  token := strings.TrimPrefix(authHeader, "Bearer ")

  // 2. 验证 token
  username, ok := store.ValidateMCPToken(token)
  if !ok {
    c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 token"})
    return
  }

  // 3. 获取用户配置
  engine, err := store.GetEngine()
  if err != nil {
    c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接失败"})
    return
  }

  // 从 settings 表读取模型配置
  var settings []store.Setting
  engine.Find(&settings)
  kv := make(map[string]string)
  for _, s := range settings {
    kv[s.Key] = s.Value
  }

  // 4. 构造响应
  mcpServers := map[string]mcpServer{
    "browser":  {Socket: "/run/picoaide.sock"},
    "computer": {Socket: "/run/picoaide.sock"},
    "agent":    {Socket: "/run/picoaide.sock"},
  }
  // 添加第三方 MCP 代理服务（如 web-search-mcp-server），有授权的才可见
  for _, name := range ListProxyServices() {
    if hasMCPGrant(name, username) {
      mcpServers[name] = mcpServer{Socket: "/run/picoaide.sock"}
    }
  }
  resp := agentConfigResponse{
    UserID:    username,
    Workspace: "/workspace",
    Tools: map[string]toolConfig{
      "kb_search":  {Enabled: true},
      "web_search": {Enabled: true},
    },
    MCPServers: mcpServers,
  }

  // 模型配置：从 settings 表直接读取
  parseUint := func(key string, defaultVal int) int {
    v := kv[key]
    if v == "" { return defaultVal }
    n, err := strconv.Atoi(v)
    if err != nil || n <= 0 { return defaultVal }
    return n
  }
  parseFloat := func(key string, defaultVal float64) float64 {
    v := kv[key]
    if v == "" { return defaultVal }
    f, err := strconv.ParseFloat(v, 64)
    if err != nil || f <= 0 { return defaultVal }
    return f
  }

  provider := kv["model.provider"]
  if provider == "" { provider = "openai" }
  requestTimeout := parseUint("model.request_timeout", 600)
  resp.Model = agentModelConfig{
    Provider:       provider,
    ModelID:        kv["model.model_id"],
    BaseURL:        kv["model.base_url"],
    MaxTokens:      parseUint("model.max_tokens", 0),
    MaxIter:        parseUint("model.max_iter", 20),
    Temperature:    parseFloat("model.temperature", 0.7),
    ContextWindow:  parseUint("model.context_window", 200000),
    RequestTimeout: requestTimeout,
  }
  resp.RequestTimeout = requestTimeout
  if resp.Model.ModelID == "" {
    resp.Model.ModelID = "openai/qwen"
  }

  // Agent 运行配置
  resp.MaxIter = parseUint("agent.max_iter", 500)

  c.JSON(http.StatusOK, resp)
}

// ============================================================
// 模型连接测试
// ============================================================

func (s *Server) handleAdminModelTest(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }

  provider := c.PostForm("provider")
  modelID := c.PostForm("model_id")
  baseURL := strings.TrimRight(c.PostForm("base_url"), "/")
  apiKey := c.PostForm("api_key")

  if provider == "" || modelID == "" {
    writeError(c, http.StatusBadRequest, "供应商和模型 ID 不能为空")
    return
  }
  if apiKey == "" {
    writeError(c, http.StatusBadRequest, "API 密钥不能为空，请先在安全配置中设置")
    return
  }

  testMessage := map[string]interface{}{
    "model": modelID,
    "messages": []map[string]string{
      {"role": "user", "content": "Hello"},
    },
    "max_tokens": 5,
  }
  body, _ := json.Marshal(testMessage)

  // 根据供应商决定 API 路径
  apiPath := "/v1/chat/completions"
  var headers map[string]string
  switch provider {
  case "anthropic":
    apiPath = "/v1/messages"
    headers = map[string]string{
      "x-api-key":         apiKey,
      "anthropic-version": "2023-06-01",
      "content-type":      "application/json",
    }
  default:
    headers = map[string]string{
      "authorization": "Bearer " + apiKey,
      "content-type":  "application/json",
    }
  }

  urlStr := baseURL + apiPath
  if baseURL == "" {
    switch provider {
    case "anthropic":
      urlStr = "https://api.anthropic.com/v1/messages"
    case "deepseek":
      urlStr = "https://api.deepseek.com/v1/chat/completions"
    case "qwen":
      urlStr = "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"
    default:
      urlStr = "https://api.openai.com/v1/chat/completions"
    }
  }

  parsedURL, err := url.Parse(urlStr)
  if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
    writeError(c, http.StatusBadRequest, "无效的 API 地址")
    return
  }

  ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
  defer cancel()

  req, err := http.NewRequestWithContext(ctx, "POST", parsedURL.String(), bytes.NewReader(body))
  if err != nil {
    writeError(c, http.StatusBadRequest, fmt.Sprintf("创建请求失败: %s", err.Error()))
    return
  }
  for k, v := range headers {
    req.Header.Set(k, v)
  }

  req.URL = parsedURL
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    writeError(c, http.StatusBadRequest, fmt.Sprintf("连接失败: %s", err.Error()))
    return
  }
  defer resp.Body.Close()

  respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
  if resp.StatusCode != http.StatusOK {
    writeError(c, http.StatusBadRequest, fmt.Sprintf("API 返回错误 (HTTP %d): %s", resp.StatusCode, string(respBody)))
    return
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "连接成功，API 正常工作",
  })
}
