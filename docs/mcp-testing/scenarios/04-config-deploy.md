# 测试场景 04：配置下发与渠道策略

## 前置条件
- PicoAide 服务运行在 {SERVER_URL}
- 当前认证模式为 local
- 超管账号: admin / admin123

## 测试用例

### TC-04-01: 访问渠道策略页
操作步骤：
1. browser_navigate({ "url": "{SERVER_URL}/admin/picoclaw" })
2. browser_get_content({})

预期结果：
- 页面加载正常
- 显示渠道策略配置

### TC-04-02: 查看全局 Picoclaw 配置
操作步骤：
1. 以超管 admin 登录
2. GET /api/config
3. 从响应中提取 picoclaw 部分的配置（包含 channel_list）

预期结果：
- 返回完整的全局配置
- picoclaw 部分包含渠道定义、模型配置等

### TC-04-03: 保存全局 Picoclaw 配置
操作步骤：
1. 获取当前配置
2. 修改 picoclaw.channel_list 添加钉钉渠道
3. POST /api/config 保存

预期结果：
- 保存成功
- 渠道配置已更新到数据库

### TC-04-04: 普通用户查看自己的渠道
操作步骤：
1. 创建测试用户 testconfig1，并确保其容器记录存在
2. 以 testconfig1 登录
3. GET /api/picoclaw/channels

预期结果：
- 返回用户可见的渠道列表
- 只有管理员允许的渠道可见

### TC-04-05: 普通用户配置渠道字段
操作步骤：
1. 以 testconfig1 登录
2. 获取 CSRF token
3. POST /api/picoclaw/config-fields 带 section=dingtalk&values={"enabled":true,"client_id":"test","client_secret":"secret"}&csrf_token=xxx&config_version=3

预期结果：
- 配置保存成功
- 返回 "配置已保存，容器正在重启中"

### TC-04-06: 普通用户读取渠道配置字段
操作步骤：
1. 以 testconfig1 登录
2. GET /api/picoclaw/config-fields?section=dingtalk&config_version=3

预期结果：
- 返回之前在 TC-04-05 中保存的值
- 字段包含 client_id, client_secret 等

### TC-04-07: 用户不能启用管理员禁用的渠道
操作步骤：
1. 以超管登录，在全局配置中将某个渠道设为 enabled=false
2. 以普通用户登录
3. 尝试 POST 配置该禁用的渠道

预期结果：
- 用户无法为该渠道保存配置
- 返回 400 错误

### TC-04-08: 配置下发到单个用户（admin config apply）
操作步骤：
1. 以超管 admin 登录
2. 创建测试用户 testconfig2
3. GET CSRF token
4. POST /api/admin/config/apply 带 username=testconfig2&csrf_token=xxx
5. 检查响应

预期结果：
- 配置下发成功
- testconfig2 的用户目录下生成了正确的 .picoclaw/config.json
- 容器重启

### TC-04-09: 配置下发到组
操作步骤：
1. 创建一个测试组，将多个用户加入该组
2. POST /api/admin/config/apply 带 group=<组名>&csrf_token=xxx

预期结果：
- 配置下发到组内所有用户
- 如果是多个用户，返回 task_id
- 可以通过 GET /api/admin/task/status?task_id=xxx 查询进度

### TC-04-10: 查看迁移规则
操作步骤：
1. 以超管登录
2. GET /api/admin/migration-rules

预期结果：
- 返回迁移规则列表
- 包含 picoaide_supported_config_version=3
- 包含 versions 列表和迁移链

### TC-04-11: 全量下发配置
操作步骤：
1. POST /api/admin/config/apply（不指定 username 和 group）
2. 检查响应

预期结果：
- 下发到所有用户
- 返回 task_id
- 任务队列开始执行

## 清理
- 删除测试用户 testconfig1, testconfig2
- 删除测试组
- 恢复全局配置（如果需要）
