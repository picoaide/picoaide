# 测试场景 01：登录流程

## 前置条件
- PicoAide 服务运行在 {SERVER_URL}
- MCP Browser 工具可用
- 超管账号: admin / admin123

## 测试用例

### TC-01-01: 访问登录页
操作步骤：
1. browser_navigate({ "url": "{SERVER_URL}/login" })
2. browser_screenshot({})（确认页面加载正常）

预期结果：
- 页面标题包含"登录"
- 显示用户名输入框（#username）
- 显示密码输入框（#password）
- 显示登录按钮

### TC-01-02: 空字段提交
操作步骤：
1. 确保用户名和密码为空
2. browser_click({ "selector": "#login-btn" })

预期结果：
- 页面提示"请输入用户名和密码"
- 保持在登录页

### TC-01-03: 错误密码登录
操作步骤：
1. browser_type({ "selector": "#username", "text": "admin" })
2. browser_type({ "selector": "#password", "text": "wrongpassword" })
3. browser_click({ "selector": "#login-btn" })

预期结果：
- 页面提示"用户名或密码错误"
- 保持在登录页
- 不创建 session cookie

### TC-01-04: 正确密码登录（超管）
操作步骤：
1. 清空密码输入框
2. browser_type({ "selector": "#password", "text": "admin123" })
3. browser_click({ "selector": "#login-btn" })

预期结果：
- 跳转到 /admin/dashboard 管理后台
- 页面显示管理后台的侧边栏导航
- API 验证：GET /api/user/info 返回 {"role": "superadmin"}

### TC-01-05: Session 持久化
操作步骤：
1. browser_navigate({ "url": "{SERVER_URL}/admin/users" })

预期结果：
- 直接进入用户管理页
- 未要求重新登录

### TC-01-06: 登出
操作步骤：
1. 点击页面顶部的退出/登出按钮（可能是 #logout-btn 或包含"退出"的链接）
2. 或者执行 API: POST /api/logout

预期结果：
- 跳转到登录页
- session cookie 被清除
- 再次访问 /admin 时被重定向到 /login

### TC-01-07: 已登出后被重定向到登录页
操作步骤：
1. 在登出状态下
2. browser_navigate({ "url": "{SERVER_URL}/admin/dashboard" })

预期结果：
- 重定向到 /login
- 页面显示登录表单

### TC-01-08: 健康检查 API
操作步骤：
1. browser_execute({ "script": "return fetch('{SERVER_URL}/api/health').then(r=>r.json())" })

预期结果：
- 返回 {"status":"ok","version":"..."}

## 清理
- 确保已登出
