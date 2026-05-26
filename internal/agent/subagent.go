package agent

import (
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "os/exec"
  "strings"
  "sync"
  "time"
)

// ============================================================
// SubAgent — 轻量子代理，在 goroutine 中执行任务
// ============================================================

type SubAgentResult struct {
  Name    string
  Success bool
  Data    string
}

type SubAgent struct {
  Name string
  task func(ctx context.Context) (string, error)
}

func NewSubAgent(name string, task func(ctx context.Context) (string, error)) *SubAgent {
  return &SubAgent{Name: name, task: task}
}

func (a *SubAgent) Run(ctx context.Context) *SubAgentResult {
  data, err := a.task(ctx)
  if err != nil {
    return &SubAgentResult{Name: a.Name, Success: false, Data: fmt.Sprintf("子代理执行失败: %s", err.Error())}
  }
  if data == "" {
    return &SubAgentResult{Name: a.Name, Success: false, Data: "子代理返回空结果"}
  }
  return &SubAgentResult{Name: a.Name, Success: true, Data: data}
}

// ============================================================
// SubAgentManager — 子代理管理器
// ============================================================

type managedAgent struct {
  agent  *SubAgent
  result *SubAgentResult
  done   chan struct{}
  cancel context.CancelFunc
}

type SubAgentManager struct {
  mu     sync.Mutex
  agents map[string]*managedAgent
}

func NewSubAgentManager() *SubAgentManager {
  return &SubAgentManager{
    agents: make(map[string]*managedAgent),
  }
}

func (m *SubAgentManager) Spawn(ctx context.Context, agent *SubAgent) error {
  if agent == nil {
    return fmt.Errorf("子代理不能为 nil")
  }
  m.mu.Lock()
  if _, ok := m.agents[agent.Name]; ok {
    m.mu.Unlock()
    return fmt.Errorf("子代理 %s 已在运行", agent.Name)
  }

  ctx, cancel := context.WithCancel(ctx)
  ma := &managedAgent{
    agent:  agent,
    done:   make(chan struct{}),
    cancel: cancel,
  }
  m.agents[agent.Name] = ma
  m.mu.Unlock()

  go func() {
    ma.result = ma.agent.Run(ctx)
    close(ma.done)
  }()

  return nil
}

func (m *SubAgentManager) Collect(name string, timeout time.Duration) *SubAgentResult {
  m.mu.Lock()
  ma, ok := m.agents[name]
  m.mu.Unlock()

  if !ok {
    return &SubAgentResult{Name: name, Success: false, Data: "子代理不存在"}
  }

  select {
  case <-ma.done:
    m.mu.Lock()
    delete(m.agents, name)
    m.mu.Unlock()
    return ma.result
  case <-time.After(timeout):
    ma.cancel()
    m.mu.Lock()
    delete(m.agents, name)
    m.mu.Unlock()
    return &SubAgentResult{Name: name, Success: false, Data: "子代理超时"}
  }
}

func (m *SubAgentManager) Cancel(name string) {
  m.mu.Lock()
  ma, ok := m.agents[name]
  if ok {
    ma.cancel()
  }
  m.mu.Unlock()
}

func (m *SubAgentManager) CancelAll() {
  m.mu.Lock()
  for _, ma := range m.agents {
    ma.cancel()
  }
  m.mu.Unlock()
}

func (m *SubAgentManager) List() []string {
  m.mu.Lock()
  defer m.mu.Unlock()
  names := make([]string, 0, len(m.agents))
  for name := range m.agents {
    names = append(names, name)
  }
  return names
}

// ============================================================
// SubAgentTool — 注册到 ToolRegistry 的子代理工具
// ============================================================

type SubAgentTool struct {
  Manager *SubAgentManager
}

func (t *SubAgentTool) Name() string { return "subagent_task" }

func (t *SubAgentTool) Description() string {
  return "启动子代理并行执行任务。适合不需要主 LLM 直接参与的后台任务（如文件搜索、数据处理）。"
}

func (t *SubAgentTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "name": map[string]interface{}{
        "type":        "string",
        "description": "子代理名称，用于后续收集结果",
      },
      "command": map[string]interface{}{
        "type":        "string",
        "description": "子代理要执行的 shell 命令",
      },
      "timeout": map[string]interface{}{
        "type":        "integer",
        "description": "超时秒数，默认 30",
      },
    },
    "required": []string{"name", "command"},
  }
}

func (t *SubAgentTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Name    string `json:"name"`
    Command string `json:"command"`
    Timeout int    `json:"timeout"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }
  if params.Name == "" || params.Command == "" {
    return &ToolResult{Success: false, Data: "name 和 command 不能为空"}, nil
  }
  if params.Timeout <= 0 {
    params.Timeout = 30
  }

  agent := NewSubAgent(params.Name, func(ctx context.Context) (string, error) {
    cmd := exec.CommandContext(ctx, "sh", "-c", params.Command)
    var stdout, stderr bytes.Buffer
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
      return msg, nil
    }
    output := strings.TrimSpace(stdout.String())
    if output == "" {
      output = strings.TrimSpace(stderr.String())
    }
    if output == "" {
      return "(无输出)", nil
    }
    return output, nil
  })

  ctx, cancel := context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
  defer cancel()

  if err := t.Manager.Spawn(ctx, agent); err != nil {
    return &ToolResult{Success: false, Data: err.Error()}, nil
  }

  result := t.Manager.Collect(params.Name, time.Duration(params.Timeout)*time.Second)
  return &ToolResult{Success: result.Success, Data: result.Data}, nil
}
