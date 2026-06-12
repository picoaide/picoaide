# PicoAide Agent 沙箱系统重设计方案（修正版 v2）

## 变更概览

| 模块 | 变更类型 | 说明 |
|------|---------|------|
| `cmd/picoaide-daemon/` | 新增 | 每用户长期 daemon 进程，在沙箱内运行 |
| `internal/daemon/` | 新增 | 服务端 daemon 编排（生命周期、RPC、事件中继） |
| `internal/sandbox/manager.go` | 小改 | `prepareSandbox` 改为启动 `picoaide-daemon` 而非 `picoagent` |
| `internal/agent/engine.go` | 新增 2 方法 | `Snapshot()` + `Restore()` |
| `internal/web/chat_stream.go` | 替换 | 被 daemon_handlers.go + daemon_stream.go + fanout.go 替代 |
| `internal/web/integration.go` | 小改 | cron execFn → daemonManager.SendRPC |
| `internal/web/server.go` | 小改 | 路由注册 |
| `manage/templates/chat.html` | 重写 | 两栏布局（任务面板 + 聊天窗） |
| `manage/modules/chat.js` | 重写 | 改为 task API + SSE events/stream |
| `manage/modules/taskpanel.js` | 新增 | 左栏任务面板（队列/历史/状态） |
| `manage/modules/files.js` | 小改 | 增加 diff 模式 |
| `common.css` | 新增样式 | 任务面板、状态栏、文件树、diff 视图 |
| SQLite 表 | 只加 1 张 | `agent_daemons`（心跳追踪） |

**不动**：所有认证、用户管理、组管理、技能管理、MCP proxy、文件管理 API、Email、定时任务 DB 表。

---

## 一、存储设计：文件优先，不污染主 SQLite

### 原则

主 SQLite (`picoaide.db`) 只存储系统配置和用户元数据。所有运行时数据用用户目录下的**压缩 JSON 文件**存储。

### 目录结构

```
users/<username>/daemon/
├── state.json                    # Daemon 运行状态（心跳时间、PID、当前任务）
├── tasks.json                    # 任务索引：[{id, status, title, created_at, ...}]
├── tasks/
│   └── <task_id>/
│       ├── events.jsonl.gz       # gzip 压缩的事件日志（append-only JSONL）
│       ├── snapshot.json         # 暂停时的 Engine 完整快照
│       └── files.json            # 文件状态快照（path → sha256/size/mtime）
```

### 唯一的新 SQLite 表

```sql
-- agent_daemons: 心跳追踪，1 行/用户（~200 行总量）
CREATE TABLE agent_daemons (
  username           TEXT PRIMARY KEY,
  status             TEXT NOT NULL DEFAULT 'stopped',  -- running|stopped|crash
  current_task_id    TEXT DEFAULT NULL,
  last_heartbeat_at  TEXT
);
```

### 事件写入流程

```
Daemon Engine 回调
  → EventBus.Emit(type, data)
    → ringBuffer.Push(event)           // 内存 1000 条环，实时 SSE
    → writeCh <- event                 // cap 500 的异步写入通道
      → bgWriter goroutine:
        → 打开 tasks/<task_id>/events.jsonl.gz
        → gzip.Writer.Write(event_json + "\n")
        → 每 200ms 或 50 条事件 flush+sync

重放：
  GET /api/user/events/stream?task_id=xxx&since_seq=N
  → 找到 tasks/<task_id>/events.jsonl.gz
  → gzip.Reader 流式读取，跳过 seq ≤ N 的行
  → SSE 逐条发送
```

### 写入模式

- **打开**: `os.OpenFile(eventsFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)`
- **压缩**: 每个事件 `gzip.Writer.Write(line)`（比原始 JSONL 压缩约 10:1）
- **Flush**: 每 200ms 或 50 条事件执行 `gzip.Flush()` + `file.Sync()` 确保持久化

---

## 二、架构总览（不变的核心设计）

```
┌──────────────────────────────────────────────────────────────┐
│                  picoaide Server (Gin, :80/:443)             │
│                                                               │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌─────────────┐  │
│  │ Web UI   │  │ IM 入站  │  │ Cron     │  │ Admin API   │  │
│  │ (SSE)    │  │ (webhook)│  │ Scheduler│  │             │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └──────┬──────┘  │
│       │              │             │               │          │
│  ┌────┴──────────────┴─────────────┴───────────────┴────┐    │
│  │              SessionHub (per-user)                     │    │
│  │  Subscribe(taskID, lastSeq) → events channel + replay  │    │
│  │  SendCommand(cmd) → DaemonManager.SendRPC()            │    │
│  └───────────────────────┬───────────────────────────────┘    │
│                          │                                     │
│  ┌───────────────────────┴───────────────────────────────┐    │
│  │              DaemonManager                              │    │
│  │  Start/Stop/Monitor per-user daemon processes           │    │
│  │  Heartbeat tracking (picoaide.db agent_daemons)         │    │
│  │  Crash recovery (backoff 1s→5s→25s)                    │    │
│  └───────────────────────┬───────────────────────────────┘    │
│                          │ start/stop/monitor                  │
└──────────────────────────┼────────────────────────────────────┘
                           │
          ┌────────────────┴────────────────┐
          │    /run/picoaide.sock             │  Unix socket bind mount
          │    (JSON-RPC 帧协议)              │
          └────────────────┬────────────────┘
                           │
┌──────────────────────────┼────────────────────────────────────┐
│   Per-User Sandbox (overlayfs + CLONE_NEWPID/NET/NET)         │
│                                                                │
│   ┌────────────────────────────────────────────────────────┐  │
│   │            picoaide-daemon (长期进程)                     │  │
│   │                                                          │  │
│   │  ┌───────────┐  ┌──────────┐  ┌──────────────────────┐  │  │
│   │  │ TaskQueue │  │  Engine  │  │     FileWatcher      │  │  │
│   │  │ pause/    │  │ (reuse)  │  │  (fsnotify, 200ms    │  │  │
│   │  │ resume    │  │          │  │   debounce)          │  │  │
│   │  └─────┬─────┘  └────┬─────┘  └──────────┬───────────┘  │  │
│   │        │              │                    │              │  │
│   │  ┌─────┴──────────────┴────────────────────┴──────────┐  │  │
│   │  │                  EventBus                            │  │  │
│   │  │  ring buffer(1000) + writeCh(500) → gzip JSONL 文件 │  │  │
│   │  └─────────────────────────────────────────────────────┘  │  │
│   └────────────────────────────────────────────────────────┘  │
│                                                                │
│   Mounts: /workspace ← host:users/<username>    (bind)        │
│           /run/picoaide.sock ← host:picoaide.sock (bind)      │
│   Network: veth → picoaide-br → 100.64.0.X, iptables DROP    │
└────────────────────────────────────────────────────────────────┘
```

### 关键设计对照

| 维度 | 架构师1（退回） | 架构师2（通过） | 最终方案 |
|------|:-:|:-:|------|
| Agent 进程 | goroutine 同进程 | 独立 daemon 二进制 | ✅ 架构师2：保留安全隔离 |
| 通信方式 | 函数调用 | Unix socket RPC | ✅ 架构师2：不改 MCP proxy |
| 事件持久化 | SQLite daemon_events | SQLite daemon_events | ✅ **文件优先**：gzip JSONL |
| 任务队列 | server goroutine 池 | daemon 内 | ✅ 架构师2：队列归 daemon |
| 文件监听 | server 侧 | daemon 侧 | ✅ 架构师2：沙箱内直接监听 |
| 事件缓冲 | ring + async writeCh | ring + 批量 INSERT | ✅ 融合：ring + writeCh → gzip |
| 崩溃隔离 | recover 同进程 | 进程级 | ✅ 架构师2 |

---

## 三、数据库：仅加 1 张表

```sql
-- migration: internal/store/migrations/20260612_000000_daemon_heartbeat.go
CREATE TABLE IF NOT EXISTS agent_daemons (
  username           TEXT PRIMARY KEY,
  status             TEXT NOT NULL DEFAULT 'stopped',   -- running|stopped|crash|stopping
  current_task_id    TEXT DEFAULT NULL,                 -- 当前执行的任务 ID
  last_heartbeat_at  TEXT NOT NULL DEFAULT ''           -- ISO8601，daemon 每 5s 更新
);
```

**为什么加这一张表**：
- Heartbeat 需要原子 CAS 更新 (`UPDATE ... WHERE status=running AND last_heartbeat_at < ?`)
- 超管需要查询所有 daemon 状态（`SELECT * FROM agent_daemons`）
- 200 用户 × 1 行 = 可忽略的 DB 负担

**其余全部文件存储**：任务索引、事件、快照全在 `users/<username>/daemon/` 下。

---

## 四、新增包与文件

```
cmd/picoaide-daemon/           新二进制
├── main.go                    入口，连接 socket，main loop
├── taskqueue.go               任务队列 + pause/resume 状态机
├── eventbus.go                EventBus: ring + writeCh + gzip writer
├── filewatcher.go             fsnotify 文件监听
├── engine.go                  Engine 包装：创建/快照/恢复
├── socket.go                  Unix socket 客户端（连接 server）
└── store.go                   文件存储：tasks.json, events.jsonl.gz, snapshot.json

internal/daemon/               服务端编排（新包）
├── manager.go                 DaemonManager: spawn/monitor/crash-recovery
├── lifecycle.go               生命周期：ensureRunning, heartbeat, idle-stop
├── rpc.go                     Server 端 socket RPC 处理
├── relay.go                   事件中继：daemon event → SessionHub fanOut
└── manager_test.go

internal/web/                  替换 chat_stream.go，新增 daemon 相关
├── daemon_handlers.go         NEW: 全套任务 API
├── daemon_stream.go           NEW: SSE events/stream + seq 重放
├── fanout.go                  NEW: per-user 多客户端事件分发
├── filesnapshot.go            NEW: 文件快照 + diff API（读 daemon/tasks/<id>/files.json）
├── chat_stream.go             REMOVE（被 daemon_* 替代）
├── server.go                  小改：路由注册
└── integration.go             小改：cron execFn

internal/agent/engine.go       新增 2 方法
├── Snapshot() *EngineSnapshot
└── Restore(s *EngineSnapshot)

internal/sandbox/manager.go    小改
└── prepareSandbox: 启动 picoaide-daemon 而非 picoagent
```

---

## 五、核心数据结构

### 5.1 EventBus（daemon 内）

```go
// cmd/picoaide-daemon/eventbus.go
type EventBus struct {
    taskID    string
    eventsDir string              // users/<username>/daemon/tasks/<task_id>/
    mu        sync.Mutex
    seq       int64
    ring      *ring.Ring          // 1000 events, for active SSE subscriber
    subs      map[string]chan *Event
    writeCh   chan *Event         // cap 500, async persistence
    gzipFile  *os.File            // events.jsonl.gz
    gzipW     *gzip.Writer
}

func (b *EventBus) Emit(typ string, data interface{}) {
    event := &Event{
        TaskID: b.taskID,
        Seq:    atomic.AddInt64(&b.seq, 1),
        Type:   typ,
        Data:   data,
        Time:   time.Now(),
    }
    // 1. 环缓冲（实时 SSE）
    b.ring.Value = event
    b.ring = b.ring.Next()
    // 2. FanOut 订阅者
    for _, ch := range b.subs {
        select { case ch <- event: default: }
    }
    // 3. 异步文件写入
    select { case b.writeCh <- event: default: }
}

func (b *EventBus) bgWriter() {
    ticker := time.NewTicker(200 * time.Millisecond)
    var batch []*Event
    for {
        select {
        case evt := <-b.writeCh:
            batch = append(batch, evt)
            if len(batch) >= 50 { b.flush(batch); batch = nil }
        case <-ticker.C:
            if len(batch) > 0 { b.flush(batch); batch = nil }
        }
    }
}

func (b *EventBus) flush(events []*Event) {
    for _, e := range events {
        data, _ := json.Marshal(e)
        b.gzipW.Write(append(data, '\n'))
    }
    b.gzipW.Flush()
    b.gzFile.Sync()
}
```

### 5.2 文件存储格式

**tasks.json**（轻量索引，只存元数据）：
```json
[
  {
    "id": "task_a1b2c3d4",
    "status": "completed",
    "source": "web",
    "title": "帮我写一个Python脚本",
    "iteration_count": 15,
    "tool_call_count": 8,
    "created_at": "2026-06-12T14:00:00",
    "completed_at": "2026-06-12T14:05:30"
  }
]
```

**events.jsonl.gz**（gzip 压缩，每行一个 JSON 事件）：
```
{"task_id":"task_a1b2c3d4","seq":1,"type":"task_started","data":{...},"time":"..."}
{"task_id":"task_a1b2c3d4","seq":2,"type":"reasoning","data":"...","time":"..."}
{"task_id":"task_a1b2c3d4","seq":3,"type":"tool_call_start","data":{"name":"read_file","input":{"path":"/workspace/main.py"}},"time":"..."}
{"task_id":"task_a1b2c3d4","seq":4,"type":"tool_call_end","data":{"name":"read_file","output":"...","duration_ms":120},"time":"..."}
{"task_id":"task_a1b2c3d4","seq":5,"type":"text_delta","data":"这是生成的代码...","time":"..."}
```

**snapshot.json**（暂停时保存的 Engine 状态）：
```json
{
  "session_key": "task_a1b2c3d4",
  "iter_count": 5,
  "messages": [...],
  "skills": ["web-browsing"],
  "frozen_system": "...",
  "pending_additions": [],
  "model_id": "deepseek-v4-flash",
  "provider_type": "deepseek"
}
```

**files.json**（任务前后的文件状态，用于 diff）：
```json
[
  {"path": "main.py", "sha256": "abc123...", "size": 2048, "mod_time": "2026-06-12T14:00:00"},
  {"path": "report.md", "sha256": "def456...", "size": 512, "mod_time": "2026-06-12T14:00:00"}
]
```

### 5.3 state.json

```json
{
  "username": "yangting",
  "status": "running",
  "pid": 12345,
  "current_task_id": "task_a1b2c3d4",
  "current_task_iter": 12,
  "current_task_tool": "write_file",
  "started_at": "2026-06-12T14:00:00",
  "last_heartbeat_at": "2026-06-12T14:05:00"
}
```

### 5.4 Engine Snapshot/Restore

```go
// internal/agent/engine.go — 新增

type EngineSnapshot struct {
    SessionKey       string          `json:"session_key"`
    IterCount        int             `json:"iter_count"`
    Messages         []LLMMessage    `json:"messages"`
    Skills           []string        `json:"skills"`
    FrozenSystem     string          `json:"frozen_system"`
    PendingAdditions []string        `json:"pending_additions"`
    ProviderType     string          `json:"provider_type"`
    ModelID          string          `json:"model_id"`
}

func (e *Engine) Snapshot() *EngineSnapshot {
    e.mu.RLock()
    defer e.mu.RUnlock()
    return &EngineSnapshot{
        SessionKey:       e.sessionKey,
        IterCount:        e.iterCount,
        Messages:         e.copyMessages(),
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
}
```

---

## 六、Daemon 生命周期

```
  [Stopped] ── ensureRunning() ──→ [Starting]
                                       │
                                spawn daemon 进程
                                       │
                                socket 连接 + handshake
                                       │
                                   [Running] ◄── heartbeat 5s
                                    │      │
                    manual stop ────┤      ├── crash detected (>30s no heartbeat)
                                    │      │
                                [Stopping]  │
                                    │      [Crashed]
                            SIGTERM → wait 5s │
                            → SIGKILL    backoff 重试 (1s→5s→25s, max 3次)
                                    │      │
                                [Stopped]   │
                                       │    │
                                  idle timeout └──→ [Running] (auto-restart)
                                  (30min 无任务无连接)

空闲自动停止:
  DaemonManager 定期检查 agent_daemons 表
  → 如果 current_task_id 为空且 tasks.json 无 pending 任务
  → 且 last_heartbeat_at > 30min 前
  → sendRPC("stop") → daemon 退出 → status=stopped
```

### 6.1 心跳与崩溃检测

```go
// daemon 侧，每 5s
func (d *Daemon) heartbeat() {
    state := d.readState()  // state.json
    state.LastHeartbeatAt = time.Now().ISO()
    d.writeState(state)
    d.sendRPC("heartbeat", state)     // 通知 server
}

// server 侧，每 10s
func (dm *DaemonManager) monitorHealth() {
    // SELECT * FROM agent_daemons WHERE status='running'
    //   AND last_heartbeat_at < datetime('now', '-30 seconds')
    // → 标记为 crashed，标记当前任务为 failed
    // → 通知客户端 daemon_disconnected
    // → backoff 重启
}
```

---

## 七、任务状态机（不变）

```
submit_task → [pending] → daemon picks → [running]
                                            │
                         pause ─→ 等当前 tool 完成(60s超时)
                                            │
                              [paused] ←── snapshot 存文件
                                            │
                         resume ─→ restore snapshot → [running]
                                            │
                         cancel ─→ cancelCtx → [cancelled]
                                            │
                         complete ─→ [completed]
                                            │
                         error ─→ [failed]
```

### Pause 流程

1. Server 收到 `POST /api/user/task/pause {task_id}`
2. 发 RPC `pause_task(task_id)` 到 daemon
3. Daemon 设 `pauseRequested = true`（engine 在下一次迭代开始时检查）
4. Engine 到达安全点 → 调用 `engine.Snapshot()`
5. 写 `tasks/<task_id>/snapshot.json`
6. 更新 `tasks.json` 中任务状态为 `paused`
7. 发 `task_paused` 事件到 server

### Resume 流程

1. Server 收到 `POST /api/user/task/resume {task_id}`
2. 发 RPC `resume_task(task_id)` 到 daemon
3. Daemon 读 `tasks/<task_id>/snapshot.json`
4. 创建新 Engine 实例 → `engine.Restore(snapshot)`
5. 插入恢复消息 `"[恢复执行 — 从暂停点继续]"`
6. 任务进入队列（如果 daemon 空闲则立即执行）

---

## 八、API 端点

### 新增（替代原 chat 端点）

```
POST /api/user/task/submit         提交任务
  Body: {"message":"帮我...", "priority":0}
  Response: {"task_id":"task_xxx", "status":"pending"}

POST /api/user/task/pause          Body: {"task_id":"task_xxx"}
POST /api/user/task/resume         Body: {"task_id":"task_xxx"}
POST /api/user/task/cancel         Body: {"task_id":"task_xxx"}
POST /api/user/task/message        Body: {"task_id":"task_xxx", "message":"用这个文件"}

GET  /api/user/task/detail         Query: ?task_id=xxx → {task, recent_events[]}
GET  /api/user/task/list           Query: ?limit=50&offset=0&status=running
GET  /api/user/task/events         Query: ?task_id=xxx&since_seq=N
  → SSE stream: event 逐条推送

GET  /api/user/events/stream       SSE, Query: ?since_seq=N&task_id=xxx
  → 断线重连专用流

GET  /api/user/daemon/status       → {running, current_task_id, uptime, queue_length}
POST /api/user/daemon/restart
POST /api/user/daemon/stop

# 文件快照（新增）
GET  /api/user/files/tree          → 实时文件树（已有 API，不改）
GET  /api/user/files/diff          Query: ?task_id=xxx → {added[], modified[], deleted[]}
GET  /api/user/files/snapshots     Query: ?task_id=xxx → [{type, file_count, created_at}]

# 管理端
GET  /api/admin/daemons            超管查看所有 daemon
GET  /api/admin/tasks              超管查看所有任务
GET  /api/admin/tasks/stats        统计
POST /api/admin/daemons/stop-all
```

### 替换映射

| 旧 | 新 |
|----|-----|
| `POST /api/user/chat/send` | `POST /api/user/task/submit` |
| `GET /api/user/chat/stream` | `GET /api/user/events/stream` |
| `POST /api/user/chat/stop` | `POST /api/user/task/cancel` |
| `GET /api/user/chat/active` | `GET /api/user/daemon/status` |
| `GET /api/user/chat/history` | `GET /api/user/task/list` + `GET /api/user/task/events` |

---

## 九、UI 改造（基于现有代码）

### 核心原则
- 不引入任何框架，保持纯 vanilla JS + 现有 SPA 架构
- 复用 `common.js` 工具函数 (`api`, `showMsg`, `$`, `confirmModal` 等)
- 复用 `common.css` CSS 变量和组件样式
- 复用现有的 `manage.js` 路由机制（path-based, dynamic import）

### 文件变更清单

| 文件 | 变更 | 说明 |
|------|------|------|
| `manage/templates/chat.html` | **重写** | 两栏布局：左 320px 任务面板 + 右聊天窗 |
| `manage/modules/chat.js` | **重写** | 改为 task/events API + SSE 流 |
| `manage/modules/taskpanel.js` | **新增** | 左栏任务面板（独立模块，chat.js 加载时初始化） |
| `manage/templates/files.html` | **小改** | 增加 diff 模式区域 |
| `manage/modules/files.js` | **小改** | 增加 diff 加载逻辑 |
| `manage/index.html` | **小改** | 导航栏加 `data-section="tasks"` 或复用 `chat` tab |
| `common.css` | **新增 ~120 行** | 任务面板、状态栏、diff 视图样式 |

### 新聊天页布局（`chat.html`）

```
┌──────────────────────────────────────────────────────────┐
│ AI 对话    [上下文 ████░░░░ 60%]           [⏸] [⏹]    │  ← toolbar
├──────────────┬───────────────────────────────────────────┤
│              │                                           │
│  任务面板     │         聊天区域                          │
│  (320px)     │  ┌───────────────────────────────────┐   │
│              │  │ message bubble                     │   │
│  ┌────────┐  │  │ tool card (expandable)             │   │
│  │ 当前任务│  │  │ reasoning block (expandable)       │   │
│  │ 进行中  │  │  │ text_delta streaming...            │   │
│  │ 5/15步  │  │  │                                    │   │
│  │ [⏸暂停] │  │  └───────────────────────────────────┘   │
│  └────────┘  │                                           │
│              │                                           │
│  ┌────────┐  │  ┌───────────────────────────────────┐   │
│  │ 队列    │  │  │ [输入框                    ] [发送]│   │
│  │ · task2 │  │  └───────────────────────────────────┘   │
│  └────────┘  │                                           │
│              │                                           │
│  ┌────────┐  │                                           │
│  │ 历史     │  │                                           │
│  │ · task0 │  │                                           │
│  │ ✓ 完成  │  │                                           │
│  └────────┘  │                                           │
└──────────────┴───────────────────────────────────────────┘
│ 🟢 Agent 运行中 | yangting | 已运行 12 分钟 | 当前: write_file │  ← status bar
└──────────────────────────────────────────────────────────┘
```

### 状态栏（全局可见，所有页面底部）

```html
<div id="daemon-status-bar">
  <span id="daemon-dot" class="status-dot running"></span>
  <span id="daemon-text">Agent 运行中 | 当前任务: 生成Python脚本 | 第 12/50 步 | 工具: write_file</span>
  <span id="daemon-queue-badge">队列: 1</span>
  <button id="daemon-stop-btn" class="btn btn-sm btn-outline">停止</button>
</div>
```

状态栏在 `manage/index.html` 中作为固定底栏，通过 `common.css` 全局样式。`chat.js` 和 `taskpanel.js` 更新它。

### 任务面板（`taskpanel.js`）

独立 JS 模块，在 `chat.js` 的 `init()` 中加载。职责：
1. 调用 `GET /api/user/task/list` 获取任务
2. 渲染三个区域：当前任务、队列、最近历史
3. 点击历史任务 → 切换右侧聊天到该任务的事件流
4. 暂停/恢复/取消按钮
5. SSE 事件监听，实时更新当前任务状态

```javascript
// manage/modules/taskpanel.js
var taskState = { current: null, queue: [], history: [] };

export async function init(ctx) {
  await loadTasks(ctx);
  ctx.onSSEEvent = handleTaskEvent;  // chat.js 调用此回调
  bindTaskActions(ctx);
}

async function loadTasks(ctx) {
  var resp = await ctx.Api.get('/api/user/task/list?limit=50');
  taskState.current = resp.tasks.find(t => t.status === 'running' || t.status === 'paused');
  taskState.queue = resp.tasks.filter(t => t.status === 'pending');
  taskState.history = resp.tasks.filter(t => t.status === 'completed' || t.status === 'cancelled');
  renderPanel();
  updateStatusBar();
}

function renderPanel() {
  var panel = ctx.$('#task-panel');
  panel.innerHTML = `
    ${renderCurrentTask()}
    ${renderQueue()}
    ${renderHistory()}
  `;
}

function handleTaskEvent(evt) {
  // SSE 事件到达时更新面板状态
  if (evt.type === 'task_started') setCurrentTask(evt.data);
  if (evt.type === 'task_completed') moveToHistory(evt.data.task_id);
  if (evt.type === 'task_paused') updateTaskStatus(evt.data.task_id, 'paused');
}
```

### 聊天模块（`chat.js`）重写要点

```javascript
// 核心状态（替换原来的 currentReader/currentRunId/sseState）
var state = {
  currentTaskId: null,
  currentReader: null,
  sseState: { /* 同现有 */ }
};

async function sendMessage(text) {
  var resp = await Api.post('/api/user/task/submit', { message: text });
  state.currentTaskId = resp.task_id;
  connectSSE(resp.task_id, 0);  // 从 seq=0 开始
}

function connectSSE(taskId, sinceSeq) {
  var url = `/api/user/task/events?task_id=${taskId}&since_seq=${sinceSeq}`;
  fetchSSE(url, handleEvent);
}

function handleEvent(evt) {
  // 同现有的 handleSSEEvent 逻辑（text_delta/ tool_call_start / reasoning ...）
  // 增加: taskpanel.handleTaskEvent(evt) 更新面板
  // 增加: updateStatusBar(evt) 更新底部状态栏
}
```

### 文件 diff 视图（`files.js` 小改）

在 `manage/modules/files.js` 增加：

```javascript
// 新函数：从 URL query 参数或 task panel 触发
async function showDiff(taskId) {
  var resp = await Api.get(`/api/user/files/diff?task_id=${taskId}`);
  // 渲染 diff 结果（覆盖在文件列表上方或独立视图）
  // added → 绿色背景，modified → 黄色背景，deleted → 红色背景
}

// 在文件列表中，如果文件被 agent 修改过，显示高亮标记
function renderFileList(files, diffResult) {
  files.forEach(f => {
    if (diffResult.modified.includes(f.path)) f.changed = true;
    if (diffResult.added.includes(f.path)) f.new = true;
  });
}
```

---

## 十、Cron 集成

```go
// internal/web/integration.go — 修改 executeCronJob

func (s *Server) executeCronJob(ctx context.Context, job *scheduler.CronJob) error {
    // 1. 确保 daemon 运行
    if err := s.daemonManager.EnsureRunning(job.UserID); err != nil {
        return fmt.Errorf("启动 daemon 失败: %w", err)
    }

    // 2. 提交任务到 daemon
    task := &daemon.Task{
        ID:       uuid.New().String(),
        Source:   "cron",
        Priority: 0,
        Message:  agent.Message{Role: "user", Content: "[定时任务] " + job.Prompt},
    }
    if err := s.daemonManager.SendRPC(job.UserID, "submit_task", task); err != nil {
        return fmt.Errorf("提交 cron 任务失败: %w", err)
    }

    return nil
}
```

Cron 任务在 `tasks.json` 中标记 `source:cron`，任务面板显示 ⏰ 图标和 `[定时]` 前缀。

---

## 十一、daemon 通信协议

### RPC 消息格式（JSON 帧，换行符分隔）

```
新行 = 消息边界（同现有 picoagent stdout 协议）
```

```go
// Server → Daemon
{"type":"submit_task","id":"rpc_1","data":{"task_id":"...","message":{...}}}
{"type":"pause_task","id":"rpc_2","data":{"task_id":"..."}}
{"type":"resume_task","id":"rpc_3","data":{"task_id":"..."}}
{"type":"cancel_task","id":"rpc_4","data":{"task_id":"..."}}
{"type":"send_message","id":"rpc_5","data":{"task_id":"...","message":"..."}}
{"type":"stop","id":"rpc_6"}
{"type":"heartbeat_ack","id":"rpc_7"}

// Daemon → Server
{"type":"event","id":"","data":{"task_id":"...","seq":1,"event_type":"text_delta","payload":"..."}}
{"type":"task_status","id":"","data":{"task_id":"...","status":"completed"}}
{"type":"file_changed","id":"","data":{"path":"main.py","operation":"modified"}}
{"type":"heartbeat","id":"","data":{"status":"running","current_task_id":"..."}}
{"type":"error","id":"...","data":{"message":"..."}}
```

---

## 十二、错误处理

| 场景 | 处理 |
|------|------|
| Daemon 崩溃 | heartbeat 30s 超时 → 标记 crashed → backoff 重启 → 当前任务标记 failed |
| 磁盘满 | gzip.Write 失败 → 事件丢写通道 → 当前任务自动 pause + 通知用户 |
| Socket 断开 | daemon 本地事件缓冲 → server 检测到断连 → 标记 crashed → 重启 |
| Pause 时 tool 未完成 | 等 60s → force cancel tool context → save partial snapshot |
| tasks.json 损坏 | 读取时容错 → 重新扫描 tasks/ 目录重建索引 |
| events.jsonl.gz 损坏 | gzip.Reader 返回 error → 跳过损坏段 → 从最近的 good seq 重放 |

---

## 十三、实施计划（6 阶段）

### Phase 1: Engine 接口 + 存储层 (1周)
- [ ] `internal/agent/engine.go`: 增加 `Snapshot()` + `Restore()`
- [ ] `cmd/picoaide-daemon/store.go`: 实现 tasks.json 读写、gzip JSONL 读写、snapshot 读写

### Phase 2: Daemon 二进制 (1周)
- [ ] `cmd/picoaide-daemon/main.go`: 入口 + main loop
- [ ] `cmd/picoaide-daemon/taskqueue.go`: 任务队列 + pause/resume
- [ ] `cmd/picoaide-daemon/eventbus.go`: EventBus + gzip writer
- [ ] `cmd/picoaide-daemon/filewatcher.go`: fsnotify
- [ ] `cmd/picoaide-daemon/socket.go`: Unix socket 客户端
- [ ] `internal/daemon/`: server 端编排 + RPC handler

### Phase 3: Web API (1周)
- [ ] `internal/web/daemon_handlers.go`: 全部任务 API
- [ ] `internal/web/daemon_stream.go`: SSE + seq 重放
- [ ] `internal/web/fanout.go`: 多客户端分发
- [ ] `internal/web/filesnapshot.go`: diff API
- [ ] 路由注册 (`server.go`) + cron 改造 (`integration.go`)

### Phase 4: 前端改造 (1.5周)
- [ ] `manage/templates/chat.html`: 重写为两栏布局
- [ ] `manage/modules/chat.js`: 重写为 task API + SSE
- [ ] `manage/modules/taskpanel.js`: 新增任务面板模块
- [ ] `common.css`: 任务面板/状态栏/diff 样式
- [ ] 状态栏集成到 `manage/index.html`
- [ ] `manage/modules/files.js`: 增加 diff 模式
- [ ] `manage/templates/files.html`: 增加 diff 区域

### Phase 5: 集成测试 (1周)
- [ ] 端到端：提交任务 → daemon 启动 → 事件流 → 持久化 → 重放
- [ ] Pause/Resume 往返测试
- [ ] 崩溃恢复测试
- [ ] 多客户端并发 fanOut 测试
- [ ] Cron 触发测试

### Phase 6: 灰度切换 (1周)
- [ ] Feature flag: `agent.v2_enabled` per-user
- [ ] 先内部测试 → 灰度 5% → 全量
- [ ] 退役 `chat_stream.go` + 旧 chat API
- [ ] 删除 `cmd/picoagent/main.go`
