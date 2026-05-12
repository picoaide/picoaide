# MCP Browser 工具使用指南

> 测试服务器地址见 [docs/ai-guide.md](../ai-guide.md)，默认 `{SERVER_URL}`

## 概述
PicoAide 的 Browser MCP 工具允许 AI 通过 MCP SSE 协议控制真实浏览器，实现对 Web UI 的自动化操作和验证。这是进行端到端测试的基础。

## 工具列表
列出所有可用的 Browser MCP 工具，每个工具包含名称、参数、用途描述：

| 工具名 | 参数 | 用途 |
|--------|------|------|
| browser_navigate | url (string) | 导航到指定 URL |
| browser_screenshot | 无 | 截取当前页面截图 |
| browser_click | selector (string) | 点击 CSS 选择器匹配的元素 |
| browser_type | selector (string), text (string) | 在指定元素中输入文字 |
| browser_get_content | selector (可选) | 获取页面文本内容 |
| browser_execute | script (string) | 在页面中执行 JS |
| browser_tabs_list | 无 | 列出所有标签页 |
| browser_tab_new | url (可选) | 新建标签页 |
| browser_tab_close | tabId (integer) | 关闭标签页 |
| browser_go_back | 无 | 浏览器后退 |
| browser_go_forward | 无 | 浏览器前进 |
| browser_reload | bypassCache (可选) | 刷新页面 |
| browser_current_tab | 无 | 获取当前标签页信息 |
| browser_tab_select | tabId (integer) | 切换标签页 |
| browser_scroll | selector/x/y | 滚动页面或元素 |
| browser_key_press | key, selector/ctrl/shift/alt/meta | 键盘操作 |
| browser_get_attribute | selector, name | 获取元素属性 |
| browser_get_links | selector/limit | 提取页面链接 |
| browser_wait | selector, timeout | 等待元素出现 |

## 基本操作模式

### 导航到页面
```
browser_navigate({ "url": "{SERVER_URL}/login" })
```

### 在输入框中输入文字
```
browser_type({ "selector": "#username", "text": "admin" })
```

### 点击按钮或链接
```
browser_click({ "selector": "#login-btn" })
```
如果普通选择器无效，可以使用更具体的 CSS 选择器：
```
browser_click({ "selector": "button[type='submit']" })
```

### 验证页面内容
```
browser_get_content({})
```
或：
```
browser_get_content({ "selector": ".error-message" })
```

### 截图验证
```
browser_screenshot({})
```
用于验证页面布局、元素可见性等视觉检查。

### 执行自定义 JS
```
browser_execute({ "script": "return document.title" })
```

### 等待元素出现
```
browser_wait({ "selector": ".user-list", "timeout": 5000 })
```

## 常见操作场景

### 登录操作
1. 导航到登录页
2. 输入用户名
3. 输入密码
4. 点击登录按钮
5. 验证跳转成功

### 表单填写
1. 导航到表单页
2. 使用 browser_type 填写各个字段
3. 使用 browser_click 提交表单
4. 验证成功提示

### 弹窗/对话框处理
- 使用 browser_get_content 获取弹窗文本
- 使用 browser_click 点击弹窗上的确认/取消按钮

## 注意事项
1. 每次操作后等待页面加载完成再执行下一步
2. 推荐使用 browser_wait 等待关键元素出现
3. 选择器尽量使用 ID 选择器（如 #username），其次是属性选择器
4. 验证点使用 browser_get_content 或 browser_screenshot
5. 操作失败时检查选择器是否正确，必要时先用 browser_get_content 获取页面内容确认当前状态

## 测试执行流程
1. 读取测试场景文档（docs/mcp-testing/scenarios/*.md）
2. 按照场景的步骤执行
3. 每一步先执行操作，然后验证预期结果
4. 实际结果与预期不符时记录为 bug
5. 测试完成后清理测试数据
