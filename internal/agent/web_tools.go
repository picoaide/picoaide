package agent

import (
  "context"
  "fmt"
  "io"
  "net/http"
  "strings"
  "time"
)

// ============================================================
// web_fetch — 获取 URL 内容
// ============================================================

type webFetchParams struct {
  URL string `json:"url" dc:"要获取内容的 URL"`
}

func NewWebFetchTool() ToolExecutor {
  return NewTypedTool[webFetchParams]("web_fetch",
    "获取指定 URL 的内容并返回纯文本",
    func(ctx context.Context, p webFetchParams) *ToolResult {
      if p.URL == "" {
        return &ToolResult{Success: false, Data: "URL 不能为空"}
      }

      ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
      defer cancel()

      req, err := http.NewRequestWithContext(ctx, "GET", p.URL, nil)
      if err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("获取失败: %v", err)}
      }
      req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")

      client := &http.Client{Timeout: 30 * time.Second}
      resp, err := client.Do(req)
      if err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("获取失败: %v", err)}
      }
      defer resp.Body.Close()

      body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
      if err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("获取失败: %v", err)}
      }

      content := strings.TrimSpace(string(body))
      if resp.StatusCode != http.StatusOK {
        content = fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, content)
      }

      const maxOutputChars = 50000
      if len(content) > maxOutputChars {
        content = content[:maxOutputChars] + "\n... (内容过长，仅显示前 50000 字符)"
      }

      return &ToolResult{Success: true, Data: content}
    })
}
