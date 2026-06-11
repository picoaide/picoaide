package agent

import (
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "sort"
  "strings"
  "time"
)

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

  path := safePath(params.Path)
  if path == "" {
    return &ToolResult{Success: false, Data: "路径不在工作区内"}, nil
  }

  ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
  defer cancel()

  cmd := exec.CommandContext(ctx, "grep", "-n", "-E", params.Pattern, path)
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
  } else {
    dir = safePath(dir)
    if dir == "" {
      return &ToolResult{Success: false, Data: "路径不在工作区内"}, nil
    }
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
  if filepath.IsAbs(p) {
    p = safePath(p)
    if p == "" {
      return &ToolResult{Success: false, Data: "路径不在工作区内"}, nil
    }
  } else {
    if params.Root == "" {
      params.Root = sandboxWorkspace
    } else {
      params.Root = safePath(params.Root)
      if params.Root == "" {
        return &ToolResult{Success: false, Data: "路径不在工作区内"}, nil
      }
    }
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
