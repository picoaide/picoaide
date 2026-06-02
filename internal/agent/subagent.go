package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "log/slog"
  "os"
  "path/filepath"
  "sync"
  "time"
)

// ============================================================
// SubAgent — 独立上下文和 LLM 能力的真正子代理
// ============================================================

type SubAgentResult struct {
  Name    string
  Success bool
  Data    string
}

type managedAgent struct {
  taskDesc   string
  serverName string // MCP 服务器名，子代理只会加载该服务器的工具
  toolsHint  string // 主代理提供的工具使用指引
  result     *SubAgentResult
  done       chan struct{}
  cancel     context.CancelFunc
}

// SubAgentManager 持有父引擎的配置和依赖，用于创建子引擎
type SubAgentManager struct {
  mu       sync.Mutex
  agents   map[string]*managedAgent
  config   *AgentConfig
  provider Provider
  tools    *ToolRegistry
}

func NewSubAgentManager(cfg *AgentConfig, provider Provider, tools *ToolRegistry) *SubAgentManager {
  return &SubAgentManager{
    agents:   make(map[string]*managedAgent),
    config:   cfg,
    provider: provider,
    tools:    tools,
  }
}

// SpawnAgent 创建独立的子代理（完整 LLM + 工具能力），在 goroutine 中运行。
// ctx 仅用于在入口处检查父上下文是否已取消（见 SubAgentSpawnTool.Execute）；
// 子代理内部的上下文使用独立的 Background，不继承调用方的超时/取消，取消通过 Cancel/CancelAll 管理。
// serverName 指定 MCP 服务器名，子代理自动加载该服务器的全部工具；toolsHint 是主代理提供的工具使用指引。
func (m *SubAgentManager) SpawnAgent(ctx context.Context, name string, taskDesc string, serverName string, toolsHint string) error {
  if name == "" || taskDesc == "" {
    return fmt.Errorf("name 和 task 不能为空")
  }
  m.mu.Lock()
  if _, ok := m.agents[name]; ok {
    m.mu.Unlock()
    return fmt.Errorf("子代理 %s 已在运行", name)
  }

  subCtx, cancel := context.WithCancel(context.Background())
  ma := &managedAgent{
    taskDesc:   taskDesc,
    serverName: serverName,
    toolsHint:  toolsHint,
    done:       make(chan struct{}),
    cancel:     cancel,
  }
  m.agents[name] = ma
  m.mu.Unlock()

  go func() {
    defer close(ma.done)
    defer func() {
      if r := recover(); r != nil {
        ma.result = &SubAgentResult{
          Name:    name,
          Success: false,
          Data:    fmt.Sprintf("子代理内部 panic: %v", r),
        }
      }
    }()
    ma.result = m.runSubAgent(subCtx, name, taskDesc, serverName, toolsHint)
  }()

  return nil
}

// runSubAgent 创建子引擎并执行任务
func (m *SubAgentManager) runSubAgent(ctx context.Context, name, taskDesc, serverName, toolsHint string) *SubAgentResult {
  // 子代理使用临时目录存储会话，父进程结束后自动清理
  workDir, err := os.MkdirTemp("", "picoaide-subagent-*")
  if err != nil {
    return &SubAgentResult{Name: name, Success: false, Data: fmt.Sprintf("创建子代理工作目录失败: %v", err)}
  }
  defer os.RemoveAll(workDir)

  store := NewSessionStore(filepath.Join(workDir, "session"))
  engine := NewEngine(m.config, m.provider, m.tools, store)
  engine.subAgentMgr = nil

  // 如果指定了 MCP 服务器，预加载该服务器的工具
  if serverName != "" {
    engine.PreloadServer(serverName)
  }

  // 如果主代理提供了工具指引，追加到 system prompt
  sysPrompt := ""
  if toolsHint != "" {
    sysPrompt = fmt.Sprintf("## 工具使用指引（由主代理指定）\n%s\n\n请严格按照上述指引使用工具。", toolsHint)
  }

  var response string
  msg := &Message{Role: RoleUser, Content: taskDesc}
  err = engine.Process(ctx, sysPrompt, nil, msg, func(ev StreamEvent) {
    switch ev.Type {
    case "text_delta":
      var text string
      if json.Unmarshal(ev.Data, &text) == nil {
        response += text
      }
    case "finish":
      if response == "" {
        var finishData struct {
          Content string `json:"content"`
        }
        if json.Unmarshal(ev.Data, &finishData) == nil && finishData.Content != "" {
          response = finishData.Content
        }
      }
    }
  })

  if err != nil {
    return &SubAgentResult{
      Name:    name,
      Success: false,
      Data:    fmt.Sprintf("子代理 %s 执行失败: %v", name, err),
    }
  }
  if response == "" {
    return &SubAgentResult{Name: name, Success: false, Data: "子代理返回空结果"}
  }
  return &SubAgentResult{Name: name, Success: true, Data: response}
}

// Collect 等待子代理完成并返回结果，超过 timeout 则取消并返回超时错误
func (m *SubAgentManager) Collect(name string, timeout time.Duration) *SubAgentResult {
  m.mu.Lock()
  ma, ok := m.agents[name]
  m.mu.Unlock()

  if !ok {
    return &SubAgentResult{Name: name, Success: false, Data: "子代理不存在"}
  }

  if timeout <= 0 {
    timeout = 600 * time.Second
  }

  timer := time.NewTimer(timeout)
  defer timer.Stop()

  select {
  case <-ma.done:
    m.mu.Lock()
    delete(m.agents, name)
    m.mu.Unlock()
    return ma.result
  case <-timer.C:
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
// SubAgentSpawnTool — 创建子代理并立即返回（用于并行启动多个子代理）
// ============================================================

type SubAgentSpawnTool struct {
  Manager *SubAgentManager
}

func (t *SubAgentSpawnTool) Name() string { return "subagent_spawn" }

func (t *SubAgentSpawnTool) Description() string {
  return "启动子代理（独立 AI）并行执行任务。子代理拥有独立的 LLM 上下文和工具调用能力。创建后立即返回，使用 subagent_collect 工具收集结果。批量任务时先 spawn 所有子代理，再逐一 collect，实现真正的并行执行。"
}

func (t *SubAgentSpawnTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "name": map[string]interface{}{
        "type":        "string",
        "description": "子代理名称，用于后续 collect 时指定",
      },
      "task": map[string]interface{}{
        "type":        "string",
        "description": "子代理要执行的任务描述，作为独立 AI 的输入消息",
      },
      "server": map[string]interface{}{
        "type":        "string",
        "description": "MCP 服务器名（可选）。指定后子代理自动加载该服务器的全部工具。从「可用 MCP 服务器」列表中选取。",
      },
      "tools_hint": map[string]interface{}{
        "type":        "string",
        "description": "工具使用指引（可选）。主代理告知子代理如何调用工具，例如「使用 get_news_sentiment 查询，searchKey 传公司名」。",
      },
    },
    "required": []string{"name", "task"},
  }
}

func (t *SubAgentSpawnTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Name      string `json:"name"`
    Task      string `json:"task"`
    Server    string `json:"server"`
    ToolsHint string `json:"tools_hint"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }
  if params.Name == "" || params.Task == "" {
    return &ToolResult{Success: false, Data: "name 和 task 不能为空"}, nil
  }

  if t.Manager == nil {
    return &ToolResult{Success: false, Data: "子代理管理器不可用（子代理中不能再次创建子代理）"}, nil
  }

  if ctx.Err() != nil {
    return &ToolResult{Success: false, Data: "父上下文已取消，无法创建子代理"}, nil
  }

  slog.Debug("subagent.spawn", "name", params.Name, "task_length", len(params.Task), "server", params.Server)

  if err := t.Manager.SpawnAgent(ctx, params.Name, params.Task, params.Server, params.ToolsHint); err != nil {
    return &ToolResult{Success: false, Data: err.Error()}, nil
  }

  data, _ := json.Marshal(map[string]string{"name": params.Name, "status": "spawned"})
  return &ToolResult{Success: true, Data: string(data)}, nil
}

// ============================================================
// SubAgentCollectTool — 收集子代理的执行结果
// ============================================================

type SubAgentCollectTool struct {
  Manager *SubAgentManager
}

func (t *SubAgentCollectTool) Name() string { return "subagent_collect" }

func (t *SubAgentCollectTool) Description() string {
  return "收集子代理的执行结果。与 subagent_spawn 配合使用。先在批量任务中 spawn 所有子代理，然后逐一 collect 获取每个子代理的结果。"
}

func (t *SubAgentCollectTool) Schema() map[string]interface{} {
  return map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
      "name": map[string]interface{}{
        "type":        "string",
        "description": "要收集结果的子代理名称",
      },
      "timeout": map[string]interface{}{
        "type":        "integer",
        "description": "等待超时秒数，默认 600 秒（10 分钟）",
      },
    },
    "required": []string{"name"},
  }
}

func (t *SubAgentCollectTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
  var params struct {
    Name    string `json:"name"`
    Timeout int    `json:"timeout"`
  }
  if err := json.Unmarshal(args, &params); err != nil {
    return &ToolResult{Success: false, Data: "参数解析失败"}, nil
  }
  if params.Name == "" {
    return &ToolResult{Success: false, Data: "name 不能为空"}, nil
  }

  if t.Manager == nil {
    return &ToolResult{Success: false, Data: "子代理管理器不可用（子代理中不能再次创建子代理）"}, nil
  }

  timeout := time.Duration(params.Timeout) * time.Second
  if timeout <= 0 {
    timeout = 600 * time.Second
  }

  slog.Debug("subagent.collect", "name", params.Name, "timeout_seconds", params.Timeout)

  result := t.Manager.Collect(params.Name, timeout)
  return &ToolResult{Success: result.Success, Data: result.Data}, nil
}
