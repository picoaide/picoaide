---
title: "浏览器扩展"
description: "PicoAide Helper 浏览器扩展的安装、授权、Cookie 同步和 browser MCP 工具"
weight: 4
draft: false
---

PicoAide Helper 是浏览器执行端。它不直接让 AI 获得浏览器控制权，只有用户登录扩展并点击授权后，扩展才会通过 WebSocket 连接 PicoAide Server。

## 安装方式

开发或内网环境可以手动加载：

1. 获取仓库中的 `picoaide-extension` 目录
2. 打开 Chrome 的 `chrome://extensions/`
3. 开启「开发者模式」
4. 点击「加载已解压的扩展程序」
5. 选择 `picoaide-extension`

扩展入口文件包括：

- `manifest.json`
- `popup.html`
- `popup.js`
- `background.js`
- `offscreen.html`
- `offscreen.js`

## 登录限制

扩展必须使用普通用户账号登录。服务端会拒绝超管登录扩展：

```text
超管用户不允许登录插件，使用普通用户登录
```

登录后扩展可以调用：

- `GET /api/user/info`
- `GET /api/csrf`
- `GET /api/mcp/token`

MCP token 首次请求时自动生成。

## Cookie 同步

Cookie 同步用于把当前页面登录态写入用户的 `.security.yml`。接口是：

```text
POST /api/cookies
```

表单字段：

| 字段 | 说明 |
| --- | --- |
| `domain` | 当前站点域名 |
| `cookies` | Cookie 字符串 |
| `csrf_token` | 当前会话 CSRF token |

Cookie 同步不等于浏览器控制授权。它只让用户容器可以在技能或脚本中复用登录态；如果 AI 需要操作真实浏览器标签页，仍然必须开启 browser MCP 连接。

## AI 浏览器控制

浏览器控制路径如下：

```text
用户点击授权
  -> 扩展连接 /api/browser/ws?token=...
  -> AI 容器调用 /api/mcp/sse/browser?token=...
  -> tools/call
  -> PicoAide 转发给扩展
  -> 扩展在当前浏览器执行
```

用户必须同时满足：

1. 扩展已登录普通用户
2. 用户已点击「授权 AI 控制当前标签页」
3. 扩展 WebSocket 保持在线
4. 用户容器配置中已注入 browser MCP server

配置示例：

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

## Browser MCP 工具

当前代码注册的 browser 工具包括：

| 工具 | 说明 |
| --- | --- |
| `browser_navigate` | 导航当前受控标签页到 URL |
| `browser_screenshot` | 截取当前标签页截图 |
| `browser_click` | 用 CSS 选择器点击元素 |
| `browser_type` | 向指定元素输入文本 |
| `browser_get_content` | 获取页面或元素文本 |
| `browser_execute` | 执行 JavaScript |
| `browser_tabs_list` | 列出标签页 |
| `browser_tab_new` | 新建标签页 |
| `browser_tab_close` | 关闭标签页 |
| `browser_go_back` | 后退 |
| `browser_go_forward` | 前进 |
| `browser_reload` | 刷新 |
| `browser_current_tab` | 获取当前受控标签页信息 |
| `browser_tab_select` | 切换受控标签页 |
| `browser_scroll` | 滚动窗口或元素 |
| `browser_key_press` | 发送键盘事件 |
| `browser_get_attribute` | 获取元素属性 |
| `browser_get_links` | 提取链接 |
| `browser_wait` | 等待元素出现 |

## 管理后台入口

扩展不内置管理后台。服务端提供页面：

| 用户类型 | 页面 |
| --- | --- |
| 普通用户 | `/manage` |
| 超级管理员 | `/admin/dashboard` |

如果普通用户访问 `/admin/*`，会被重定向到 `/manage`。如果超管访问 `/manage`，会被重定向到 `/admin/dashboard`。

## 常见问题

### AI 看不到 browser 工具

检查用户容器 `config.json` 是否包含 browser MCP server，并确认配置已经应用到该用户容器。

### 返回代理未连接

说明扩展没有建立 WebSocket。让用户打开扩展并重新点击授权。

### Cookie 同步成功但 AI 不能操作页面

Cookie 同步只写入登录态，不会打开浏览器控制连接。需要 browser MCP 控制时必须启用授权连接。
