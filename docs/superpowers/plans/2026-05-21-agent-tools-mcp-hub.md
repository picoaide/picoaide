# Agent 工具系统与 MCP Hub 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 构建 Agent 完整工具体系 + 统一 MCP 端点 `/api/mcp/sse/agent`

**Architecture:**
- 沙箱内置工具在 `internal/agent/tool_registry.go` 注册，本地执行
- PicoAide 平台工具通过服务端 Go Handler 在 `/api/mcp/sse/agent` 端点暴露
- 浏览器/桌面工具通过现有 WebSocket Hub 在同一端点聚合
- 第三方 MCP 通过子进程/HTTP 代理动态注入

**Tech Stack:** Go, Gin, MCP SDK (go-sdk), SQLite (xorm)

---

### Task 1: 沙箱内置工具 — 新增 write_file / edit_file / append_file / list_dir / glob / delete_file

**Files:**
- Modify: `internal/agent/tool_registry.go`
- Test: `internal/agent/tool_registry_test.go` (create if needed)

- [ ] **Step 1: 添加 WriteFileTool**

在 `internal/agent/tool_registry.go` 末尾，在 GrepTool 后新增：

```go
// ============================================================
// write_file — 写入文件内容
// ============================================================

type WriteFileTool struct{}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
  return "写入内容到指定文件。如果文件已存在且 overwrite=false 则报错，overwrite=true 则覆盖。"
}

func (t *WriteFileTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "path": map[string]interface{}{
        "type":        "string",
        "description": "文件路径（相对 /workspace 或绝对路径）",
      },
      "content": map[string]interface{}{
        "type":        "string",
        "description": "要写入的文件内容",
      },
      "overwrite": map[string]interface{}{
        "type":        "boolean",
        "description": "是否覆盖已有文件，默认 false",
      },
    },
    "required": []string{"path", "content"},
  }
}

func (t *WriteFileTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Path      string `json:"path"`
    Content   string `json:"content"`
    Overwrite bool   `json:"overwrite"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }
  if params.Path == "" {
    return &ToolResult{Success: false, Data: "路径不能为空"}, nil
  }

  // 检查文件是否已存在
  if _, err := os.Stat(params.Path); err == nil && !params.Overwrite {
    return &ToolResult{Success: true, Data: fmt.Sprintf("文件已存在: %s（设置 overwrite=true 可覆盖）", params.Path)}, nil
  }

  if err := os.WriteFile(params.Path, []byte(params.Content), 0644); err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("写入失败: %v", err)}, nil
  }
  return &ToolResult{Success: true, Data: fmt.Sprintf("已写入 %d 字符到 %s", len(params.Content), params.Path)}, nil
}
```

- [ ] **Step 2: 添加 EditFileTool**

```go
// ============================================================
// edit_file — 精确字符串替换编辑
// ============================================================

type EditFileTool struct{}

func (t *EditFileTool) Name() string { return "edit_file" }

func (t *EditFileTool) Description() string {
  return "通过精确的 old_text → new_text 替换来编辑文件。要求替换字符串必须唯一，返回差异对比。"
}

func (t *EditFileTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "path": map[string]interface{}{
        "type":        "string",
        "description": "文件路径",
      },
      "old_text": map[string]interface{}{
        "type":        "string",
        "description": "要被替换的旧文本",
      },
      "new_text": map[string]interface{}{
        "type":        "string",
        "description": "替换后的新文本",
      },
    },
    "required": []string{"path", "old_text", "new_text"},
  }
}

func (t *EditFileTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Path    string `json:"path"`
    OldText string `json:"old_text"`
    NewText string `json:"new_text"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }

  data, err := os.ReadFile(params.Path)
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("读取失败: %v", err)}, nil
  }

  content := string(data)
  count := strings.Count(content, params.OldText)
  if count == 0 {
    return &ToolResult{Success: true, Data: "未找到匹配的旧文本"}, nil
  }
  if count > 1 {
    return &ToolResult{Success: true, Data: fmt.Sprintf("找到 %d 处匹配，old_text 必须唯一", count)}, nil
  }

  newContent := strings.Replace(content, params.OldText, params.NewText, 1)
  if err := os.WriteFile(params.Path, []byte(newContent), 0644); err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("写入失败: %v", err)}, nil
  }
  return &ToolResult{Success: true, Data: fmt.Sprintf("替换成功（1 处）"), nil}, nil
}
```

- [ ] **Step 3: 添加 AppendFileTool**

```go
// ============================================================
// append_file — 追加内容到文件末尾
// ============================================================

type AppendFileTool struct{}

func (t *AppendFileTool) Name() string { return "append_file" }

func (t *AppendFileTool) Description() string {
  return "将内容追加到文件末尾。文件不存在时自动创建。"
}

func (t *AppendFileTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "path": map[string]interface{}{
        "type":        "string",
        "description": "文件路径",
      },
      "content": map[string]interface{}{
        "type":        "string",
        "description": "要追加的内容",
      },
    },
    "required": []string{"path", "content"},
  }
}

func (t *AppendFileTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Path    string `json:"path"`
    Content string `json:"content"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }

  f, err := os.OpenFile(params.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("打开文件失败: %v", err)}, nil
  }
  defer f.Close()

  if _, err := f.WriteString(params.Content); err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("追加失败: %v", err)}, nil
  }
  return &ToolResult{Success: true, Data: fmt.Sprintf("已追加 %d 字符到 %s", len(params.Content), params.Path)}, nil
}
```

- [ ] **Step 4: 添加 ListDirTool**

```go
// ============================================================
// list_dir — 列出目录内容
// ============================================================

type ListDirTool struct{}

func (t *ListDirTool) Name() string { return "list_dir" }

func (t *ListDirTool) Description() string {
  return "列出指定目录下的文件和子目录，输出格式 DIR: name / FILE: name"
}

func (t *ListDirTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "path": map[string]interface{}{
        "type":        "string",
        "description": "目录路径，默认为 /workspace",
      },
    },
  }
}

func (t *ListDirTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Path string `json:"path"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    params.Path = ""
  }
  if params.Path == "" {
    params.Path = "/workspace"
  }

  entries, err := os.ReadDir(params.Path)
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("列出目录失败: %v", err)}, nil
  }

  var lines []string
  for _, e := range entries {
    if e.IsDir() {
      lines = append(lines, "DIR: "+e.Name())
    } else {
      fi, _ := e.Info()
      size := ""
      if fi != nil {
        size = fmt.Sprintf(" (%d bytes)", fi.Size())
      }
      lines = append(lines, "FILE: "+e.Name()+size)
    }
  }
  if len(lines) == 0 {
    return &ToolResult{Success: true, Data: "(目录为空)"}, nil
  }
  return &ToolResult{Success: true, Data: strings.Join(lines, "\n")}, nil
}
```

- [ ] **Step 5: 添加 GlobTool**

```go
// ============================================================
// glob — 按通配符模式搜索文件
// ============================================================

type GlobTool struct{}

func (t *GlobTool) Name() string { return "glob" }

func (t *GlobTool) Description() string {
  return "使用 glob 通配符模式搜索文件，如 **/*.go、*.txt、src/**/*.ts"
}

func (t *GlobTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "pattern": map[string]interface{}{
        "type":        "string",
        "description": "glob 搜索模式",
      },
      "root": map[string]interface{}{
        "type":        "string",
        "description": "搜索根目录，默认为 /workspace",
      },
    },
    "required": []string{"pattern"},
  }
}

func (t *GlobTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Pattern string `json:"pattern"`
    Root    string `json:"root"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }
  if params.Root == "" {
    params.Root = "/workspace"
  }

  matches, err := filepath.Glob(filepath.Join(params.Root, params.Pattern))
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("搜索失败: %v", err)}, nil
  }
  if len(matches) == 0 {
    return &ToolResult{Success: true, Data: "(无匹配文件)"}, nil
  }

  const maxGlobResults = 100
  if len(matches) > maxGlobResults {
    matches = matches[:maxGlobResults]
  }
  return &ToolResult{Success: true, Data: strings.Join(matches, "\n")}, nil
}
```

需要在 import 中添加 `"path/filepath"`。

- [ ] **Step 6: 添加 DeleteFileTool**

```go
// ============================================================
// delete_file — 删除文件或空目录
// ============================================================

type DeleteFileTool struct{}

func (t *DeleteFileTool) Name() string { return "delete_file" }

func (t *DeleteFileTool) Description() string {
  return "删除指定文件或空目录。目录非空时不会删除。"
}

func (t *DeleteFileTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "path": map[string]interface{}{
        "type":        "string",
        "description": "要删除的文件或目录路径",
      },
    },
    "required": []string{"path"},
  }
}

func (t *DeleteFileTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Path string `json:"path"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }

  if err := os.RemoveAll(params.Path); err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("删除失败: %v", err)}, nil
  }
  return &ToolResult{Success: true, Data: fmt.Sprintf("已删除: %s", params.Path)}, nil
}
```

- [ ] **Step 7: 将新工具注册到 NewToolRegistry**

在 Agent 入口处（`cmd/picoagent/main.go`）注册新工具：

```go
registry.Register(&WriteFileTool{})
registry.Register(&EditFileTool{})
registry.Register(&AppendFileTool{})
registry.Register(&ListDirTool{})
registry.Register(&GlobTool{})
registry.Register(&DeleteFileTool{})
```

找到 `cmd/picoagent/main.go` 中现有注册位置，追加上述注册。

- [ ] **Step 8: 编译验证**

```bash
go build ./cmd/picoagent/
```

Expected: 编译通过，无错误。

---

### Task 2: `picoaide` 平台工具定义 + Handler

**Files:**
- Create: `internal/web/picoaide_tools.go`
- Modify: `internal/web/mcp_service.go` (`init()` 中注册)
- Modify: `internal/web/server.go` (路由)
- Test: `internal/web/picoaide_tools_test.go`

- [ ] **Step 1: 创建 picoaide_tools.go**

```go
package web

// picoaideToolDefs PicoAide 平台管理工具
var picoaideToolDefs = []ToolDef{
  {
    Name:        "picoaide_user_info",
    Description: "获取当前用户的详细信息（用户名、角色、认证模式）",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "picoaide_skills_list",
    Description: "列出当前用户已绑定的所有技能",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "picoaide_config_get",
    Description: "读取 Agent 配置（Picoclaw 配置）",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "picoaide_config_set",
    Description: "更新 Agent 配置字段",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "key":   map[string]interface{}{"type": "string", "description": "配置键名"},
        "value": map[string]interface{}{"type": "string", "description": "配置值"},
      },
      "required": []string{"key", "value"},
    },
  },
  {
    Name:        "picoaide_shared_folders",
    Description: "列出当前用户可访问的团队共享文件夹",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "picoaide_cookies_list",
    Description: "列出已授权的 Cookie 域名列表",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "picoaide_cookies_delete",
    Description: "撤销指定域名的 Cookie 授权",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "domain": map[string]interface{}{"type": "string", "description": "要撤销的 Cookie 域名"},
      },
      "required": []string{"domain"},
    },
  },
}
```

- [ ] **Step 2: 实现服务端 Handler map**

在 `picoaide_tools.go` 添加 handler 映射函数：

```go
// picoaideHandlers 服务端 handler 映射
var picoaideHandlers = map[string]func(s *Server, c *gin.Context, args map[string]interface{}, username string){
  "picoaide_user_info":        handlePicoaideUserInfo,
  "picoaide_skills_list":      handlePicoaideSkillsList,
  "picoaide_config_get":       handlePicoaideConfigGet,
  "picoaide_config_set":       handlePicoaideConfigSet,
  "picoaide_shared_folders":   handlePicoaideSharedFolders,
  "picoaide_cookies_list":     handlePicoaideCookiesList,
  "picoaide_cookies_delete":   handlePicoaideCookiesDelete,
}

// picoaideHandleMCPToolCall 服务端工具调用分发
func (s *Server) picoaideHandleMCPToolCall(c *gin.Context, id json.Number, name string, args map[string]interface{}, username string) {
  handler, ok := picoaideHandlers[name]
  if !ok {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": fmt.Sprintf("未知工具: %s", name)},
      },
      "isError": true,
    })
    return
  }
  handler(s, c, args, username)
}

func handlePicoaideUserInfo(s *Server, c *gin.Context, args map[string]interface{}, username string) {
  // 从数据库获取用户信息
  engine, err := auth.GetEngine()
  if err != nil {
    writeMCPResult(c.Writer, json.Number("0"), mcpError(json.Number("0"), -32603, "数据库连接失败"))
    return
  }
  var user auth.LocalUser
  has, _ := engine.Where("username = ?", username).Get(&user)
  if !has {
    writeMCPResult(c.Writer, json.Number("0"), mcpError(json.Number("0"), -32603, "用户未找到"))
    return
  }
  writeMCPResult(c.Writer, json.Number("0"), map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": fmt.Sprintf("用户名: %s\n角色: %s\n认证源: %s", user.Username, user.Role, user.Source)},
    },
  })
}
```

需要 import `"fmt"` 和 `"github.com/picoaide/picoaide/internal/auth"`。

- [ ] **Step 3: 实现 skills_list handler**

```go
func handlePicoaideSkillsList(s *Server, c *gin.Context, args map[string]interface{}, username string) {
  engine, err := auth.GetEngine()
  if err != nil {
    writeMCPResult(c.Writer, json.Number("0"), mcpError(json.Number("0"), -32603, "数据库连接失败"))
    return
  }
  var skills []auth.UserSkill
  engine.Where("username = ?", username).Find(&skills)
  if len(skills) == 0 {
    writeMCPResult(c.Writer, json.Number("0"), map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "暂无绑定技能"},
      },
    })
    return
  }
  var lines []string
  for _, sk := range skills {
    lines = append(lines, fmt.Sprintf("- %s (来源: %s)", sk.SkillName, sk.Source))
  }
  writeMCPResult(c.Writer, json.Number("0"), map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": strings.Join(lines, "\n")},
    },
  })
}
```

需要 import `"strings"`。

- [ ] **Step 4: 实现 config_get / config_set / shared_folders / cookies 等 handler**

```go
func handlePicoaideConfigGet(s *Server, c *gin.Context, args map[string]interface{}, username string) {
  // 复用已有的 handleConfigGet 逻辑
  s.handleConfigGet(c)
}

func handlePicoaideConfigSet(s *Server, c *gin.Context, args map[string]interface{}, username string) {
  s.handleConfigSave(c)
}

func handlePicoaideSharedFolders(s *Server, c *gin.Context, args map[string]interface{}, username string) {
  s.handleSharedFolders(c)
}

func handlePicoaideCookiesList(s *Server, c *gin.Context, args map[string]interface{}, username string) {
  s.handleUserCookies(c)
}

func handlePicoaideCookiesDelete(s *Server, c *gin.Context, args map[string]interface{}, username string) {
  s.handleUserCookiesDelete(c)
}
```

注意：这些 handler 需要从 Gin Context 中提取 username，但 MCP 调用已经验证了 token。对于 handlers 内部要重新验证 session 的，需要调整；简单做法是先用 MCP token 的 username，然后走内部逻辑。

- [ ] **Step 5: 在 mcp_service.go 中注册 "agent" 聚合服务**

在 `init()` 函数中新增：

```go
func init() {
  RegisterService("browser", browserSvc, browserToolDefs, "picoaide-browser")
  RegisterService("computer", computerSvc, computerToolDefs, "picoaide-computer")
  RegisterPicoaideService("agent", picoaideToolDefs, "picoaide-agent")
}
```

添加 `RegisterPicoaideService` 函数：

```go
// RegisterPicoaideService 注册 PicoAide 平台服务（服务端 handler）
func RegisterPicoaideService(name string, tools []ToolDef, serverName string) {
  serviceRegistry[name] = &ServiceInfo{
    Hub:        nil,  // 无需 WebSocket Hub
    Tools:      tools,
    ServerName: serverName,
    Version:    "1.0.0",
  }
}
```

- [ ] **Step 6: 修改 handleMCPToolCall 支持服务端 Handler**

```go
// handleMCPToolCall 处理 tools/call 请求（通用）
func (s *Server) handleMCPToolCall(c *gin.Context, id json.Number, params json.RawMessage, username string, info *ServiceInfo) {
  var p struct {
    Name      string                 `json:"name"`
    Arguments map[string]interface{} `json:"arguments"`
  }
  if err := json.Unmarshal(params, &p); err != nil {
    writeMCPResult(c.Writer, id, mcpError(id, -32700, "参数解析失败"))
    return
  }

  // 服务端 handler 优先（picoaide_* 工具无 Hub）
  if info.Hub == nil && len(picoaideHandlers) > 0 {
    if handler, ok := picoaideHandlers[p.Name]; ok {
      handler(s, c, p.Arguments, username)
      return
    }
  }

  // 查找代理连接
  conn, ok := info.Hub.GetConnection(username)
  if !ok {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": info.ServerName + " 代理未连接"},
      },
      "isError": true,
    })
    return
  }

  // 发送命令到代理并等待响应...
  // （后续代码不变）
}
```

- [ ] **Step 7: 注册路由**

在 `server.go` 路由注册处，MCP SSE 的 `:service` 参数路由已经存在（`/mcp/sse/:service`），agent 服务会自动匹配，无需新增路由。

---

### Task 3: 沙箱内置 Web 工具 — web_search / web_fetch

**Files:**
- Create: `internal/agent/web_tools.go`

- [ ] **Step 1: 创建 web_tools.go**

```go
package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "io"
  "net/http"
  "net/url"
  "strings"
  "time"
)

// ============================================================
// web_search — 网络搜索
// ============================================================

type WebSearchTool struct{}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
  return "搜索网络获取最新信息。返回搜索结果标题和链接。"
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

func (t *WebSearchTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Query string `json:"query"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }

  ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
  defer cancel()

  req, _ := http.NewRequestWithContext(ctx, "GET", "https://html.duckduckgo.com/html/?q="+url.QueryEscape(params.Query), nil)
  req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PicoAide/1.0)")

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("搜索失败: %v", err)}, nil
  }
  defer resp.Body.Close()

  body, _ := io.ReadAll(io.LimitReader(resp.Body, 200*1024))
  html := string(body)

  // 简单提取搜索结果标题和链接
  var results []string
  lines := strings.Split(html, "\n")
  for _, line := range lines {
    if strings.Contains(line, "class=\"result__a\"") {
      results = append(results, extractSearchResult(line))
    }
  }

  if len(results) == 0 {
    return &ToolResult{Success: true, Data: "未找到搜索结果"}, nil
  }

  const maxResults = 10
  if len(results) > maxResults {
    results = results[:maxResults]
  }
  return &ToolResult{Success: true, Data: strings.Join(results, "\n\n")}, nil
}

func extractSearchResult(html string) string {
  // 粗略提取标题和链接
  text := html
  if start := strings.Index(text, ">"); start >= 0 {
    text = text[start+1:]
  }
  if end := strings.Index(text, "</a>"); end >= 0 {
    text = text[:end]
  }
  text = strings.TrimSpace(stripHTMLTags(text))
  return text
}

func stripHTMLTags(s string) string {
  var result strings.Builder
  inTag := false
  for _, r := range s {
    if r == '<' {
      inTag = true
      continue
    }
    if r == '>' {
      inTag = false
      continue
    }
    if !inTag {
      result.WriteRune(r)
    }
  }
  return result.String()
}
```

- [ ] **Step 2: 添加 web_fetch 工具**

```go
// ============================================================
// web_fetch — 获取 URL 内容
// ============================================================

type WebFetchTool struct{}

func (t *WebFetchTool) Name() string { return "web_fetch" }

func (t *WebFetchTool) Description() string {
  return "获取指定 URL 的内容并返回纯文本。适合读取网页、API 响应等。"
}

func (t *WebFetchTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "url": map[string]interface{}{
        "type":        "string",
        "description": "要获取的 URL",
      },
    },
    "required": []string{"url"},
  }
}

func (t *WebFetchTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    URL string `json:"url"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }

  ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
  defer cancel()

  req, _ := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
  req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PicoAide/1.0)")

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    return &ToolResult{Success: true, Data: fmt.Sprintf("获取失败: %v", err)}, nil
  }
  defer resp.Body.Close()

  body, _ := io.ReadAll(io.LimitReader(resp.Body, 500*1024))
  content := string(body)

  const maxFetchLen = 50000
  if len(content) > maxFetchLen {
    content = content[:maxFetchLen] + "\n... (内容过长，仅显示前 50000 字符)"
  }

  return &ToolResult{Success: true, Data: content}, nil
}
```

- [ ] **Step 3: 注册 web 工具**

在 `cmd/picoagent/main.go` 中追加注册：

```go
registry.Register(&WebSearchTool{})
registry.Register(&WebFetchTool{})
```

需在 main.go 添加 import `"github.com/picoaide/picoaide/internal/agent"`（如果 tools 在同一包则无需 import）。

- [ ] **Step 4: 编译验证**

```bash
go build ./cmd/picoagent/
```

Expected: 编译通过。

---

### Task 4: Agent 配置中注册 "agent" MCP 服务

**Files:**
- Modify: `internal/web/agent_config.go`

- [ ] **Step 1: 添加 "agent" MCP 服务器到配置响应**

```go
MCPServers: map[string]mcpServer{
  "browser":  {URL: "http://host:80/api/mcp/sse/browser"},
  "computer": {URL: "http://host:80/api/mcp/sse/computer"},
  "agent":    {URL: "http://host:80/api/mcp/sse/agent"},
},
```

- [ ] **Step 2: 编译验证**

```bash
go build ./cmd/picoaide/
```

Expected: 编译通过。

---

---

### Task 5: 数据库迁移 — mcp_servers + mcp_server_grants 表

**Files:**
- Create: `internal/auth/migrations/20260521_000000_mcp_servers.go`
- Test: (migrations are tested via integration test)

- [ ] **Step 1: 创建迁移文件**

```go
package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20260521000000",
    Desc:      "创建 mcp_servers 和 mcp_server_grants 表",
    Up: func(engine *xorm.Engine) error {
      _, err := engine.Exec(`CREATE TABLE IF NOT EXISTS mcp_servers (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT UNIQUE NOT NULL,
        transport TEXT NOT NULL DEFAULT 'stdio',
        command TEXT NOT NULL DEFAULT '',
        args TEXT NOT NULL DEFAULT '[]',
        url TEXT NOT NULL DEFAULT '',
        env TEXT NOT NULL DEFAULT '{}',
        enabled INTEGER NOT NULL DEFAULT 1,
        created_at DATETIME DEFAULT (datetime('now','localtime')),
        updated_at DATETIME DEFAULT (datetime('now','localtime'))
      )`)
      if err != nil {
        return err
      }
      _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS mcp_server_grants (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        server_id INTEGER NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
        grant_type TEXT NOT NULL,
        grant_value TEXT NOT NULL,
        UNIQUE(server_id, grant_type, grant_value)
      )`)
      return err
    },
  })
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./cmd/picoaide/
```

Expected: 编译通过。

---

### Task 6: MCP 服务器 CRUD 管理 API

**Files:**
- Create: `internal/web/admin_mcp.go`
- Test: `internal/web/admin_mcp_test.go`

- [ ] **Step 1: 创建 admin_mcp.go — 定义 handler 函数**

```go
package web

import (
  "encoding/json"
  "fmt"
  "net/http"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
)

// ============================================================
// MCP 服务器 CRUD 管理
// ============================================================

type mcpServerReq struct {
  Name      string `json:"name" binding:"required"`
  Transport string `json:"transport"`
  Command   string `json:"command"`
  Args      string `json:"args"`
  URL       string `json:"url"`
  Env       string `json:"env"`
  Enabled   bool   `json:"enabled"`
}

type mcpServerGrantReq struct {
  ServerID   int64  `json:"server_id" binding:"required"`
  GrantType  string `json:"grant_type" binding:"required"`
  GrantValue string `json:"grant_value" binding:"required"`
}

func (s *Server) handleAdminMCPServersList(c *gin.Context) {
  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }
  rows, err := engine.Query("SELECT * FROM mcp_servers ORDER BY id")
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": rows})
}

func (s *Server) handleAdminMCPServerCreate(c *gin.Context) {
  var req mcpServerReq
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
    return
  }
  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }
  _, err = engine.Exec(`INSERT INTO mcp_servers (name, transport, command, args, url, env, enabled)
    VALUES (?, ?, ?, ?, ?, ?, ?)`,
    req.Name, req.Transport, req.Command, req.Args, req.URL, req.Env, req.Enabled)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "创建失败: "+err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}

func (s *Server) handleAdminMCPServerUpdate(c *gin.Context) {
  id := c.Param("id")
  var req mcpServerReq
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
    return
  }
  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }
  _, err = engine.Exec(`UPDATE mcp_servers SET name=?, transport=?, command=?, args=?, url=?, env=?, enabled=? WHERE id=?`,
    req.Name, req.Transport, req.Command, req.Args, req.URL, req.Env, req.Enabled, id)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "更新失败: "+err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}

func (s *Server) handleAdminMCPServerDelete(c *gin.Context) {
  id := c.Param("id")
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
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}

func (s *Server) handleAdminMCPServerGrantsList(c *gin.Context) {
  serverID := c.Query("server_id")
  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }
  rows, err := engine.Query("SELECT * FROM mcp_server_grants WHERE server_id=?", serverID)
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true, "data": rows})
}

func (s *Server) handleAdminMCPServerGrantAdd(c *gin.Context) {
  var req mcpServerGrantReq
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
    return
  }
  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }
  _, err = engine.Exec(`INSERT INTO mcp_server_grants (server_id, grant_type, grant_value) VALUES (?, ?, ?)`,
    req.ServerID, req.GrantType, req.GrantValue)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "授权失败: "+err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}

func (s *Server) handleAdminMCPServerGrantRemove(c *gin.Context) {
  id := c.Param("id")
  engine, err := auth.GetEngine()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "数据库连接失败")
    return
  }
  _, err = engine.Exec("DELETE FROM mcp_server_grants WHERE id=?", id)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "删除授权失败: "+err.Error())
    return
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}
```

- [ ] **Step 2: 在 server.go 注册路由**

在 `admin` 路由组内新增：

```go
admin.GET("/mcp/servers", s.handleAdminMCPServersList)
admin.POST("/mcp/servers/create", s.handleAdminMCPServerCreate)
admin.POST("/mcp/servers/update/:id", s.handleAdminMCPServerUpdate)
admin.POST("/mcp/servers/delete/:id", s.handleAdminMCPServerDelete)
admin.GET("/mcp/servers/grants", s.handleAdminMCPServerGrantsList)
admin.POST("/mcp/servers/grants/add", s.handleAdminMCPServerGrantAdd)
admin.POST("/mcp/servers/grants/remove/:id", s.handleAdminMCPServerGrantRemove)
```

- [ ] **Step 3: 编译验证**

```bash
go build ./cmd/picoaide/
```

Expected: 编译通过。

---

### Task 7: MCP 子进程代理 — 第三方 MCP 管理

**Files:**
- Create: `internal/web/mcp_proxy.go`
- Test: `internal/web/mcp_proxy_test.go`

- [ ] **Step 1: 创建 MCPProxy — 管理第三方 MCP 子进程**

```go
package web

import (
  "bufio"
  "context"
  "encoding/json"
  "fmt"
  "log/slog"
  "os/exec"
  "sync"
)

// ============================================================
// MCPProxy 管理第三方 MCP 服务器的子进程生命周期
// ============================================================

// MCPProxy 单个第三方 MCP 服务器代理
type MCPProxy struct {
  Name      string
  ServerID  int64
  Transport string // stdio | streamable-http
  Command   string
  Args      []string
  URL       string
  Env       map[string]string

  mu      sync.Mutex
  cmd     *exec.Cmd
  stdin   *bufio.Writer
  scanner *bufio.Scanner
  tools   []ToolDef
  running bool
}

// MCPProxyManager 管理所有第三方 MCP 代理
type MCPProxyManager struct {
  mu      sync.Mutex
  proxies map[string]*MCPProxy // keyed by name
}

var (
  mcpProxyManager = &MCPProxyManager{proxies: make(map[string]*MCPProxy)}
  mcpProxyMu      sync.RWMutex
)

// Start 启动第三方 MCP 服务器的子进程
func (p *MCPProxy) Start(ctx context.Context) error {
  p.mu.Lock()
  defer p.mu.Unlock()

  if p.running {
    return nil
  }

  cmd := exec.CommandContext(ctx, p.Command, p.Args...)
  stdin, err := cmd.StdinPipe()
  if err != nil {
    return fmt.Errorf("创建 stdin pipe 失败: %w", err)
  }
  stdout, err := cmd.StdoutPipe()
  if err != nil {
    return fmt.Errorf("创建 stdout pipe 失败: %w", err)
  }
  cmd.Stderr = nil

  if err := cmd.Start(); err != nil {
    return fmt.Errorf("启动 MCP 子进程失败: %w", err)
  }

  p.cmd = cmd
  p.stdin = bufio.NewWriter(stdin)
  p.scanner = bufio.NewScanner(stdout)
  p.running = true

  go func() {
    cmd.Wait()
    p.mu.Lock()
    p.running = false
    p.mu.Unlock()
  }()

  return nil
}

// Stop 停止 MCP 子进程
func (p *MCPProxy) Stop() {
  p.mu.Lock()
  defer p.mu.Unlock()
  if p.cmd != nil && p.cmd.Process != nil {
    p.cmd.Process.Kill()
  }
  p.running = false
}

// ListTools 从 MCP 服务器获取工具列表
func (p *MCPProxy) ListTools(ctx context.Context) ([]ToolDef, error) {
  p.mu.Lock()
  defer p.mu.Unlock()

  if !p.running {
    return nil, fmt.Errorf("MCP 服务器 %s 未运行", p.Name)
  }

  // 发送 tools/list JSON-RPC
  req := map[string]interface{}{
    "jsonrpc": "2.0",
    "id":      1,
    "method":  "tools/list",
    "params":  map[string]interface{}{},
  }
  data, _ := json.Marshal(req)
  p.stdin.Write(data)
  p.stdin.Write([]byte("\n"))
  p.stdin.Flush()

  // 读取响应
  if p.scanner.Scan() {
    var resp struct {
      Result struct {
        Tools []struct {
          Name        string                 `json:"name"`
          Description string                 `json:"description"`
          InputSchema map[string]interface{} `json:"inputSchema"`
        } `json:"tools"`
      } `json:"result"`
    }
    if err := json.Unmarshal(p.scanner.Bytes(), &resp); err != nil {
      return nil, fmt.Errorf("解析 tools/list 响应失败: %w", err)
    }

    var tools []ToolDef
    for _, t := range resp.Result.Tools {
      tools = append(tools, ToolDef{
        Name:        "mcp_" + p.Name + "_" + t.Name,
        Description: t.Description,
        InputSchema: t.InputSchema,
      })
    }
    p.tools = tools
    return tools, nil
  }

  return nil, fmt.Errorf("MCP 服务器 %s 无响应", p.Name)
}

// CallTool 调用 MCP 服务器的工具
func (p *MCPProxy) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
  p.mu.Lock()
  defer p.mu.Unlock()

  if !p.running {
    return nil, fmt.Errorf("MCP 服务器 %s 未运行", p.Name)
  }

  // 还原工具名（去掉 mcp_<name>_ 前缀）
  originalName := toolName
  prefix := "mcp_" + p.Name + "_"
  if len(toolName) > len(prefix) && toolName[:len(prefix)] == prefix {
    originalName = toolName[len(prefix):]
  }

  req := map[string]interface{}{
    "jsonrpc": "2.0",
    "id":      2,
    "method":  "tools/call",
    "params": map[string]interface{}{
      "name":      originalName,
      "arguments": args,
    },
  }
  data, _ := json.Marshal(req)
  p.stdin.Write(data)
  p.stdin.Write([]byte("\n"))
  p.stdin.Flush()

  if p.scanner.Scan() {
    var resp struct {
      Result interface{} `json:"result"`
      Error  interface{} `json:"error"`
    }
    if err := json.Unmarshal(p.scanner.Bytes(), &resp); err != nil {
      return nil, fmt.Errorf("解析 tools/call 响应失败: %w", err)
    }
    if resp.Error != nil {
      return nil, fmt.Errorf("MCP 工具错误: %v", resp.Error)
    }
    return resp.Result, nil
  }

  return nil, fmt.Errorf("MCP 服务器 %s 无响应", p.Name)
}

// LoadMCPServers 从数据库加载已启用的 MCP 服务器并启动
func LoadMCPServers(ctx context.Context) error {
  engine, err := auth.GetEngine()
  if err != nil {
    return err
  }
  rows, err := engine.Query("SELECT * FROM mcp_servers WHERE enabled=1")
  if err != nil {
    return err
  }

  mcpProxyMu.Lock()
  defer mcpProxyMu.Unlock()

  for _, row := range rows {
    name := string(row["name"])
    proxy := &MCPProxy{
      Name:      name,
      Transport: string(row["transport"]),
      Command:   string(row["command"]),
    }

    // 解析 args JSON
    var args []string
    json.Unmarshal([]byte(row["args"]), &args)
    proxy.Args = args

    proxy.URL = string(row["url"])

    // 解析 env JSON
    var env map[string]string
    json.Unmarshal([]byte(row["env"]), &env)
    proxy.Env = env

    if err := proxy.Start(ctx); err != nil {
      slog.Error("启动 MCP 服务器失败", "name", name, "error", err)
      continue
    }

    // 获取工具列表
    tools, err := proxy.ListTools(ctx)
    if err != nil {
      slog.Error("获取 MCP 工具列表失败", "name", name, "error", err)
      continue
    }
    slog.Info("MCP 服务器已加载", "name", name, "tools", len(tools))

    mcpProxyManager.proxies[name] = proxy
  }

  return nil
}

// GetAuthorizedTools 获取用户可用的第三方 MCP 工具
func GetAuthorizedTools(username string) []ToolDef {
  mcpProxyMu.RLock()
  defer mcpProxyMu.RUnlock()

  var tools []ToolDef
  for name, proxy := range mcpProxyManager.proxies {
    if hasMCPGrant(name, username) {
      tools = append(tools, proxy.tools...)
    }
  }
  return tools
}

// GetUserGroups 获取用户所在的组列表
func GetUserGroups(username string) []string {
  engine, err := auth.GetEngine()
  if err != nil {
    return nil
  }
  rows, err := engine.Query(`
    SELECT g.name FROM groups g
    INNER JOIN user_groups ug ON g.id = ug.group_id
    WHERE ug.username = ?`, username)
  if err != nil {
    return nil
  }
  var groups []string
  for _, row := range rows {
    groups = append(groups, string(row["name"]))
  }
  return groups
}

// hasMCPGrant 检查用户是否有权访问指定 MCP 服务器
func hasMCPGrant(serverName, username string) bool {
  engine, err := auth.GetEngine()
  if err != nil {
    return false
  }

  // 检查用户直接授权
  row, err := engine.Query(`
    SELECT 1 FROM mcp_server_grants g
    INNER JOIN mcp_servers s ON g.server_id = s.id
    WHERE s.name = ? AND g.grant_type = 'user' AND g.grant_value = ?
    LIMIT 1`, serverName, username)
  if err == nil && len(row) > 0 {
    return true
  }

  // 检查组授权
  groups := GetUserGroups(username)
  for _, group := range groups {
    row, err := engine.Query(`
      SELECT 1 FROM mcp_server_grants g
      INNER JOIN mcp_servers s ON g.server_id = s.id
      WHERE s.name = ? AND g.grant_type = 'group' AND g.grant_value = ?
      LIMIT 1`, serverName, group)
    if err == nil && len(row) > 0 {
      return true
    }
  }

  return false
}
```

注意：需要在文件开头添加 import `"github.com/picoaide/picoaide/internal/auth"`。

- [ ] **Step 2: 编译验证**

```bash
go build ./cmd/picoaide/
```

Expected: 编译通过。

---

### Task 8: 统一端点集成授权

**Files:**
- Modify: `internal/web/mcp_service.go`

- [ ] **Step 1: 修改 "agent" 服务的 tools/list 和 tools/call 以聚合第三方 MCP**

在 `handleMCPSSEServicePost` 的 `tools/list` 分支中，针对 "agent" 服务需要动态注入第三方 MCP 工具和浏览器/计算机工具：

```go
case "tools/list":
  tools := make([]map[string]interface{}, 0)

  // 1. 服务端工具（picoaide_*）
  if info.Hub == nil {
    for _, t := range info.Tools {
      tools = append(tools, map[string]interface{}{
        "name":        t.Name,
        "description": t.Description,
        "inputSchema": t.InputSchema,
      })
    }

    // 2. 浏览器工具（browser_*）
    browserTools := browserToolDefs
    hub := browserSvc
    for _, t := range browserTools {
      if _, ok := hub.GetConnection(username); ok {
        tools = append(tools, map[string]interface{}{
          "name":        t.Name,
          "description": t.Description,
          "inputSchema": t.InputSchema,
        })
      }
    }

    // 3. 计算机工具（computer_*）
    computerTools := computerToolDefs
    hub2 := computerSvc
    for _, t := range computerTools {
      if _, ok := hub2.GetConnection(username); ok {
        tools = append(tools, map[string]interface{}{
          "name":        t.Name,
          "description": t.Description,
          "inputSchema": t.InputSchema,
        })
      }
    }

    // 4. 第三方 MCP 工具（按用户授权过滤）
    mcpTools := GetAuthorizedTools(username)
    for _, t := range mcpTools {
      tools = append(tools, map[string]interface{}{
        "name":        t.Name,
        "description": t.Description,
        "inputSchema": t.InputSchema,
      })
    }

  } else {
    // 原有逻辑（browser/computer 服务）
    for i, t := range info.Tools {
      tools[i] = map[string]interface{}{
        "name":        t.Name,
        "description": t.Description,
        "inputSchema": t.InputSchema,
      }
    }
  }

  writeMCPResult(c.Writer, req.ID, map[string]interface{}{
    "tools": tools,
  })
```

- [ ] **Step 2: 修改 tools/call 以支持第三方 MCP 调用**

在 `handleMCPToolCall` 中，`info.Hub == nil` 分支里，当 handler 不在 `picoaideHandlers` 时，尝试第三方 MCP：

```go
// 服务端 handler 优先
if info.Hub == nil {
  if handler, ok := picoaideHandlers[p.Name]; ok {
    handler(s, c, p.Arguments, username)
    return
  }
  // 第三方 MCP 工具
  for _, proxy := range mcpProxyManager.proxies {
    prefix := "mcp_" + proxy.Name + "_"
    if len(p.Name) > len(prefix) && p.Name[:len(prefix)] == prefix {
      result, err := proxy.CallTool(c.Request.Context(), p.Name, p.Arguments)
      if err != nil {
        writeMCPResult(c.Writer, id, map[string]interface{}{
          "content": []map[string]interface{}{
            {"type": "text", "text": "执行失败: " + err.Error()},
          },
          "isError": true,
        })
        return
      }
      writeMCPResult(c.Writer, id, formatMCPResult(result))
      return
    }
  }
  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": "未知工具: " + p.Name},
    },
    "isError": true,
  })
  return
}
```

- [ ] **Step 3: 在服务启动时加载 MCP 服务器**

在 `mcp_service.go` 或 `server.go` 中，服务启动时调用：

在 `Server.Start()` 中：

```go
// 启动时加载第三方 MCP 服务器
if err := LoadMCPServers(context.Background()); err != nil {
  slog.Error("加载 MCP 服务器失败", "error", err)
}
```

需 import `"context"` 和 `"log/slog"`。

- [ ] **Step 4: 编译验证**

```bash
go build ./cmd/picoaide/
```

