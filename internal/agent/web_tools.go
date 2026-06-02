package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "io"
  "net/http"
  "strings"
  "time"
)

// ============================================================
// web_fetch — 获取 URL 内容
// ============================================================

type WebFetchTool struct {
  client *http.Client
}

func (t *WebFetchTool) Name() string { return "web_fetch" }

func (t *WebFetchTool) Description() string {
  return "获取指定 URL 的内容并返回纯文本"
}

func (t *WebFetchTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "url": map[string]interface{}{
        "type":        "string",
        "description": "要获取内容的 URL",
      },
    },
    "required": []string{"url"},
  }
}

func (t *WebFetchTool) httpClient() *http.Client {
  if t.client != nil {
    return t.client
  }
  return &http.Client{Timeout: 30 * time.Second}
}

func (t *WebFetchTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    URL string `json:"url"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }
  if params.URL == "" {
    return &ToolResult{Success: false, Data: "URL 不能为空"}, nil
  }

  ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
  defer cancel()

  req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("获取失败: %v", err)}, nil
  }
  req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")

  resp, err := t.httpClient().Do(req)
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("获取失败: %v", err)}, nil
  }
  defer resp.Body.Close()

  body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("获取失败: %v", err)}, nil
  }

  content := strings.TrimSpace(string(body))
  if resp.StatusCode != http.StatusOK {
    content = fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, content)
  }

  const maxOutputChars = 50000
  if len(content) > maxOutputChars {
    content = content[:maxOutputChars] + "\n... (内容过长，仅显示前 50000 字符)"
  }

  return &ToolResult{Success: true, Data: content}, nil
}
