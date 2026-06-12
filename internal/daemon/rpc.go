package daemon

import "encoding/json"

// ============================================================
// RPC 协议：Server ↔ Daemon，JSON 帧 + 换行符分隔
// ============================================================

type RPCMessage struct {
  Type string          `json:"type"`
  Data json.RawMessage `json:"data"`
}

// Server → Daemon
type SubmitTaskReq struct {
  TaskID   string `json:"task_id"`
  Source   string `json:"source"`
  Priority int    `json:"priority"`
  Message  string `json:"message"`
}

type TaskActionReq struct {
  TaskID string `json:"task_id"`
}

type SendMessageReq struct {
  TaskID  string `json:"task_id"`
  Message string `json:"message"`
}

// Daemon → Server
type TaskStatusEvent struct {
  TaskID string `json:"task_id"`
  Status string `json:"status"`
  Iter   int    `json:"iter,omitempty"`
}

type HeartbeatEvent struct {
  Status        string `json:"status"`
  CurrentTaskID string `json:"current_task_id,omitempty"`
  CurrentIter   int    `json:"current_iter,omitempty"`
  CurrentTool   string `json:"current_tool,omitempty"`
}

type ErrorEvent struct {
  Message string `json:"message"`
}
