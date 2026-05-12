# 测试场景 02：认证模式切换（重点）

## 前置条件
- PicoAide 服务运行在 {SERVER_URL}
- MCP Browser 工具可用
- 超管账号: admin / admin123
- LDAP 已在测试服务器上预先配置好（具体配置参数已知）
- 当前认证模式已知

## 测试用例

### TC-02-01: 查看当前认证模式
操作步骤：
1. browser_execute({ "script": "return fetch('{SERVER_URL}/api/login/mode').then(r=>r.json())" })

预期结果：
- 返回 `{"success":true,"auth_mode":"...","provider":{...}}`
- 确认当前模式（可能是 local 或 ldap）
- provider 字段包含对应认证源的元信息

### TC-02-02: 在 local 模式下创建一个普通用户
前置条件：
- 如果当前是 ldap 模式，先执行 TC-02-09 切回 local
- 以超管 admin 登录

操作步骤：
1. browser_execute({ "script": "return fetch('{SERVER_URL}/api/admin/users/create', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'username=testuser_local&password=test123' }).then(r=>r.json())" })
2. browser_execute({ "script": "return fetch('{SERVER_URL}/api/login', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'username=testuser_local&password=test123' }).then(r=>r.json())" })
3. browser_execute({ "script": "return fetch('{SERVER_URL}/api/user/info').then(r=>r.json())" })

预期结果：
- 用户创建成功，返回 `{"success":true}`
- testuser_local 可以成功登录，返回 `{"success":true,"username":"testuser_local"}`
- GET /api/user/info 返回 `{"role":"user","username":"testuser_local",...}`

### TC-02-03: 从 local 切换到 ldap 模式（关键测试）
操作步骤：
1. browser_execute({ "script": "return fetch('{SERVER_URL}/api/login', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'username=admin&password=admin123' }).then(r=>r.json())" })
2. browser_execute({ "script": "return fetch('{SERVER_URL}/api/config').then(r=>r.json())" })
3. 获取 CSRF token: browser_execute({ "script": "return fetch('{SERVER_URL}/api/csrf').then(r=>r.json())" })
4. browser_execute({ "script": `return fetch('{SERVER_URL}/api/config', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'config=' + encodeURIComponent(JSON.stringify({"web":{"auth_mode":"ldap"}})) + '&csrf_token=xxx' }).then(r=>r.json())` })

预期结果：
- 配置保存成功，响应包含 `{"success":true,"message":"配置已保存，认证方式已切换并清空旧认证数据","cleanup":{...}}`
- cleanup 对象包含以下非零字段：
  - `containers_removed` — 被删除的 Docker 容器数
  - `container_records` — 被清空的容器记录数（应当 >= containers_removed）
  - `users_removed` — 被删除的普通用户数（>= 1，包含 testuser_local）
  - `groups_cleared: true`
  - `directories_purged: true`
- 验证：GET /api/user/info 不再返回 testuser_local（应返回 401 未认证）
- 验证：超管 admin 仍然可登录（执行步骤 1. 后正常）
- 验证：检查组和容器记录是否已清空

### TC-02-04: 在 ldap 模式下超管逃生通道
操作步骤：
1. 确认当前是 ldap 模式（可调 API /api/login/mode）
2. browser_execute({ "script": "return fetch('{SERVER_URL}/api/login', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'username=admin&password=admin123' }).then(r=>r.json())" })

预期结果：
- 登录成功返回 `{"success":true,"username":"admin"}`
- 超管走本地密码逃生通道，不受 LDAP 认证影响
- 进入管理后台后可正常操作

### TC-02-05: 在 ldap 模式下用 LDAP 用户登录
操作步骤：
1. 确认当前是 ldap 模式
2. browser_execute({ "script": "return fetch('{SERVER_URL}/api/login', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'username=<LDAP用户名>&password=<LDAP密码>' }).then(r=>r.json())" })

预期结果：
- 登录成功，返回 `{"success":true,"username":"<LDAP用户名>","initializing":true/false}`
- 如果是首次登录，initializing 为 true（该用户之前不在 local_users 表中）
- 该用户被写入 local_users 表，source 为 "ldap"

### TC-02-06: ldap 模式同步用户
操作步骤：
1. 以超管 admin 登录
2. browser_execute({ "script": "return fetch('{SERVER_URL}/api/csrf').then(r=>r.json())" })
3. browser_execute({ "script": "return fetch('{SERVER_URL}/api/admin/auth/sync-users', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'csrf_token=xxx' }).then(r=>r.json())" })

预期结果：
- 同步完成，返回 `{"success":true,...}`
- 响应显示同步的账号数量（provider_user_count, local_user_synced, allowed_user_count 等）
- LDAP 用户被同步到 local_users 表
- 如果同步了组，group_member_count > 0

### TC-02-07: ldap 模式测试 LDAP 连接
操作步骤：
1. 以超管 admin 登录
2. browser_execute({ "script": "return fetch('{SERVER_URL}/api/csrf').then(r=>r.json())" })
3. browser_execute({ "script": `return fetch('{SERVER_URL}/api/admin/auth/test-ldap', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: new URLSearchParams({ host: 'ldap://...', bind_dn: '...', bind_password: '...', base_dn: '...', filter: '(objectClass=inetOrgPerson)', username_attribute: 'uid', csrf_token: 'xxx' }).toString() }).then(r=>r.json())` })

预期结果：
- 连接成功，返回 `{"success":true}`
- 返回字段包含 users 列表（显示找到的用户数）
- 如果组查询也成功，返回 groups 列表

### TC-02-08: 在 ldap 模式下使用白名单拒绝未授权用户
操作步骤：
1. 以超管 admin 登录
2. 确认 LDAP 上至少有 2 个测试用户（如 user_allowed 和 user_denied）
3. 获取 CSRF token
4. browser_execute({ "script": `return fetch('{SERVER_URL}/api/admin/whitelist', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'whitelist=' + encodeURIComponent('user_allowed') + '&csrf_token=xxx' }).then(r=>r.json())` })
5. 用 user_allowed 登录 — 应成功
6. 用 user_denied 登录 — 应被拒绝

预期结果：
- 白名单设置成功
- user_allowed 正常登录
- user_denied 返回 403，提示"请联系管理员添加白名单"

### TC-02-09: 从 ldap 切回 local 模式（关键测试）
操作步骤：
1. 以超管 admin 登录
2. browser_execute({ "script": "return fetch('{SERVER_URL}/api/config').then(r=>r.json())" })
3. 获取 CSRF token
4. browser_execute({ "script": `return fetch('{SERVER_URL}/api/config', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'config=' + encodeURIComponent(JSON.stringify({"web":{"auth_mode":"local"}})) + '&csrf_token=xxx' }).then(r=>r.json())` })

预期结果：
- 配置保存成功，响应包含 cleanup 对象
- cleanup 字段显示所有 LDAP 用户和容器记录被清除：
  - `users_removed` 等于之前同步的 LDAP 用户数
  - `container_records` 表示之前 LDAP 用户的容器记录
  - `containers_removed` 表示实际删除的 Docker 容器
  - `groups_cleared: true`
  - `directories_purged: true`
- 验证：尝试用 LDAP 用户登录 — 应返回 401
- 验证：调 GET /api/user/info 用 LDAP 用户 cookie — 应返回 401
- 验证：超管 admin 仍然可登录
- 验证：之前创建的 testuser_local 也被清空（普通用户全部删除）

### TC-02-10: 切换后重新创建 local 用户
操作步骤：
1. 确认当前是 local 模式
2. 以超管 admin 登录
3. POST /api/admin/users/create 创建新用户 testuser2, 密码 test456
4. 用 testuser2/test456 登录

预期结果：
- 用户创建成功
- testuser2 可以正常登录
- GET /api/user/info 返回 role=user

### TC-02-11: 保存配置时不能改变 internal 字段
操作步骤：
1. 以超管 admin 登录
2. GET /api/config 获取当前配置（确认不包含 internal 字段）
3. 构造带有 internal 的配置，获取 CSRF token 后保存
4. 再次 GET /api/config 确认 internal 没有被写入
5. 检查 web.password 是否也被屏蔽

预期结果：
- 配置保存成功（不报错）
- 重新获取配置时不包含 internal 相关键（`removeFixedConfigFields` 删除了 `internal` 键，`SaveRawToDB` 中 `isInternalSettingKey` 跳过 `internal.*`）
- web.password 字段也被跳过保存

### TC-02-12: local 模式下修改密码
操作步骤：
1. 确认当前是 local 模式
2. 以 testuser2 登录
3. GET CSRF token
4. POST /api/user/password（old_password=test456, new_password=newpass789, csrf_token=xxx）
5. 用新密码登录 testuser2/newpass789 — 应成功
6. 用旧密码登录 testuser2/test456 — 应被拒绝

预期结果：
- 密码修改成功，返回 `{"success":true,"message":"密码修改成功"}`
- 新密码可正常登录
- 旧密码返回 401 "用户名或密码错误"

### TC-02-13: 统一认证模式下修改密码被拒绝
操作步骤：
1. 以超管 admin 登录，将 auth_mode 切换为 ldap
2. 以 LDAP 用户登录
3. 获取 CSRF token
4. POST /api/user/password（old_password=xxx, new_password=yyy, csrf_token=xxx）

预期结果：
- 返回 403 "非本地用户不支持修改密码，请联系管理员在公司认证中心修改"
- 代码逻辑：`handleChangePassword` 中 `s.cfg.UnifiedAuthEnabled()` 返回 true 时直接拒绝

### TC-02-14: ldap 模式下白名单过滤后清理白名单
操作步骤：
1. 在 ldap 模式下，以超管登录
2. POST /api/admin/whitelist 设置白名单只允许 user_allowed
3. 用不在白名单中的 LDAP 用户登录 — 应被拒绝
4. POST /api/admin/whitelist 清空白名单（提交空 whitelist 字段）
5. 用之前被拒绝的 LDAP 用户登录 — 应成功

预期结果：
- 白名单过滤正常工作，拒绝不在白名单的用户
- 清空白名单后所有 LDAP 用户可以正常登录
- 白名单清空后 `LoadWhitelist` 返回空 map，`AllowedByWhitelist` 对空白名单直接返回 true

### TC-02-15: 容器配置同步验证（切换后新建用户自动创建目录）
操作步骤：
1. 确保当前是 local 模式（若不是，先切换回 local）
2. 以超管 admin 登录
3. 创建新用户 testuser_container
4. 检查用户目录结构：`users/testuser_container/` 是否被创建
5. 检查容器记录是否被创建

预期结果：
- 切换认证模式后，新用户正常初始化
- 用户目录 `users/testuser_container/.picoclaw/` 被创建
- 容器记录在 containers 表中创建

### TC-02-16: 认证模式切换时容器实际删除验证
操作步骤：
1. 在 local 模式下创建用户 testuser_docker
2. 确认该用户的 Docker 容器已启动（docker ps 可见）
3. 切换认证模式到 ldap
4. 检查 Docker 容器是否被删除

预期结果：
- 切换前：testuser_docker 的容器存在且运行中
- 切换后：testuser_docker 的 Docker 容器被实际删除（`purgeOrdinaryAuthProviderStateForConfig` 中调用 `dockerpkg.Remove`）
- Docker 引擎上的容器也被停止和移除

### TC-02-17: 多次切换认证模式不报错
操作步骤：
1. 从 local -> ldap（应正常）
2. 从 ldap -> local（应正常）
3. 从 local -> ldap（应正常）

预期结果：
- 每次切换都成功
- 响应包含 cleanup 数据
- 服务不崩溃
- 切换后对应模式的登录流程正常工作

### TC-02-18: 白名单新增用户后自动同步创建容器
前置条件：
- LDAP 模式，自动同步间隔设为 1 分钟
- 白名单已开启
- LDAP 服务器上有若干用户

操作步骤：
1. 在认证配置页，点击"搜索 LDAP 用户..."搜索框旁的"搜索"按钮（无需输入任何内容）
2. 搜索结果列表中出现 LDAP 用户，每个用户右侧有"+"按钮
3. 点击某个不在白名单中的 LDAP 用户的"+"按钮，该用户立即出现在白名单列表中
4. 点击"保存全部认证配置"按钮持久化白名单
5. 等待 1 分钟
6. 切换到用户管理页 /admin/users
7. 查看用户列表

预期结果：
- 新用户出现在用户列表中，source 为 "ldap"
- 容器的 status 为 "running"
- 自动分配了 IP 地址（新 IP）
- 镜像版本与已有用户一致
- 用户的 LDAP 组已同步

### TC-02-19: 白名单删除用户后自动清理过期账号
前置条件：
- 接续 TC-02-18，该 LDAP 用户已被同步到本地并拥有运行中容器

操作步骤：
1. 回到认证配置页 /admin/auth
2. 在白名单用户列表中找到刚才添加的用户，点击其"×"按钮
3. 在弹出的确认对话框中点击"确认"
4. 点击"保存全部认证配置"按钮
5. 等待 1 分钟
6. 切换到用户管理页 /admin/users
7. 查看用户列表

预期结果：
- 该用户不再出现在用户列表中
- 该用户的容器已停止并删除
- 该用户的数据库记录已清除
- 其余用户不受影响

### TC-02-20: 白名单通过 "手动输入用户名" 添加
操作步骤：
1. 在认证配置页的白名单区域，找到"手动输入用户名"输入框
2. 输入 LDAP 服务器上存在的用户名
3. 点击旁边的"添加"按钮
4. 用户名出现在白名单列表中
5. 点击"保存全部认证配置"
6. 等待 1 分钟
7. 检查用户列表

预期结果：
- 手动输入的用户名被成功加入白名单
- 自动同步后该用户被正确初始化

## 清理
- 确保最终回到 local 模式
- 删除测试创建的用户（testuser_local, testuser2, testuser_container, testuser_docker）
- 清空白名单
- 确认超管 admin 可正常登录
