# 测试场景：<场景名称>

## 前置条件
- PicoAide 服务运行在 {SERVER_URL}
- MCP Browser 工具可用
- 超管账号: admin / admin123
- LDAP 已在测试服务器上预先配置好（如涉及 LDAP 测试）

## 测试用例

### TC-<编号>: <用例名称>
**涉及功能**：<页面名称> - <功能点>

操作步骤：
1. browser_navigate({ "url": "{SERVER_URL}/<页面路径>" })
2. browser_type({ "selector": "<选择器>", "text": "<值>" })
3. browser_click({ "selector": "<选择器>" })
4. browser_get_content({ "selector": "<选择器>" })
5. browser_screenshot({})

预期结果：
- 页面正确加载
- 操作成功，显示成功提示
- API 响应返回正确状态码和数据

### TC-<编号>: <用例名称>

操作步骤：
1. browser_execute({ "script": "return fetch('{SERVER_URL}/api/xxx', ...).then(r=>r.json())" })

预期结果：
- API 返回 {"success":true,...}

## 清理
- 删除测试创建的数据
- 确保服务回到初始状态

---

## 编写规范

### 用例编号规则
- 格式：TC-<场景编号>-<用例序号>
- 如：01 号场景的用例 01 为 TC-01-01

### 操作步骤写法
- 使用具体的 MCP 工具调用
- browser_navigate 用于页面跳转
- browser_type 用于输入文字（需指定 CSS 选择器）
- browser_click 用于点击操作
- browser_get_content 用于获取页面文字验证
- browser_screenshot 用于视觉验证
- browser_execute 用于调用 API 或执行 JS
- browser_wait 用于等待元素加载

### 预期结果写法
- 描述页面应显示的内容
- 描述 API 应返回的响应
- 描述系统行为（如跳转、提示等）

### 验证点选择
- 优先使用 browser_get_content 获取文本验证
- 必要时使用 browser_screenshot 截图验证
- 关键操作通过 API 调用验证后端状态
