# AI Agent 系统

## 架构

picoagent（沙箱内 AI Agent）基于 ADK v2（`google.golang.org/adk/v2`）构建。

```
picoagent (沙箱内)
  │
  ├── ADK llmagent + runner ←── ADKProviderAdapter → Provider → LLM (Anthropic/OpenAI/DeepSeek)
  │
  ├── ADK mcptoolset ←── MCP Tool Manager → 宿主机 picoaide (Unix socket)
  │
  ├── Tool Registry
  │   ├── filesystem tools (read_file, write_file, edit_file, etc.)
  │   ├── grep/glob tools
  │   ├── command tool (shell)
  │   └── web_fetch tool
  │
  ├── SessionStore (live.jsonl + archive-{date}.jsonl)
  │
  └── MemoryEvolution (MEMORY.md / USER.md 提取)
```

## 关键文件

| 文件 | 职责 |
|------|------|
| `cmd/picoagent/main.go` | Agent 入口：配置获取、系统提示构建、工具注册、运行循环 |
| `internal/agent/adk_run.go` | ADK 运行循环桥接，将 ADK 事件转换为 StreamEvent |
| `internal/agent/provider_adapter.go` | 将 Provider 接口包装为 ADK model.LLM |
| `internal/agent/provider.go` | Provider 接口 + 3 个实现（Anthropic/OpenAI/DeepSeek） |
| `internal/agent/mcp_tool.go` | MCP 工具管理器（基于 ADK mcptoolset） |
| `internal/agent/gentool.go` | TypedTool 泛型包装器 |
| `internal/agent/tool_registry.go` | 工具注册表 |
| `internal/agent/tools_filesystem.go` | 文件系统工具 |
| `internal/agent/tools_fs_query.go` | 文件搜索工具 |
| `internal/agent/tools_command.go` | shell 命令工具 |
| `internal/agent/web_tools.go` | web_fetch 工具 |
| `internal/agent/tool_update_memory.go` | update_memory 工具 |
| `internal/agent/session.go` | 消息/事件类型定义 |
| `internal/agent/session_io.go` | 会话 JSONL 存储 |
| `internal/agent/evolution.go` | 记忆演化引擎 |
| `internal/agent/agent_test.go` | Agent 测试 |

## 工具列表

| 工具 | 说明 |
|------|------|
| `command` | 沙箱内 shell 命令执行 |
| `read_file` | 读取文件 |
| `write_file` | 写入文件 |
| `edit_file` | 编辑文件 |
| `append_file` | 追加内容 |
| `list_dir` | 目录列表 |
| `delete_file` | 删除文件 |
| `grep` | 文本搜索 |
| `glob` | 文件模式匹配 |
| `web_fetch` | HTTP 获取 URL 内容 |

## MCP 服务

沙箱内 picoagent 通过 Unix socket 连接到宿主机 picoaide，使用 MCP 协议调用外部工具。宿主机的 ServiceHub 管理 WebSocket 代理连接，支持 browser、computer、agent、email 等服务。

文件：`internal/web/service_hub.go`、`internal/web/mcp_service.go`

## 会话存储

- 双层存储：`live.jsonl`（压缩的 LLM 视图）+ `archive-{date}.jsonl`（永久完整存档）
- `live.meta.json`：元数据（消息数、token 数等）

## 记忆演化

会话结束后执行：
1. 提取决策（Decisions）、知识（Knowledge）、进度（Progress）、偏好（Preferences）
2. 写入 `MEMORY.md` 和 `USER.md`
