package agent

import (
  "bytes"
  "context"
  "encoding/json"
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

  // 保存完整输出到文件，供 read_file 读取（使用 ID 避免并发覆盖）
  fullOutput := output
  callID := fmt.Sprintf("%d", time.Now().UnixNano())
  saveCmdOutput(callID, fullOutput)

  // 截断过长输出
  const maxLen = 2000
  if len(output) > maxLen {
    output = output[:maxLen] + fmt.Sprintf("\n... (输出过长，完整内容已保存到 /workspace/.cmd_output_%s.txt，可用 read_file 工具读取)", callID)
  }

  return &ToolResult{Success: true, Data: output}, nil
}
