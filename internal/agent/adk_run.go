package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "log/slog"

  "google.golang.org/adk/v2/agent"
  "google.golang.org/adk/v2/agent/llmagent"
  "google.golang.org/adk/v2/runner"
  "google.golang.org/adk/v2/session"
  "google.golang.org/adk/v2/tool"
  "google.golang.org/genai"
)

// ADKRun 使用 ADK 的 llmagent + runner 处理一条消息。
func ADKRun(ctx context.Context, cfg *AgentConfig, p Provider, toolsH *ToolRegistry,
  mcpToolsets []tool.Toolset, sessionSvc session.Service, sysPrompt string, msg *Message, cb func(StreamEvent)) (err error) {

  defer func() {
    if r := recover(); r != nil {
      slog.Error("adk_run.panic", "panic", r)
      cb(ErrorEvent(fmt.Sprintf("ADK 引擎异常: %v", r)))
      err = fmt.Errorf("ADK 引擎异常: %v", r)
    }
  }()

  slog.Debug("adk_run.start")

  adapter := NewADKProviderAdapter(p, cfg.Model.Provider)
  adapter.SetDisableTools(cfg.Model.DisableToolCall)
  adapter.SetRequestTimeout(cfg.RequestTimeout)

  var toolsets []tool.Toolset
  if ts := toolsH.AsADKToolset(); ts != nil {
    toolsets = append(toolsets, ts)
  }
  toolsets = append(toolsets, mcpToolsets...)

  adkAgent, err := llmagent.New(llmagent.Config{
    Name:             "pico",
    Model:            adapter,
    Description:      "PicoAide AI Agent",
    Instruction:      sysPrompt,
    Toolsets:         toolsets,
    DisallowTransferToParent: true,
    DisallowTransferToPeers:  true,
  })
  if err != nil {
    cb(ErrorEvent(fmt.Sprintf("创建 ADK agent 失败: %v", err)))
    return fmt.Errorf("创建 ADK agent 失败: %w", err)
  }

  r, err := runner.New(runner.Config{
    AppName:           "picoaide",
    Agent:             adkAgent,
    SessionService:    sessionSvc,
    AutoCreateSession: true,
  })
  if err != nil {
    cb(ErrorEvent(fmt.Sprintf("创建 ADK runner 失败: %v", err)))
    return fmt.Errorf("创建 ADK runner 失败: %w", err)
  }

  userContent := &genai.Content{
    Role:  "user",
    Parts: []*genai.Part{{Text: msg.Content}},
  }

  slog.Debug("adk_run.run", "user_id", cfg.UserID)

  for event, runErr := range r.Run(ctx, cfg.UserID, "default", userContent, agent.RunConfig{}) {
    if runErr != nil {
      slog.Error("adk_run.error", "error", runErr.Error())
      cb(ErrorEvent(runErr.Error()))
      return runErr
    }

    slog.Debug("adk_run.event",
      "type", fmt.Sprintf("%T", event),
      "author", event.Author,
      "content", contentPreview(event),
    )

    adkEventToStreamEvent(event, cb)
  }

  slog.Debug("adk_run.complete")
  return nil
}

// adkEventToStreamEvent 将 ADK session.Event 转换为 picoagent 的 StreamEvent。
func adkEventToStreamEvent(ev *session.Event, cb func(StreamEvent)) {
  if ev == nil || ev.Content == nil {
    return
  }

  for _, part := range ev.Content.Parts {
    switch {
    case part.Text != "":
      cb(TextDelta(part.Text))

    case part.FunctionCall != nil:
      tc := part.FunctionCall
      input, _ := json.Marshal(tc.Args)
      cb(StreamEvent{
        Type: "tool_call_start",
        Data: mustJSON(ToolCallData{
          ID:   tc.ID,
          Name: tc.Name,
          Input: input,
        }),
      })

    case part.FunctionResponse != nil:
      // tool result - handled internally by ADK
    }
  }

  if ev.IsFinalResponse() {
    cb(FinishEvent("", map[string]int{}))
  }
}

func contentPreview(ev *session.Event) string {
  if ev == nil || ev.Content == nil {
    return ""
  }
  for _, p := range ev.Content.Parts {
    if p.Text != "" {
      s := p.Text
      if len(s) > 60 {
        s = s[:60] + "..."
      }
      return s
    }
  }
  return "(no text)"
}
