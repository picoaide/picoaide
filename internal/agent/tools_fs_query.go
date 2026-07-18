package agent

import (
  "bytes"
  "context"
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

type grepParams struct {
  Pattern string `json:"pattern" dc:"搜索模式（支持 grep 正则语法）"`
  Path    string `json:"path" dc:"文件路径"`
}

func NewGrepTool() ToolExecutor {
  return NewTypedTool[grepParams]("grep",
    "在文件中搜索文本，返回匹配的行。适合在大型输出中查找关键信息。",
    func(ctx context.Context, p grepParams) *ToolResult {
      path := safePath(p.Path)
      if path == "" {
        return &ToolResult{Success: false, Data: "路径不在工作区内"}
      }

      ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
      defer cancel()

      cmd := exec.CommandContext(ctx, "grep", "-n", "-E", p.Pattern, path)
      var stdout, stderr bytes.Buffer
      cmd.Stdout = &stdout
      cmd.Stderr = &stderr

      if err := cmd.Run(); err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
          return &ToolResult{Success: true, Data: "(无匹配)"}
        }
        errOutput := strings.TrimSpace(stderr.String())
        msg := "grep 执行失败"
        if errOutput != "" {
          msg += ": " + errOutput
        }
        return &ToolResult{Success: false, Data: msg}
      }

      output := strings.TrimSpace(stdout.String())
      if output == "" {
        return &ToolResult{Success: true, Data: "(无匹配)"}
      }

      const maxGrepLen = 4000
      if len(output) > maxGrepLen {
        output = output[:maxGrepLen] + "\n... (匹配行过多，仅显示前 4000 字符)"
      }

      return &ToolResult{Success: true, Data: output}
    })
}

// ============================================================
// list_dir — 列出目录内容
// ============================================================

type listDirParams struct {
  Path string `json:"path,omitempty" dc:"目录路径，默认为 /workspace"`
}

func NewListDirTool() ToolExecutor {
  return NewTypedTool[listDirParams]("list_dir",
    "列出目录中的文件和子目录。默认列出 /workspace 目录。",
    func(ctx context.Context, p listDirParams) *ToolResult {
      dir := p.Path
      if dir == "" {
        dir = sandboxWorkspace
      } else {
        dir = safePath(dir)
        if dir == "" {
          return &ToolResult{Success: false, Data: "路径不在工作区内"}
        }
      }

      entries, err := os.ReadDir(dir)
      if err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("读取目录失败: %v", err)}
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
        return &ToolResult{Success: true, Data: "(目录为空)"}
      }

      return &ToolResult{Success: true, Data: strings.Join(lines, "\n")}
    })
}

// ============================================================
// glob — 按 glob 模式搜索文件
// ============================================================

type globParams struct {
  Pattern string `json:"pattern" dc:"glob 搜索模式（如 *.txt、**/*.go）"`
  Root    string `json:"root,omitempty" dc:"搜索根目录，默认为 /workspace"`
}

func NewGlobTool() ToolExecutor {
  return NewTypedTool[globParams]("glob",
    "按 glob 模式搜索文件。返回匹配的文件路径列表，最多 100 条。",
    func(ctx context.Context, p globParams) *ToolResult {
      if p.Pattern == "" {
        return &ToolResult{Success: false, Data: "模式不能为空"}
      }

      pat := p.Pattern
      if filepath.IsAbs(pat) {
        pat = safePath(pat)
        if pat == "" {
          return &ToolResult{Success: false, Data: "路径不在工作区内"}
        }
      } else {
        root := p.Root
        if root == "" {
          root = sandboxWorkspace
        } else {
          root = safePath(root)
          if root == "" {
            return &ToolResult{Success: false, Data: "路径不在工作区内"}
          }
        }
        pat = filepath.Join(root, pat)
      }

      matches, err := filepath.Glob(pat)
      if err != nil {
        return &ToolResult{Success: false, Data: fmt.Sprintf("glob 搜索失败: %v", err)}
      }

      if len(matches) == 0 {
        return &ToolResult{Success: true, Data: "(无匹配)"}
      }

      const maxGlobResults = 100
      if len(matches) > maxGlobResults {
        matches = matches[:maxGlobResults]
      }

      sort.Strings(matches)
      return &ToolResult{Success: true, Data: strings.Join(matches, "\n")}
    })
}
