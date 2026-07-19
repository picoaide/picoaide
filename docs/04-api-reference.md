# API 参考

所有路由注册在 `/api` 和 `/api/v1` 双前缀下。`/api/version` 为单一路径。

## 认证方式

| 方式 | 说明 |
|------|------|
| Session Cookie | HMAC 签名 cookie，登录后设置 |
| CSRF Token | POST 请求需 `X-CSRF-Token` header |
| MCP Token | `Authorization: Bearer <token>` 或 `?token=` query |

## 公开端点

```http
GET  /api/version           # 服务端版本
GET  /api/health            # 健康检查
POST /api/login             # 用户名密码登录（JSON body: username, password）
GET  /api/login/mode        # 当前认证模式
GET  /api/login/auth        # OIDC 跳转
GET  /api/login/callback    # OIDC 回调
POST /api/logout            # 登出
GET  /api/csrf              # 获取 CSRF token（需 session）
```

## 用户端点

```http
GET  /api/user/info                 # 当前用户信息
POST /api/user/password             # 修改密码（old_password, new_password）
GET  /api/config                    # 读取配置
POST /api/config                    # 保存配置
```

### 对话

```http
GET  /api/user/chat/history        # 历史消息
POST /api/user/chat/send           # 发送消息
GET  /api/user/chat/stream?run_id= # SSE 流式响应
POST /api/user/chat/stop           # 停止对话
GET  /api/user/chat/active         # 查询活跃会话
```

### 文件

```http
GET  /api/files?path=              # 文件列表（JSON，含面包屑）
POST /api/files/upload             # 上传文件（multipart）
GET  /api/files/download?path=     # 下载文件
POST /api/files/delete             # 删除（path）
POST /api/files/mkdir              # 创建目录（path, name）
GET  /api/files/edit?path=         # 读取文本文件
POST /api/files/edit               # 保存文本文件（path, content）
```

### 技能

```http
GET  /api/user/skills                      # 技能列表（含安装状态）
POST /api/user/skills/install             # 安装（skill_name）
POST /api/user/skills/uninstall           # 卸载（skill_name）
```

### 其他

```http
GET  /api/channels                    # 渠道列表
GET  /api/channels/config-fields?section=  # 渠道配置字段定义
POST /api/channels/config-fields      # 保存渠道配置
GET  /api/user/email                  # 读取邮箱配置
POST /api/user/email                  # 保存邮箱配置
POST /api/user/email/test             # 测试邮箱连接
POST /api/user/email/delete           # 删除邮箱配置
GET  /api/shared-folders              # 可见共享文件夹
GET  /api/user/cookies                # Cookie 域名列表
POST /api/user/cookies/delete         # 取消 Cookie 授权
GET  /api/cron                        # 定时任务列表
POST /api/cron/create                 # 创建定时任务
POST /api/cron/update                 # 更新定时任务
POST /api/cron/delete                 # 删除定时任务
POST /api/cron/toggle                 # 启用/禁用定时任务
GET  /api/mcp/token                   # 获取 MCP token
```

## 超管端点

### 用户管理

```http
GET  /api/admin/users?page=&page_size=&search=    # 用户列表
POST /api/admin/users/create                       # 创建用户（username）
POST /api/admin/users/batch-create                 # 批量创建（usernames: []string）
POST /api/admin/users/delete                       # 删除用户（username）
```

### 超管账户

```http
GET  /api/admin/superadmins                # 超管列表
POST /api/admin/superadmins/create         # 创建（username）
POST /api/admin/superadmins/delete         # 删除（username）
POST /api/admin/superadmins/reset          # 重置密码（username）
POST /api/admin/password                   # 修改密码（old_password, new_password）
```

### 认证

```http
GET  /api/admin/whitelist                        # 白名单列表
POST /api/admin/whitelist                        # 更新白名单
POST /api/admin/auth/test-ldap                   # 测试 LDAP 连接
GET  /api/admin/auth/ldap-users                  # LDAP 用户列表
POST /api/admin/auth/sync-users                  # 同步用户
POST /api/admin/auth/sync-groups                 # 同步组
GET  /api/admin/auth/providers                   # 已注册认证源
```

### 用户组

```http
GET  /api/admin/groups?page=&page_size=&search=   # 组列表
POST /api/admin/groups/create                      # 创建组（name, description, parent_id）
POST /api/admin/groups/delete                      # 删除组（name）
GET  /api/admin/groups/members?name=               # 组成员
POST /api/admin/groups/members/add                 # 添加成员（group_name, usernames）
POST /api/admin/groups/members/remove              # 移除成员（group_name, username）
POST /api/admin/groups/skills/bind                 # 绑定技能（group_name, skill_name）
POST /api/admin/groups/skills/unbind               # 解绑技能（group_name, skill_name）
```

### 技能

```http
GET  /api/admin/skills?source=&page=&page_size=     # 技能列表
POST /api/admin/skills/deploy                        # 部署到用户/组
POST /api/admin/skills/remove                        # 移除技能
POST /api/admin/skills/user/bind                     # 绑定到单个用户
POST /api/admin/skills/user/unbind                   # 解绑用户技能
GET  /api/admin/skills/sources                       # 技能来源列表
POST /api/admin/skills/sources/git                   # 添加 Git 源
POST /api/admin/skills/sources/remove                # 移除来源
POST /api/admin/skills/sources/pull                  # 拉取更新
GET  /api/admin/skills/registry/list?source=&q=      # 注册中心列表
POST /api/admin/skills/registry/install              # 从注册中心安装
GET  /api/admin/skills/defaults                      # 默认技能列表
POST /api/admin/skills/defaults/toggle               # 切换默认安装
```

### 共享文件夹

```http
GET  /api/admin/shared-folders                 # 列表
POST /api/admin/shared-folders/create          # 创建（name, description, is_public）
POST /api/admin/shared-folders/update          # 更新
POST /api/admin/shared-folders/delete          # 删除（id）
POST /api/admin/shared-folders/groups/set      # 设置可见范围（folder_id, group_ids）
POST /api/admin/shared-folders/test            # 测试挂载（folder_id, username）
POST /api/admin/shared-folders/mount           # 手动挂载（folder_id）
```

### MCP 服务

```http
GET  /api/admin/mcp/servers                         # 服务器列表
POST /api/admin/mcp/servers/create                  # 创建
POST /api/admin/mcp/servers/update/:id              # 更新
POST /api/admin/mcp/servers/delete/:id              # 删除
GET  /api/admin/mcp/servers/grants?server_id=       # 授权列表
POST /api/admin/mcp/servers/grants/add              # 添加授权
POST /api/admin/mcp/servers/grants/remove/:id       # 移除授权
POST /api/admin/mcp/servers/reload                  # 重新加载
GET  /api/admin/mcp/servers/tools?name=             # 工具列表
```

### 其他

```http
POST /api/admin/model/test              # 测试模型连接
GET  /api/admin/tls/status              # TLS 证书状态
POST /api/admin/tls/verify              # 验证证书
POST /api/admin/tls/save                # 保存证书
POST /api/admin/tls/toggle              # 开关 HTTPS
POST /api/admin/tls/clear               # 清除证书
GET  /api/admin/tasks/stats             # 任务统计
```

## MCP SSE 端点

```http
GET  /api/mcp/sse/:service      # SSE 连接（browser/computer/agent/email）
POST /api/mcp/sse/:service      # JSON-RPC 消息
```

## 错误响应

```json
{"success": false, "error": "错误描述"}
```

HTTP 状态码：400（参数错误）、401（未登录）、403（权限不足）、404（未找到）、500（服务器错误）。
