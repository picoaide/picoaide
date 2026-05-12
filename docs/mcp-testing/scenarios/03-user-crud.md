# 测试场景 03：用户管理 CRUD

## 前置条件
- PicoAide 服务运行在 {SERVER_URL}
- 当前认证模式为 local
- 超管账号: admin / admin123

## 测试用例

### TC-03-01: 访问用户管理页
操作步骤：
1. browser_navigate({ "url": "{SERVER_URL}/admin/users" })
2. browser_get_content({})

预期结果：
- 显示用户列表
- 包含创建用户等操作按钮

### TC-03-02: 创建用户（自动生成密码）
操作步骤：
1. 以超管 admin 登录
2. POST /api/admin/users/create 带 username=testcrud1（不传 password）
3. 检查响应

预期结果：
- 返回 {"success":true,"username":"testcrud1","password":"<自动生成的密码>"}
- 用户已创建到 local_users 表

### TC-03-03: 创建用户（指定密码）
操作步骤：
1. POST /api/admin/users/create 带 username=testcrud2&password=testpass123

预期结果：
- 成功创建
- 可以用 testpass123 登录

### TC-03-04: 创建用户（重复用户名）
操作步骤：
1. POST /api/admin/users/create 带 username=testcrud1&password=xxx

预期结果：
- 返回 400 错误，提示"用户名已存在"

### TC-03-05: 创建用户（无效用户名）
操作步骤：
1. POST /api/admin/users/create 带 username="../../etc"

预期结果：
- 返回 400 错误
- 用户名不合法（含路径遍历字符）

### TC-03-06: 用户搜索
操作步骤：
1. GET /api/admin/users?search=testcrud
2. 检查返回列表

预期结果：
- 只返回匹配 testcrud 的用户（testcrud1, testcrud2）

### TC-03-07: 用户分页
操作步骤：
1. 先创建多个用户
2. GET /api/admin/users?page=1&page_size=5
3. 检查分页信息

预期结果：
- 返回 total, total_pages, page, page_size 等分页字段
- 用户列表正确

### TC-03-08: 删除用户
操作步骤：
1. GET CSRF token
2. POST /api/admin/users/delete 带 username=testcrud1&csrf_token=xxx
3. 检查响应

预期结果：
- 返回 {"success":true,"message":"用户已删除"}
- 用户目录被归档（移到 archive/）
- GET /api/admin/users 不再包含 testcrud1

### TC-03-09: 普通用户不能访问超管端点
操作步骤：
1. 以普通用户 testcrud2 登录
2. GET /api/admin/users

预期结果：
- 返回 403 "仅超级管理员可访问"

### TC-03-10: 批量创建用户
操作步骤：
1. 以超管 admin 登录
2. POST /api/admin/users/batch-create 带 usernames=batch1,batch2,batch3&csrf_token=xxx
3. 检查响应

预期结果：
- 成功创建
- 列出创建的每个用户及其密码

### TC-03-11: 本地模式下统一认证模式拒绝创建用户
操作步骤：
1. 先切换认证模式为 ldap（POST /api/config 修改 web.auth_mode="ldap"）
2. 尝试 POST /api/admin/users/create

预期结果：
- 返回 400 错误
- 提示当前为统一认证模式，不支持创建本地用户

## 清理
- 删除所有测试用户（testcrud1, testcrud2, batch1, batch2, batch3）
- 如果切换了认证模式，切回 local
