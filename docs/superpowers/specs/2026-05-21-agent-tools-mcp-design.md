# Agent 工具系统与 MCP Hub 设计文档

## 概述

PicoAide 已经重构为 overlayfs + chroot 沙箱架构，Agent（picoagent）在沙箱内运行。当前 Agent 只有 3 个内置工具（command、read_file、grep），能力极其薄弱。本文档设计 Agent 的完整工具系统和 PicoAide 的 MCP Hub 架构。

## 架构总览

```
┌──────────────────────────────────────────────────────┐
│                    PicoAide Server                    │
│                                                      │
│  /api/mcp/sse/agent  (统一 MCP SSE 端点)             │
│  ┌────────────────────────────────────────────────┐  │
│  │  AggregatedService "agent"                     │  │
│  │                                                │  │
│  │  tools/list → 合并全部工具：                    │  │
│  │    ├── picoaide_*         服务端 Go Handler     │  │
│  │    ├── browser_*          WebSocket → 扩展      │  │
│  │    ├── computer_*         WebSocket → 桌面代理  │  │
│  │    └── mcp_<name>_*      子进程/HTTP → 第三方   │  │
│  └────────────────────────────────────────────────┘  │
│                                                      │
│  沙箱 (overlayfs + chroot)                           │
│  ┌────────────────────────────────────────────────┐  │
│  │  picoagent                                     │  │
│  │  ├── 内置工具 (command/read_file/grep + 新增)   │  │
│  │  ├── /api/mcp/sse/agent  ← HTTP SSE            │  │
│  │  └── /workspace/skills/   ← 只读挂载            │  │
│  └────────────────────────────────────────────────┘  │
│                                                      │
│  外部插件:                                           │
│  ├── 浏览器扩展 ←→ /api/browser/ws (WebSocket)       │
│  └── 桌面代理   ←→ /api/computer/ws (WebSocket)      │
└──────────────────────────────────────────────────────┘
```

## 四层工具体系

### 第 1 层：沙箱内置工具（internal/agent/tool_registry.go）

在沙箱内本地执行，不经过网络，低延迟。

| 工具 | 说明 | 状态 |
|------|------|------|
| `command` | 执行 shell 命令 | ✅ 已有 |
| `read_file` | 读取文件内容 | ✅ 已有 |
| `grep` | 搜索文件内容 | ✅ 已有 |
| `write_file` | 写入文件 | ❌ 新增 |
| `edit_file` | 精确替换编辑 | ❌ 新增 |
| `append_file` | 追加到文件末尾 | ❌ 新增 |
| `list_dir` | 列出目录 | ❌ 新增 |
| `glob` | 按模式搜索文件 | ❌ 新增 |
| `delete_file` | 删除文件/目录 | ❌ 新增 |
| `web_search` | 网络搜索 | ❌ 新增 |
| `web_fetch` | 获取 URL 内容 | ❌ 新增 |

### 第 2 层：PicoAide 平台工具（通过 MCP SSE 端点）

需要 PicoAide 服务端能力。

| 工具 | 说明 | handler |
|------|------|---------|
| `picoaide_user_info` | 当前用户信息（用户名、角色、认证模式） | `handleUserInfo` |
| `picoaide_skills_list` | 列出已绑定的技能 | `handleUserSkills` |
| `picoaide_config_get` | 读取 Agent 配置 | `handleConfigGet` |
| `picoaide_config_set` | 更新配置字段 | `handleConfigSave` |
| `picoaide_shared_folders` | 列出团队共享文件夹 | `handleSharedFolders` |
| `picoaide_cookies_list` | 列出已授权的 Cookie 域名 | `handleUserCookies` |
| `picoaide_cookies_delete` | 撤销 Cookie 域名授权 | `handleUserCookiesDelete` |

### 第 3 层：浏览器 + 桌面工具（通过 WebSocket 代理到外部插件）

已有，保持兼容。在统一端点中通过 `browser_` / `computer_` 前缀暴露。

| 来源 | 工具数量 | 命名前缀 |
|------|---------|---------|
| 浏览器扩展 | 19 个 | `browser_` |
| 桌面代理 | 15 个 | `computer_` |

### 第 4 层：第三方 MCP 工具（动态注入）

通过 MCP Hub 机制，管理员安装第三方 MCP 服务器后动态注入。

| 来源 | 命名规范 |
|------|---------|
| 第三方 MCP | `mcp_<server_name>_<tool_name>` |

## 统一端点实现

### AggregatedService

新增 `AggregatedService` 结构体，聚合多个 ToolSource：

```go
type ToolSourceType int

const (
  SourceServerSide ToolSourceType = iota // 服务端 Go handler
  SourceWebSocket                        // WebSocket 代理
  SourceMCPProxy                         // 第三方 MCP 代理
)

type ToolSource struct {
  Type    ToolSourceType
  Hub     *ServiceHub        // 仅 SourceWebSocket
  Tools   []ToolDef
  Handler func(ctx context.Context, args map[string]interface{}, username string) (interface{}, error) // 仅 SourceServerSide
  MCPName string             // 仅 SourceMCPProxy
}

type AggregatedService struct {
  ServerName string
  Version    string
  Sources    []ToolSource
}
```

### 路由逻辑

tools/list → 遍历所有 Source，合并 ToolDefs（用前缀区分命名空间）

tools/call:
```
picoaide_*      → 查 Handler map → 调用 Go handler
browser_*       → browserSvc.GetConnection() → WebSocket
computer_*      → computerSvc.GetConnection() → WebSocket
mcp_<name>_*    → 查 MCPProxy map → 子进程/HTTP
```

## 数据库设计

### mcp_servers 表

| 列 | 类型 | 说明 |
|----|------|------|
| id | INTEGER PK | |
| name | TEXT UNIQUE NOT NULL | MCP 服务器名称，如 "git" |
| transport | TEXT NOT NULL | "stdio" 或 "streamable-http" |
| command | TEXT | stdio 模式：可执行文件路径 |
| args | TEXT | JSON 数组，命令行参数 |
| url | TEXT | HTTP 模式：目标 URL |
| env | TEXT | JSON 对象，环境变量 |
| enabled | INTEGER DEFAULT 1 | 是否启用 |
| created_at | DATETIME | |
| updated_at | DATETIME | |

### mcp_server_grants 表

| 列 | 类型 | 说明 |
|----|------|------|
| id | INTEGER PK | |
| server_id | INTEGER FK | 关联 mcp_servers |
| grant_type | TEXT | "user" 或 "group" |
| grant_value | TEXT | 用户名或组名 |

### 索引

- mcp_server_grants(server_id, grant_type, grant_value) UNIQUE

## 授权流程

1. 管理员通过 API 安装 MCP 服务器 → 写入 `mcp_servers`
2. 管理员授权给用户或组 → 写入 `mcp_server_grants`
3. Agent 连接 `/api/mcp/sse/agent` → `tools/list` 查询 `mcp_server_grants`
4. 只返回有权限的第三方 MCP 工具（picoaide_* browser_* computer_* 始终返回）

## Skill 注入机制

技能不由 Agent 自行安装卸载。由管理员在后台绑定：
- 管理员将技能绑定到用户或组
- 沙箱启动时，PicoAide 将绑定的技能目录只读挂载到 `/workspace/skills/<name>/`
- Agent 的 `list_dir`/`read_file` 等工具可直接访问技能文件

## API 端点变更

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/mcp/sse/agent` | GET | 新增：统一 MCP SSE 端点 |
| `/api/mcp/sse/agent` | POST | 新增：统一 MCP JSON-RPC |
| `/api/browser/ws` | GET | 不变：浏览器扩展 WebSocket |
| `/api/computer/ws` | GET | 不变：桌面代理 WebSocket |
| `/api/mcp/sse/browser` | GET/POST | 保持兼容 |
| `/api/mcp/sse/computer` | GET/POST | 保持兼容 |

## 向后兼容

- `/api/mcp/sse/browser` 和 `/api/mcp/sse/computer` 保持原有行为
- `/api/browser/ws` 和 `/api/computer/ws` WebSocket 地址不变
- 新增 `/api/mcp/sse/agent` 为统一端点，不破坏现有连接
