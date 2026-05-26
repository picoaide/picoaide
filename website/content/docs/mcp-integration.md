---
title: "MCP 与 AI 集成"
description: "PicoAide MCP 协议说明、Token 管理、三服务架构和 AI 工具调用指南"
weight: 12
draft: false
---

MCP（Model Context Protocol）是 AI Agent 与外部工具之间的通信协议。PicoAide 提供 MCP SSE 端点，让 AI Agent 可以通过标准接口调用浏览器、桌面和平台工具。本文档面向 AI Agent 的配置者和使用者。

## MCP 架构概览

PicoAide 注册了三个 MCP 服务，通过 SSE 端点和 WebSocket 中继实现完整的工具调用链路：

```text
AI Agent（沙箱内 picoagent）
     │  SSE 连接 (/api/mcp/sse/:service)
     ▼
PicoAide MCP Service 层
     ├── browser 服务 → WebSocket → 浏览器扩展 (19 个工具)
     ├── computer 服务 → WebSocket → 桌面客户端 (15 个工具)
     └── agent 服务 → 服务端处理 → 平台工具 + 第三方 MCP 聚合 (6+ 个工具)
```

- **browser 服务**：通过 `browserSvc` ServiceHub 中继到浏览器扩展 WebSocket
- **computer 服务**：通过 `computerSvc` ServiceHub 中继到桌面客户端 WebSocket
- **agent 服务**：服务端直接处理的平台工具 + 聚合所有浏览器/桌面/第三方工具

## MCP 服务端点

| 服务 | SSE 端点 | 执行端 | 工具来源 |
|------|---------|--------|---------|
| browser | `/api/mcp/sse/browser` | 浏览器扩展 (PicoAide Helper) | 19 个浏览器控制工具 |
| computer | `/api/mcp/sse/computer` | 桌面客户端 | 15 个桌面控制工具 |
| agent | `/api/mcp/sse/agent` | 服务端 + 代理聚合 | 平台工具 + 代理工具 + 第三方 MCP 工具 |

## 获取 MCP Token

MCP Token 是 AI Agent 连接 MCP 端点的凭证。每个普通用户拥有独立的 Token。

### 通过 API 获取

```text
GET /api/mcp/token
```

首次请求 Token 时会自动生成并持久化。Token 格式为 `用户名:随机hex`。

### Token 认证方式

MCP Token 可以通过以下方式传递给服务端：

1. **Query string**：`?token=<token>`
2. **Authorization Header**：`Authorization: Bearer <token>`

### 注意事项

- **MCP Token 是敏感信息**，等同于你的身份凭证，不要分享给他人
- 超管不能获取 MCP Token（超管身份不应用于工具调用场景）
- Token 存储在 `mcp_tokens` 表中，可通过管理员重置

## 配置 AI Agent 工具

AI Agent（沙箱内的 picoagent）需要配置 MCP Server 才能连接 PicoAide 的 MCP 服务。

### Agent 连接配置

沙箱内的 picoagent 通过 `/api/picoagent/me` 端点自动获取配置，包括 MCP 服务地址和 Token。当 Agent 启动时，它会从 PicoAide 服务端获取运行配置，其中包含所有 MCP 服务的 socket 路径和连接信息。

### PicoAide MCP 工具地址

在沙箱内部，picoagent 通过以下地址连接 MCP 服务：

- MCP SSE 端点：`http://100.64.0.1:80/api/mcp/sse/browser?token=<MCP-Token>`
- 其中 `100.64.0.1` 是 PicoAide 服务端在 `picoaide-br` 网络中的网关地址
- 端口与 PicoAide 服务端监听端口一致（默认 `:80`）

## 工具调用流程

### 完整执行路径

```text
1. AI Agent 通过 SSE 发送 tools/call 请求到 MCP 端点
2. PicoAide MCP Service 接收并解析 JSON-RPC 请求
3. 根据工具名称前缀分发到对应处理器：
   ├── browser_* → browserSvc → WebSocket → 浏览器扩展
   ├── computer_* → computerSvc → WebSocket → 桌面客户端
   ├── picoaide_* → 服务端直接处理
   └── mcp_<name>_* → MCPProxyManager → 第三方 MCP 服务器
4. 执行端在目标设备上执行操作
5. 结果沿原路径返回给 AI Agent
```

### browser 工具调用示例

```json
// AI Agent 发送工具调用请求
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "browser_navigate",
    "arguments": {
      "url": "https://example.com"
    }
  }
}

// PicoAide 返回结果
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "已成功导航到 https://example.com"
      }
    ],
    "isError": false
  }
}
```

### ServiceHub 连接管理

ServiceHub 负责管理用户级 WebSocket 连接：

- **注册**：用户执行端连接时注册到对应 Service Hub
- **单连接**：同一个用户的新连接会替换旧连接
- **超时**：工具调用 30 秒超时
- **保活**：30 秒 Ping 检测连接状态
- **清理**：连接断开时自动清理

## 平台工具（agent 服务）

agent 服务独有地提供了 PicoAide 平台工具，这些工具在服务端直接处理，不需要外部执行端：

| 工具 | 说明 |
|------|------|
| `picoaide_user_info` | 获取当前用户信息（用户名、角色、来源、创建时间） |
| `picoaide_skills_list` | 列出已安装技能 |
| `picoaide_shared_folders` | 列出可访问的共享文件夹 |
| `picoaide_cron_create` | 创建定时任务（参数：schedule, prompt） |
| `picoaide_cron_list` | 列出定时任务列表 |
| `picoaide_cron_delete` | 删除定时任务（参数：id） |

Agent 服务还会自动聚合所有可用工具——当调用 `tools/list` 时，返回结果包含浏览器工具、桌面工具、平台工具和第三方 MCP 工具。

## 第三方 MCP 服务器

PicoAide 支持通过 `MCPProxyManager` 代理第三方 MCP 服务器。

### 支持的传输协议

| 协议 | 说明 | 配置 |
|------|------|------|
| stdio | 通过 stdin/stdout 通信的子进程 | command + args |
| http | 通过 HTTP POST 通信 | url + headers |
| sse | 通过 Server-Sent Events 通信 | url + headers |

### 工具命名

第三方 MCP 工具以 `mcp_<服务器名>_<工具名>` 格式命名，避免与内置工具冲突。例如，名为 `database` 的 MCP 服务器的 `query` 工具会暴露为 `mcp_database_query`。

### 授权控制

通过 `mcp_server_grants` 表控制访问权限：
- `user` 级别：仅指定用户可访问
- `*` 通配符：所有用户可访问

## 验证工具可用性

### 检查工具列表

AI Agent 连接 MCP 端点后，调用 `tools/list` 可以查看所有可用工具。如果 browser/computer 工具对应的执行端未连接，这些工具不会出现在列表中。

### 确认执行端在线

- **browser 工具**：需要在浏览器扩展中登录并点击「授权 AI 控制」
- **computer 工具**：需要启动桌面客户端并保持登录
- **agent 工具**：始终可用，无需额外连接

## 最佳实践

### browser 工具组合使用

**场景：AI 分析网页内容并保存**

```text
1. browser_navigate → 打开目标网页
2. browser_wait → 等待页面加载完成
3. browser_screenshot → 截取页面截图
4. browser_get_content → 提取页面文本内容
5. AI 分析后调用 file_write 保存到工作区
```

### computer 工具组合使用

**场景：AI 读取桌面应用数据**

```text
1. computer_screenshot → 截取屏幕
2. computer_screen_text → OCR 识别界面文字
3. computer_mouse_click → 点击应用中的按钮
4. computer_screenshot → 确认操作结果
```

### 工具调用注意事项

- **超时不等待**：如果执行端未连接，工具调用立即返回错误，不会超时等待
- **保持连接**：浏览器扩展和桌面客户端需要保持 WebSocket 在线
- **一次一个操作**：browser/computer 工具一次执行一个操作，复杂任务需要 AI 规划多步
- **用户可随时中断**：用户关闭扩展或客户端后，所有工具调用都会立即失败

### MCP 协议支持

PicoAide MCP 端点支持的 JSON-RPC 方法：

| 方法 | 说明 |
|------|------|
| `initialize` | 初始化连接，协商协议版本 |
| `notifications/initialized` | 通知服务端初始化完成 |
| `tools/list` | 获取当前可用的工具列表 |
| `tools/call` | 调用指定工具 |

协议版本为 `2024-11-05`，支持 Legacy SSE 传输层和 Streamable HTTP 协议（通过 `Mcp-Protocol-Version` header 协商）。
