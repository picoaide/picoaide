# PicoAgent 实现计划

## 目标

将 PicoAide 从 "PicoClaw 批量管理工具" 改造为企业 AI Agent 平台：
- IM 统一接入层（钉钉/飞书/企微）
- Go 内置 agent 引擎（参考 OpenCode 架构）
- nsjail 沙箱隔离，每次消息触发一次沙箱（函数式调用）
- **完全兼容 PicoClaw 的会话格式（JSONL）、初始化文件、工作目录结构**
- **用户模板机制，管理员可自定义模板文件**

---

## 一、兼容性要求（新增）

### 1.1 会话格式兼容

PicoAgent 必须读写与 PicoClaw 相同的 JSONL 格式：

```
workspace/sessions/
├── {session_key}.jsonl          ← 消息记录（每行 JSON）
└── {session_key}.meta.json      ← 元数据（摘要、skip偏移、scope）
```

消息 JSON 格式：

```json
{
  "role": "user|assistant|tool|system",
  "content": "消息文本",
  "media": ["media://ref1"],
  "attachments": [{"type": "image", "ref": "media://...", ...}],
  "reasoning_content": "推理内容",
  "tool_calls": [{"id": "call_abc", "type": "function", "function": {"name": "read_file", "arguments": "{}"}}],
  "tool_call_id": "call_abc"
}
```

元数据 JSON 格式：

```json
{
  "key": "sk_v1_xxx",
  "summary": "对话摘要",
  "skip": 5,
  "count": 42,
  "created_at": "...",
  "updated_at": "...",
  "scope": {"version": 1, "agent_id": "...", "channel": "...", ...},
  "aliases": ["agent:xxx:yyy"]
}
```

### 1.2 首次初始化兼容

第一次创建用户时，生成与 PicoClaw 相同的目录结构：

```
workspace/
├── AGENT.md                     ← 代理定义（YAML frontmatter + 内容）
├── SOUL.md                      ← 人格指令
├── USER.md                      ← 用户偏好（模板）
├── memory/
│   └── MEMORY.md                ← 长期记忆
├── skills/                      ← （可选默认技能）
├── cron/
│   └── jobs.json                ← 定时任务
└── sessions/                    ← 会话存储
```

### 1.3 会话 Key 机制兼容

- 会话 key = `sk_v1_` + SHA256(scope) （与 PicoClaw 相同）
- scope 由 agent_id、channel、chat_id、sender 等维度构成
- 同一会话的消息总是追加到同一个 .jsonl 文件

### 1.4 用户模板

管理员可修改的模板目录：

```
<WorkDir>/user-template/
├── AGENT.md
├── SOUL.md
├── USER.md
└── memory/
    └── MEMORY.md
```

创建新用户时，整个 user-template/ 复制到该用户的 workspace/。

---

## 二、现有代码处理清单

### 2.1 删除（PicoClaw 专用，~4300 行）

| 文件 | 行数 | 原因 |
|------|------|------|
| internal/user/picoclaw_adapter.go | 889 | 100% PicoClaw Adapter |
| internal/user/picoclaw_adapter_test.go | 616 | 随 adapter |
| internal/user/picoclaw_adapter_db_test.go | 68 | 随 adapter |
| internal/user/picoclaw_embed.go | 120 | 100% PicoClaw embed |
| internal/user/picoclaw_embed_test.go | 61 | 随 embed |
| internal/user/picoclaw_fixups.go | 564 | 配置兼容性修复 |
| internal/user/picoclaw_migration.go | 574 | 配置迁移 |
| internal/user/picoclaw_migration_test.go | 971 | 随 migration |
| internal/user/picoclaw_rules/ | 目录 | 适配器数据 |

### 2.2 保留（~1800 行）

service_hub、browser/computer MCP、mcp_*、admin_auth/superadmins/tls、auth、authsource、logger、util

### 2.3 修改（~5500 行）

| 文件 | 修改点 |
|------|--------|
| user.go | 路径参数化；初始化时复制模板文件 |
| picoclaw_config_fields.go | UI Schema 模式保留 |
| handlers.go / server.go | PicoClaw 路由替换 |
| admin_containers.go | Docker → nsjail |
| admin_config.go | migration-rules 删除 |
| config/ | 清理 PicoClaw 字段 |
| docker.go | 仅开发测试 |
| main.go | 清理引用 |

### 2.4 提取到 util/

deepGet / setByPath / deleteByPath / deepCopyInterface → internal/util/map.go

---

## 三、新增代码清单（~5200 行）

### 3.1 新增模块总览

```
cmd/
  picoaide/          ← 宿主二进制
    main.go
  picoagent/         ← 沙箱内二进制
    main.go

internal/
├── im/               ← IM 网关（~1000 行）
│   ├── im.go
│   ├── dingtalk.go
│   ├── feishu.go
│   └── wecom.go
├── sandbox/          ← nsjail 管理（~700 行）
│   ├── manager.go
│   ├── rootfs.go
│   └── config.go
├── agent/            ← Agent 引擎（~2800 行）
│   ├── engine.go          ← 主循环
│   ├── compactor.go       ← 上下文压缩
│   ├── overflow.go        ← token 预算
│   ├── retry.go           ← 重试退避
│   ├── structured_output.go
│   ├── provider.go        ← LLM 封装
│   ├── tool_registry.go   ← 工具注册
│   ├── tool_exec.go       ← 工具执行
│   ├── permission.go      ← 权限
│   ├── session.go         ← 会话模型
│   ├── session_io.go      ← JSONL 读写（兼容 PicoClaw 格式）
│   └── cron.go            ← 定时任务
└── web/
    └── agent_config.go    ← GET /api/picoagent/me
```

### 3.2 关键新增：session_io.go

与 PicoClaw 完全兼容的 JSONL 读写：

```go
// SessionStore 兼容 PicoClaw 的 JSONL 格式
type SessionStore struct {
    workspace string // workspace 路径
}

// BuildKey 生成与 PicoClaw 相同的 sk_v1_xxx 格式
func (s *SessionStore) BuildKey(scope SessionScope) string

// LoadHistory 从 {key}.jsonl 读取历史消息
func (s *SessionStore) LoadHistory(key string) ([]*Message, error)

// AppendMessage 追加一条消息到 {key}.jsonl
func (s *SessionStore) AppendMessage(key string, msg *Message) error

// LoadMeta 读取 {key}.meta.json
func (s *SessionStore) LoadMeta(key string) (*SessionMeta, error)

// SaveMeta 写入 {key}.meta.json
func (s *SessionStore) SaveMeta(key string, meta *SessionMeta) error
```

### 3.3 关键新增：session_key.go

```go
type SessionScope struct {
    Version    int
    AgentID    string
    Channel    string
    Account    string
    Dimensions []string
    Values     map[string]string
}

// BuildSessionKey = "sk_v1_" + SHA256(canonical signature)
func BuildSessionKey(scope SessionScope) string

// SanitizeKey 与 PicoClaw 相同：: → _, / → _, \ → _
func SanitizeKey(key string) string
```

### 3.4 关键新增：用户初始化

```go
// InitializeUser 创建用户的 workspace 目录
// 1. 复制 <WorkDir>/user-template/ → users/<name>/.picoclaw/workspace/
// 2. 创建 cron/jobs.json (空数组)
// 3. 创建 sessions/ 目录
func InitializeUser(workDir, username string) error
```

### 3.5 bundle/ 目录（双架构）

Makefile 自动下载 x86_64 + aarch64 两个版本，分别命名：

```
bundle/
├── picoagent.x86_64     ← go build GOARCH=amd64
├── picoagent.aarch64    ← go build GOARCH=arm64
├── busybox.x86_64       ← 静态编译
├── busybox.aarch64
├── bun.x86_64           ← JS/TS 运行时
├── bun.aarch64
├── jq.x86_64 / jq.aarch64
├── curl.x86_64 / curl.aarch64
├── wget.x86_64 / wget.aarch64
└── unzip.x86_64 / unzip.aarch64
```

prepare-rootfs.sh 根据当前 arch 复制对应文件。

### 3.6 user-template/ 目录

```
user-template/
├── AGENT.md          ← 代理定义（含 frontmatter）
├── SOUL.md           ← 人格
├── USER.md           ← 用户偏好模板
└── memory/
    └── MEMORY.md     ← 长期记忆
```

---

## 四、整体架构

```
PicoAide Host (Go)

  收到消息 (钉钉/飞书/企微/定时任务)
      │
      ▼
  1. sandbox.Run(token, messageJSON):
      ├ 加载会话: workspace/sessions/{key}.jsonl
      ├ nsjail -- /bin/picoagent
      │   ← stdout JSON Lines → 实时转发 IM 流式
      └ 保存会话: 追加到 {key}.jsonl

PicoAgent (沙箱内):
  1. 读 PICOAGENT_TOKEN
  2. GET /api/picoagent/me → 配置
  3. 读 AGENT.md / SOUL.md / USER.md → 构建系统提示
  4. 加载会话历史 → Agent.Process()
  5. stdout JSON Lines 流式输出
  6. 保存会话 / 保存元数据 → exit

初始化:
  picoaide init → 创建 WorkDir/rootfs
  picoaide create-user → 复制 user-template/ → users/<name>/.picoclaw/workspace/
```

---

## 五、阶段计划

### Phase 1: 基础设施（5-7 天）
1. [ ] 提取 deepGet/setByPath/deleteByPath/deepCopyInterface → internal/util/map.go
2. [ ] 删除 PicoClaw 专用代码（6 .go + rules 目录）
3. [ ] 清理 config/：types.go, globalconfig.go, dbconfig.go, flatten.go, defaults.go
4. [ ] 清理 web/：删除 migration-routes handler、替换路由名
5. [ ] 创建 user-template/ 目录（AGENT.md / SOUL.md / USER.md / memory/MEMORY.md）
6. [ ] 修改 user.go：新增 InitializeUser（复制模板 → 工作目录）
7. [ ] Makefile 添加 bundle target
8. [ ] scripts/prepare-rootfs.sh 准备

### Phase 2: Agent 引擎（2 周）
1. [ ] session.go + session_key.go + session_io.go — 会话模型 + JSONL 读写
2. [ ] provider.go — LLM 封装（先 Anthropic）
3. [ ] tool_registry.go — 工具注册
4. [ ] tool_exec.go — 工具执行
5. [ ] engine.go — 主循环
6. [ ] cmd/picoagent/main.go — 沙箱入口（读 token → 加载 AGENT.md → Process → stdout → 保存会话）
7. [ ] sandbox/manager.go — nsjail 函数式调用
8. [ ] sandbox/rootfs.go + config.go
9. [ ] overflow.go + compactor.go + retry.go

### Phase 3: 外围（1 周）
1. [ ] permission.go + structured_output.go + cron.go
2. [ ] internal/web/agent_config.go（/api/picoagent/me）

### Phase 4: IM + 集成（1 周）
1. [ ] internal/im/ 接口 + 钉钉接入
2. [ ] Web 路由适配 + 端到端测试
