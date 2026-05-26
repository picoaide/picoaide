package web

import (
  "context"
  "encoding/json"
  "log/slog"
  "path/filepath"

  "github.com/picoaide/picoaide/internal/agent"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/im"
  "github.com/picoaide/picoaide/internal/logger"
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

  // 注册 IM 渠道（根据配置）
  if s.loadConfig() != nil {
    // 钉钉 — 从 user_channels 读取每用户凭据
    dingtalkProvider := im.NewDingTalkProvider()
    engine, err := auth.GetEngine()
    if err == nil {
      var channels []auth.UserChannel
      engine.Where("channel = ? AND configured = ? AND enabled = ?", "dingtalk", true, true).Find(&channels)
      for _, ch := range channels {
        var creds map[string]string
        if json.Unmarshal([]byte(ch.Credentials), &creds) == nil {
          if creds["client_id"] != "" && creds["client_secret"] != "" {
            dingtalkProvider.AddUser(ch.Username, creds["client_id"], creds["client_secret"], creds["default_chat"])
          }
        }
      }
    }
    gw.Register(dingtalkProvider)

    // 飞书 — 从 user_channels 读取每用户凭据
    feishuProvider := im.NewFeishuProvider()
    feishuEngine, feishuErr := auth.GetEngine()
    if feishuErr == nil {
      var feishuChannels []auth.UserChannel
      feishuEngine.Where("channel = ? AND configured = ? AND enabled = ?", "feishu", true, true).Find(&feishuChannels)
      for _, ch := range feishuChannels {
        var creds map[string]string
        if json.Unmarshal([]byte(ch.Credentials), &creds) == nil {
          if creds["app_id"] != "" && creds["app_secret"] != "" {
            feishuProvider.AddUser(ch.Username, creds["app_id"], creds["app_secret"], creds["default_chat"])
          }
        }
      }
    }
    gw.Register(feishuProvider)

    // 企微 — 从 user_channels 读取每用户凭据
    wecomProvider := im.NewWeComProvider()
    wecomEngine, wecomErr := auth.GetEngine()
    if wecomErr == nil {
      var wecomChannels []auth.UserChannel
      wecomEngine.Where("channel = ? AND configured = ? AND enabled = ?", "wecom", true, true).Find(&wecomChannels)
      for _, ch := range wecomChannels {
        var creds map[string]string
        if json.Unmarshal([]byte(ch.Credentials), &creds) == nil {
          if creds["bot_id"] != "" && creds["secret"] != "" {
            wecomProvider.AddUser(ch.Username, creds["bot_id"], creds["secret"], creds["default_chat"])
          }
        }
      }
    }
    gw.Register(wecomProvider)
  }

  // 3. Cron 调度器
  engine, err := auth.GetEngine()
  if err != nil {
    return nil, err
  }
  cronStore := scheduler.NewSQLCronStore(engine)
  if err := cronStore.InitTable(); err != nil {
    slog.Warn("初始化 cron 表失败", "error", err)
  }

  cronScheduler := scheduler.NewCronScheduler(cronStore, func(ctx context.Context, job *scheduler.CronJob) error {
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
  // 查找用户
  username := msg.UserID
  logger.DebugProcess("im_message_recv",
    "platform", msg.Platform,
    "user_id", msg.UserID,
    "chat_id", msg.ChatID,
    "text_length", len(msg.Text),
  )
  if username == "" {
    return
  }

  // 保存当前会话 ID 作为该用户的默认通知渠道
  if msg.ChatID != "" {
    if existing, err := auth.GetUserChannel(username, msg.Platform); err == nil && existing != nil {
      var creds map[string]string
      if json.Unmarshal([]byte(existing.Credentials), &creds) == nil {
        if creds["default_chat"] != msg.ChatID {
          creds["default_chat"] = msg.ChatID
          updated, _ := json.Marshal(creds)
          auth.UpsertUserChannelWithCreds(username, msg.Platform, existing.Enabled, existing.Configured, string(updated))
        }
      }
    }
  }

  workspace := filepath.Join(config.WorkDir(), "users", username)
  if err := user.InitializeUser(filepath.Join(config.WorkDir(), "user-template"), workspace); err != nil {
    slog.Error("初始化用户工作目录失败", "username", username, "error", err)
    return
  }

  // 检查用户是否开启分段回复
  streamOutput := true
  if uc, err := auth.GetUserChannel(username, msg.Platform); err == nil && uc != nil {
    var creds map[string]string
    if json.Unmarshal([]byte(uc.Credentials), &creds) == nil {
      if v, ok := creds["stream"]; ok {
        streamOutput = v == "true"
      }
    }
  }

  // 通过 chatRun 启动沙箱（Web 端可同时查看实时内容）
  input := agent.Message{Role: agent.RoleUser, Content: msg.Text}
  inputJSON, _ := json.Marshal(input)
  run := s.startChatSandbox(username, msg.Text, inputJSON)

  // 订阅 chatRun 事件，转发到 IM
  notifCh, events := run.subscribe()
  defer run.unsubscribe(notifCh)

  var fullResponse string
  var lastSent int
  cursor := len(events)
  flushIM := func(text string) {
    if s.agentIntegration == nil { return }
    err := s.agentIntegration.imGateway.Send(ctx, msg.Platform, msg.ChatID, text)
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
  for {
    select {
    case <-ctx.Done():
      return
    case _, ok := <-notifCh:
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
}

// executeCronJob 执行定时任务 → 启动沙箱 → 结果发到 IM
func (s *Server) executeCronJob(ctx context.Context, sb *sandbox.Manager, store *scheduler.SQLCronStore, job *scheduler.CronJob) error {
  workspace := filepath.Join(config.WorkDir(), "users", job.UserID)

  mcpToken, err := auth.GetMCPToken(job.UserID)
  if err != nil {
    mcpToken, _ = auth.GenerateMCPToken(job.UserID)
  }

  input := agent.Message{
    Role:    agent.RoleUser,
    Content: "[定时任务] " + job.Prompt,
  }
  inputJSON, _ := json.Marshal(input)
  apiKeys := s.loadAPIKeys()

  events, err := sb.Run(ctx, mcpToken, inputJSON, workspace, apiKeys, buildSkillMounts(job.UserID), job.UserID)
  if err != nil {
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

  if response != "" && s.agentIntegration != nil {
    channels, err := auth.ListUserChannelByUsername(job.UserID)
    if err == nil {
      for _, ch := range channels {
        if ch.Enabled && ch.Configured {
          platform := ch.Channel
          if err := s.agentIntegration.imGateway.SendToUser(ctx, platform, job.UserID, response); err != nil {
            slog.Warn("定时任务通知发送失败", "platform", platform, "error", err)
          }
        }
      }
    }
  }

  return nil
}

// loadAPIKeys 从 settings 表加载 API 密钥
func (s *Server) loadAPIKeys() map[string]string {
  keys := make(map[string]string)
  engine, err := auth.GetEngine()
  if err != nil {
    return keys
  }

  var settings []auth.Setting
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
  engine, err := auth.GetEngine()
  if err != nil {
    return "", false
  }
  var setting auth.Setting
  has, err := engine.Where("key = ?", key).Get(&setting)
  if err != nil || !has {
    return "", false
  }
  return setting.Value, true
}

// buildSkillMounts 查询用户绑定的技能，返回沙箱只读挂载列表
func buildSkillMounts(username string) []sandbox.Mount {
  skillNames, err := auth.GetUserSkills(username)
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
