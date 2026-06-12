# PicoAide Agent 沙箱系统重设计方案

## 一、架构总览

```
                            ┌──────────────────────────────────────────────────┐
                            │            picoaide Server (Gin, :80/:443)        │
                            │                                                   │
  Clients                    │  ┌─────────┐  ┌──────────┐  ┌───────────────┐   │
  ┌───────┐  ┌───────┐      │  │ Web UI  │  │ IM 入站  │  │ CronScheduler │   │
  │浏览器  │  │IM渠道 │      │  │ (SSE)   │  │ (webhook)│  │ (cron→task)   │   │
  └───┬───┘  └───┬───┘      │  └────┬────┘  └─────┬────┘  └───────┬───────┘   │
      │          │           │       │             │               │            │
      ▼          ▼           │  ┌────┴─────────────┴───────────────┴──────┐    │
      ┌──────────────────┐   │  │          SessionHub (per-user)           │    │
      │ 浏览器扩展 (WS)   │   │  │  fanOut.Subscribe / event replay        │    │
      │ MCP agent 工具   │   │  │  Command → DaemonManager.SendRPC()       │    │
      └────────┬─────────┘   │  └────────────────┬────────────────────────┘    │
               │              │                   │ Unix socket RPC              │
               ▼              │  ┌────────────────┴────────────────────────┐    │
      ┌──────────────────┐   │  │          DaemonManager                   │    │
      │ PicoClaw Desktop │   │  │  Start/Stop/Monitor per-user daemons     │    │
      │   (WS)           │   │  │  Crash recovery (backoff 1s→5s→25s)      │    │
      └──────────────────┘   │  └────────────────┬────────────────────────┘    │
                              │                   │ start/stop/monitor           │
                              │  ┌────────────────┴────────────────────────┐    │
                              │  │          EventPersister                  │    │
                              │  │  SQLite batch insert (daemon_events)     │    │
                              │  │  Replay from last_seq                   │    │
                              │  │  Cleanup: 7d TTL, max 100k/user         │    │
                              │  └─────────────────────────────────────────┘    │
                              └──────────────────────────────────────────────────┘
                                          │
                     Unix socket bind-mount ─ /run/picoaide.sock
                                          │
            ┌─────────────────────────────┼──────────────────────────────┐
            │           Per-User Sandbox (overlayfs + CLONE_NEW*)         │
            │                                                              │
            │  ┌──────────────────────────────────────────────────────┐   │
            │  │           picoaide-daemon (long-lived process)        │   │
            │  │                                                       │   │
            │  │  ┌─────────────┐  ┌────────────┐  ┌───────────────┐  │   │
            │  │  │  TaskQueue  │  │   Engine   │  │  FileWatcher  │  │   │
            │  │  │ pause/resume│  │  (reuse)   │  │  (fsnotify)   │  │   │
            │  │  └──────┬──────┘  └─────┬──────┘  └───────┬───────┘  │   │
            │  │         │               │                  │          │   │
            │  │  ┌──────┴───────────────┴──────────────────┴──────┐   │   │
            │  │  │            EventBus (daemon 内)                 │   │   │
            │  │  │  ring buffer 1000 + writeCh(500)→server socket │   │   │
            │  │  └────────────────────────────────────────────────┘   │   │
            │  └──────────────────────────────────────────────────────┘   │
            │                                                              │
            │  Mounts: /workspace ← host:users/<username>    (bind mount)  │
            │          /run/picoaide.sock ← host:picoaide.sock (bind)      │
            │          /skills/*.skill ← host:skills/*          (ro bind)  │
            │                                                              │
            │  Network: veth → picoaide-br → 100.64.0.X/32, iptables DROP  │
            └──────────────────────────────────────────────────────────────┘
```

### 核心决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| Agent 进程 | **独立 daemon 进程**（每用户1个）在沙箱内 | 保留 CLONE_NEWPID/NET 安全隔离 |
| Server→Daemon 通信 | **Unix socket JSON-RPC**（复用 /run/picoaide.sock） | 零改动 MCP proxy，沙箱已有 bind mount |
| 事件持久化 | **EventBus 环缓冲 + 异步 SQLite 批量写** | 实时发送 + 持久化 + seq 重放 |
| 任务队列 | **在 daemon 内**，server 通过 socket RPC 下发 | 队列属于 daemon，不需要跨进程锁 |
| 文件监听 | **daemon 侧 fsnotify**，快照存 server 端 | 文件在沙箱内，daemon 直接监听 |
| 崩溃隔离 | 进程级隔离（1 用户 = 1 进程） | 单个用户崩溃不影响其他用户 |
| MCP proxy | **完全不动**（service_hub.go, mcp_service.go, browser/computer tools） | 二进制接口不变 |

---

## 二、数据库新增表

全部在 server 端 SQLite（`picoaide.db`），通过 migration `20260612_000000_daemon_tables.go` 创建。

### 2.1 `agent_daemons` — daemon 运行状态

```sql
CREATE TABLE agent_daemons (
  username           TEXT PRIMARY KEY,
  status             TEXT NOT NULL DEFAULT 'stopped',  -- running|stopped|crash
  pid                INTEGER NOT NULL DEFAULT 0,
  current_task_id    TEXT DEFAULT NULL,
  idle_since         TEXT,                             -- ISO8601, 用于空闲自动停止
  started_at         TEXT,
  stopped_at         TEXT,
  restart_count      INTEGER NOT NULL DEFAULT 0,
  last_heartbeat_at  TEXT
);
```

### 2.2 `user_tasks` — 任务队列与状态

```sql
CREATE TABLE user_tasks (
  id              TEXT PRIMARY KEY,                    -- UUID v4
  username        TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'pending',     -- pending|running|paused|completed|cancelled|failed
  source          TEXT NOT NULL DEFAULT 'web',         -- web|im|cron
  priority        INTEGER NOT NULL DEFAULT 0,          -- 越大越优先
  title           TEXT NOT NULL DEFAULT '',            -- 用户消息前80字符
  input_message   TEXT NOT NULL,                       -- JSON: agent.Message
  response        TEXT DEFAULT '',                     -- 最终完整回复
  error_message   TEXT DEFAULT '',
  iteration_count INTEGER NOT NULL DEFAULT 0,
  tool_call_count INTEGER NOT NULL DEFAULT 0,
  engine_snapshot TEXT,                                -- JSON: pause 时的完整引擎状态
  created_at      TEXT NOT NULL,
  started_at      TEXT,
  paused_at       TEXT,
  resumed_at      TEXT,
  completed_at    TEXT
);
CREATE INDEX idx_user_tasks_username ON user_tasks(username);
CREATE INDEX idx_user_tasks_status  ON user_tasks(status);
```

### 2.3 `daemon_events` — 事件日志（持久化 + 重放）

```sql
CREATE TABLE daemon_events (
  seq         INTEGER PRIMARY KEY AUTOINCREMENT,
  username    TEXT NOT NULL,
  task_id     TEXT NOT NULL,
  event_type  TEXT NOT NULL,                          -- 见 EventType 枚举
  payload     TEXT NOT NULL DEFAULT '',               -- JSON
  created_at  TEXT NOT NULL DEFAULT (datetime('now','localtime'))
);
CREATE INDEX idx_daemon_events_user_task ON daemon_events(username, task_id, seq);
```

### 2.4 `file_snapshots` — 文件状态快照（diff 用）

```sql
CREATE TABLE file_snapshots (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  username     TEXT NOT NULL,
  task_id      TEXT NOT NULL,
  snapshot_type TEXT NOT NULL,                        -- before_task|after_pause|after_complete
  file_path    TEXT NOT NULL,                         -- 相对 workspace 路径
  file_hash    TEXT NOT NULL,                         -- SHA256
  file_size    INTEGER NOT NULL,
  mod_time     TEXT NOT NULL,
  created_at   TEXT NOT NULL
);
CREATE INDEX idx_file_snapshots_task ON file_snapshots(task_id, snapshot_type);
```

---

## 三、新增包结构

```
cmd/picoaide-daemon/        新二进制：每用户后台 agent 进程
├── main.go                 入口：连接 socket → main loop → 处理 RPC
├── taskqueue.go            任务队列（FIFO + 优先级 + pause/resume）
├── filewatcher.go          fsnotify 工作区文件监听 + 变更事件
├── socket.go               Unix socket 客户端（连接 server, JSON-RPC 帧协议）
├── eventbus.go             EventBus: 环缓冲 + 异步发送到 server
└── engine.go               Engine 包装：创建/复用/快照/恢复 engine

internal/daemon/            服务端 daemon 编排
├── manager.go              DaemonManager: start/stop/restart per-user daemons
├── lifecycle.go            Daemon 生命周期: spawn, heartbeat, crash recovery
├── rpc.go                  Server 端 socket RPC 处理
└── relay.go                事件中继: daemon → EventPersister → FanOut

internal/web/
├── daemon_handlers.go      NEW: 任务提交/暂停/恢复/取消/列表 API
├── daemon_stream.go        NEW: SSE 事件流 + seq 重放
├── fanout.go               NEW: 多客户端事件分发
├── filesnapshot.go         NEW: 文件快照 + diff API
└── (现有文件不改或者小改: server.go 路由注册, integration.go cron 集成)

internal/agent/             不改，被 daemon 二进制直接 import
└── engine.go               Engine 增加两个方法: Snapshot() + Restore()

internal/scheduler/         小改
└── cron.go                 execFn 改为: 确保 daemon 运行 → SubmitTask(source=cron)
```

---

## 四、Daemon 生命周期

### 4.1 状态机

```
  user_dir_init ──→ [Stopped]
                        │
                  ensureRunning()
                        │
                   [Starting] ── 超时 ──→ [Stopped] (retry 3次)
                        │
                 socket 连接成功
                        │
                   [Running] ◄── 心跳 5s, 空闲超时(默认30min) → auto-stop
                    │   │
            crash ──┘   │ manual_stop
                    │   │
                 [Crashed] [Stopping]
                    │           │
            backoff 重试   process exited
            (1s→5s→25s)       │
                    │      [Stopped]
                    │
             [Running]
```

### 4.2 启动流程

```
DaemonManager.EnsureRunning(username):
  1. 检查 agent_daemons 表
     - status=running 且心跳正常 → 返回
     - status=stopped/crash → 执行 spawn

  2. spawnDaemon(username):
     a. 复用现有 sandbox.Manager.prepareSandbox() 准备 overlayfs + 网络命名空间
        (调用 internal/sandbox/manager.go 的现有函数，但启动的是 picoaide-daemon 而非 picoagent)
     b. exec.Command("/bin/picoaide-daemon", "--socket=/run/picoaide.sock",
        "--workspace=/workspace", "--username="+username)
     c. CLONE_NEWNS | CLONE_NEWPID | CLONE_NEWNET（不变）
     d. cmd.Start()
     e. setupNetNS(cmd.Process.Pid, username)  // 不变
     f. 等待 daemon 通过 socket 回连并注册
     g. 更新 agent_daemons: status=running, pid=X

  3. 心跳 goroutine:
     - daemon 每 5s 通过 socket 发 heartbeat RPC
     - server 更新 last_heartbeat_at
     - server 监控 goroutine: 30s 无心跳 → 标记 crashed
```

### 4.3 崩溃恢复

```go
func (m *DaemonManager) handleCrash(username string) {
    m.updateDB("agent_daemons", username, map[string]interface{}{
        "status": "crash",
    })
    // 标记所有 running/paused 任务为 failed
    m.engine.Exec(`
        UPDATE user_tasks SET status='failed',
        error_message='daemon crashed' WHERE username=? AND status IN ('running','paused')
    `, username)
    // 通知所有客户端 "daemon_disconnected"
    m.fanOut.Broadcast(username, &Event{Type: "daemon_disconnected"})

    // Backoff 重试: 1s, 5s, 25s
    backoff := time.Duration(m.restartCount*2+1) * time.Second
    if backoff > 30*time.Second { backoff = 30*time.Second }
    time.Sleep(backoff)
    // 重启（最多 3 次 / 5 分钟滑动窗口）
    m.spawnDaemon(username)
}
```

### 4.4 空闲自动停止

```
DaemonManager 定期检查:
  SELECT * FROM agent_daemons WHERE idle_since < NOW() - 30min
  如果 user_tasks 中该用户无 pending/running 任务:
    → 发送 stop RPC 到 daemon
    → 标记 status=stopped
    → daemon 进程退出
  下次有新任务时 ensureRunning() 自动重新启动

可配置: settings 表 key="agent.daemon_idle_timeout_seconds" (默认 1800)
```

---

## 五、任务状态机

### 5.1 状态转换

```
                         submit_task(msg, source)
                                   │
                          ┌────────▼──────────┐
                          │     pending        │
                          └────────┬───────────┘
                                   │ daemon dequeues
                          ┌────────▼───────────┐
          ┌───────────────│      running       │───────────────┐
          │               └──────┬────┬────────┘               │
          │                      │    │                         │
     pause_task ─→ 等待当前工具完成 │    │ cancel_task            │
          │               ┌──────►│    │◄──────┘                │
          │               │       │    │ cancelCtx → 循环退出  │
    ┌─────▼──────────┐    │       │    │                         │
    │ save snapshot  │    │       │    │                  ┌──────▼──────────┐
    │ set status=    │    │       │    │                  │   cancelled     │
    │   paused       │    │       │    │                  └─────────────────┘
    └─────┬──────────┘    │       │    │
          │               │       │    │ engine.Process err
    resume_task ─→ restore│       │    │                  ┌──────▼──────────┐
          │    snapshot   │       │    │                  │   failed        │
    ┌─────▼──────────┐    │       │    │                  │ error_message=X │
    │ engine.Process │    │       │    │                  └─────────────────┘
    │ 继续执行       ├────┘       │    │
    └─────┬──────────┘            │    │
          │                       │    │  engine.Process 正常返回
          │             ┌─────────▼────▼──┐
          └─────────────►    completed    │
                        │ response=...    │
                        └─────────────────┘
```

### 5.2 Pause 实现

```go
// daemon 内
func (tq *TaskQueue) Pause(taskID string) error {
    tq.mu.Lock()
    if tq.current == nil || tq.current.ID != taskID {
        tq.mu.Unlock()
        return ErrTaskNotFound
    }
    // 设全局暂停标志（engine 在每次迭代开始检查）
    tq.pauseRequested.Store(true)
    tq.mu.Unlock()

    // 等待 engine 到达安全点（最多 60s）
    select {
    case <-tq.pauseReady:
        // engine 已暂停，执行快照保存
    case <-time.After(60 * time.Second):
        // 超时：强制取消当前 tool context
        tq.current.Cancel()
        <-tq.pauseReady
    }

    // 保存完整引擎状态到 server 端 SQLite
    snapshot := tq.engine.Snapshot()
    tq.sendRPC("save_snapshot", TaskSnapshot{
        TaskID:        taskID,
        LLMMessages:   snapshot.Messages,
        IterCount:     snapshot.IterCount,
        Skills:        snapshot.Skills,
        CompactorState: snapshot.CompactorState,
    })

    tq.current.Status = "paused"
    tq.current.Snapshot = snapshot
    tq.current = nil
    tq.sendEvent(taskID, "task_paused", nil)

    // 唤醒等待中的下一个任务
    tq.cond.Signal()
    return nil
}
```

### 5.3 Resume 实现

```go
func (tq *TaskQueue) Resume(taskID string) error {
    // 从 DB 加载快照
    snap := tq.loadSnapshot(taskID)

    // 创建新 Engine 并恢复状态
    engine := agent.NewEngine(tq.cfg, tq.provider, tq.tools, tq.store)
    engine.SetPauseChecker(func() bool { return tq.pauseRequested.Load() })
    engine.SetOnPause(func(s *agent.EngineSnapshot) { tq.pauseReady <- s })
    engine.Restore(snap)

    // 插入恢复标记消息
    msg := &agent.Message{Role: user, Content: "[恢复执行 — 从暂停点继续]"}

    // 如果无当前任务，立即执行
    // 如果有当前任务，设为最高优先级插入队列头部
    task := &Task{ID: taskID, Status: "pending", Priority: 999}
    tq.enqueue(task)
    return nil
}
```

### 5.4 Agent Engine 的 Snapshot/Restore 接口

```go
// internal/agent/engine.go — 新增方法

type EngineSnapshot struct {
    SessionKey       string            `json:"session_key"`
    IterCount        int               `json:"iter_count"`
    Messages         []LLMMessage      `json:"messages"`          // 完整 LLM 对话历史
    Tools            []string          `json:"tools"`             // loaded tool names
    Skills           []string          `json:"skills"`            // loaded skill names
    FrozenSystem     string            `json:"frozen_system"`     // cached system prompt
    PendingAdditions []string          `json:"pending_additions"` // pending system additions
    ProviderType     string            `json:"provider_type"`     // "deepseek" | "openai" | ...
    ModelID          string            `json:"model_id"`
    CompactorState   json.RawMessage   `json:"compactor_state"`   // compression state (nullable)
}

func (e *Engine) Snapshot() *EngineSnapshot {
    e.mu.RLock()
    defer e.mu.RUnlock()
    // 导出所有可变状态
    return &EngineSnapshot{
        SessionKey:       e.sessionKey,
        IterCount:        e.iterCount,
        Messages:         copyMessages(e.llmMessages),
        Skills:           e.skills,
        FrozenSystem:     e.frozenSystem,
        PendingAdditions: e.pendingAdditions,
        ProviderType:     e.provider.ProviderType(),
        ModelID:          e.config.ModelID,
    }
}

func (e *Engine) Restore(s *EngineSnapshot) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.sessionKey = s.SessionKey
    e.iterCount = s.IterCount
    e.llmMessages = s.Messages
    e.skills = s.Skills
    e.frozenSystem = s.FrozenSystem
    e.pendingAdditions = s.PendingAdditions
    // provider/tools/config 由外部传入，不从这里恢复
}
```

---

## 六、事件持久化

### 6.1 事件类型

```go
const (
    EvtTaskQueued     = "task_queued"
    EvtTaskStarted    = "task_started"
    EvtTaskPaused     = "task_paused"
    EvtTaskResumed    = "task_resumed"
    EvtTaskCompleted  = "task_completed"
    EvtTaskCancelled  = "task_cancelled"
    EvtTaskFailed     = "task_failed"
    EvtTextDelta      = "text_delta"
    EvtToolCallStart  = "tool_call_start"
    EvtToolCallEnd    = "tool_call_end"
    EvtReasoning      = "reasoning"
    EvtError          = "error"
    EvtProgress       = "progress"
    EvtFileChanged    = "file_changed"
    EvtDaemonConnected   = "daemon_connected"
    EvtDaemonDisconnected = "daemon_disconnected"
)
```

### 6.2 写入路径

```
Daemon Engine → EventBus.Emit(type, data)
                  │
       ┌──────────┼──────────┐
       │          │          │
  ring buffer    writeCh     → sendRPC(server, event)
  (内存 1000)   (cap 500)         │
       │          │               │
  task 订阅者   async goroutine    ▼
  (实时 SSE)   batch insert    Server.EventPersister.Persist(event)
                SQLite               │
                    │       ┌────────┼────────┐
                    │    SQLite   FanOut    IM转发
                    │   INSERT  Broadcast  (text_delta)
                    │
              daemon_events 表
```

### 6.3 客户端重连回放

```
GET /api/user/events/stream?since_seq=42&task_id=xxx

Phase 1: SQLite 重放
  SELECT seq, event_type, payload FROM daemon_events
  WHERE username = ? AND seq > 42 AND (task_id = ? OR ? = '')
  ORDER BY seq ASC
  → 通过 SSE 逐条发送

Phase 2: 实时订阅
  fanOut.Subscribe(username, taskID)
  → 接收后续 live 事件
  → SSE 转发
```

### 6.4 性能优化

- **批量写入**: 200ms 间隔或 50 条事件批量 INSERT，减少 SQLite 事务开销
- **环缓冲**: 内存保存最近 1000 条事件，活跃客户端从环缓冲读，不查 SQLite
- **自动清理**: 每 24h 清理 7 天前的事件，单用户最多保留 100000 条

---

## 七、多客户端 FanOut

```
                    ┌──────────────┐
                    │  SessionHub  │  (per user)
                    │              │
                    │  clients: [  │
                    │    web-tab-1 │── SSE ──→ 浏览器
                    │    web-tab-2 │── SSE ──→ 另一个标签页
                    │    dingtalk  │── webhook → 钉钉
                    │    extension │── WS ──→ 浏览器扩展
                    │  ]           │
                    │              │
                    │ commands → DaemonManager.SendRPC()
                    │ events   ← EventPersister → broadcast
                    └──────────────┘

非阻塞 fanOut:
  for client := range clients {
      select {
      case client.ch <- event:
      default: // 客户端太慢，跳过（可通过 replay 补回）
      }
  }
```

---

## 八、Cron 定时任务集成

### 8.1 流程

```
CronScheduler 触发:
  1. EnsureRunning(username)  // daemon 未运行则启动
  2. 构造任务:
     Task{
       Source: "cron",
       Priority: 0,  // 与手动任务同等优先级
       Message: { Role: "user", Content: "[定时任务] " + job.Prompt }
     }
  3. daemonManager.SendRPC(username, RPC{Type:"submit_task", Data:task})
  4. 不等待完成，不阻塞 cron ticker
```

### 8.2 特殊处理

- 如果 daemon 正在执行另一个 cron 任务 → 排队等待
- Cron 任务失败不重试（cron 本身会在下一个周期再次触发）
- Cron 任务和手动任务共享队列，用户可在 UI 上看到
- 任务面板中 cron 任务显示 ⏰ 图标和 `[定时]` 前缀

---

## 九、文件系统集成

### 9.1 文件监听

```go
// daemon 内: cmd/picoaide-daemon/filewatcher.go
func NewFileWatcher(workspace string, callback func(FileChange)) *FileWatcher {
    w := &FileWatcher{
        watcher:  fsnotify.NewWatcher(),
        debounce: 200 * time.Millisecond,
        pending:  make(map[string]time.Time),
        callback: callback,
    }
    // 递归监听 workspace 下所有目录
    filepath.Walk(workspace, func(path string, info os.FileInfo, err error) error {
        if info.IsDir() { w.watcher.Add(path) }
        return nil
    })
    return w
}
// 200ms 消抖后通过 EventBus.Emit("file_changed", ...) 发送
```

### 9.2 快照

```
任务开始前: TakeSnapshot(taskID, "before_task")
暂停时:     TakeSnapshot(taskID, "after_pause")
完成时:     TakeSnapshot(taskID, "after_complete")

快照内容: 遍历 workspace → [{path, size, mod_time, sha256}]
存储: file_snapshots 表
```

### 9.3 Diff 对比

```
GET /api/user/files/diff?task_id=xxx&snapshot_type=before_task_vs_after_complete

返回:
{
  "added":    [{path:"new_file.py", size:1024}],
  "modified": [{path:"main.py", old_size:2048, new_size:4096}],
  "deleted":  [{path:"old.txt"}]
}
```

### 9.4 文件树 API

```
GET /api/user/files/tree?task_id=xxx

返回递归树结构:
{
  "name": "workspace",
  "children": [
    { "name": "src", "type": "directory", "children": [...] },
    { "name": "report.md", "type": "file", "size": 2048, "changed": true }
  ]
}
```

---

## 十、API 端点变更

### 新增端点

```
# 任务管理
POST /api/user/task/submit        Body:{message, priority?} → {task_id, status}
POST /api/user/task/pause         Body:{task_id}
POST /api/user/task/resume        Body:{task_id}
POST /api/user/task/cancel        Body:{task_id}
GET  /api/user/task/detail        Query:?task_id=xxx → {task详情+events最新}
GET  /api/user/task/list          Query:?limit=50&offset=0 → {tasks[], total}
GET  /api/user/task/messages      Query:?task_id=xxx → {messages[]}
POST /api/user/task/message       Body:{task_id, message}  # 任务执行中注入消息

# 事件流（替代原 chat/stream）
GET  /api/user/events/stream      SSE, Query:?since_seq=N&task_id=xxx

# 文件快照
GET  /api/user/files/tree         实时文件树
GET  /api/user/files/diff         Query:?task_id=xxx&before=X&after=Y
GET  /api/user/files/snapshots    Query:?task_id=xxx

# Daemon 管理
GET  /api/user/daemon/status      → {running, task_count, queue_length, uptime}
POST /api/user/daemon/restart
POST /api/user/daemon/stop

# 管理端
GET  /api/admin/daemons           超管查看所有 daemon 状态
GET  /api/admin/tasks             超管查看所有任务
GET  /api/admin/tasks/stats       统计
POST /api/admin/daemons/restart-all
```

### 替换的端点

| 旧端点 | 新端点 |
|--------|--------|
| `POST /api/user/chat/send` | `POST /api/user/task/submit` |
| `GET /api/user/chat/stream` | `GET /api/user/events/stream` |
| `POST /api/user/chat/stop` | `POST /api/user/task/cancel` |
| `GET /api/user/chat/active` | `GET /api/user/daemon/status` |
| `GET /api/user/chat/history` | `GET /api/user/task/list` |

### 保持不变

所有认证、用户管理、组管理、技能管理、配置管理、共享文件夹、MCP SSE/WS、文件上传下载、Email 等端点完全不变。

---

## 十一、关键风险与缓解

| 风险 | 严重度 | 缓解措施 |
|------|--------|---------|
| Daemon 进程泄漏 | 中 | 空闲超时自动停止 + Manager 定期巡检僵尸进程 |
| Socket 断开事件丢失 | 中 | daemon 侧本地 SQLite 缓冲 → 重连后批量补发 |
| Pause 时 tool 在中间状态 | 低 | 等 tool 完成（60s 超时后强制 cancel） |
| 2 个客户端同时 pause | 低 | Daemon 内 TaskQueue 互斥锁串行化 |
| Engine Snapshot 太大 | 低 | 限制 LLMMessages 当前对话轮数（同现有 compact） |
| 内存占用 | 低 | 每 daemon ~50MB，200 用户 ≈ 10GB，可配置 cgroup 限制 |

---

## 十二、实施步骤

### Phase 1: 数据层（第 1-2 周）
1. 创建 migration `20260612_000000_daemon_tables.go`（4 张表）
2. 实现 `EventPersister`（internal/daemon/relay.go）
3. 实现文件快照存储读写

### Phase 2: Daemon 进程（第 2-3 周）
4. 实现 `cmd/picoaide-daemon/main.go` + socket + taskqueue + eventbus + filewatcher
5. 实现 `internal/daemon/manager.go`（spawn/monitor/crash recovery）
6. Engine 增加 `Snapshot()` / `Restore()` / `SetPauseChecker()` 方法
7. 集成现有 sandbox.Manager 的 overlayfs/netns 准备逻辑

### Phase 3: Web 层（第 3-4 周）
8. 实现 `daemon_handlers.go`（全部任务管理 API）
9. 实现 `daemon_stream.go`（SSE + seq 重放）
10. 实现 `fanout.go`（多客户端事件分发）
11. 实现 `filesnapshot.go`（文件树 + diff API）
12. 修改 cron integration: `execFn` 改为 task submit

### Phase 4: 前端（第 4-5 周）
13. 任务面板 UI（左侧队列+历史+状态）
14. 聊天窗口 UI（右侧 SSE 事件流）
15. 文件树面板（可折叠，差异高亮）
16. Daemon 状态栏（页面底部，所有页面可见）
17. 暂停/恢复/取消 控件
18. 时间线回放功能

### Phase 5: 测试与切换（第 5-6 周）
19. 全量集成测试
20. 与旧系统并行运行（feature flag `agent.v2_enabled`）
21. 灰度切换 → 全量 → 退役旧代码

---

## 十三、测试策略

### Go 单元测试
- `internal/daemon/*_test.go`: DaemonManager 生命周期 + 崩溃恢复
- `cmd/picoaide-daemon/*_test.go`: TaskQueue 状态机 + EventBus 缓冲
- `internal/web/daemon_*_test.go`: API handler + SSE 流
- `internal/agent/engine_test.go`: Snapshot/Restore 往返测试

### 集成测试
- 用户提交任务 → daemon 启动 → 任务完成 → 事件持久化 → 重放
- 多客户端并发 → fanOut 分发 → 重连不回放重复事件
- Cron 触发 → daemon 未运行 → 自动启动 → 任务提交
- Pause/Resume 往返: 快照保存 → engine 重建 → 任务继续
- Daemon 崩溃 → 自动重启 → 任务状态正确

### Web UI E2E
- 通过 browser MCP 代理模拟用户操作
