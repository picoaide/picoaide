package im

import (
  "context"
  "fmt"
)

// ============================================================
// IM 网关接口（参考 PicoClaw 设计，简化版）
// ============================================================

// Message 入站消息
type Message struct {
  Platform string // dingtalk | feishu | wecom
  UserID   string // 发送者 ID
  ChatID   string // 聊天 ID
  Username string // 发送者昵称
  Text     string // 消息文本
  Raw      map[string]string // 平台原始数据
}

// SendMsg 出站消息
type SendMsg struct {
  ChatID string
  Text   string
}

// Provider IM 渠道接口
type Provider interface {
  Name() string
  Start(ctx context.Context) error
  Stop(ctx context.Context) error
  Send(ctx context.Context, msg SendMsg) error
  SendToUser(ctx context.Context, username string, text string) error
  SetOnMessage(handler func(ctx context.Context, msg Message))
}

// ============================================================
// 网关管理器
// ============================================================

type Gateway struct {
  providers map[string]Provider
  onMessage func(ctx context.Context, msg Message)
}

func NewGateway() *Gateway {
  return &Gateway{
    providers: make(map[string]Provider),
  }
}

func (g *Gateway) Register(p Provider) {
  g.providers[p.Name()] = p
  p.SetOnMessage(func(ctx context.Context, msg Message) {
    if g.onMessage != nil {
      g.onMessage(ctx, msg)
    }
  })
}

func (g *Gateway) SetOnMessage(handler func(ctx context.Context, msg Message)) {
  g.onMessage = handler
}

func (g *Gateway) Start(ctx context.Context) error {
  for name, p := range g.providers {
    if err := p.Start(ctx); err != nil {
      return fmt.Errorf("%s 启动失败: %w", name, err)
    }
  }
  return nil
}

func (g *Gateway) Stop(ctx context.Context) {
  for _, p := range g.providers {
    p.Stop(ctx)
  }
}

func (g *Gateway) GetProvider(name string) Provider {
  return g.providers[name]
}

func (g *Gateway) Send(ctx context.Context, platform, chatID, text string) error {
  p, ok := g.providers[platform]
  if !ok {
    return fmt.Errorf("不支持的平台: %s", platform)
  }
  return p.Send(ctx, SendMsg{ChatID: chatID, Text: text})
}

func (g *Gateway) SendToUser(ctx context.Context, platform, username, text string) error {
  p, ok := g.providers[platform]
  if !ok {
    return fmt.Errorf("不支持的平台: %s", platform)
  }
  return p.SendToUser(ctx, username, text)
}
