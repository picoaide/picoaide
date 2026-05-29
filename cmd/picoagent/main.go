package main

import (
  "bufio"
  "bytes"
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "log/slog"
  "net"
  "net/http"
  "os"
  "os/signal"
  "path/filepath"
  "strings"
  "syscall"
  "time"

  "github.com/picoaide/picoaide/internal/agent"
)

func main() {
  // 初始化 slog：所有日志输出到 stderr，带 [PICOAGENT] 前缀
  slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
    ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
      if a.Key == slog.MessageKey {
        a.Value = slog.StringValue("[PICOAGENT] " + a.Value.String())
      }
      return a
    },
  })))

  slog.Debug("picoagent.starting")

  token := os.Getenv("PICOAGENT_TOKEN")
  if token == "" {
    slog.Debug("picoagent.no_token")
    errorExit("PICOAGENT_TOKEN 环境变量未设置")
  }
  slog.Debug("picoagent.token_ok", "token_length", len(token))

  sock := os.Getenv("PICOAGENT_SOCKET")
  if sock == "" {
    slog.Debug("picoagent.no_socket")
    errorExit("PICOAGENT_SOCKET 环境变量未设置")
  }
  slog.Debug("picoagent.socket_ok", "socket", sock)

  // 1. 获取配置
  slog.Debug("picoagent.fetching_config")
  cfg, err := fetchConfigWithRetry(sock, token)
  if err != nil {
    slog.Debug("picoagent.config_fetch_failed", "error", err.Error())
    errorExit("获取配置失败: " + err.Error())
  }
  slog.Debug("picoagent.config_loaded",
    "user_id", cfg.UserID,
    "model_id", cfg.Model.ModelID,
    "provider", cfg.Model.Provider,
    "base_url", cfg.Model.BaseURL,
    "max_tokens", cfg.Model.MaxTokens,
    "max_iter", cfg.Model.MaxIter,
    "temperature", cfg.Model.Temperature,
    "context_window", cfg.Model.ContextWindow,
    "request_timeout", cfg.RequestTimeout,
    "tools_count", len(cfg.Tools),
    "mcp_servers_count", len(cfg.MCPServers),
    "workspace", cfg.Workspace,
  )

  // 2. 初始化存储
  slog.Debug("picoagent.initializing_store")
  store := agent.NewSessionStore(cfg.Workspace)
  slog.Debug("picoagent.store_ready", "workspace", cfg.Workspace)

  // 3. 构建系统提示
  slog.Debug("picoagent.building_sysprompt")
  sysPrompt := buildSystemPrompt(cfg.Workspace)
  slog.Debug("picoagent.sysprompt_ready", "length", len(sysPrompt))

  // 4. 读取密钥文件
  slog.Debug("picoagent.lookup_apikey")
  apiKey := lookupAPIKey()
  slog.Debug("picoagent.apikey_status", "found", apiKey != "")

  // 5. 选择模型
  slog.Debug("picoagent.model_info",
    "model_id", cfg.Model.ModelID,
    "provider", cfg.Model.Provider,
    "base_url", cfg.Model.BaseURL,
  )

  provider, err := agent.NewProvider(cfg.Model.Provider, cfg.Model.ModelID, cfg.Model.BaseURL, apiKey)
  if err != nil {
    errorExit("创建 provider 失败: " + err.Error())
  }
  slog.Debug("picoagent.provider_ready")

  // 5. 注册工具
  slog.Debug("picoagent.registering_tools")
  tools := agent.NewToolRegistry()
  cmdTimeout := time.Duration(cfg.RequestTimeout) * time.Second
  tools.Register(&agent.CommandTool{Timeout: cmdTimeout})
  tools.Register(&agent.ReadFileTool{})
  tools.Register(&agent.GrepTool{})
  tools.Register(&agent.WriteFileTool{})
  tools.Register(&agent.EditFileTool{})
  tools.Register(&agent.AppendFileTool{})
  tools.Register(&agent.ListDirTool{})
  tools.Register(&agent.GlobTool{})
  tools.Register(&agent.DeleteFileTool{})
  tools.Register(&agent.WebSearchTool{})
  tools.Register(&agent.WebFetchTool{})
  tools.Register(&agent.UpdateMemoryTool{Workspace: cfg.Workspace})
  slog.Debug("picoagent.tools_registered", "count", 12)

  // 连接 MCP 服务器（使用独立 context，不影响主流程超时）
  mcpCtx, mcpCancel := context.WithTimeout(context.Background(), 10*time.Second)
  if len(cfg.MCPServers) > 0 {
    mcpManager := agent.NewMCPToolManager()
    mcpManager.WorkspaceDir = cfg.Workspace
    mcpManager.SetToken(token)
    for serverName, serverCfg := range cfg.MCPServers {
      if serverCfg.Socket == "" {
        continue
      }
      if err := mcpManager.Connect(mcpCtx, serverName, &serverCfg, token); err != nil {
        slog.Debug("picoagent.mcp_connect_failed", "server", serverName, "error", err.Error())
        continue
      }
      slog.Debug("picoagent.mcp_connected", "server", serverName, "socket", serverCfg.Socket)
    }
    mcpManager.RegisterAll(tools)
  }
  mcpCancel()

  // 6. 创建引擎 + 设置压缩器摘要 LLM
  slog.Debug("picoagent.creating_engine")
  engine := agent.NewEngine(cfg, provider, tools, store)
  summarizer := agent.NewLLMSummarizer(provider, cfg.Model.ModelID)
  engine.SetSummarizer(summarizer)

  // 6a. 注册子代理工具
  subAgentTool := &agent.SubAgentTool{Manager: engine.SubAgentManager()}
  tools.Register(subAgentTool)

  // 6b. 加载技能
  skills, err := agent.LoadSkills(cfg.Workspace)
  if err != nil {
    slog.Debug("picoagent.skills_load_failed", "error", err.Error())
  } else if len(skills) > 0 {
    engine.SetSkills(skills)
    slog.Debug("picoagent.skills_loaded", "count", len(skills))
  }

  // 7. 计算统一 session key（跨渠道）
  scope := agent.SessionScope{
    Version:    1,
    AgentID:    "pico",
    Channel:    "unified",
    Account:    cfg.UserID,
    Dimensions: []string{"user"},
    Values:     map[string]string{"user": cfg.UserID},
  }
  sessionKey := agent.BuildSessionKey(scope)

  history, _ := store.LoadLive(sessionKey)
  slog.Debug("picoagent.session_loaded",
    "session_key", sessionKey,
    "history_count", len(history),
    "user_id", cfg.UserID,
  )

  // 8. 多轮消息循环 — 每轮从 stdin 读取一条消息并处理，
  //    完成后等待下一条消息（沙箱可追加），stdin 关闭（EOF）或空闲超时退出
  slog.Debug("picoagent.reading_input")
  timeout := cfg.RequestTimeout
  engine.SetSessionKey(sessionKey)
  heartbeatCtx, heartbeatCancel := context.WithCancel(context.Background())
  defer heartbeatCancel()
  agent.StartHeartbeat(heartbeatCtx, 15*time.Second, func(event agent.StreamEvent) {
    data, _ := json.Marshal(event)
    fmt.Println(string(data))
  })

  // 信号处理（只需注册一次，作用所有消息）
  signalCtx, signalCancel := context.WithCancel(context.Background())
  defer signalCancel()
  sigCh := make(chan os.Signal, 1)
  signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
  defer signal.Stop(sigCh)
  go func() {
    select {
    case <-sigCh:
      slog.Debug("picoagent.signal_received")
      signalCancel()
      time.Sleep(2 * time.Second)
      os.Exit(1)
    case <-signalCtx.Done():
    }
  }()

  stdinReader := bufio.NewReader(os.Stdin)
  idleTimeout := 1 * time.Minute
  inputCh := make(chan inputResult, 1)
  // 单 goroutine 循环读取 stdin，避免 per-iteration 泄漏
  go func() {
    for {
      msg, err := readInputFrom(stdinReader)
      inputCh <- inputResult{msg, err}
      if err != nil {
        return
      }
    }
  }()

  var inputMsg *agent.Message
msgLoop:
  for {
    select {
    case result := <-inputCh:
      if result.err != nil {
        if errors.Is(result.err, io.EOF) {
          slog.Debug("picoagent.stdin_closed")
        } else {
          slog.Debug("picoagent.no_input", "error", result.err.Error())
        }
        break msgLoop
      }
      inputMsg = result.msg
    case <-time.After(idleTimeout):
      slog.Debug("picoagent.idle_timeout")
      break msgLoop
    case <-signalCtx.Done():
      slog.Debug("picoagent.signal_exit")
      break msgLoop
    }

    slog.Debug("picoagent.input_received",
      "role", inputMsg.Role,
      "content_length", len(inputMsg.Content),
      "content_preview", truncateString(inputMsg.Content, 100),
    )

    // 保存用户消息
    store.AppendMessage(sessionKey, inputMsg)

    // 处理消息（超时时间从配置读取，默认 120 秒）
    var ctx context.Context
    var cancel context.CancelFunc
    if timeout > 0 {
      ctx, cancel = context.WithTimeout(signalCtx, time.Duration(timeout)*time.Second)
    } else {
      ctx, cancel = context.WithCancel(signalCtx)
    }

    slog.Debug("picoagent.engine_process_start",
      "model_id", cfg.Model.ModelID,
      "timeout", timeout,
      "history_count", len(history),
      "input_length", len(inputMsg.Content),
    )

    var fullResponse string
    err = engine.Process(ctx, sysPrompt, history, inputMsg, func(event agent.StreamEvent) {
      data, _ := json.Marshal(event)
      fmt.Println(string(data))
      if event.Type == "text_delta" {
        var text string
        if json.Unmarshal(event.Data, &text) == nil {
          fullResponse += text
        }
      }
    })
    cancel()

    if err != nil {
      slog.Debug("picoagent.engine_error", "error", err.Error(), "response_length", len(fullResponse))
      errorEvent := agent.ErrorEvent(err.Error())
      data, _ := json.Marshal(errorEvent)
      fmt.Println(string(data))

      if fullResponse != "" {
        partialMsg := &agent.Message{
          Role:    agent.RoleAssistant,
          Content: fullResponse + "\n\n[响应中断: " + err.Error() + "]",
        }
        store.AppendMessage(sessionKey, partialMsg)
      }
      // 从 store 重新加载 history，确保下一轮包含当前轮已保存的内容
      history, _ = store.LoadLive(sessionKey)
      // 继续读取下一条消息，不退出
      continue
    }

    slog.Debug("picoagent.engine_complete",
      "response_length", len(fullResponse),
      "response_preview", truncateString(fullResponse, 100),
    )

    // 保存助手响应到会话
    if fullResponse != "" {
      assistantMsg := &agent.Message{
        Role:    agent.RoleAssistant,
        Content: fullResponse,
      }
      store.AppendMessage(sessionKey, assistantMsg)
    }

    // 从 store 重新加载 history，确保下一轮包含本轮完整上下文
    history, _ = store.LoadLive(sessionKey)
  }

  // 会话结束，触发记忆进化
  slog.Debug("picoagent.evolving_memory")
  evolver := agent.NewMemoryEvolution(cfg.Workspace, store)
  if summarizer != nil {
    evolver.SetSummarizer(summarizer)
  }
  evolveCtx, evolveCancel := context.WithTimeout(context.Background(), 30*time.Second)
  result, evolveErr := evolver.Evolve(evolveCtx, sessionKey)
  evolveCancel()
  if evolveErr != nil {
    slog.Debug("picoagent.evolve_error", "error", evolveErr.Error())
  } else if result.HasChanges {
    slog.Info("memory_evolution",
      "user", cfg.UserID,
      "session", sessionKey,
      "decisions", len(result.Decisions),
      "knowledge", len(result.Knowledge),
      "preferences", len(result.Preferences),
    )
    // 通过审计端点写入 DB
    summary := fmt.Sprintf("decisions=%d,knowledge=%d,preferences=%d,progress_completed=%d,progress_inprogress=%d,progress_blocked=%d",
      len(result.Decisions), len(result.Knowledge), len(result.Preferences),
      len(result.Progress.Completed), len(result.Progress.InProgress), len(result.Progress.Blocked))
    files := "MEMORY.md"
    if len(result.Preferences) > 0 {
      files += ",USER.md"
    }
    if err := postAuditLog(evolveCtx, sock, token, cfg.UserID, sessionKey, summary, files); err != nil {
      slog.Debug("picoagent.audit_post_error", "error", err.Error())
    }
  }

  fmt.Fprintf(os.Stderr, "[PICOAGENT] done\n")
  os.Stdout.Sync()
}

func truncateString(s string, maxLen int) string {
  if len(s) <= maxLen {
    return s
  }
  return s[:maxLen] + "..."
}

func errorExit(msg string) {
  fmt.Fprintf(os.Stderr, "[PICOAGENT] error: %s\n", msg)
  os.Stdout.Sync()
  event := agent.ErrorEvent(msg)
  data, _ := json.Marshal(event)
  fmt.Println(string(data))
  os.Stderr.Sync()
  os.Exit(1)
}

func lookupAPIKey() string {
  return os.Getenv("PICOAGENT_API_KEY")
}

func fetchConfig(sock, token string) (*agent.AgentConfig, error) {
  client := &http.Client{
    Timeout: 10 * time.Second,
    Transport: &http.Transport{
      DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
        return net.Dial("unix", sock)
      },
    },
  }

  req, err := http.NewRequest("GET", "http://localhost/api/picoagent/me", nil)
  if err != nil {
    return nil, fmt.Errorf("创建请求失败: %w", err)
  }
  req.Header.Set("Authorization", "Bearer "+token)

  resp, err := client.Do(req)
  if err != nil {
    return nil, fmt.Errorf("HTTP 请求失败: %w", err)
  }
  defer resp.Body.Close()

  body, err := io.ReadAll(resp.Body)
  if err != nil {
    return nil, fmt.Errorf("读取响应失败: %w", err)
  }

  if resp.StatusCode != http.StatusOK {
    return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
  }

  var cfg agent.AgentConfig
  if err := json.Unmarshal(body, &cfg); err != nil {
    return nil, fmt.Errorf("解析配置失败: %w", err)
  }
  return &cfg, nil
}

// fetchConfigWithRetry 获取配置，连接类错误自动重试 3 次
func fetchConfigWithRetry(sock, token string) (*agent.AgentConfig, error) {
  var lastErr error
  for attempt := 0; attempt < 3; attempt++ {
    if attempt > 0 {
      slog.Debug("picoagent.config_retry", "attempt", attempt+1)
      time.Sleep(time.Second)
    }
    cfg, err := fetchConfig(sock, token)
    if err == nil {
      return cfg, nil
    }
    if !isConfigRetryable(err) {
      return nil, err
    }
    lastErr = err
  }
  return nil, fmt.Errorf("获取配置重试 3 次失败: %w", lastErr)
}

func isConfigRetryable(err error) bool {
  if err == nil {
    return false
  }
  msg := err.Error()
  return strings.Contains(msg, "connection") ||
    strings.Contains(msg, "dial") ||
    strings.Contains(msg, "refused") ||
    strings.Contains(msg, "timeout") ||
    strings.Contains(msg, "reset")
}

type inputResult struct {
  msg *agent.Message
  err error
}

// postAuditLog POST 记忆进化审计记录到服务端，失败时重试 1 次
// ctx 用于控制 HTTP 请求超时和取消，避免阻塞演进流程
func postAuditLog(ctx context.Context, sock, token, username, sessionKey, changesSummary, filesModified string) error {
  doPost := func() error {
    body := map[string]string{
      "username":        username,
      "session_key":     sessionKey,
      "changes_summary": changesSummary,
      "files_modified":  filesModified,
    }
    bodyJSON, _ := json.Marshal(body)

    client := &http.Client{
      Timeout: 10 * time.Second,
      Transport: &http.Transport{
        DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
          return net.Dial("unix", sock)
        },
      },
    }

    req, err := http.NewRequestWithContext(ctx, "POST", "http://localhost/api/picoagent/audit", bytes.NewReader(bodyJSON))
    if err != nil {
      return fmt.Errorf("创建审计请求失败: %w", err)
    }
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")

    resp, err := client.Do(req)
    if err != nil {
      return fmt.Errorf("审计请求失败: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
      respBody, _ := io.ReadAll(resp.Body)
      return fmt.Errorf("审计请求 HTTP %d: %s", resp.StatusCode, string(respBody))
    }
    return nil
  }

  err := doPost()
  if err != nil {
    slog.Debug("picoagent.audit_retry", "error", err.Error())
    select {
    case <-time.After(time.Second):
    case <-ctx.Done():
      return ctx.Err()
    }
    return doPost()
  }
  return nil
}

func readInputFrom(r *bufio.Reader) (*agent.Message, error) {
  var msg agent.Message
  decoder := json.NewDecoder(r)
  if err := decoder.Decode(&msg); err != nil {
    return nil, fmt.Errorf("stdin JSON 解析失败: %w", err)
  }
  if msg.Role == "" {
    return nil, fmt.Errorf("消息缺少 role 字段")
  }
  return &msg, nil
}

func readInput() (*agent.Message, error) {
  return readInputFrom(bufio.NewReader(os.Stdin))
}

func buildSystemPrompt(workspace string) string {
  var parts []string

  agentContent := readFile(filepath.Join(workspace, "AGENT.md"))
  if agentContent != "" {
    parts = append(parts, "## AGENT.md\n\n"+agentContent)
  }

  soulContent := readFile(filepath.Join(workspace, "SOUL.md"))
  if soulContent != "" {
    parts = append(parts, "## SOUL.md\n\n"+soulContent)
  }

  userContent := readFile(filepath.Join(workspace, "USER.md"))
  if userContent != "" {
    parts = append(parts, "## USER.md\n\n"+userContent)
  }

  memoryContent := readFile(filepath.Join(workspace, "memory", "MEMORY.md"))
  if memoryContent != "" {
    parts = append(parts, "## Memory\n\n"+memoryContent)
  }

  return strings.Join(parts, "\n\n")
}

func readFile(path string) string {
  data, err := os.ReadFile(path)
  if err != nil {
    return ""
  }
  return string(bytes.TrimSpace(data))
}
