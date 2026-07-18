package agent

import (
  "context"
  "fmt"
  "log/slog"
  "os"
  "path/filepath"
  "strings"
)

// ============================================================
// read_file — 读取文件内容（无截断，适合小文件）
// ============================================================

type readFileParams struct {
  Path string `json:"path" dc:"文件路径（相对 /workspace 或绝对路径）"`
}

func NewReadFileTool() ToolExecutor {
  return NewTypedTool[readFileParams]("read_file",
    "读取指定文件的内容。适合查看命令输出保存的文件（.cmd_output.txt）或工作区中的代码文件。文件过大时自动截断。",
    func(ctx context.Context, p readFileParams) *ToolResult {
      if p.Path == "" {
        return &ToolResult{Success: false, Data: "路径不能为空"}
      }

      cleanPath := safePath(p.Path)
      if cleanPath == "" {
        return &ToolResult{Success: false, Data: "路径不合法，必须在工作区内"}
      }

      data, err := os.ReadFile(cleanPath)
      if err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("读取失败: %v", err)}
      }

      content := strings.TrimSpace(string(data))
      if content == "" {
        return &ToolResult{Success: true, Data: "(文件为空)"}
      }

      const maxReadLen = 8000
      if len(content) > maxReadLen {
        content = content[:maxReadLen] + "\n... (文件过长，仅显示前 8000 字符)"
      }

      return &ToolResult{Success: true, Data: content}
    })
}

// ============================================================
// write_file — 写入文件内容
// ============================================================

type writeFileParams struct {
  Path      string `json:"path" dc:"文件路径（绝对路径或相对 /workspace 的路径）"`
  Content   string `json:"content" dc:"写入的内容"`
  Overwrite bool   `json:"overwrite,omitempty" dc:"是否覆盖已存在的文件，默认为 false"`
}

func NewWriteFileTool() ToolExecutor {
  return NewTypedTool[writeFileParams]("write_file",
    "写入文件内容。如果文件已存在且未设置 overwrite=true，则拒绝写入。",
    func(ctx context.Context, p writeFileParams) *ToolResult {
      if p.Path == "" {
        return &ToolResult{Success: false, Data: "路径不能为空"}
      }

      cleanPath := safePath(p.Path)
      if cleanPath == "" {
        return &ToolResult{Success: false, Data: "路径不合法，必须在工作区内"}
      }

      if !p.Overwrite {
        if _, err := os.Stat(cleanPath); err == nil {
          return &ToolResult{Success: false, Data: fmt.Sprintf("文件已存在: %s，设置 overwrite=true 以覆盖", p.Path)}
        }
      }

      dir := filepath.Dir(cleanPath)
      if err := os.MkdirAll(dir, 0755); err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("创建目录失败: %v", err)}
      }

      const maxWriteSize = 10 << 20
      if len(p.Content) > maxWriteSize {
        return &ToolResult{Success: false, Data: fmt.Sprintf("文件过大 (超过 %dMB)", maxWriteSize/(1<<20))}
      }

      if err := os.WriteFile(cleanPath, []byte(p.Content), 0644); err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("写入失败: %v", err)}
      }

      return &ToolResult{Success: true, Data: "写入成功: " + p.Path}
    })
}

// ============================================================
// edit_file — 精确替换文件中的字符串
// ============================================================

type editFileParams struct {
  Path    string `json:"path" dc:"文件路径"`
  OldText string `json:"old_text" dc:"要替换的旧文本"`
  NewText string `json:"new_text" dc:"替换后的新文本"`
}

func NewEditFileTool() ToolExecutor {
  return NewTypedTool[editFileParams]("edit_file",
    "精确替换文件中的字符串。old_text 必须在文件中唯一匹配，否则拒绝操作。",
    func(ctx context.Context, p editFileParams) *ToolResult {
      if p.Path == "" {
        return &ToolResult{Success: false, Data: "路径不能为空"}
      }

      cleanPath := safePath(p.Path)
      if cleanPath == "" {
        return &ToolResult{Success: false, Data: "路径不合法，必须在工作区内"}
      }

      data, err := os.ReadFile(cleanPath)
      if err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("读取失败: %v", err)}
      }

      content := string(data)
      count := strings.Count(content, p.OldText)

      if count == 0 {
        return &ToolResult{Success: false, Data: fmt.Sprintf("未找到匹配文本: %s", p.OldText)}
      }
      if count > 1 {
        return &ToolResult{Success: false, Data: fmt.Sprintf("找到 %d 个匹配，请提供更精确的 old_text 以确保唯一匹配", count)}
      }

      newContent := strings.Replace(content, p.OldText, p.NewText, 1)
      if err := os.WriteFile(cleanPath, []byte(newContent), 0644); err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("写入失败: %v", err)}
      }

      return &ToolResult{Success: true, Data: "替换成功"}
    })
}

// ============================================================
// append_file — 追加内容到文件末尾
// ============================================================

type appendFileParams struct {
  Path    string `json:"path" dc:"文件路径"`
  Content string `json:"content" dc:"要追加的内容"`
}

func NewAppendFileTool() ToolExecutor {
  return NewTypedTool[appendFileParams]("append_file",
    "追加内容到文件末尾。文件不存在时会自动创建。",
    func(ctx context.Context, p appendFileParams) *ToolResult {
      if p.Path == "" {
        return &ToolResult{Success: false, Data: "路径不能为空"}
      }

      cleanPath := safePath(p.Path)
      if cleanPath == "" {
        return &ToolResult{Success: false, Data: "路径不合法，必须在工作区内"}
      }

      dir := filepath.Dir(cleanPath)
      if err := os.MkdirAll(dir, 0755); err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("创建目录失败: %v", err)}
      }

      f, err := os.OpenFile(cleanPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
      if err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("打开文件失败: %v", err)}
      }
      defer func() {
        if cerr := f.Close(); cerr != nil {
          slog.Error("追加文件关闭失败", "path", cleanPath, "error", cerr)
        }
      }()

      if _, err := f.WriteString(p.Content); err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("写入失败: %v", err)}
      }

      return &ToolResult{Success: true, Data: "追加成功"}
    })
}

// ============================================================
// delete_file — 删除文件或目录
// ============================================================

type deleteFileParams struct {
  Path string `json:"path" dc:"要删除的文件或目录路径"`
}

func NewDeleteFileTool() ToolExecutor {
  return NewTypedTool[deleteFileParams]("delete_file",
    "删除文件或目录。目录会被递归删除。文件不存在时不会报错。",
    func(ctx context.Context, p deleteFileParams) *ToolResult {
      if p.Path == "" {
        return &ToolResult{Success: false, Data: "路径不能为空"}
      }

      cleanPath := safePath(p.Path)
      if cleanPath == "" {
        return &ToolResult{Success: false, Data: "路径不合法，必须在工作区内"}
      }

      if err := os.RemoveAll(cleanPath); err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("删除失败: %v", err)}
      }

      return &ToolResult{Success: true, Data: "删除成功"}
    })
}
