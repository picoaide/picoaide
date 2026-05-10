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

登录成功后，扩展会自动获取 MCP Token 并建立与 PicoAide Server 的 WebSocket 连接。

## Cookie 同步

Cookie 同步功能将浏览器中的登录状态传递给 AI，使 AI 能够访问需要认证的内部网站。

点击「同步登录状态给 AI」按钮，扩展会：

1. 采集当前浏览器的 Cookie 数据
2. 将 Cookie 发送到 PicoAide Server
3. AI 容器获取 Cookie 后可以模拟用户身份访问内部系统

这在使用技能调用内部网站时特别有用，AI 可以复用用户已有的登录态。

## AI 浏览器控制

通过 MCP 协议，AI 可以直接控制浏览器标签页。点击「授权 AI 控制当前标签页」按钮后，AI 可以执行以下操作：

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

## 管理后台功能

管理员用户登录后，扩展会显示额外的管理功能按钮：

- **配置管理**：查看和修改 PicoAide 系统配置
- **管理后台**：打开 PicoAide Web 管理界面，进行用户管理、容器管理、镜像管理等操作

管理功能仅对具有管理员权限的用户可见。

## 工作原理

浏览器扩展的工作流程：

```
用户点击「授权 AI 控制标签页」
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
PicoAide Server 将结果通过 SSE 返回给 AI
```

扩展始终以用户身份运行，所有操作受浏览器权限约束。用户可以随时断开 AI 控制连接。
