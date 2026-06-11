package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "log/slog"
  "os"
  "path/filepath"
  "strings"
)

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
