---
title: "MCP 与 AI 集成"
description: "PicoAide MCP 协议说明、Token 管理、工具配置和 AI 集成最佳实践"
weight: 11
draft: false
---

MCP（Model Context Protocol）是 AI Agent 与外部工具之间的通信协议。PicoAide 提供 MCP SSE 端点，让 AI Agent 可以通过标准接口调用浏览器和桌面工具。本文档面向 AI Agent 的配置者和使用者。

## 什么是 MCP

MCP 定义了一种标准方式，让 AI Agent（如运行在容器中的 PicoClaw）可以发现和调用外部工具。PicoAide 实现了 MCP 的服务端角色，提供标准的 `tools/list` 和 `tools/call` 接口。

PicoAide 的 MCP 架构不要求 AI Agent 直接连接浏览器或桌面，而是通过 WebSocket 中继到用户授权的执行端：

```text
AI Agent (PicoClaw 容器)
    │  SSE 连接
    ▼
PicoAide MCP Service
    │  WebSocket 中继
    ▼
用户授权执行端 (浏览器扩展 / 桌面客户端)
```

## MCP 服务端点

PicoAide 注册了两个 MCP 服务：

| 服务 | SSE 端点 | 对应执行端 |
|------|---------|-----------|
| browser | `/api/mcp/sse/browser` | 浏览器扩展 (PicoAide Helper) |
| computer | `/api/mcp/sse/computer` | 桌面客户端 |

每个服务都有独立的工具集：

- **browser**：19 个浏览器控制工具（导航、点击、输入、截图、标签页管理等）
- **computer**：15 个桌面控制工具（截图、鼠标、键盘、OCR、文件操作等）

## 获取 MCP Token

MCP Token 是 AI Agent 连接 MCP 端点的凭证。每个普通用户拥有独立的 Token。

### 通过 Web 面板获取

1. 登录 PicoAide Web 面板（`/manage`）
2. 你的 MCP Token 在面板中显示
3. 复制 Token 用于容器配置

### 通过 API 获取

```text
GET /api/mcp/token
```

首次请求 Token 时会自动生成。Token 格式为 `用户名:随机hex`。

### Token 认证方式

MCP Token 可以通过以下方式传递给服务端：

1. **Query string**：`?token=<token>`
2. **Authorization Header**：`Authorization: Bearer <token>`

### 注意事项

- **MCP Token 是敏感信息**，等同于你的身份凭证
- 超管不能获取 MCP Token（超管身份不应用于工具调用场景）
- 如果需要刷新 Token，联系管理员重置

## 配置 AI Agent 工具

AI Agent（PicoClaw 容器）需要配置 MCP Server 才能在 `tools/list` 中看到 browser 和 computer 工具。

### 容器内的 config.json

在你的容器配置 `config.json` 中添加如下内容：

```json
{
  "tools": {
    "mcp": {
      "servers": {
        "browser": {
          "enabled": true,
          "type": "sse",
          "url": "http://100.64.0.1:80/api/mcp/sse/browser?token=<你的MCP-Token>"
        },
        "computer": {
          "enabled": true,
          "type": "sse",
          "url": "http://100.64.0.1:80/api/mcp/sse/computer?token=<你的MCP-Token>"
        }
      }
    }
  }
}
```

### 地址说明

- `100.64.0.1` 是 Picoaide 服务端在 `picoaide-net` 网络中的网关地址
- 端口与 PicoAide 服务端监听端口一致（默认 `:80`）
- Token 需要替换为实际值

配置保存并重启容器后，AI Agent 的 `tools/list` 响应中就会包含 browser 和 computer 工具。

## 验证工具可用性

### 检查 AI Agent 是否已连接

AI Agent 连接 MCP 端点后，会在 MCP Service 中注册。你可以通过工具调用结果判断：

```text
# 如果 browser 工具可用：
AI 成功调用 browser_screenshot 返回截图

# 如果 browser 工具不可用：
返回 "picoaide-browser 代理未连接"
```

### 确认执行端是否在线

- **browser 工具**：需要用户在浏览器扩展中完成授权
- **computer 工具**：需要用户启动桌面客户端并登录

## 工具调用流程详解

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

### 执行路径

```text
1. AI Agent 通过 SSE 发送 tools/call 请求
2. PicoAide MCP Service 接收请求
3. ServiceHub 查找该用户的对应执行端 WebSocket
4. 将命令转发给执行端（浏览器扩展或桌面客户端）
5. 执行端在用户设备上执行操作
6. 结果沿原路径返回
```

## 最佳实践

### browser 工具组合使用

**场景：AI 分析网页内容并保存**

```text
1. browser_navigate → 打开目标网页
2. browser_wait → 等待页面加载完成
3. browser_screenshot → 截取页面截图（用于视觉分析）
4. browser_get_content → 提取页面文本内容
5. AI 分析内容后调用 file_write 保存到工作区
```

**场景：AI 自动填写并提交表单**

```text
1. browser_navigate → 打开表单页面
2. browser_screenshot → 查看表单布局
3. browser_get_content → 获取表单字段信息
4. browser_type → 逐字段输入内容
5. browser_click → 点击提交按钮
6. browser_screenshot → 确认提交成功
```

### computer 工具组合使用

**场景：AI 读取桌面应用数据**

```text
1. computer_screenshot → 截取屏幕
2. computer_screen_text → OCR 识别界面文字
3. computer_mouse_click → 点击应用中的按钮
4. computer_screenshot → 确认操作结果
```

**场景：AI 整理工作文件**

```text
1. computer_whitelist → 获取可访问目录
2. computer_file_list → 浏览目录内容
3. computer_file_read → 读取文件内容
4. AI 分析后调用 computer_file_write 写出
5. computer_file_search → 确认结果文件存在
```

### 工具调用注意事项

- **超时不等待**：如果执行端未连接，工具调用会立即返回「未连接」错误，不会超时等待
- **保持连接**：执行端（浏览器扩展/桌面客户端）需要保持 WebSocket 在线
- **一次一个操作**：browser/computer 工具一次执行一个操作，复杂任务需要 AI 规划多步执行
- **用户可随时中断**：用户关闭扩展或客户端后，所有工具调用都会失败

### MCP 协议支持

PicoAide MCP 端点支持的 JSON-RPC 方法：

| 方法 | 说明 |
|------|------|
| `initialize` | 初始化连接，协商协议版本 |
| `notifications/initialized` | 通知服务端初始化完成 |
| `tools/list` | 获取当前可用的工具列表 |
| `tools/call` | 调用指定工具 |

协议版本为 `2024-11-05`，同时支持 Legacy SSE 和 Streamable HTTP 传输层。
