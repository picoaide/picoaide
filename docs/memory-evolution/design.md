# PicoAide 记忆自我进化系统 — 设计文档

> 版本: 1.0
> 日期: 2026-05-28

---

## 1. 背景

### 1.1 问题

PicoAide 的 picoagent 在用户工作区维护四个文件用于智能体记忆：

| 文件 | 功能 | 现状 |
|------|------|------|
| `AGENT.md` | Agent 行为配置 | 初始化时创建，永不更新 |
| `SOUL.md` | 人格设定 | 初始化时创建，永不更新 |
| `USER.md` | 用户偏好/信息 | 初始化时创建，永不更新 |
| `memory/MEMORY.md` | 长期记忆 | 初始化时创建，永不更新 |

这些文件在每次对话开始时被读入系统提示词，但**没有任何写入触发条件**。
picoagent 已有 `compactor`（对话压缩器），但其生成的结构化摘要只用于会话内上下文窗口管理，不回写到长期记忆文件。

### 1.2 为什么需要自我进化

- **跨会话学习**：用户今天在会话中做出的决策、明确的技术偏好、项目进展，下一个会话应该知道
- **减少重复**：用户不必重复告知自己的偏好和工作上下文
- **企业效率**：新员工入职后，AI 能快速学习其工作风格和项目背景
- **审计追溯**：记忆变更应有记录，管理员可查看 AI 学到了什么

### 1.3 参考实现

调研了 OpenClaw / Hermes 生态的自我进化方案：

| 项目 | 核心机制 |
|------|----------|
| ClawMem | Hook 生命周期（session_start / before_prompt_build / agent_end）+ SQLite + 混合 RAG 检索 |
| Bonsai Memory | Agent 主动通过工具调用重组记忆结构（渐进式分层） |
| PicoClaw pkg/memory | JSONL 追加存储 + compactor 会话摘要 |
| Hermes MemoryProvider | `prefetch()` / `on_session_end()` / `on_pre_compress()` 插件接口 |

参考 ClawMem 的 `agent_end` hook（决策提取+handoff 生成+反馈循环）和
Herme 的 `on_session_end()` 模式，本方案在 picoagent 的 msgLoop 退出点实现记忆进化。

---

## 2. 设计原则

| 原则 | 说明 |
|------|------|
| **克制第一** | 只提取对企业工作有长期价值的信息。忽略问候语、闲聊、过程性调试 |
| **低侵入** | 进化在**会话结束后**异步执行（用户已关闭聊天），不影响对话流畅性 |
| **有上限** | MEMORY.md ≤ 5000 tokens，USER.md ≤ 2000 tokens，超限自动裁剪 |
| **可追溯** | 每次进化备份旧版本到 `memory/archive/`，保留 90 天 |
| **可控** | AGENT.md / SOUL.md **永不自动进化**（管理员模板）。所有变更写入审计日志（DB + 文件） |
| **透明** | AgentProtocol 声明记忆机制，AI 和用户都知道记忆会如何变化 |

---

## 3. 总体架构

```
┌───────────────────────────────────────────────────────────────────┐
│  picoagent msgLoop                                                │
│                                                                   │
│   每次消息:                                                       │
│     stdin → engine.Process() → stdout (响应流)                    │
│            ↕ store.AppendMessage(sessionKey)                      │
│                                                                   │
│   对话中: AI 可调用 update_memory 工具主动管理记忆               │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
    │
    ▼  [stdin EOF / 60s idle timeout]
    │
┌───────────────────────────────────────────────────────────────────┐
│  MemoryEvolution.Evolve()                                         │
│                                                                   │
│  ① LoadArchive + LoadLive → 读取全部会话消息（上限 200 条）      │
│  ② 读取当前 MEMORY.md 和 USER.md                                 │
│  ③ LLM 提取调用 → 结构化 JSON                                    │
│  ④ 与现有内容对比 -> 去重 -> 合并                                 │
│  ⑤ 写回 MEMORY.md / USER.md                                      │
│  ⑥ 备份旧版本到 memory/archive/（保留 90 天）                    │
│  ⑦ 审计日志（slog + DB）                                         │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
    │
    ▼
  exit(0)
```

---

## 4. 组件设计

### 4.1 MemoryEvolution — `internal/agent/evolution.go`

#### 结构体

```go
type MemoryEvolution struct {
  workspace string
  provider  Provider          // 复用同一 LLM provider
  model     string            // 复用同一 model
  store     *SessionStore     // 读取会话存档和 live
  maxMsgs   int               // 送入 LLM 的存档消息上限（默认 200）
}

type EvolutionResult struct {
  Decisions  []Decision  `json:"decisions"`
  Knowledge  []Knowledge `json:"knowledge"`
  Progress   Progress    `json:"progress"`
  Preferences []Preference `json:"preferences"`
  HasChanges bool
}

// 每类的条目约束：≤40 字，confidence≥0.7，每类最多 5 条
type Decision struct {
  Topic     string  `json:"topic"`
  Decision  string  `json:"decision"`
  Rationale string  `json:"rationale"`
}

type Knowledge struct {
  Topic string `json:"topic"`
  Fact  string `json:"fact"`
}

type Progress struct {
  Completed  []string `json:"completed"`
  InProgress []string `json:"in_progress"`
  Blocked    []string `json:"blocked"`
}

type Preference struct {
  Aspect      string  `json:"aspect"`      // 沟通风格|技术偏好|工作习惯|关注重点
  Description string  `json:"description"`
}
```

#### Evolve 方法流程

```
func (e *MemoryEvolution) Evolve(ctx context.Context, sessionKey string) error {
  1. 检查会话是否有足够内容（≥5轮且≥1次工具调用，否则 Skip）
  2. 创建 30s 超时 context
  3. 并行读取：
     - store.LoadLive(sessionKey)     → 当前 live 消息
     - store.LoadArchive(sessionKey)  → 存档消息
     - readFile(MEMORY.md)            → 现有记忆
     - readFile(USER.md)              → 现有用户信息
  4. 合并消息列表（live + archive），上限 e.maxMsgs 条
  5. 调用 LLM 提取（复用 LLMSummarizer 模式）:
     - Prompt 包含：会话记录 + 现有 MEMORY.md + 提取指令
     - 期望输出：JSON 格式的 EvolutionResult
     - Temperature=0.2, MaxTokens=2048
  6. 解析 JSON：
     - 成功 → 进入合并流程
     - 失败（非 JSON/缺字段）→ log.Warn + Skip
  7. 合并 MEMORY.md：
     - 按 ## 章节解析现有内容
     - decisions: 按 topic 去重 → 追加新决策
     - knowledge: 按 topic 去重 → 追加新知识
     - progress: completed 从 in_progress 移除，追加新项
     - 预算控制：超 5000 tokens 时从最旧开始裁剪
  8. 合并 USER.md：
     - 按 ## 章节解析
     - preferences: 相同 aspect 保留高 confidence
     - 预算控制：超 2000 tokens 时裁剪低 confidence 项
  9. 写回文件 + 备份：
     - os.WriteFile(MEMORY.md, ...)
     - os.WriteFile(USER.md, ...)
     - cp MEMORY.md → memory/archive/MEMORY.md.YYYYMMDD_HHMMSS
      - 清理 90 天前的备份
  10. 返回变更摘要
}
```

#### LLM 提取 Prompt

```
你是一个企业 AI 助手的记忆提取器。分析以下会话记录，与现有记忆对比，
提取新增的有长期价值的企业工作信息。

[现有的 MEMORY.md]
{{existingMemory}}

[会话记录]
{{conversationLog}}

输出格式（只输出 JSON，不要其他内容）：
{
  "decisions": [
    {"topic": "...", "decision": "...", "rationale": "..."}
  ],
  "knowledge": [
    {"topic": "...", "fact": "..."}
  ],
  "progress": {
    "completed": ["..."],
    "in_progress": ["..."],
    "blocked": ["..."]
  },
  "preferences": [
    {"aspect": "沟通风格|技术偏好|工作习惯|关注重点", "description": "..."}
  ]
}

约束：
- 只提取与现有记忆不同的新增信息
- 每条 ≤ 40 字
- 只输出企业工作相关信息，忽略问候语、闲聊、技术调试过程
- 没有新信息时返回空数组 {}
- 必须返回合法的 JSON
```

### 4.2 合并引擎

合并引擎负责将 EvolutionResult 合并到现有文件内容中，确保去重和预算控制。

#### MEMORY.md 合并

```
输入：existingContent (string), result (EvolutionResult)
输出：newContent (string)

1. 解析 existingContent 按 ## 章节：
   map[string][]string  = { "关键决策": [...], "项目知识": [...], ... }

2. 对每类新增信息执行合并:

   decisions:
     for each d in result.Decisions:
       if topic 已存在 → 跳过
       else → 追加 "[日期]: {d.Decision}（{d.Rationale}）"

   knowledge:
     for each k in result.Knowledge:
       if topic 已存在 → 跳过
       else → 追加 "- {k.Fact}"

   progress:
     for each c in result.Progress.Completed:
       从 InProgress/Blocked 章节移除匹配项
       追加到 Completed 章节（如章节不存在则创建）
     for each p in result.Progress.InProgress:
       追加到 InProgress 章节
     for each b in result.Progress.Blocked:
       追加到 Blocked 章节

3. 预算控制：
   估算总 tokens (len(content)/4)
   for tokens > 5000:
     从优先级别最低的章节移除最旧条目
     优先级: decisions > knowledge > progress.completed
```

#### USER.md 合并

```
输入：existingContent (string), result.Preferences
输出：newContent (string)

1. 解析现有内容获取现有偏好列表

2. for each p in result.Preferences:
   if 相同 aspect 已存在:
     保留 confidence 更高的（evolution 不输出 confidence，默认 0.8）
     相同 confidence → 用新值替换
   else:
     追加新偏好

3. 预算控制：超 2000 tokens 时移除最早且 confidence 最低的条目
```

### 4.3 UpdateMemoryTool — `internal/agent/tool_update_memory.go`

#### 工具定义

```go
type UpdateMemoryTool struct {
  Workspace string
}

func (t *UpdateMemoryTool) Name() string       { return "update_memory" }
func (t *UpdateMemoryTool) Description() string { return "主动更新长期记忆。用于记录重要决策、知识点、进度状态或用户偏好。" }
```

#### 参数定义（JSON Schema 风格）

```json
{
  "section": "decisions|knowledge|progress|preferences",
  "action": "add|update|delete",
  "entries": [
    {
      "topic": "决策/知识标题",
      "content": "具体内容",
      "rationale": "原因（仅 decisions 需要）"
    }
  ]
}
```

#### 执行流程

```
Execute(ctx, params):
  1. 解析参数 → section, action, entries
  2. 参数合法性校验：section in enum, action in enum, entries 非空
  3. 按目标文件分支：
     - decisions/knowledge/progress → 操作 MEMORY.md
     - preferences → 操作 USER.md
  4. 读取当前文件内容
  5. 按 section/action 执行操作：
     - add:    合并引擎追加（去重）
     - update: 按 topic 匹配替换
     - delete: 按 topic 匹配删除
  6. 写回文件
  7. 备份旧版本到 memory/archive/
  8. 返回操作摘要（如 "已添加 2 条决策"）
```

### 4.4 AgentProtocol 透明度声明

在 `engine.go` 的 AgentProtocol const 末尾追加以下章节：

```
## 记忆管理

本工作区具备自动记忆进化能力：
- 每次对话结束后，系统会自动提取关键决策、知识点、进度状态到 MEMORY.md
- 系统会自动学习你的工作偏好并更新 USER.md
- 你也可以使用 update_memory 工具主动更新记忆
- AGENT.md 和 SOUL.md 由管理员管理，请勿手动修改
- 所有记忆修改都会备份，可追溯最近 90 天的变更历史
- 使用 update_memory 更新优于直接 write_file，可确保格式一致性和去重
```

### 4.5 审计日志

采用双路日志确保可靠性：

#### 文件日志（slog）

```go
slog.Info("memory_evolution",
  "username",     username,
  "session_key",  sessionKey,
  "changes",      changesSummary,
  "files",        filesModified,
)
```

#### DB 日志

通过 `POST /api/picoagent/audit` 端点写入 `memory_evolution_log` 表：

```go
type MemoryEvolutionLog struct {
  ID             int64     `xorm:"pk autoincr 'id'"`
  Username       string    `xorm:"notnull index 'username'"`
  SessionKey     string    `xorm:"notnull 'session_key'"`
  ChangesSummary string    `xorm:"text 'changes_summary'"`
  FilesModified  string    `xorm:"text 'files_modified'"`
  CreatedAt      time.Time `xorm:"created 'created_at'"`
}
```

---

## 5. 文件变更清单

### 5.1 新建文件

| 文件 | 说明 | 估计行数 |
|------|------|----------|
| `internal/auth/migrations/20260528_120000_add_memory_evolution_log.go` | DB 迁移 | 40 |
| `internal/agent/evolution.go` | MemoryEvolution 核心 | 350 |
| `internal/agent/evolution_test.go` | MemoryEvolution 测试 | 250 |
| `internal/agent/tool_update_memory.go` | update_memory 工具 | 180 |
| `internal/agent/tool_update_memory_test.go` | update_memory 测试 | 120 |
| `internal/web/memory_evolution.go` | 审计端点 handler | 60 |

### 5.2 修改文件

| 文件 | 改动 |
|------|------|
| `cmd/picoagent/main.go` | msgLoop 后调用 evolver.Evolve() + 注册 UpdateMemoryTool |
| `internal/agent/engine.go` | AgentProtocol 追加记忆管理章节 |
| `internal/web/server.go` | 注册 POST /picoagent/audit 路由 |
| `internal/auth/auth.go` | 新增 struct 导入（MemoryEvolutionLog） |

---

## 6. 风险与应对

| 风险 | 影响 | 应对 |
|------|------|------|
| LLM 返回非 JSON | 进化失败，数据不丢失 | 健壮解析 + fallback 跳过 + log.Warn |
| MEMORY.md 并发写 | 竞态条件 | 单进程串行；启动时 lockfile 防多实例 |
| 存档过大（>200 条） | LLM 超时/高成本 | 限流：最多取 200 条最新消息 |
| 工具写后 session-end 又写 | 重复条目 | session-end 做 dedup（与现有内容对比） |
| SIGKILL 导致丢失 | 最后一轮不持久 | 接受——用户可手动 update_memory |
| DB 审计调用失败 | 日志丢失 | 文件日志兜底 + HTTP 重试 1 次 |
| picoagent 进程被沙箱杀死 | 进化不执行 | 启动脚本中增加 trap 信号处理 |

---

## 7. 实施计划

各任务按依赖顺序排列，每任务采用 TDD（红-绿-重构）并执行独立代码审计。

| # | 任务 | 依赖 | 时间估计 |
|---|------|------|----------|
| 1 | DB Migration + Model | 无 | 小 |
| 2 | MemoryEvolution 核心 + 测试 | 无 | 大 |
| 3 | UpdateMemoryTool + 测试 | 依赖 evolution.go 的合并引擎 | 中 |
| 4 | AgentProtocol + main.go 接入 | 依赖 evolution.go + 工具 | 小 |
| 5 | 服务端审计端点 | 依赖迁移完成 | 小 |
| 6 | 最终验证（make check + deploy） | 全部完成 | 中 |
