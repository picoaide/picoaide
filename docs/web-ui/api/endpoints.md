# API 端点参考

所有 API 端点返回 JSON 格式。成功响应包含 `"success": true`，失败响应包含 `"success": false` 和 `"error"` 字段。

---

## 公开端点（无需认证）

### GET /api/health
服务健康检查
- 认证: 无
- 响应:
  ```json
  {"status":"ok","version":"1.0.0"}
  ```

### GET /api/login/mode
返回当前认证模式及认证源信息
- 认证: 无
- 响应:
  ```json
  {"success":true,"auth_mode":"local","provider":{"name":"local","display_name":"本地","has_password":true,"has_browser":false,"has_directory":false}}
  ```
- 说明: `auth_mode` 可取值 `local`、`ldap`、`oidc` 或自定义认证源名称；`provider` 反映当前认证源的能力

### POST /api/login
用户名密码登录
- 认证: 限速（10次/5分钟）
- 请求体: `username`、`password` (form)
- 成功响应:
  ```json
  {"success":true,"username":"xxx","initializing":false}
  ```
- 失败响应: `{"success":false,"error":"用户名或密码错误"}`
- 说明: 超管有本地密码逃生通道（统一认证模式下仍可用本地密码）；新外部用户首次登录自动初始化环境

### GET /api/login/auth
启动浏览器认证流程（OIDC SSO 入口）
- 认证: 无
- 说明: 浏览器重定向到认证源授权页面，设置 `auth_state` cookie（10分钟有效）

### GET /api/login/callback
浏览器认证回调（OIDC SSO 回调）
- 认证: 无（验证 `auth_state` cookie + query `state` + `code`）
- 参数: `state`、`code` (query)
- 说明: 成功时重定向到 `/manage` 或 `/initializing`；新用户自动同步组信息

### POST /api/logout
登出
- 认证: 可选（有 session 则清除）
- 响应: `{"success":true,"message":"已登出"}`

---

## 用户会话端点

### GET /api/user/info
获取当前用户信息
- 认证: 普通用户
- 响应:
  ```json
  {"success":true,"username":"xxx","role":"user","auth_mode":"local","unified_auth":false,"initializing":false}
  ```
- 说明: `initializing` 表示容器或配置尚未准备就绪

### GET /api/user/init-status
获取用户初始化状态
- 认证: 普通用户（非超管）
- 响应:
  ```json
  {"success":true,"ready":true,"status":"running","image_ready":true,"has_config":true}
  ```

### POST /api/user/password
修改密码
- 认证: 普通用户（非超管），CSRF
- 限制: 仅本地模式可用，统一认证模式下返回 403
- 请求体: `old_password`、`new_password`（form，新密码至少 6 位）
- 成功响应: `{"success":true,"message":"密码修改成功"}`

### GET /api/csrf
获取当前用户的 CSRF token
- 认证: 普通用户/超管
- 响应:
  ```json
  {"success":true,"csrf_token":"<32位hex>"}
  ```
- 说明: token 按小时滚动窗口派生，同一小时内对同一用户有效

### POST /api/cookies
同步浏览器 Cookie 到用户的 `.security.yml`
- 认证: 非超管用户，CSRF
- 请求体: `domain`、`cookies` (form)
- 成功响应: `{"success":true,"message":"已同步 xxx 的登录状态"}`

### GET /api/mcp/token
获取/自动生成当前用户的 MCP token
- 认证: 非超管用户
- 响应:
  ```json
  {"success":true,"token":"用户名:随机hex"}
  ```
- 说明: token 格式为 `用户名:随机hex`，可用于 MCP SSE 服务和 WebSocket 代理认证

### GET /api/mcp/sse/:service
建立 MCP SSE 连接（Streamable HTTP 协议）
- 认证: MCP token（query `token` 或 `Authorization: Bearer`）
- 参数: `service` 为 `browser` 或 `computer`
- 说明: 支持 `Mcp-Protocol-Version` header，返回 SSE 事件流

### POST /api/mcp/sse/:service
发送 MCP JSON-RPC 2.0 消息
- 认证: MCP token
- 参数: `service` 为 `browser` 或 `computer`
- 请求体: JSON-RPC 2.0 请求（`jsonrpc`、`method`、`params` 等）
- 说明: 支持 `tools/list`、`tools/call` 等方法

### GET /api/browser/ws
浏览器扩展 WebSocket 代理连接
- 认证: MCP token（query `token`）
- 协议: WebSocket
- 说明: 浏览器扩展通过此端点实时接收并执行命令

### GET /api/computer/ws
桌面代理 WebSocket 连接
- 认证: MCP token（query `token`）
- 协议: WebSocket
- 说明: 桌面代理通过此端点实时接收并执行命令

---

## 用户配置端点

### GET /api/dingtalk
获取当前用户的钉钉配置
- 认证: 普通用户（非超管）
- 响应:
  ```json
  {"success":true,"client_id":"xxx","client_secret":"xxx"}
  ```

### POST /api/dingtalk
保存钉钉配置并重启容器
- 认证: 普通用户（非超管），CSRF
- 请求体: `client_id`、`client_secret` (form)
- 响应: `{"success":true,"message":"配置已保存，容器正在重启中，请稍候片刻即可使用。"}`

### GET /api/picoclaw/channels
获取当前用户的渠道列表及启用状态
- 认证: 普通用户（非超管）
- 参数: `config_version` (query, 可选)
- 响应:
  ```json
  {"success":true,"channels":[...]}
  ```

### GET /api/picoclaw/config-fields
获取渠道配置字段定义
- 认证: 普通用户（非超管）
- 参数: `config_version`、`section` (query)
- 响应:
  ```json
  {"success":true,"fields":{...}}
  ```

### POST /api/picoclaw/config-fields
保存渠道配置字段
- 认证: 普通用户（非超管），CSRF
- 请求体: `config_version`、`section`、`values` (form, JSON 字符串)
- 响应: 配置保存后自动重启容器

---

## 文件管理端点

文件管理作用于用户的 `.picoclaw/workspace/` 沙盒目录，通过 `os.Root` 限制访问范围，防止目录遍历。

### GET /api/files
列出文件
- 认证: 普通用户（非超管）
- 参数: `path` (query, 可选，默认为根目录)
- 响应:
  ```json
  {"success":true,"path":"subdir","entries":[{"name":"file.txt","is_dir":false,"size":1024,"size_str":"1.0 KB","mod_time":"2024-01-01 12:00","rel_path":"subdir/file.txt"}],"breadcrumb":[{"name":"/","path":"."},{"name":"subdir","path":"subdir"}]}
  ```

### POST /api/files/upload
上传文件（最大 32MB）
- 认证: 普通用户（非超管）
- 请求体: multipart/form-data，`path`(目录) + `file`(文件)
- 说明: 不受 1MB 通用请求体大小限制

### GET /api/files/download
下载文件
- 认证: 普通用户（非超管）
- 参数: `path` (query)
- 响应: 文件二进制流

### POST /api/files/delete
删除文件或目录
- 认证: 普通用户（非超管）
- 请求体: `path` (form)
- 说明: 递归删除目录

### POST /api/files/mkdir
创建目录
- 认证: 普通用户（非超管）
- 请求体: `path` (form)

### GET /api/files/edit
读取文本文件内容
- 认证: 普通用户（非超管）
- 参数: `path` (query)
- 响应:
  ```json
  {"success":true,"filename":"file.txt","content":"文件内容","path":"file.txt"}
  ```

### POST /api/files/edit
保存文本文件内容
- 认证: 普通用户（非超管）
- 请求体: `path`、`content` (form)

---

## 超管配置端点

### GET /api/config
读取全局配置（JSON 格式）
- 认证: 超管
- 响应: 完整的嵌套配置 JSON
- 安全: `internal.*` 和 `web.password` 等敏感键在加载时被过滤，不会出现在 API 响应中

### POST /api/config
保存全局配置
- 认证: 超管，CSRF
- 请求体: `config` (form, JSON 字符串)
- 说明:
  - 认证模式切换时自动清理旧认证源数据（删除普通用户、容器、目录、组）
  - 实时重启定时同步定时器
  - 切换认证模式时响应中包含清理结果:
    ```json
    {"success":true,"message":"配置已保存，认证方式已切换并清空旧认证数据","cleanup":{"containers_removed":3,"container_records":3,"users_removed":5,...}}
    ```

### POST /api/admin/config/apply
下发配置到指定用户/组/全部用户并重启容器
- 认证: 超管，CSRF
- 请求体: `username` 或 `group` (form, 可选，不指定时下发到全部用户)
- 响应（单用户）: `{"success":true,"message":"配置已下发并重启"}`
- 响应（多用户）: `{"success":true,"message":"已提交配置下发任务，共 N 个用户","task_id":"xxx"}`

---

## 超管用户端点

### GET /api/admin/users
用户列表（分页+搜索）
- 认证: 超管
- 参数: `page`、`page_size`(默认 20，最大 100)、`search`、`runtime`(默认 true，设为 false 跳过容器运行时状态查询)
- 响应:
  ```json
  {"success":true,"users":[{"username":"xxx","source":"local","status":"running","image_tag":"v1","image_ready":true,"ip":"100.64.0.2","role":"user","groups":["group1"]}],"page":1,"page_size":20,"total":10,"total_pages":1}
  ```

### POST /api/admin/users/create
创建用户
- 认证: 超管，CSRF
- 请求体: `username` (form)
- 成功响应: `{"success":true,"message":"用户 xxx 创建成功","username":"xxx","password":"随机密码"}`
- 说明: 自动生成随机密码，自动初始化用户环境

### POST /api/admin/users/batch-create
批量导入用户
- 认证: 超管，CSRF
- 请求体: `usernames` (form，逗号分隔)
- 响应: `{"success":true,"task_id":"xxx","message":"已提交导入任务，共 N 个用户"}`
- 说明: 异步任务执行，通过 `GET /api/admin/task/status` 查询进度

### POST /api/admin/users/delete
删除用户
- 认证: 超管，CSRF
- 请求体: `username` (form)
- 说明: 删除容器记录和用户目录（目录归档到 `archived/`）

---

## 超管认证端点

### GET /api/admin/auth/providers
获取可用认证源列表
- 认证: 超管
- 响应:
  ```json
  {"success":true,"providers":[{"name":"local","display_name":"本地","config_fields":[]},{"name":"ldap","display_name":"LDAP","config_fields":[{"key":"host","label":"LDAP 地址","type":"text"}]}]}
  ```
- 说明: 前端据此动态渲染配置表单

### POST /api/admin/auth/test-ldap
测试 LDAP 连接
- 认证: 超管，CSRF
- 请求体: `host`、`bind_dn`、`bind_password`、`base_dn`、`filter`、`username_attribute`、`group_search_mode`、`group_base_dn`、`group_filter`、`group_member_attribute` (form)
- 响应:
  ```json
  {"success":true,"message":"连接成功，找到 N 个用户","user_count":10,"users":["user1","user2"],"groups":[...],"group_error":""}
  ```

### GET /api/admin/auth/ldap-users
获取 LDAP/目录源用户列表
- 认证: 超管
- 参数: `source` (query, 可选，指定认证源名称)、`page`、`page_size`、`search`
- 响应:
  ```json
  {"success":true,"users":["user1","user2"],"page":1,"page_size":50,"total":10,"total_pages":1}
  ```

### POST /api/admin/auth/sync-users
同步目录源用户到本地
- 认证: 超管，CSRF
- 说明: 仅在支持 DirectoryProvider 的认证源下可用；自动新初始化用户、补齐镜像、清理过期用户
- 响应:
  ```json
  {"success":true,"message":"同步完成，...","result":{"provider_user_count":10,...}}
  ```

### POST /api/admin/auth/sync-groups
手动同步 LDAP 组
- 认证: 超管，CSRF
- 说明: 从 LDAP 拉取组和成员关系，写入本地 `groups` 和 `user_groups` 表

### GET /api/admin/whitelist
白名单列表
- 认证: 超管
- 参数: `page`、`page_size`、`search`
- 响应: 用户名列表（分页）

### POST /api/admin/whitelist
更新白名单
- 认证: 超管，CSRF
- 请求体: `users`(form，逗号分隔的完整名单，替换现有)、或 `add` + `remove`(form，增量修改)
- 响应: `{"success":true,"message":"白名单已更新"}`

---

## 超管组端点

### GET /api/admin/groups
用户组列表
- 认证: 超管
- 参数: `page`、`page_size`、`search`
- 响应:
  ```json
  {"success":true,"unified_auth":false,"groups":[{"id":1,"name":"group1","source":"local","description":"","member_count":3,"parent_id":0,"parent_name":""}],"page":1,"page_size":50,"total":5,"total_pages":1}
  ```

### POST /api/admin/groups/create
创建用户组
- 认证: 超管，CSRF
- 请求体: `name`、`description`(可选)、`parent_id`(可选)
- 限制: 统一认证模式下不允许手动创建组
- 说明: 支持树形组结构（`parent_id` 指定父组）

### POST /api/admin/groups/delete
删除用户组
- 认证: 超管，CSRF
- 请求体: `name` (form)

### GET /api/admin/groups/members
获取组成员列表
- 认证: 超管
- 参数: `name` (query)

### POST /api/admin/groups/members/add
添加组成员（仅 local 模式）
- 认证: 超管，CSRF
- 请求体: `group_name`、`usernames`(逗号分隔) (form)
- 说明: 统一认证模式下返回 403

### POST /api/admin/groups/members/remove
移除组成员（仅 local 模式）
- 认证: 超管，CSRF
- 请求体: `group_name`、`username` (form)
- 说明: 统一认证模式下返回 403

### POST /api/admin/groups/skills/bind
绑定技能到组
- 认证: 超管，CSRF
- 请求体: `group`、`skills`(逗号分隔或 JSON 数组) (form)

### POST /api/admin/groups/skills/unbind
解绑组技能
- 认证: 超管，CSRF
- 请求体: `group`、`skills`(逗号分隔) (form)

---

## 超管镜像端点

### GET /api/admin/images
本地镜像列表
- 认证: 超管
- 响应:
  ```json
  {"success":true,"images":[{"id":"abc123","full_id":"sha256:...","repo_tags":["ghcr.io/picoaide/picoaide:v1"],"size":123456789,"size_str":"117.7 MB","created":1700000000,"created_str":"2024-01-01 12:00","user_count":3,"users":["user1","user2","user3"]}]}
  ```

### POST /api/admin/images/pull
拉取镜像（SSE 流式推送进度）
- 认证: 超管，CSRF
- 请求体: `tag` (form)
- 说明: 响应为 `text/event-stream`，实时推送拉取进度

### POST /api/admin/images/delete
删除本地镜像
- 认证: 超管，CSRF
- 请求体: `id` (form，镜像 ID 或引用)
- 说明: 仅删除本地 Docker 镜像，不影响容器记录

### POST /api/admin/images/migrate
用户镜像迁移（旧镜像 → 新镜像，重建容器）
- 认证: 超管，CSRF
- 请求体: `image`(旧)、`target`(新)、`users`(可选，逗号分隔特定用户) (form)
- 说明: 自动检查迁移规则的兼容性

### POST /api/admin/images/upgrade
批量升级用户镜像（取最新版本）
- 认证: 超管，CSRF
- 请求体: 可选参数指定升级范围
- 说明: 按升级候选人列表升级

### GET /api/admin/images/registry
获取远程仓库标签列表（按版本号降序排列）
- 认证: 超管

### GET /api/admin/images/local-tags
获取本地镜像标签列表
- 认证: 超管
- 响应:
  ```json
  {"success":true,"tags":["v1","v2","v3"]}
  ```

### GET /api/admin/images/upgrade-candidates
获取可升级的镜像候选人列表
- 认证: 超管
- 说明: 比较本地标签和远程仓库，列出可升级的组合

### GET /api/admin/images/users
获取使用指定镜像的用户列表
- 认证: 超管
- 参数: `image` (query)
- 响应:
  ```json
  {"success":true,"users":["user1","user2"],"page":1,"page_size":50,"total":2,"total_pages":1}
  ```

---

## 超管技能端点

### GET /api/admin/skills
技能仓库列表
- 认证: 超管
- 响应:
  ```json
  {"success":true,"skills":[{"name":"my-skill","display_name":"我的技能","description":"...","version":"1.0","type":"personal"}]}
  ```

### POST /api/admin/skills/deploy
部署技能到用户/组
- 认证: 超管，CSRF
- 请求体: `skill`、`target_type`(user/group)、`target_name` (form)
- 说明: 将已下载的技能部署到目标

### GET /api/admin/skills/download
下载技能到本地缓存
- 认证: 超管
- 参数: `repo`、`path` (query)
- 说明: 从技能仓库下载指定技能文件

### POST /api/admin/skills/remove
删除已下载的技能
- 认证: 超管，CSRF
- 请求体: `skill` (form)

### POST /api/admin/skills/upload
上传 ZIP 技能包
- 认证: 超管，CSRF
- 请求体: multipart/form-data，`file`(ZIP 文件)
- 说明: 不受 1MB 通用请求体大小限制

### GET /api/admin/skills/repos/list
技能仓库列表
- 认证: 超管

### POST /api/admin/skills/repos/add
添加技能仓库（Git clone）
- 认证: 超管，CSRF
- 请求体: `url`、`name`、`branch`(可选)、`ssh_key`(可选) (form)
- 说明: 支持 SSH 和 HTTPS 认证

### POST /api/admin/skills/repos/save
保存技能仓库配置
- 认证: 超管，CSRF
- 请求体: `name`、`url`、`branch`、`ssh_key` (form)

### POST /api/admin/skills/repos/pull
拉取技能仓库更新
- 认证: 超管，CSRF
- 请求体: `name` (form)

### POST /api/admin/skills/repos/remove
移除技能仓库
- 认证: 超管，CSRF
- 请求体: `name` (form)

### POST /api/admin/skills/install
安装技能仓库（Git clone 并自动解析技能列表）
- 认证: 超管，CSRF
- 请求体: `url`(必填)、`name`(可选)、`branch`(可选)、`ssh_key`(可选) (form)

---

## 超管管理端点

### GET /api/admin/superadmins
超管列表
- 认证: 超管
- 响应:
  ```json
  {"success":true,"admins":["admin1","admin2"]}
  ```

### POST /api/admin/superadmins/create
创建超管账户
- 认证: 超管，CSRF
- 请求体: `username` (form)
- 响应:
  ```json
  {"success":true,"message":"超管创建成功","username":"xxx","password":"随机密码"}
  ```
- 说明: 自动生成 12 位随机密码

### POST /api/admin/superadmins/delete
删除超管账户
- 认证: 超管，CSRF
- 请求体: `username` (form)
- 限制: 不能删除自己，至少保留一个超管

### POST /api/admin/superadmins/reset
重置超管密码
- 认证: 超管，CSRF
- 请求体: `username` (form)
- 响应:
  ```json
  {"success":true,"message":"密码已重置","password":"随机密码"}
  ```

### POST /api/admin/container/start
启动用户容器
- 认证: 超管，CSRF
- 请求体: `username` (form)
- 说明: 自动检查镜像是否存在

### POST /api/admin/container/stop
停止用户容器
- 认证: 超管，CSRF
- 请求体: `username` (form)

### POST /api/admin/container/restart
重启用户容器
- 认证: 超管，CSRF
- 请求体: `username` (form)

### POST /api/admin/container/debug
以调试模式启动用户容器
- 认证: 超管，CSRF
- 请求体: `username` (form)
- 说明: 调试模式容器可能在重启时被清理

### GET /api/admin/container/logs
获取用户容器日志
- 认证: 超管
- 参数: `username`、`tail`(可选，日志行数) (query)
- 响应:
  ```json
  {"success":true,"username":"xxx","logs":"容器日志内容"}
  ```

### GET /api/admin/picoclaw/channels
获取 Picoclaw 渠道管理列表（全部渠道定义）
- 认证: 超管
- 参数: `config_version` (query，可选，默认使用当前版本)
- 响应:
  ```json
  {"success":true,"channels":[...]}
  ```

### GET /api/admin/migration-rules
获取 Picoclaw Adapter 信息和迁移规则列表
- 认证: 超管

### POST /api/admin/migration-rules/refresh
刷新 Picoclaw Adapter（从远程 URL 拉取或上传 ZIP）
- 认证: 超管，CSRF
- 请求体: 可选 `file` (multipart/form-data, ZIP 文件)
- 说明: 无文件时从远程 URL 拉取；支持 SHA256 校验

### POST /api/admin/migration-rules/upload
上传 Picoclaw Adapter ZIP 包
- 认证: 超管，CSRF
- 请求体: multipart/form-data，`file`(ZIP 文件)
- 说明: 不受 1MB 通用请求体大小限制

### GET /api/admin/task/status
查询异步任务状态
- 认证: 超管
- 响应: 包含队列信息和任务进度

---

## UI 路由

| 路径 | 方法 | 说明 |
|------|------|------|
| `/` | GET | 重定向到 `/login`（302） |
| `/login` | GET | 登录页面（`login.html`） |
| `/initializing` | GET | 初始化等待页面（需普通用户会话） |
| `/manage` | GET | 用户管理面板（需普通用户会话，外部用户需环境就绪） |
| `/manage/` | GET | 301 重定向到 `/manage` |
| `/admin` | GET | 301 重定向到 `/admin/dashboard` |
| `/admin/` | GET | 301 重定向到 `/admin/dashboard` |
| `/admin/dashboard` | GET | 超管后台首页（需超管会话） |
| `/admin/superadmins` | GET | 超管账户管理页 |
| `/admin/users` | GET | 用户管理页 |
| `/admin/groups` | GET | 用户组管理页 |
| `/admin/images` | GET | 镜像管理页 |
| `/admin/picoclaw` | GET | Picoclaw 配置管理页 |
| `/admin/models` | GET | 模型配置页 |
| `/admin/skills` | GET | 技能管理页 |
| `/admin/auth` | GET | 认证配置页 |
| `/admin/settings` | GET | 全局设置页 |

静态资源：`/css/*`、`/js/*`、`/images/*`、`/admin/modules/*`、`/admin/templates/*`、`/manage.js`、`/login.js`、`/initializing.js`、`/admin/admin.js`。
