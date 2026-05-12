# 认证配置页面

## 页面路由

`/admin/auth`

## 权限要求

超管（`superadmin`）登录

## 功能概述

配置系统的认证方式，包括切换认证模式、配置 LDAP 参数、管理白名单。认证模式切换时会自动清理旧数据。

## 功能详细说明

### 1. 认证模式选择

**功能**：切换系统的认证模式

**API 端点**：`GET /api/config` 读取当前配置，`POST /api/config` 保存配置

**支持模式**：

| 模式 | 说明 | 密码登录 | SSO 登录 |
|------|------|----------|----------|
| `local` | 本地模式，用户名密码由 PicoAide 管理 | 支持 | 不支持 |
| `ldap` | LDAP 模式，密码委托 LDAP 验证 | 支持 | 不支持 |
| `oidc` | OIDC 模式，SSO 跳转登录 | 不支持 | 支持 |

**配置字段**（在 `web` 配置段下）：

```yaml
web:
  auth_mode: "local"  # local / ldap / oidc
```

**读取配置请求**：`GET /api/config`

**响应格式**：
```json
{
  "web": {
    "auth_mode": "local"
  },
  "ldap": {
    "host": "ldap.example.com",
    "bind_dn": "cn=admin,dc=example,dc=com",
    "bind_password": "***",
    "base_dn": "dc=example,dc=com",
    "filter": "(objectClass=inetOrgPerson)",
    "username_attribute": "uid"
  }
}
```

**保存配置请求**：`POST /api/config`
请求体（form）：`config=<JSON>`，其中 JSON 为嵌套结构
```json
{
  "web": {
    "auth_mode": "ldap"
  }
}
```

注意：`internal.*` 和 `web.password` 不会被保存，即使客户端提交了也会被忽略。

### 2. 认证模式切换逻辑

模式切换时在服务端触发清理逻辑（位于 `handlers.go` 的 `handleConfigSave`）：

```
检测 oldMode != newMode
  → 调用 purgeOrdinaryAuthProviderStateForConfig()
  → 记录审计日志
  → 重启定时同步
```

**`purgeOrdinaryAuthProviderStateForConfig` 清理内容**：

1. 删除所有 Docker 容器（如果 Docker 可用）
2. 清空 `containers` 表
3. 删除所有普通用户：`DELETE FROM local_users WHERE role != 'superadmin'`
4. 清空 `groups` 和 `user_groups` 表
5. 删除 `users/` 目录下所有内容
6. 删除 `archive/` 目录下所有内容

**超管账户不受影响**。

### 3. LDAP 配置

**功能**：配置 LDAP 服务器连接参数，仅在 `ldap` 模式下生效

**配置字段**：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `ldap.host` | string | — | LDAP 服务器地址，如 `ldap.example.com:389` |
| `ldap.bind_dn` | string | — | 绑定 DN，如 `cn=admin,dc=example,dc=com` |
| `ldap.bind_password` | string | — | 绑定密码 |
| `ldap.base_dn` | string | — | 搜索基础 DN，如 `dc=example,dc=com` |
| `ldap.filter` | string | `(objectClass=person)` | 用户搜索过滤器 |
| `ldap.username_attribute` | string | `uid` | 用户名属性字段 |

**LDAP 组配置字段**：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `ldap.group_search_mode` | string | `""` | 组搜索模式：空字符串/禁用, `member_of`, `group_search` |
| `ldap.group_base_dn` | string | — | 组搜索基础 DN |
| `ldap.group_filter` | string | `(objectClass=group)` | 组搜索过滤器 |
| `ldap.group_member_attribute` | string | `member` | 组成员属性字段 |

**保存 LDAP 配置**：`POST /api/config`
```json
{
  "ldap.host": "ldap.example.com:389",
  "ldap.bind_dn": "cn=admin,dc=example,dc=com",
  "ldap.bind_password": "secret",
  "ldap.base_dn": "dc=example,dc=com",
  "ldap.filter": "(objectClass=person)",
  "ldap.username_attribute": "uid",
  "ldap.group_search_mode": "group_search",
  "ldap.group_base_dn": "ou=groups,dc=example,dc=com",
  "ldap.group_filter": "(objectClass=groupOfNames)",
  "ldap.group_member_attribute": "member"
}
```

### 4. 测试 LDAP 连接

**触发方式**：点击"测试连接"按钮

**API 端点**：`POST /api/admin/auth/test-ldap`

**请求格式**：
```json
{
  "host": "ldap.example.com:389",
  "bind_dn": "cn=admin,dc=example,dc=com",
  "bind_password": "secret",
  "base_dn": "dc=example,dc=com",
  "filter": "(objectClass=person)",
  "username_attribute": "uid"
}
```

**响应格式（成功）**：
```json
{
  "success": true,
  "message": "连接成功，找到 12 个用户",
  "user_count": 12,
  "users": ["alice", "bob", ...],
  "groups": [
    {"name": "developers", "members": ["alice", "bob"]}
  ],
  "group_error": ""
}
```

**响应格式（失败）**：
```json
{
  "success": false,
  "error": "LDAP 地址、Bind DN 和 Base DN 不能为空"
}
```

### 5. 同步用户

**触发方式**：点击"同步用户"按钮

**API 端点**：`POST /api/admin/auth/sync-users`

**请求格式**：空

**响应格式**：
```json
{
  "success": true,
  "message": "同步完成，ldap 5 个账号，允许 5 个，写入本地用户 5 个，新初始化 2 个，补齐镜像 1 个，移除本地普通登录凭据 5 个",
  "result": {
    "provider_user_count": 5,
    "allowed_user_count": 5,
    "local_user_synced": 5,
    "initialized_count": 2,
    "image_updated_count": 1,
    "deleted_local_auth": 5,
    "archived_stale_users": 0,
    "invalid_username_count": 0,
    "group_member_count": 3
  }
}
```

### 6. 获取可用认证源列表

**API 端点**：`GET /api/admin/auth/providers`

**响应格式**：
```json
{
  "success": true,
  "providers": [
    {
      "name": "local",
      "display_name": "本地",
      "has_password": true,
      "has_browser": false,
      "has_directory": false
    },
    {
      "name": "ldap",
      "display_name": "LDAP",
      "has_password": true,
      "has_browser": false,
      "has_directory": true
    },
    {
      "name": "oidc",
      "display_name": "OIDC",
      "has_password": false,
      "has_browser": true,
      "has_directory": false
    }
  ]
}
```

### 7. 白名单管理

**功能**：管理用户白名单，仅在对应认证模式启用时生效

**API 端点**：

- `GET /api/admin/whitelist` — 获取白名单列表
- `POST /api/admin/whitelist` — 更新白名单

**读取请求**：`GET /api/admin/whitelist`

**响应格式**：
```json
{
  "success": true,
  "users": ["user1", "user2"],
  "page": 1,
  "page_size": 50,
  "total": 2,
  "total_pages": 1
}
```

**更新请求**：`POST /api/admin/whitelist`（form 格式）
```
add=user1,user2
```
或：
```
remove=user3
```
或全量替换：
```
users=user1,user2,user3
```

## 涉及的 API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/config` | GET | 读取全局配置 |
| `/api/config` | POST | 保存全局配置 |
| `/api/admin/auth/providers` | GET | 获取可用认证源 |
| `/api/admin/auth/test-ldap` | POST | 测试 LDAP 连接 |
| `/api/admin/auth/sync-users` | POST | 同步 LDAP 用户 |
| `/api/admin/whitelist` | GET | 获取白名单 |
| `/api/admin/whitelist` | POST | 更新白名单 |
