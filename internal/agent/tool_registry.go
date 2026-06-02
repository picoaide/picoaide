package agent

import (
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "log/slog"
  "os"
  "os/exec"
  "path/filepath"
  "sort"
  "strings"
  "sync"
  "time"
)

var sandboxWorkspace = "/workspace"

// safePath 验证路径在沙箱工作区内，防止路径遍历攻击
func safePath(path string) string {
  if path == "" {
    return ""
  }
  cleaned := filepath.Clean(path)
  // 绝对路径必须在 /workspace 下
  if filepath.IsAbs(cleaned) {
    if !strings.HasPrefix(cleaned, sandboxWorkspace+"/") && cleaned != sandboxWorkspace {
      return ""
    }
    return cleaned
  }
  // 相对路径拼上 workspace
  return filepath.Join(sandboxWorkspace, cleaned)
}

// ============================================================
// 工具注册和执行
// ============================================================

type ToolResult struct {
  Success bool
  Data    string
}

type ToolExecutor interface {
  Name() string
  Description() string
  Schema() map[string]interface{}
  Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error)
}

type ToolEntry struct {
  executor   ToolExecutor
  serverName string // "" 表示基础工具；非空表示属于某个 MCP 服务器
}

type ToolRegistry struct {
  mu      sync.RWMutex
  entries map[string]*ToolEntry
}

func NewToolRegistry() *ToolRegistry {
  return &ToolRegistry{
    entries: make(map[string]*ToolEntry),
  }
}

func (r *ToolRegistry) Register(executor ToolExecutor) {
  r.mu.Lock()
  defer r.mu.Unlock()
  r.entries[executor.Name()] = &ToolEntry{executor: executor}
}

// SetServer 标记某工具属于指定的 MCP 服务器
func (r *ToolRegistry) SetServer(toolName, serverName string) {
  r.mu.Lock()
  defer r.mu.Unlock()
  if e, ok := r.entries[toolName]; ok {
    e.serverName = serverName
  }
}

// Resolve 返回当前可用的工具定义列表
func (r *ToolRegistry) Resolve(ctx context.Context) []ToolDef {
  r.mu.RLock()
  defer r.mu.RUnlock()

  var defs []ToolDef
  for _, entry := range r.entries {
    defs = append(defs, ToolDef{
      Name:        entry.executor.Name(),
      Description: entry.executor.Description(),
      InputSchema: entry.executor.Schema(),
    })
  }
  return defs
}

// Execute 执行指定工具
func (r *ToolRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (result *ToolResult, err error) {
  // 全局 panic recovery，防止任何工具 panic 导致整个 picoagent 崩溃
  defer func() {
    if r := recover(); r != nil {
      slog.Error("tool.panic_recovered", "tool", name, "panic", r)
      result = &ToolResult{Success: false, Data: fmt.Sprintf("工具执行异常: %v", r)}
      err = nil
    }
  }()

  r.mu.RLock()
  entry, ok := r.entries[name]
  r.mu.RUnlock()

  if !ok {
    slog.Debug("tool.execute_not_found", "tool", name)
    return nil, fmt.Errorf("工具未找到: %s", name)
  }

  start := time.Now()
  result, err = entry.executor.Execute(ctx, args)
  // 工具 Execute 内部若 panic 且未设置返回值，result 可能为 nil
  if result == nil && err == nil {
    result = &ToolResult{Success: false, Data: fmt.Sprintf("工具 %s 执行返回空结果（可能发生 panic 已被捕获）", name)}
  }
  duration := time.Since(start)

  slog.Debug("tool.execute_complete",
    "tool", name,
    "success", result != nil && result.Success,
    "duration_ms", duration.Milliseconds(),
  )

  return result, err
}

// LookupByServer 查找指定 MCP 服务器上的工具，返回其 Executor
func (r *ToolRegistry) LookupByServer(serverName, toolName string) ToolExecutor {
  r.mu.RLock()
  defer r.mu.RUnlock()
  for _, entry := range r.entries {
    if entry.serverName == serverName && entry.executor.Name() == toolName {
      return entry.executor
    }
  }
  return nil
}

// ListBasic 返回所有基础工具（不属于任何 MCP 服务器的工具）
func (r *ToolRegistry) ListBasic() []ToolDef {
  r.mu.RLock()
  defer r.mu.RUnlock()

  var defs []ToolDef
  for _, entry := range r.entries {
    if entry.serverName == "" {
      defs = append(defs, ToolDef{
        Name:        entry.executor.Name(),
        Description: entry.executor.Description(),
        InputSchema: entry.executor.Schema(),
      })
    }
  }
  return defs
}

// ListByServer 返回指定 MCP 服务器的所有工具
func (r *ToolRegistry) ListByServer(serverName string) []ToolDef {
  r.mu.RLock()
  defer r.mu.RUnlock()

  var defs []ToolDef
  for _, entry := range r.entries {
    if entry.serverName == serverName {
      defs = append(defs, ToolDef{
        Name:        entry.executor.Name(),
        Description: entry.executor.Description(),
        InputSchema: entry.executor.Schema(),
      })
    }
  }
  return defs
}

// ListServers 返回所有已注册的 MCP 服务器名（去重）
func (r *ToolRegistry) ListServers() []string {
  r.mu.RLock()
  defer r.mu.RUnlock()

  seen := make(map[string]bool)
  var servers []string
  for _, entry := range r.entries {
    if entry.serverName != "" && !seen[entry.serverName] {
      seen[entry.serverName] = true
      servers = append(servers, entry.serverName)
    }
  }
  return servers
}

// ============================================================
// Shell 命令执行工具（沙箱内可执行任意命令）
// 参考 OpenCode 模式：输出过长时保存到文件，告知 AI 用 read_file 查看
// ============================================================

type CommandTool struct {
  Timeout time.Duration // 命令超时，0 表示默认 120 秒
}

func (t *CommandTool) Name() string { return "command" }

func (t *CommandTool) Description() string {
  return "在沙箱中执行 shell 命令（如 ls、cat、pwd、find 等），返回命令输出。工作目录为 /workspace。当输出超过 2000 字符时会被截断，完整输出保存到 /workspace/.cmd_output.txt，可用 read_file 工具读取。"
}

func (t *CommandTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "command": map[string]interface{}{
        "type":        "string",
        "description": "要执行的 shell 命令",
      },
    },
    "required": []string{"command"},
  }
}

func (t *CommandTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Command string `json:"command"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }
  if params.Command == "" {
    return &ToolResult{Success: false, Data: "命令不能为空"}, nil
  }

  timeout := t.Timeout
  if timeout <= 0 {
    timeout = 120 * time.Second
  }
  ctx, cancel := context.WithTimeout(ctx, timeout)
  defer cancel()

  var stdout, stderr bytes.Buffer
  cmd := exec.CommandContext(ctx, "sh", "-c", params.Command)
  cmd.Dir = sandboxWorkspace
  cmd.Stdout = &stdout
  cmd.Stderr = &stderr

  if err := cmd.Run(); err != nil {
    output := strings.TrimSpace(stdout.String())
    errOutput := strings.TrimSpace(stderr.String())
    msg := fmt.Sprintf("命令执行失败: %v", err)
    if errOutput != "" {
      msg += "\n" + errOutput
    }
    if output != "" {
      msg += "\n" + output
    }
    return &ToolResult{Success: false, Data: msg}, nil
  }

  output := strings.TrimSpace(stdout.String())
  if output == "" {
    output = strings.TrimSpace(stderr.String())
  }
  if output == "" {
    output = "(无输出)"
  }

  // 保存完整输出到文件，供 read_file 读取（追加时间戳避免并发覆盖）
  fullOutput := output
  saveCmdOutput(fullOutput)

  // 截断过长输出
  const maxLen = 2000
  if len(output) > maxLen {
    output = output[:maxLen] + "\n... (输出过长，完整内容已保存到 /workspace/.cmd_output.txt，可用 read_file 工具读取)"
  }

  return &ToolResult{Success: true, Data: output}, nil
}

// saveCmdOutput 将命令完整输出写入沙箱文件系统
func saveCmdOutput(content string) {
  const cmdOutputPath = "/workspace/.cmd_output.txt"
  const maxOutputSize = 1 << 20 // 1MB 限制，避免撑爆 overlay tmpfs
  if len(content) > maxOutputSize {
    content = content[:maxOutputSize] + "\n... (输出超出 1MB 限制，已截断)"
  }
  os.WriteFile(cmdOutputPath, []byte(content), 0644)
}

// ============================================================
// read_file — 读取文件内容（无截断，适合小文件）
// ============================================================

type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
  return "读取指定文件的内容。适合查看命令输出保存的文件（.cmd_output.txt）或工作区中的代码文件。文件过大时自动截断。"
}

func (t *ReadFileTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "path": map[string]interface{}{
        "type":        "string",
        "description": "文件路径（相对 /workspace 或绝对路径）",
      },
    },
    "required": []string{"path"},
  }
}

func (t *ReadFileTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Path string `json:"path"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }
  if params.Path == "" {
    return &ToolResult{Success: false, Data: "路径不能为空"}, nil
  }

  cleanPath := safePath(params.Path)
  if cleanPath == "" {
    return &ToolResult{Success: false, Data: "路径不合法，必须在工作区内"}, nil
  }

  data, err := os.ReadFile(cleanPath)
  if err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("读取失败: %v", err)}, nil
  }

  content := strings.TrimSpace(string(data))
  if content == "" {
    return &ToolResult{Success: true, Data: "(文件为空)"}, nil
  }

  const maxReadLen = 8000
  if len(content) > maxReadLen {
    content = content[:maxReadLen] + "\n... (文件过长，仅显示前 8000 字符)"
  }

  return &ToolResult{Success: true, Data: content}, nil
}

// ============================================================
// grep — 在文件中搜索文本（返回匹配行）
// ============================================================

type GrepTool struct{}

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Description() string {
  return "在文件中搜索文本，返回匹配的行。适合在大型输出中查找关键信息。"
}

func (t *GrepTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "pattern": map[string]interface{}{
        "type":        "string",
        "description": "搜索模式（支持 grep 正则语法）",
      },
      "path": map[string]interface{}{
        "type":        "string",
        "description": "文件路径",
      },
    },
    "required": []string{"pattern", "path"},
  }
}

func (t *GrepTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Pattern string `json:"pattern"`
    Path    string `json:"path"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }

  ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
  defer cancel()

  cmd := exec.CommandContext(ctx, "grep", "-n", "-E", params.Pattern, params.Path)
  var stdout, stderr bytes.Buffer
  cmd.Stdout = &stdout
  cmd.Stderr = &stderr

  if err := cmd.Run(); err != nil {
    if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
      return &ToolResult{Success: true, Data: "(无匹配)"}, nil
    }
    errOutput := strings.TrimSpace(stderr.String())
    msg := "grep 执行失败"
    if errOutput != "" {
      msg += ": " + errOutput
    }
    return &ToolResult{Success: false, Data: msg}, nil
  }

  output := strings.TrimSpace(stdout.String())
  if output == "" {
    return &ToolResult{Success: true, Data: "(无匹配)"}, nil
  }

  const maxGrepLen = 4000
  if len(output) > maxGrepLen {
    output = output[:maxGrepLen] + "\n... (匹配行过多，仅显示前 4000 字符)"
  }

  return &ToolResult{Success: true, Data: output}, nil
}

// ============================================================
// write_file — 写入文件内容
// ============================================================

type WriteFileTool struct{}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
  return "写入文件内容。如果文件已存在且未设置 overwrite=true，则拒绝写入。"
}

func (t *WriteFileTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "path": map[string]interface{}{
        "type":        "string",
        "description": "文件路径（绝对路径或相对 /workspace 的路径）",
      },
      "content": map[string]interface{}{
        "type":        "string",
        "description": "写入的内容",
      },
      "overwrite": map[string]interface{}{
        "type":        "boolean",
        "description": "是否覆盖已存在的文件，默认为 false",
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

  cleanPath := safePath(params.Path)
  if cleanPath == "" {
    return &ToolResult{Success: false, Data: "路径不合法，必须在工作区内"}, nil
  }

  if !params.Overwrite {
    if _, err := os.Stat(cleanPath); err == nil {
      return &ToolResult{Success: false, Data: fmt.Sprintf("文件已存在: %s，设置 overwrite=true 以覆盖", params.Path)}, nil
    }
  }

  dir := filepath.Dir(cleanPath)
  if err := os.MkdirAll(dir, 0755); err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("创建目录失败: %v", err)}, nil
  }

  // 限制写入大小，防止恶意写入撑爆 overlay tmpfs
  const maxWriteSize = 10 << 20 // 10MB
  if len(params.Content) > maxWriteSize {
    return &ToolResult{Success: false, Data: fmt.Sprintf("文件过大 (超过 %dMB)", maxWriteSize/(1<<20))}, nil
  }

  if err := os.WriteFile(cleanPath, []byte(params.Content), 0644); err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("写入失败: %v", err)}, nil
  }

  return &ToolResult{Success: true, Data: "写入成功: " + params.Path}, nil
}

// ============================================================
// edit_file — 精确替换文件中的字符串
// ============================================================

type EditFileTool struct{}

func (t *EditFileTool) Name() string { return "edit_file" }

func (t *EditFileTool) Description() string {
  return "精确替换文件中的字符串。old_text 必须在文件中唯一匹配，否则拒绝操作。"
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
        "description": "要替换的旧文本",
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
  if params.Path == "" {
    return &ToolResult{Success: false, Data: "路径不能为空"}, nil
  }

  cleanPath := safePath(params.Path)
  if cleanPath == "" {
    return &ToolResult{Success: false, Data: "路径不合法，必须在工作区内"}, nil
  }

  data, err := os.ReadFile(cleanPath)
  if err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("读取失败: %v", err)}, nil
  }

  content := string(data)
  count := strings.Count(content, params.OldText)

  if count == 0 {
    return &ToolResult{Success: false, Data: fmt.Sprintf("未找到匹配文本: %s", params.OldText)}, nil
  }
  if count > 1 {
    return &ToolResult{Success: false, Data: fmt.Sprintf("找到 %d 个匹配，请提供更精确的 old_text 以确保唯一匹配", count)}, nil
  }

  newContent := strings.Replace(content, params.OldText, params.NewText, 1)
  if err := os.WriteFile(cleanPath, []byte(newContent), 0644); err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("写入失败: %v", err)}, nil
  }

  return &ToolResult{Success: true, Data: "替换成功"}, nil
}

// ============================================================
// append_file — 追加内容到文件末尾
// ============================================================

type AppendFileTool struct{}

func (t *AppendFileTool) Name() string { return "append_file" }

func (t *AppendFileTool) Description() string {
  return "追加内容到文件末尾。文件不存在时会自动创建。"
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
  if params.Path == "" {
    return &ToolResult{Success: false, Data: "路径不能为空"}, nil
  }

  cleanPath := safePath(params.Path)
  if cleanPath == "" {
    return &ToolResult{Success: false, Data: "路径不合法，必须在工作区内"}, nil
  }

  dir := filepath.Dir(cleanPath)
  if err := os.MkdirAll(dir, 0755); err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("创建目录失败: %v", err)}, nil
  }

  f, err := os.OpenFile(cleanPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
  if err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("打开文件失败: %v", err)}, nil
  }
  defer func() {
    if cerr := f.Close(); cerr != nil {
      slog.Error("追加文件关闭失败", "path", cleanPath, "error", cerr)
    }
  }()

  if _, err := f.WriteString(params.Content); err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("写入失败: %v", err)}, nil
  }

  return &ToolResult{Success: true, Data: "追加成功"}, nil
}

// ============================================================
// list_dir — 列出目录内容
// ============================================================

type ListDirTool struct{}

func (t *ListDirTool) Name() string { return "list_dir" }

func (t *ListDirTool) Description() string {
  return "列出目录中的文件和子目录。默认列出 /workspace 目录。"
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
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }

  dir := params.Path
  if dir == "" {
    dir = sandboxWorkspace
  }

  entries, err := os.ReadDir(dir)
  if err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("读取目录失败: %v", err)}, nil
  }

  sort.Slice(entries, func(i, j int) bool {
    return entries[i].Name() < entries[j].Name()
  })

  var lines []string
  for _, entry := range entries {
    info, err := entry.Info()
    if err != nil {
      continue
    }
    if entry.IsDir() {
      lines = append(lines, fmt.Sprintf("DIR: %s", entry.Name()))
    } else {
      lines = append(lines, fmt.Sprintf("FILE: %s (%d bytes)", entry.Name(), info.Size()))
    }
  }

  if len(lines) == 0 {
    return &ToolResult{Success: true, Data: "(目录为空)"}, nil
  }

  return &ToolResult{Success: true, Data: strings.Join(lines, "\n")}, nil
}

// ============================================================
// glob — 按 glob 模式搜索文件
// ============================================================

type GlobTool struct{}

func (t *GlobTool) Name() string { return "glob" }

func (t *GlobTool) Description() string {
  return "按 glob 模式搜索文件。返回匹配的文件路径列表，最多 100 条。"
}

func (t *GlobTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "pattern": map[string]interface{}{
        "type":        "string",
        "description": "glob 搜索模式（如 *.txt、**/*.go）",
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
  if params.Pattern == "" {
    return &ToolResult{Success: false, Data: "模式不能为空"}, nil
  }

  p := params.Pattern
  if !filepath.IsAbs(p) && params.Root == "" {
    params.Root = sandboxWorkspace
  }
  if !filepath.IsAbs(p) && params.Root != "" {
    p = filepath.Join(params.Root, p)
  }

  matches, err := filepath.Glob(p)
  if err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("glob 搜索失败: %v", err)}, nil
  }

  if len(matches) == 0 {
    return &ToolResult{Success: true, Data: "(无匹配)"}, nil
  }

  const maxGlobResults = 100
  if len(matches) > maxGlobResults {
    matches = matches[:maxGlobResults]
  }

  sort.Strings(matches)
  return &ToolResult{Success: true, Data: strings.Join(matches, "\n")}, nil
}

// ============================================================
// delete_file — 删除文件或目录
// ============================================================

type DeleteFileTool struct{}

func (t *DeleteFileTool) Name() string { return "delete_file" }

func (t *DeleteFileTool) Description() string {
  return "删除文件或目录。目录会被递归删除。文件不存在时不会报错。"
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
  if params.Path == "" {
    return &ToolResult{Success: false, Data: "路径不能为空"}, nil
  }

  cleanPath := safePath(params.Path)
  if cleanPath == "" {
    return &ToolResult{Success: false, Data: "路径不合法，必须在工作区内"}, nil
  }

  if err := os.RemoveAll(cleanPath); err != nil {
    return &ToolResult{Success: false, Data: fmt.Sprintf("删除失败: %v", err)}, nil
  }

  return &ToolResult{Success: true, Data: "删除成功"}, nil
}
