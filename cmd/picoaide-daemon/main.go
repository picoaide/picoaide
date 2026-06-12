package main

import (
  "bufio"
  "context"
  "encoding/json"
  "errors"
  "flag"
  "fmt"
  "io"
  "net"
  "os"
  "os/signal"
  "path/filepath"
  "syscall"
  "time"

  "log/slog"

  "github.com/picoaide/picoaide/internal/agent"
  "github.com/picoaide/picoaide/internal/daemon"
  daemonstore "github.com/picoaide/picoaide/internal/daemon/store"
)

var (
  socket    string
  workspace string
  username  string
  token     string
)

func main() {
  flag.StringVar(&socket, "socket", "/run/picoaide.sock", "Unix socket 路径")
  flag.StringVar(&workspace, "workspace", "/workspace", "工作目录路径")
  flag.StringVar(&username, "username", "", "用户名")
  flag.StringVar(&token, "token", "", "认证 token")
  flag.Parse()

  setupLogger()

  if username == "" {
    slog.Debug("daemon.no_username")
    os.Exit(1)
  }

  slog.Debug("daemon.starting",
    "socket", socket,
    "workspace", workspace,
    "username", username,
  )

  daemonDir := filepath.Join(workspace, "daemon")
  tasksDir := filepath.Join(daemonDir, "tasks")
  if err := os.MkdirAll(tasksDir, 0755); err != nil {
    slog.Debug("daemon.mkdir_failed", "error", err.Error())
    os.Exit(1)
  }

  conn, err := net.Dial("unix", socket)
  if err != nil {
    slog.Debug("daemon.dial_failed", "error", err.Error())
    os.Exit(1)
  }
  defer conn.Close()
  slog.Debug("daemon.connected")

  cfg, err := fetchConfig(conn)
  if err != nil {
    slog.Debug("daemon.config_failed", "error", err.Error())
    os.Exit(1)
  }
  slog.Debug("daemon.config_loaded",
    "model", cfg.Model.ModelID,
    "provider", cfg.Model.Provider,
  )

  apiKey := lookupAPIKey()
  provider, err := agent.NewProvider(cfg.Model.Provider, cfg.Model.ModelID, cfg.Model.BaseURL, apiKey)
  if err != nil {
    slog.Debug("daemon.provider_failed", "error", err.Error())
    os.Exit(1)
  }

  sessStore := agent.NewSessionStore(cfg.Workspace)
  tools := agent.NewToolRegistry()
  cmdTimeout := time.Duration(cfg.RequestTimeout) * time.Second
  if cmdTimeout <= 0 {
    cmdTimeout = 120 * time.Second
  }
  tools.Register(&agent.CommandTool{Timeout: cmdTimeout})
  tools.Register(&agent.ReadFileTool{})
  tools.Register(&agent.GrepTool{})
  tools.Register(&agent.WriteFileTool{})
  tools.Register(&agent.EditFileTool{})
  tools.Register(&agent.AppendFileTool{})
  tools.Register(&agent.ListDirTool{})
  tools.Register(&agent.GlobTool{})
  tools.Register(&agent.DeleteFileTool{})
  tools.Register(&agent.WebFetchTool{})
  tools.Register(&agent.UpdateMemoryTool{Workspace: cfg.Workspace})

  engine := agent.NewEngine(cfg, provider, tools, sessStore)
  summarizer := agent.NewLLMSummarizer(provider, cfg.Model.ModelID, cfg.Model.MaxTokens)
  engine.SetSummarizer(summarizer)

  subAgentMgr := agent.NewSubAgentManager(cfg, provider, tools)
  engine.SetSubAgentManager(subAgentMgr)
  tools.Register(&agent.SubAgentSpawnTool{Manager: subAgentMgr})
  tools.Register(&agent.SubAgentCollectTool{Manager: subAgentMgr})
  tools.Register(&agent.QueryServerTool{Registry: tools})

  skills, err := agent.LoadSkills(cfg.Workspace)
  if err == nil && len(skills) > 0 {
    engine.SetSkills(skills)
    slog.Debug("daemon.skills_loaded", "count", len(skills))
  }

  sysPrompt := buildSystemPrompt(cfg.Workspace)
  sessionKey := agent.BuildSessionKey(agent.SessionScope{
    Version:    1,
    AgentID:    "pico",
    Channel:    "daemon",
    Account:    cfg.UserID,
    Dimensions: []string{"user"},
    Values:     map[string]string{"user": cfg.UserID},
  })
  engine.SetSessionKey(sessionKey)

  taskStore := daemonstore.NewTaskStore(daemonDir)
  wsPath := cfg.Workspace
  if wsPath == "" {
    wsPath = workspace
  }

  eventCB := func(typ string, data json.RawMessage) {
    sendRPC(conn, daemon.RPCMessage{Type: typ, Data: data})
  }

  tq := NewTaskQueue(engine, taskStore, daemonDir, wsPath, sysPrompt, eventCB)

  ctx, cancel := context.WithCancel(context.Background())
  defer cancel()

  go tq.Run(ctx)

  heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
  defer heartbeatCancel()
  go runHeartbeat(heartbeatCtx, conn)

  sigCh := make(chan os.Signal, 1)
  signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
  defer signal.Stop(sigCh)
  go func() {
    select {
    case <-sigCh:
      slog.Debug("daemon.signal_received")
      cancel()
      time.Sleep(2 * time.Second)
      tq.FlushAll()
      os.Exit(0)
    case <-ctx.Done():
    }
  }()

  scanner := bufio.NewScanner(conn)
  scanner.Buffer(make([]byte, 512*1024), 512*1024)
  for scanner.Scan() {
    line := scanner.Bytes()
    if len(line) == 0 {
      continue
    }
    var rpc daemon.RPCMessage
    if err := json.Unmarshal(line, &rpc); err != nil {
      slog.Debug("daemon.parse_error", "error", err.Error())
      continue
    }
    slog.Debug("daemon.rpc_received", "type", rpc.Type)

    switch rpc.Type {
    case "submit_task":
      var req daemon.SubmitTaskReq
      if json.Unmarshal(rpc.Data, &req) == nil {
        tq.Submit(req.TaskID, req.Source, req.Priority, req.Message)
      }
    case "pause_task":
      var req daemon.TaskActionReq
      if json.Unmarshal(rpc.Data, &req) == nil {
        if err := tq.Pause(req.TaskID); err != nil {
          sendRPC(conn, daemon.RPCMessage{
            Type: "error",
            Data: mustRawJSON(daemon.ErrorEvent{Message: err.Error()}),
          })
        }
      }
    case "resume_task":
      var req daemon.TaskActionReq
      if json.Unmarshal(rpc.Data, &req) == nil {
        if err := tq.Resume(req.TaskID); err != nil {
          sendRPC(conn, daemon.RPCMessage{
            Type: "error",
            Data: mustRawJSON(daemon.ErrorEvent{Message: err.Error()}),
          })
        }
      }
    case "cancel_task":
      var req daemon.TaskActionReq
      if json.Unmarshal(rpc.Data, &req) == nil {
        tq.Cancel(req.TaskID)
      }
    case "send_message":
      var req daemon.SendMessageReq
      if json.Unmarshal(rpc.Data, &req) == nil {
        if err := tq.SendMessage(req.TaskID, req.Message); err != nil {
          sendRPC(conn, daemon.RPCMessage{
            Type: "error",
            Data: mustRawJSON(daemon.ErrorEvent{Message: err.Error()}),
          })
        }
      }
    case "stop":
      slog.Debug("daemon.stop_requested")
      tq.FlushAll()
      cancel()
      return
    }
  }
  if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
    slog.Debug("daemon.scan_error", "error", err.Error())
  }
}

func setupLogger() {
  slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
  })))
}

func lookupAPIKey() string {
  if key := os.Getenv("PICOAGENT_API_KEY"); key != "" {
    return key
  }
  return os.Getenv("PICODAEMON_API_KEY")
}

func sendRPC(conn net.Conn, rpc daemon.RPCMessage) {
  data, _ := json.Marshal(rpc)
  conn.Write(append(data, '\n'))
}

func mustRawJSON(v interface{}) json.RawMessage {
  data, _ := json.Marshal(v)
  return data
}

func fetchConfig(conn net.Conn) (*agent.AgentConfig, error) {
  req := daemon.RPCMessage{
    Type: "get_config",
    Data: mustRawJSON(map[string]string{"username": username}),
  }
  sendRPC(conn, req)

  conn.SetReadDeadline(time.Now().Add(30 * time.Second))
  scanner := bufio.NewScanner(conn)
  scanner.Buffer(make([]byte, 512*1024), 512*1024)
  if !scanner.Scan() {
    conn.SetReadDeadline(time.Time{})
    return nil, fmt.Errorf("读取配置响应失败: %v", scanner.Err())
  }
  conn.SetReadDeadline(time.Time{})

  var resp daemon.RPCMessage
  if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
    return nil, fmt.Errorf("解析配置响应失败: %w", err)
  }
  if resp.Type == "config_response" {
    var cfg agent.AgentConfig
    if err := json.Unmarshal(resp.Data, &cfg); err != nil {
      return nil, fmt.Errorf("解析 AgentConfig 失败: %w", err)
    }
    return &cfg, nil
  }
  if resp.Type == "error" {
    var errEvt daemon.ErrorEvent
    json.Unmarshal(resp.Data, &errEvt)
    return nil, fmt.Errorf("获取配置失败: %s", errEvt.Message)
  }
  return nil, fmt.Errorf("未知配置响应类型: %s", resp.Type)
}

func runHeartbeat(ctx context.Context, conn net.Conn) {
  ticker := time.NewTicker(5 * time.Second)
  defer ticker.Stop()
  for {
    select {
    case <-ticker.C:
      sendRPC(conn, daemon.RPCMessage{
        Type: "heartbeat",
        Data: mustRawJSON(daemon.HeartbeatEvent{Status: "alive"}),
      })
    case <-ctx.Done():
      return
    }
  }
}

func buildSystemPrompt(workspace string) string {
  var parts []string
  addFile := func(path string, title string) {
    data, _ := os.ReadFile(filepath.Join(workspace, path))
    if len(data) > 0 {
      parts = append(parts, "## "+title+"\n\n"+string(data))
    }
  }
  addFile("AGENT.md", "AGENT.md")
  addFile("SOUL.md", "SOUL.md")
  addFile("USER.md", "USER.md")
  addFile("memory/MEMORY.md", "Memory")
  result := ""
  for _, p := range parts {
    result += p + "\n\n"
  }
  return result
}
