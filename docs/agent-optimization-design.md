# Agent 引擎优化设计 v2

## 概述

三个优化方向：

1. **MCP 服务器摘要 + 子代理执行** — 替代标签过滤。主 agent 只感知服务器级别的一行摘要，不加载具体工具。工具加载由子代理按需完成
2. **引擎进度反馈** — 长时间无文本输出时自动发进度消息给用户
3. **子代理继承工具集** — 主 agent 指定子代理使用哪个 MCP 服务器，子代理自动加载该服务器的全部工具

---

## 1. MCP 服务器摘要 + 按需加载

### 问题

当前所有 215 个 MCP 工具的定义（~43K tokens）在每次 LLM 调用时完整发送。AI 只需要其中几个，却要处理全部。

### 方案

每个 MCP 服务器在连接时，自动将其工具列表归纳为一行摘要。主 agent 只感知摘要，不感知具体工具。

**摘要示例**（自动生成）：

```
tyc-mcp:    企业工商信息、舆情新闻、司法风险查询（12 个工具）
browser:    Chrome 浏览器自动化：导航、截图、点击、表单（19 个工具）
computer:   桌面自动化：截图、鼠标键盘、文件管理（15 个工具）
```

### MCP 摘要自动生成

在 `MCPToolManager.Connect` 中，连接成功后遍历该服务器的工具列表，根据工具名称和描述自动生成一行摘要：

```go
// mcp_tool.go
func (m *MCPToolManager) generateSummary(serverName string) string {
    entries := m.toolsByServer[serverName]
    // 从工具名和描述中提取关键词
    // "get_company_info: 查询企业工商信息" → "企业工商信息"
    // 合并同类能力，去重，取前 3-5 个核心能力
}
```

实现策略：
- 从工具描述中提取动词+名词（如"查询企业工商信息"、"获取新闻舆情"）
- 去重后取前 5 个能力点
- 格式：`<服务器名>: <能力1>、<能力2>...（N 个工具）`

### ToolRegistry 分组

```go
type ToolEntry struct {
    executor   ToolExecutor
    serverName string  // 空 = 内置基础工具
}

func (r *ToolRegistry) ListBasic() []ToolDef     // 所有 serverName="" 的工具
func (r *ToolRegistry) ListByServer(name string) []ToolDef  // 指定 MCP 服务器的工具
func (r *ToolRegistry) ListServers() []string    // 所有已注册的 MCP 服务器名
```

内置工具注册时 `serverName=""`，MCP 工具注册时 `serverName="tyc-mcp"` 等。

### AgentProtocol 更新

system prompt 中不再列具体工具，改为：

```
## 可用 MCP 服务器
- tyc-mcp: 企业工商信息、舆情新闻、司法风险查询（12 个工具）
- browser: Chrome 浏览器自动化（19 个工具）
- computer: 桌面自动化（15 个工具）

使用方法：
- 单次查询 → query_server(server, tool, args)
- 批量任务 → subagent_task(server, task_desc)
- 基础工具（文件操作、命令执行）可直接使用
```

### query_server 工具

新增代理工具，AI 可以快速调用某个 MCP 服务器的工具，无需创建子代理：

```json
{
    "name": "query_server",
    "description": "快速调用 MCP 服务器的工具，返回结果。适用于单次查询。批量任务请用 subagent_task。",
    "parameters": {
        "server": {"type": "string", "description": "MCP 服务器名称，如 tyc-mcp"},
        "tool":   {"type": "string", "description": "工具名（不含 mcp_server_ 前缀）"},
        "args":   {"type": "object", "description": "工具参数"}
    }
}
```

执行逻辑：`ToolRegistry.Execute(ctx, "mcp_"+server+"_"+tool, argsJSON)`

### 主 agent 工具集总览

| 工具 | 说明 | 数量 |
|------|------|------|
| command / read/write/edit/delete/append file / grep / glob / list_dir | 文件与命令 | 9 |
| web_search / web_fetch | 网络 | 2 |
| update_memory | 记忆 | 1 |
| subagent_task | 子代理 | 1 |
| query_server | MCP 代理调用 | 1 |
| **合计** | | **14**（原 228） |

---

## 2. 子代理 + MCP 服务器绑定

### 问题

子代理目前只能执行 shell 命令，没有 MCP 工具能力，无法完成需要调用 MCP 的批量任务。

### 方案

`subagent_task` 工具新增 `server` 参数。指定后，子代理自动加载该服务器的全部工具：

```json
{
    "name": "subagent_task",
    "properties": {
        "name": {"type": "string", "description": "子代理名称"},
        "task": {"type": "string", "description": "任务描述"},
        "server": {"type": "string", "description": "MCP 服务器名（可选），子代理将拥有该服务器的全部工具"},
        "tools_hint": {"type": "string", "description": "工具使用指引（可选）"}
    },
    "required": ["name", "task"]
}
```

### 子代理工具集构建

```go
func (m *SubAgentManager) runSubAgent(ctx context.Context, name, taskDesc string, serverName string, toolsHint string) {
    // 基础工具 + 指定 MCP 服务器的工具
    if serverName != "" {
        engine.PreloadServer(serverName)
    }
    // system prompt 追加工具指引
    if toolsHint != "" {
        engine.AppendToSystemPrompt(toolsHint)
    }
}
```

子代理的工具集：
- 基础工具（~14 个）：command、文件操作、web、query_server、subagent_task（禁用递归）
- 如果指定了 server：加载该 MCP 服务器的全部工具
- **子代理不支持再创建子代理**（`subagent_task` 返回错误"子代理中不能再次创建子代理"）

### 主 agent 并行工作流

```
主 agent 视角:

1. 获取 50 家公司列表（使用 command 或 read_file）
2. 拆分为 5 批，每批 10 家
3. 并发创建 5 个子代理:
   subagent_task(name="batch-1", server="tyc-mcp",
       task="查询以下 10 家公司的天眼查舆情...",
       tools_hint="使用 get_news_sentiment，searchKey 传公司名")
4. 等待所有子代理完成
5. 汇总结果

主 agent 上下文变化:
- 第 1 步后: 50 家公司列表 (~500 tokens)
- 第 3 步后: 5 个子代理名 (~50 tokens)
- 第 5 步后: 汇总摘要 (~500 tokens)
- 全程没有 MCP 工具定义
```

---

## 3. 引擎进度反馈

（与 v1 设计相同，无变化）

在 `StreamEvent` 中新增 `progress` 类型，引擎自动检测 N 轮无文本输出时发送。

### 触发规则

```
首次检测到 5 轮无 text_delta → emit progress {iterations: 5, elapsed: "2m30s", status: "running"}
之后每 5 轮无输出 → emit progress 一次
恢复文本输出 → emit progress {status: "done"}
```

### IM 端处理

`handleIMMessage` 的事件循环中捕获 `progress` 事件并转发：

```go
case "progress":
    var p ProgressData
    json.Unmarshal(evt.Data, &p)
    if p.Status == "running" && p.Iterations % 10 == 5 {
        flushIM(fmt.Sprintf("⏳ 正在处理，已完成 %d 轮操作...", p.Iterations))
    }
```

---

## 文件变更清单

### 第一阶段：MCP 摘要 + ToolRegistry 重构

| 文件 | 变更 |
|------|------|
| `internal/agent/tool_registry.go` | 新增 ListBasic / ListByServer / ListServers 方法；移除标签系统的痕迹 |
| `internal/agent/mcp_tool.go` | 新增 serverSummaries map + generateSummary + toolsByServer |
| `internal/agent/provider.go` | 新增 QueryServerTool、StreamEvent progress 类型 |
| `internal/agent/engine.go` | 首轮工具集改用 ListBasic；system prompt 加入服务器摘要 |

### 第二阶段：子代理 + MCP 绑定

| 文件 | 变更 |
|------|------|
| `internal/agent/subagent.go` | SpawnAgent 增加 server 参数；runSubAgent 加载对应服务器工具 |
| `internal/agent/provider.go` | SubAgentTool schema 增加 server 字段 |
| `internal/agent/engine.go` | 新增 PreloadServer、AppendToSystemPrompt |

### 第三阶段：进度反馈

| 文件 | 变更 |
|------|------|
| `internal/agent/engine.go` | 主循环中增加进度检测 |
| `internal/agent/provider.go` | ProgressData 结构体 |
| `internal/web/integration.go` | 处理 progress 事件 |

---

## 测试策略

### MCP 摘要（单元测试）
- `TestGenerateSummary_FromToolNames`
- `TestListByServer_ReturnsCorrectTools`
- `TestListBasic_ReturnsBuiltinTools`

### 子代理绑定（集成测试）
- `TestSubAgentTool_WithServer` — 子代理加载指定服务器的工具
- `TestSubAgentTool_Result` — 子代理执行并返回结果

### 进度反馈（单元测试）
- `TestEngine_EmitsProgressAfterSilentIterations`
- `TestEngine_NoProgressWhenTextProduced`
