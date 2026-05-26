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
// web_search — DuckDuckGo HTML 搜索
// ============================================================

type WebSearchTool struct {
  client  *http.Client
  baseURL string // 测试用，覆盖默认搜索 URL
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
  return "搜索网络获取最新信息，返回搜索结果标题和摘要"
}

func (t *WebSearchTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "query": map[string]interface{}{
        "type":        "string",
        "description": "搜索关键词",
      },
    },
    "required": []string{"query"},
  }
}

func (t *WebSearchTool) httpClient() *http.Client {
  if t.client != nil {
    return t.client
  }
  return &http.Client{Timeout: 15 * time.Second}
}

func (t *WebSearchTool) searchURL(query string) string {
  u := t.baseURL
  if u == "" {
    u = "https://html.duckduckgo.com/html/"
  }
  return u + "?q=" + strings.ReplaceAll(query, " ", "+")
}

func (t *WebSearchTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Query string `json:"query"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }
  if params.Query == "" {
    return &ToolResult{Success: false, Data: "查询不能为空"}, nil
  }

  ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
  defer cancel()

  req, err := http.NewRequestWithContext(ctx, "GET", t.searchURL(params.Query), nil)
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("搜索失败: %v", err)}, nil
  }
  req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")

  resp, err := t.httpClient().Do(req)
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("搜索失败: %v", err)}, nil
  }
  defer resp.Body.Close()

  body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("搜索失败: %v", err)}, nil
  }

  if resp.StatusCode != http.StatusOK {
    return &ToolResult{Success: true, Data: fmt.Sprintf("搜索失败: HTTP %d", resp.StatusCode)}, nil
 }

  results := extractSearchResults(string(body))
  if len(results) == 0 {
    return &ToolResult{Success: true, Data: "无搜索结果"}, nil
  }

  var lines []string
  for i, r := range results {
    lines = append(lines, fmt.Sprintf("%d. %s\n   %s", i+1, r.title, r.url))
  }

  return &ToolResult{Success: true, Data: strings.Join(lines, "\n\n")}, nil
}

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

// ============================================================
// 搜索辅助函数
// ============================================================

type searchResult struct {
  title string
  url   string
}

// extractSearchResults 从 DuckDuckGo HTML 中提取搜索结果
func extractSearchResults(html string) []searchResult {
  var results []searchResult
  remaining := html

  for {
    // 查找 class="result__a"
    idx := strings.Index(remaining, `class="result__a"`)
    if idx == -1 {
      break
    }

    // 从该位置向前找 <a 标签起始
    before := remaining[:idx]
    aStart := strings.LastIndex(before, `<a`)
    if aStart == -1 {
      remaining = remaining[idx+len(`class="result__a"`):]
      continue
    }

    tag := remaining[aStart:]
    aEnd := strings.Index(tag, `</a>`)
    if aEnd == -1 {
      break
    }

    fullTag := tag[:aEnd+4]

    // 提取 href
    href := extractAttr(fullTag, "href")
    if href == "" || strings.HasPrefix(href, "#") {
      remaining = tag[aEnd+4:]
      continue
    }

    // 提取并清理文本
    textStart := strings.Index(fullTag, ">")
    if textStart == -1 {
      remaining = tag[aEnd+4:]
      continue
    }
    text := fullTag[textStart+1 : aEnd]
    text = stripHTMLTags(text)
    text = strings.TrimSpace(text)

    if text != "" {
      results = append(results, searchResult{title: text, url: href})
    }

    remaining = tag[aEnd+4:]

    if len(results) >= 10 {
      break
    }
  }

  return results
}

// extractAttr 从 HTML 标签中提取属性值
func extractAttr(tag, name string) string {
  pattern := name + `="`
  idx := strings.Index(tag, pattern)
  if idx == -1 {
    return ""
  }
  start := idx + len(pattern)
  end := strings.IndexByte(tag[start:], '"')
  if end == -1 {
    return ""
  }
  return tag[start : start+end]
}

// stripHTMLTags 移除 HTML 标签，保留文本
func stripHTMLTags(s string) string {
  var buf strings.Builder
  inTag := false
  for _, r := range s {
    if r == '<' {
      inTag = true
    } else if r == '>' {
      inTag = false
    } else if !inTag {
      buf.WriteRune(r)
    }
  }
  return strings.TrimSpace(buf.String())
}

