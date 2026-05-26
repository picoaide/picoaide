package agent

import (
  "context"
  "encoding/json"
  "time"
)

// ============================================================
// 心跳 — Engine 运行时定期向宿主办发送存活信号
// ============================================================

// StartHeartbeat 启动一个 goroutine 定期发送 heartbeat 事件。
// ctx 取消或超时时自动停止。返回的 cancel 函数可手动停止。
func StartHeartbeat(ctx context.Context, interval time.Duration, cb func(StreamEvent)) {
  go func() {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for {
      select {
      case <-ticker.C:
        cb(StreamEvent{
          Type: "heartbeat",
          Data: json.RawMessage(`{}`),
        })
      case <-ctx.Done():
        return
      }
    }
  }()
}
