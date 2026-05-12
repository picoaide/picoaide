---
title: "浏览器扩展"
description: "PicoAide Helper 浏览器扩展的安装、授权、使用和 Browser MCP 工具详解"
weight: 9
draft: false
---

PicoAide Helper 是 AI 操作浏览器的执行端。它已上架 Chrome 网上应用店，你也可以在内网环境中手动加载。核心机制是：用户授权 → WebSocket 连接 → AI 通过 MCP 工具控制浏览器。

## 安装方式

### 方式一：Chrome 网上应用店安装（推荐）

PicoAide Helper 已发布到 Chrome Web Store，可以直接安装：

1. 访问 [Chrome Web Store](https://chromewebstore.google.com/detail/picoaide-helper/nbmhmeodjpfmoldjomngknknakklebje)
2. 点击「添加至 Chrome」
3. 确认权限后完成安装

### 方式二：手动加载（内网环境）

如果 Chrome 扩展商店无法访问，可以手动加载：

1. 获取 `picoaide-extension` 目录（从 Git 仓库或管理员处获取）
2. 打开 Chrome 的 `chrome://extensions/`
3. 开启右上角的「开发者模式」
4. 点击左上角「加载已解压的扩展程序」
5. 选择 `picoaide-extension` 目录

### 验证安装

安装成功后，Chrome 工具栏会出现 PicoAide Helper 图标。点击图标打开扩展弹窗。

## 登录限制

### 必须使用普通用户

扩展必须使用**普通用户**账号登录。服务端会拒绝超管登录：

```text
超管用户不允许登录插件，使用普通用户登录
```

如果还没有普通用户账号，请联系管理员创建。

### 登录步骤

1. 点击 Chrome 工具栏中的 PicoAide Helper 图标
2. 在弹窗中输入 PicoAide 服务器地址（如 `http://picoaide.example.com`）
3. 输入普通用户的用户名和密码
4. 点击登录
5. 登录成功后，弹窗会显示已登录状态和用户信息

## 授权 AI 控制浏览器

登录扩展不代表 AI 可以控制你的浏览器。需要额外完成授权步骤：

### 授权流程

```text
1. 用户点击扩展图标打开弹窗
2. 点击「授权 AI 控制当前标签页」按钮
3. 扩展通过 WebSocket 连接到 PicoAide Server
4. AI Agent 可以通过 MCP 协议控制你当前所在的标签页
```

### 授权后的变化

- 扩展弹窗会显示"已授权"状态和连接时长
- AI 可以看到你当前页面的标题和 URL
- AI 可以执行导航、点击、输入、截图等操作
- 你可以在任何时候点击「断开连接」取消授权

### 授权状态说明

| 状态 | 含义 |
|------|------|
| 未登录 | 需要先登录扩展 |
| 已登录，未授权 | 已连接服务器，但 AI 无法控制浏览器 |
| 已授权 | AI 可以控制当前标签页 |
| 连接断开 | 网络故障或服务端重启，需要重新授权 |

## Cookie 同步

Cookie 同步用于把当前页面的登录态写入你的 `.security.yml` 配置文件，让 AI Agent 在容器中可以复用这些登录态。

### 同步操作

1. 导航到需要同步登录态的网站
2. 确认已经登录该网站
3. 在扩展弹窗中点击「同步 Cookie」
4. 成功同步后，AI Agent 的配置中会包含该站点的 Cookie

### 注意事项

- **Cookie 同步不等于浏览器控制授权**。它只是把登录态传递给 AI Agent 的配置文件。
- Cookie 同步后，AI 可以在容器的配置中读取这些 Cookie，用于 API 调用或脚本执行。
- 如果 AI 需要操作真实的浏览器页面，仍然必须开启 browser MCP 连接（即完成授权流程）。

## Browser MCP 工具列表

授权连接建立后，AI Agent 可以看到并使用以下 19 个浏览器操作工具：

| 工具 | 说明 | 典型使用场景 |
|------|------|-------------|
| `browser_navigate` | 导航当前受控标签页到 URL | AI 打开指定网页 |
| `browser_screenshot` | 截取当前标签页截图 | AI 分析页面布局 |
| `browser_click` | 用 CSS 选择器点击元素 | AI 点击按钮或链接 |
| `browser_type` | 向指定元素输入文本 | AI 填写表单 |
| `browser_get_content` | 获取页面或元素文本 | AI 提取文章内容 |
| `browser_execute` | 执行 JavaScript | AI 执行页面脚本 |
| `browser_tabs_list` | 列出所有标签页 | AI 查看用户打开的页面 |
| `browser_tab_new` | 新建标签页 | AI 打开新页面 |
| `browser_tab_close` | 关闭标签页 | AI 关闭不需要的页面 |
| `browser_go_back` | 后退 | AI 返回上一页 |
| `browser_go_forward` | 前进 | AI 前进到下一页 |
| `browser_reload` | 刷新 | AI 刷新当前页面 |
| `browser_current_tab` | 获取当前受控标签页信息 | AI 确认当前页面状态 |
| `browser_tab_select` | 切换受控标签页 | AI 切换到另一个标签页 |
| `browser_scroll` | 滚动窗口或元素 | AI 滚动查看更多内容 |
| `browser_key_press` | 发送键盘事件 | AI 模拟键盘操作 |
| `browser_get_attribute` | 获取元素属性 | AI 读取元素的属性值 |
| `browser_get_links` | 提取页面链接 | AI 收集页面上所有链接 |
| `browser_wait` | 等待元素出现 | AI 等待页面加载完成 |

## 典型使用场景

### AI 阅读网页内容做摘要

```text
1. 用户导航到需要摘要的文章页面
2. 用户打开扩展并授权 AI 控制
3. AI 调用 browser_get_content 获取页面文本
4. AI 对文本进行处理并生成摘要
5. AI 将摘要保存到工作区的文件中
```

### AI 填写表单

```text
1. AI 调用 browser_navigate 打开表单页面
2. AI 调用 browser_screenshot 查看页面布局
3. AI 定位表单字段，调用 browser_type 输入内容
4. AI 调用 browser_click 点击提交按钮
5. AI 调用 browser_screenshot 确认提交成功
```

## 常见问题

### AI 看不到 browser 工具

检查用户容器 `config.json` 是否包含 browser MCP server 的配置，并确认配置已经应用到该用户：

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

### 返回"代理未连接"

说明扩展没有建立 WebSocket 连接。确认：
1. 扩展已用普通用户登录
2. 用户已点击授权按钮
3. 浏览器没有关闭或进入休眠

### Cookie 同步成功但 AI 不能操作页面

Cookie 同步只写入登录态，不会打开浏览器控制连接。需要 browser MCP 控制时必须在扩展中完成授权流程。

### 扩展图标显示异常

尝试在 `chrome://extensions/` 页面找到 PicoAide Helper，点击「启用」或重新加载扩展。
