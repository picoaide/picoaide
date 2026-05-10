---
title: "浏览器扩展"
description: "PicoAide 浏览器扩展使用指南"
weight: 4
draft: false
---

PicoAide 浏览器扩展（PicoAide Helper）是连接 AI 与浏览器的重要桥梁，让 AI 能够控制浏览器标签页、读取页面内容、执行页面操作。

## 安装

### Chrome Web Store 安装

从 Chrome Web Store 搜索 "PicoAide Helper" 并安装。

### 手动安装

如果无法访问 Chrome Web Store，可以从源码手动安装：

1. 下载 `picoaide-extension` 目录
2. 打开 Chrome，访问 `chrome://extensions/`
3. 开启右上角「开发者模式」
4. 点击「加载已解压的扩展程序」
5. 选择扩展目录

## 登录配置

安装完成后，点击浏览器工具栏的 PicoAide 图标，在弹出窗口中配置连接：

1. **服务器地址**：填写 PicoAide 服务器地址，如 `http://192.168.1.100:80`
2. **用户名**：PicoAide 系统中的用户名
3. **密码**：对应的登录密码
4. 点击「登录」按钮完成认证

登录成功后，扩展可以获取当前用户的 MCP Token。浏览器控制连接不会自动开启；只有用户点击「授权 AI 控制当前标签页」后，扩展才会建立到 PicoAide Server 的 WebSocket 连接。

## Cookie 同步

Cookie 同步功能将浏览器中的登录状态传递给 AI，使 AI 能够访问需要认证的内部网站。

点击「同步登录状态给 AI」按钮，扩展会：

1. 采集当前浏览器的 Cookie 数据
2. 将 Cookie 发送到 PicoAide Server
3. AI 容器获取 Cookie 后可以模拟用户身份访问内部系统

这在使用技能调用内部网站时特别有用，AI 可以复用用户已有的登录态。

## AI 浏览器控制

浏览器控制必须通过 MCP 协议完成。AI 不能直接调用浏览器扩展，也不能只靠普通 HTTP API 操作浏览器；它必须调用用户配置中的 `browser` MCP server：

```json
{
  "tools": {
    "mcp": {
      "servers": {
        "browser": {
          "enabled": true,
          "type": "sse",
          "url": "http://100.64.0.1:80/api/mcp/sse/browser?token=<mcp-token>"
        }
      }
    }
  }
}
```

使用前需要同时满足：

1. 用户已在浏览器扩展中登录普通用户账号。
2. 用户已点击「授权 AI 控制当前标签页」。
3. 扩展显示 AI 浏览器控制已开启，并保持浏览器打开。
4. AI 容器的 `config.json` 中已注入 `tools.mcp.servers.browser`。

如果扩展未连接，AI 调用 `browser_*` MCP 工具时会收到 `picoaide-browser 代理未连接`。这时需要让用户打开插件并重新点击「授权 AI 控制当前标签页」。

连接成功后，AI 可以执行以下操作：

### 导航操作

| 工具                 | 说明                     |
| -------------------- | ------------------------ |
| `browser_navigate`   | 导航到指定 URL           |
| `browser_go_back`    | 浏览器后退               |
| `browser_go_forward` | 浏览器前进               |
| `browser_reload`     | 刷新当前标签页           |

### 页面交互

| 工具                 | 说明                               |
| -------------------- | ---------------------------------- |
| `browser_click`      | 通过 CSS 选择器点击页面元素        |
| `browser_type`       | 在指定元素中输入文字               |
| `browser_screenshot` | 截取当前标签页屏幕截图             |
| `browser_get_content`| 获取页面文本内容                   |
| `browser_execute`    | 在页面中执行 JavaScript 代码       |
| `browser_scroll`     | 滚动页面或指定元素                 |
| `browser_key_press`  | 向当前焦点元素或指定元素发送按键   |
| `browser_get_attribute` | 获取页面元素属性或 DOM 属性值   |
| `browser_get_links`  | 提取页面或指定区域内的链接         |

### 标签页管理

| 工具               | 说明               |
| ------------------ | ------------------ |
| `browser_tabs_list`| 列出所有标签页     |
| `browser_tab_new`  | 新建标签页         |
| `browser_tab_close`| 关闭指定标签页     |
| `browser_tab_select` | 切换当前受控标签页 |
| `browser_current_tab` | 获取当前受控标签页信息 |

### 等待操作

| 工具            | 说明                       |
| --------------- | -------------------------- |
| `browser_wait`  | 等待页面中指定元素出现     |

### 使用示例

AI 可以通过组合这些工具完成复杂的浏览器操作，例如：

1. 导航到内部 OA 系统
2. 等待登录页面加载
3. 填写用户名和密码
4. 点击登录按钮
5. 等待主页面加载
6. 截取页面截图反馈给用户

## 管理后台入口

扩展不内置管理后台。点击「打开管理后台」会跳转到 PicoAide Server 提供的 Web 页面：

- 普通用户：`/manage`
- 超级管理员：`/admin/dashboard`

## 工作原理

浏览器扩展的工作流程：

```
用户点击「授权 AI 控制当前标签页」
    │
    ▼
扩展通过 WebSocket 连接 PicoAide Server
(/api/browser/ws?token=xxx)
    │
    ▼
PicoAide 将 AI 的 MCP 工具调用请求
通过 WebSocket 转发给扩展
    │
    ▼
扩展在浏览器中执行对应操作
（导航、点击、输入、截图等）
    │
    ▼
扩展将执行结果通过 WebSocket 返回
    │
    ▼
PicoAide Server 将结果通过 MCP SSE 返回给 AI
```

扩展始终以用户身份运行，所有操作受浏览器权限约束。用户可以随时断开 AI 控制连接。

## 常见连接问题

- `picoaide-browser 代理未连接`：扩展没有建立 WebSocket。请确认用户已登录插件，并点击「授权 AI 控制当前标签页」。
- `获取 MCP token 失败`：当前插件登录态不是普通用户，或会话已过期。请重新登录普通用户账号。
- AI 看不到 `browser_*` 工具：用户容器配置没有注入 `tools.mcp.servers.browser`，需要在管理后台重新应用配置或重建用户配置。
- Cookie 同步只负责把当前网站登录态写入用户安全配置，不会自动开启浏览器控制。需要操作真实浏览器时，仍必须启用 browser MCP 连接。
