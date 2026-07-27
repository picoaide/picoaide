package web

import (
  "context"
  "encoding/json"
  "fmt"
  "log/slog"
  "path/filepath"
  "github.com/picoaide/picoaide/internal/agent"
  "github.com/picoaide/picoaide/internal/store"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/im"
  "github.com/picoaide/picoaide/internal/sandbox"
  "github.com/picoaide/picoaide/internal/scheduler"
  "github.com/picoaide/picoaide/internal/skill"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// PicoAgent 集成层 — 串联沙箱/IM/Cron
// ============================================================

type AgentIntegration struct {
  sandbox   *sandbox.Manager
  imGateway *im.Gateway
  cron      *scheduler.CronScheduler
  cronStore *scheduler.SQLCronStore
}

// initAgentIntegration 在 Server 启动时初始化所有 PicoAgent 组件
func (s *Server) initAgentIntegration() (*AgentIntegration, error) {
  workDir := config.WorkDir()

  // 1. 沙箱管理器
  rootfs := filepath.Join(workDir, "rootfs")
  sb := sandbox.NewManager(rootfs, workDir)

  // 2. IM 网关
  gw := im.NewGateway()
  gw.SetOnMessage(func(ctx context.Context, msg im.Message) {
    s.handleIMMessage(ctx, msg)
  })

  // 注册所有 IM 渠道（每用户连接模式）
  channelProviders := map[string]func() im.Provider{
    "dingtalk": func() im.Provider { return im.NewDingTalkProvider() },
    "feishu":   func() im.Provider { return im.NewFeishuProvider() },
    "wechat":   func() im.Provider { return im.NewWeChatProvider() },
    "wecom":    func() im.Provider { return im.NewWeComProvider() },
  }

  for chKey, factory := range channelProviders {
    if !store.GetChannelEnabled(chKey) {
      continue
    }
    provider := factory()
    gw.Register(provider)

    // 加载已配置的用户连接
    users, err := store.ListConfiguredUserChannelsByChannel(chKey)
    if err != nil {
      slog.Warn("查询渠道已配置用户失败", "channel", chKey, "error", err)
      continue
    }
    for _, uc := range users {
      var creds map[string]string
      if json.Unmarshal([]byte(uc.Credentials), &creds) != nil || creds == nil {
        continue
      }
      switch p := provider.(type) {
      case *im.DingTalkProvider:
        if creds["client_id"] != "" && creds["client_secret"] != "" {
          p.AddUser(uc.Username, creds["client_id"], creds["client_secret"], creds["default_chat"])
        }
      case *im.FeishuProvider:
        if creds["app_id"] != "" && creds["app_secret"] != "" {
          p.AddUser(uc.Username, creds["app_id"], creds["app_secret"], creds["default_chat"])
        }
      case *im.WeChatProvider:
        if creds["bot_id"] != "" && creds["secret"] != "" {
          p.AddUser(uc.Username, creds["bot_id"], creds["secret"], creds["default_chat"])
        }
      case *im.WeComProvider:
        if creds["bot_id"] != "" && creds["secret"] != "" {
          p.AddUser(uc.Username, creds["bot_id"], creds["secret"], creds["default_chat"])
        }
      }
    }
  }

  // 3. Cron 调度器
  engine, err := store.GetEngine()
  if err != nil {
    return nil, err
  }
  cronStore := scheduler.NewSQLCronStore(engine)
  if err := cronStore.InitTable(); err != nil {
    slog.Warn("初始化 cron 表失败", "error", err)
  }

  cronTimeout := s.loadConfig().GetCronJobTimeout()
  cronScheduler := scheduler.NewCronScheduler(cronStore, cronTimeout, func(ctx context.Context, job *scheduler.CronJob) error {
    return s.executeCronJob(ctx, sb, cronStore, job)
  })

  ai := &AgentIntegration{
    sandbox:   sb,
    imGateway: gw,
    cron:      cronScheduler,
    cronStore: cronStore,
  }

  return ai, nil
}

// handleIMMessage 处理 IM 消息 → 启动沙箱 → 返回响应
func (s *Server) handleIMMessage(ctx context.Context, msg im.Message) {
  username := msg.UserID
  slog.Debug("process", "event", "process", "phase", "im_message_recv", "platform", msg.Platform, "username", username, "chat_id", msg.ChatID, "text_length", len(msg.Text))
  if username == "" {
    return
  }

  // 保存当前会话信息
  if msg.ChatID != "" {
    if existing, err := store.GetUserChannel(username, msg.Platform); err == nil && existing != nil {
      var creds map[string]string
      if json.Unmarshal([]byte(existing.Credentials), &creds) == nil {
        changed := false
        if creds["default_chat"] != msg.ChatID {
          creds["default_chat"] = msg.ChatID
          changed = true
        }
        if webhook := msg.Raw["webhook"]; webhook != "" && creds["webhook"] != webhook {
          creds["webhook"] = webhook
          changed = true
        }
        if changed {
          updated, _ := json.Marshal(creds)
          store.UpsertUserChannelWithCreds(username, msg.Platform, existing.Enabled, existing.Configured, string(updated))
        }
      }
    }
  }

  if err := user.InitializeUser(filepath.Join(config.WorkDir(), "user-template"), filepath.Join(config.WorkDir(), "users"), username); err != nil {
    slog.Error("初始化用户工作目录失败", "username", username, "error", err)
    return
  }

  // 检查用户是否开启分段回复
  streamOutput := true
  if uc, err := store.GetUserChannel(username, msg.Platform); err == nil && uc != nil {
    var creds map[string]string
    if json.Unmarshal([]byte(uc.Credentials), &creds) == nil {
      if v, ok := creds["stream"]; ok {
        streamOutput = v == "true"
      }
    }
  }

  // 构造输入消息
  input := agent.Message{Role: agent.RoleUser, Content: msg.Text}
  inputJSON, _ := json.Marshal(input)

  // 如果用户已有活跃沙箱，追加消息到现有会话
  if s.agentIntegration != nil && s.agentIntegration.sandbox != nil {
    if err := s.agentIntegration.sandbox.SendInput(username, inputJSON); err == nil {
      slog.Debug("chat.sandbox_appended", "username", username, "text_length", len(msg.Text))
      return
    }
  }

  // 没有活跃沙箱，创建新沙箱（标记 im 来源，避免 goroutine 重复转发）
  run := s.startChatSandbox(username, msg.Text, inputJSON, "", "im")

  // 订阅 chatRun 事件，转发到 IM
  notifCh, events := run.subscribe()
  defer run.unsubscribe(notifCh)

  var fullResponse string
  var lastSent int
  cursor := len(events)
  sendCtx := context.Background()
  reqID := msg.Raw["req_id"]
  flushIM := func(text string) {
    if s.agentIntegration == nil { return }
    var err error
    if reqID != "" {
      err = s.agentIntegration.imGateway.SendWithReqID(sendCtx, msg.Platform, msg.ChatID, text, reqID)
    } else {
      err = s.agentIntegration.imGateway.Send(sendCtx, msg.Platform, msg.ChatID, text)
    }
    if err != nil {
      slog.Warn("IM 发送失败", "platform", msg.Platform, "error", err)
    }
  }

  // 处理已存在的事件
  for i := 0; i < cursor; i++ {
    evt := events[i]
    if evt.Type == "text_delta" {
      var text string
      if json.Unmarshal(evt.Data, &text) == nil {
        fullResponse += text
      }
    }
  }

  // 等待并处理新事件
  // 使用 Background 上下文而非 DingTalk 回调上下文，避免因回调超时导致长任务结果丢失
  for {
    _, ok := <-notifCh
    if !ok {
      // 通道关闭，run 已完成，处理剩余事件
      run.mu.Lock()
      remaining := run.events[cursor:]
      run.mu.Unlock()
      for _, evt := range remaining {
        if evt.Type == "text_delta" {
          var text string
          if json.Unmarshal(evt.Data, &text) == nil {
            fullResponse += text
          }
        }
      }
      if lastSent < len(fullResponse) {
        flushIM(fullResponse[lastSent:])
      }
      return
    }
    run.mu.Lock()
    newEvents := run.events[cursor:]
    cursor = len(run.events)
    run.mu.Unlock()
    for _, evt := range newEvents {
      switch evt.Type {
      case "text_delta":
        var text string
        if json.Unmarshal(evt.Data, &text) == nil {
          fullResponse += text
        }
      case "tool_call_start":
        if streamOutput && lastSent < len(fullResponse) {
          flushIM(fullResponse[lastSent:])
          lastSent = len(fullResponse)
        }
      case "error":
        var errMsg string
        if json.Unmarshal(evt.Data, &errMsg) == nil {
          slog.Error("PicoAgent 错误", "error", errMsg)
          if lastSent < len(fullResponse) {
            flushIM(fullResponse[lastSent:])
          }
          flushIM("发生错误: " + errMsg)
        }
      }
    }
  }
}

// executeCronJob 执行定时任务 → 启动沙箱 → 结果发到 IM
func (s *Server) executeCronJob(ctx context.Context, sb *sandbox.Manager, cronStore *scheduler.SQLCronStore, job *scheduler.CronJob) error {
  workspace := filepath.Join(config.WorkDir(), "users", job.UserID)

  mcpToken, err := store.GetMCPToken(job.UserID)
  if err != nil {
    mcpToken, _ = store.GenerateMCPToken(job.UserID)
  }

  input := agent.Message{
    Role:    agent.RoleUser,
    Content: "[定时任务] " + job.Prompt,
  }
  inputJSON, _ := json.Marshal(input)
  apiKeys := s.loadAPIKeys()
  apiKeys["PICOAGENT_CHANNEL"] = "cron"

  // 沙箱使用 Background 上下文，不设累计超时；picoagent 内部通过 perIterTimeout 控制每轮 LLM 调用
  sandboxCtx := context.Background()
  events, err := sb.Run(sandboxCtx, mcpToken, inputJSON, workspace, apiKeys, buildSkillMounts(job.UserID), job.UserID)
  if err != nil {
    s.notifyCronFailure(ctx, job, err)
    return err
  }

  var response string
  for event := range events {
    if event.Type == "text_delta" {
      var text string
      json.Unmarshal(event.Data, &text)
      response += text
    }
  }

  if s.agentIntegration != nil {
    if response != "" {
      s.sendCronResult(ctx, job, response)
    } else {
      s.notifyCronFailure(ctx, job, fmt.Errorf("任务未生成有效输出"))
    }
  }

  return nil
}

// sendCronResult 向用户推送定时任务执行结果
func (s *Server) sendCronResult(ctx context.Context, job *scheduler.CronJob, result string) {
  channels, err := store.ListUserChannelByUsername(job.UserID)
  if err != nil {
    return
  }
  for _, ch := range channels {
    if ch.Enabled && ch.Configured {
      if err := s.agentIntegration.imGateway.SendToUser(ctx, ch.Channel, job.UserID, result); err != nil {
        slog.Warn("定时任务结果发送失败", "platform", ch.Channel, "error", err, "hint", "请用户在钉钉中向 PicoAide 机器人发送一条消息以建立会话")
      }
    }
  }
}

// notifyCronFailure 向用户推送定时任务执行失败通知
func (s *Server) notifyCronFailure(ctx context.Context, job *scheduler.CronJob, jobErr error) {
  if s.agentIntegration == nil {
    return
  }
  channels, err := store.ListUserChannelByUsername(job.UserID)
  if err != nil {
    return
  }
  msg := fmt.Sprintf("⚠️ 定时任务执行失败\n任务: %s\n错误: %v", job.Prompt, jobErr)
  if len(msg) > 500 {
    msg = msg[:500] + "..."
  }
  for _, ch := range channels {
    if ch.Enabled && ch.Configured {
      if err := s.agentIntegration.imGateway.SendToUser(ctx, ch.Channel, job.UserID, msg); err != nil {
        slog.Warn("定时任务失败通知发送失败", "platform", ch.Channel, "error", err)
      }
    }
  }
}

// loadAPIKeys 从 settings 表加载 API 密钥
func (s *Server) loadAPIKeys() map[string]string {
  keys := make(map[string]string)
  engine, err := store.GetEngine()
  if err != nil {
    return keys
  }

  var settings []store.Setting
  if err := engine.Find(&settings); err != nil {
    return keys
  }

  for _, st := range settings {
    if st.Key == "model.api_key" && st.Value != "" {
      keys["default"] = st.Value
    }
  }
  return keys
}

// getConfig 从 Server config 或 settings 表获取配置值
func (s *Server) getConfig(key string) (string, bool) {
  if s.loadConfig() == nil {
    return "", false
  }
  engine, err := store.GetEngine()
  if err != nil {
    return "", false
  }
  var setting store.Setting
  has, err := engine.Where("key = ?", key).Get(&setting)
  if err != nil || !has {
    return "", false
  }
  return setting.Value, true
}

// buildSkillMounts 查询用户绑定的技能，返回沙箱只读挂载列表
func buildSkillMounts(username string) []sandbox.Mount {
  skillNames, err := store.GetUserSkills(username)
  if err != nil || len(skillNames) == 0 {
    return nil
  }
  var mounts []sandbox.Mount
  for _, name := range skillNames {
    source := findSkillSource(name)
    if source == "" {
      continue
    }
    srcPath := filepath.Clean(filepath.Join(skill.SkillsRootDir(), source, name))
    mounts = append(mounts, sandbox.Mount{
      Source: srcPath,
      Target: filepath.Join("workspace", "skills", name),
    })
  }
  return mounts
}
