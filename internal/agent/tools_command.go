package agent

import (
  "bytes"
  "context"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "time"
)

func saveCmdOutput(id, content string) {
  os.WriteFile(filepath.Join(sandboxWorkspace, ".cmd_output_"+id+".txt"), []byte(content), 0644)
}

// ============================================================
// Shell 命令执行工具（沙箱内可执行任意命令）
// 参考 OpenCode 模式：输出过长时保存到文件，告知 AI 用 read_file 查看
// ============================================================

type commandParams struct {
  Command string `json:"command" dc:"要执行的 shell 命令"`
}

func NewCommandTool(timeout time.Duration) ToolExecutor {
  return NewTypedTool[commandParams]("command",
    "在沙箱中执行 shell 命令（如 ls、cat、pwd、find 等），返回命令输出。工作目录为 /workspace。当输出超过 2000 字符时会被截断，完整输出保存到 /workspace/.cmd_output.txt，可用 read_file 工具读取。",
    func(ctx context.Context, p commandParams) *ToolResult {
      if p.Command == "" {
        return &ToolResult{Success: false, Data: "命令不能为空"}
      }

      cmdTimeout := timeout
      if cmdTimeout <= 0 {
        cmdTimeout = 120 * time.Second
      }
      cmdCtx, cmdCancel := context.WithTimeout(ctx, cmdTimeout)
      defer cmdCancel()

      var stdout, stderr bytes.Buffer
      cmd := exec.CommandContext(cmdCtx, "sh", "-c", p.Command)
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
        return &ToolResult{Success: false, Data: msg}
      }

      output := strings.TrimSpace(stdout.String())
      if output == "" {
        output = strings.TrimSpace(stderr.String())
      }
      if output == "" {
        output = "(无输出)"
      }

      fullOutput := output
      callID := fmt.Sprintf("%d", time.Now().UnixNano())
      saveCmdOutput(callID, fullOutput)

      const maxLen = 2000
      if len(output) > maxLen {
        output = output[:maxLen] + fmt.Sprintf("\n... (输出过长，完整内容已保存到 /workspace/.cmd_output_%s.txt，可用 read_file 工具读取)", callID)
      }

      return &ToolResult{Success: true, Data: output}
    })
}
