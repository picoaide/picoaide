---
title: "认证与安全配置"
description: "PicoAide 认证体系 — Local、LDAP、OIDC 模式配置和白名单管理"
weight: 5
draft: false
---

PicoAide 支持三种认证模式，覆盖从单机部署到企业集成的全部场景。本文档详细说明每种模式的配置方法和安全机制。

## 认证模式概览

| 模式 | 配置值 | 用户来源 | 密码管理 | 适用场景 |
|------|--------|---------|---------|---------|
| 本地 | `local` | SQLite 本地用户 |  argon2id 哈希，PicoAide 管理 | 小团队、个人部署、测试环境 |
| LDAP | `ldap` | 目录服务器同步 | LDAP 服务器管理 | 企业内网、已有 AD/OpenLDAP |
| OIDC | `oidc` | IdP 浏览器跳转登录 | IdP 管理 | SSO 平台、Keycloak/Azure AD |

### 查看当前认证模式

```text
GET /api/login/mode
```

返回当前认证模式（`local`、`ldap` 或 `oidc`）。

### 切换认证模式

通过管理后台的「认证配置」页面或 API 修改 `web.auth_mode` 配置项。切换模式时请注意：

> **切换认证模式会清除所有普通用户数据**。系统会停止并删除所有普通用户的容器，归档用户目录，清空组成员关系。超管账户不受影响。

## 本地模式 (Local)

### 配置

`web.auth_mode = local` 时使用本地认证。无需额外配置，默认即为此模式。

### 用户管理

- 超管可以在管理后台手动创建/删除普通用户
- 普通用户可以在 `/manage` 页面修改自己的密码
- 密码使用 argon2id 算法哈希存储

### 密码重置

```bash
picoaide reset-password <username>
```

只对本地用户有效。如果用户是 LDAP 或 OIDC 来源，此命令无效。

## LDAP 模式

### 基础配置

在管理后台或 API 中配置以下 LDAP 参数：

| 字段 | 说明 | 示例 |
|------|------|------|
| `ldap.host` | LDAP 服务器地址 | `ldap.example.com:389` |
| `ldap.bind_dn` | Bind DN | `cn=admin,dc=example,dc=com` |
| `ldap.bind_password` | Bind 密码 | |
| `ldap.base_dn` | 用户搜索根 DN | `ou=people,dc=example,dc=com` |
| `ldap.filter` | 用户过滤器 | `(&(objectClass=person)(uid={{username}}))` |
| `ldap.username_attribute` | 用户名属性 | `uid` |

### 用户搜索流程

```text
1. 用户输入用户名和密码
2. PicoAide 使用 bind_dn + bind_password 绑定 LDAP
3. 在 base_dn 范围内使用 filter 搜索用户
4. 找到用户后，使用用户的 DN 和输入的密码进行第二次绑定验证
5. 验证通过后，检查白名单
6. 首次登录的用户自动创建本地快照和容器目录
```

### 白名单机制

白名单是 LDAP 模式下的额外访问控制层：

1. 启用白名单：设置 `ldap.whitelist_enabled = true`
2. 在管理后台的白名单页面添加允许登录的用户名
3. 不在白名单中的 LDAP 用户即使认证通过也无法登录

白名单适合在迁移阶段或测试阶段限制用户范围。

### 组同步

PicoAide 支持两种组同步模式：

**member_of 模式**（`ldap.group_search_mode = member_of`）：

用户对象的 `memberOf` 属性直接标识所属组。适用于 Active Directory。

**group_search 模式**（`ldap.group_search_mode = group_search`）：

通过搜索组对象来查找成员关系。适用于 OpenLDAP。

组同步相关配置：

| 字段 | 说明 |
|------|------|
| `ldap.group_base_dn` | 组搜索根 DN，如 `ou=groups,dc=example,dc=com` |
| `ldap.group_filter` | 组过滤器，如 `(&(objectClass=groupOfNames)(cn={{groupname}}))` |
| `ldap.group_member_attribute` | 组成员属性，如 `member` |

**定时同步**：

设置 `ldap.sync_interval` 可以定时从 LDAP 同步用户和组。支持 Go duration 格式：

- `30m` — 每 30 分钟同步一次
- `1h` — 每小时同步一次
- `24h` — 每天同步一次
- `0` — 关闭定时同步（默认）

也可以通过管理后台手动触发同步。

### 连接测试

在管理后台的认证配置页面可以测试 LDAP 连接：

- 测试基础连接（Bind 是否成功）
- 测试用户搜索（按用户名查找）
- 测试组查询（按组名查找组和成员）

## OIDC 模式

### 配置

在管理后台或 API 中配置 OIDC 参数：

| 字段 | 说明 | 示例 |
|------|------|------|
| `oidc.issuer_url` | Issuer URL | `https://keycloak.example.com/auth/realms/myrealm` |
| `oidc.client_id` | 客户端 ID | `picoaide` |
| `oidc.client_secret` | 客户端密钥 | |
| `oidc.redirect_url` | 回调地址 | `https://picoaide.example.com/api/login/callback` |

### 登录流程

```text
1. 用户访问登录页面，点击 OIDC 登录按钮
2. 浏览器跳转到 OIDC Provider 的认证页面
3. 用户在 Provider 侧输入凭证
4. Provider 回调到 PicoAide 的 callback 端点
5. PicoAide 验证 ID Token，提取用户信息
6. 创建或更新本地用户快照
7. 生成会话 Cookie，完成登录
```

### 声明映射

OIDC Provider 返回的 ID Token 中的声明字段可以通过配置映射到用户信息。默认使用 `preferred_username` 作为用户名。

## 超管账户管理

### 超管的特点

- 超管始终在本地存储，不依赖任何外部认证源
- 超管不能登录浏览器扩展和桌面客户端
- 超管不能获取 MCP Token
- 超管访问 `/manage` 会被重定向到 `/admin/dashboard`

### 创建超管

```text
POST /api/admin/superadmins/create
```

返回随机生成的密码。也可以在初始化时创建。

### 删除超管

系统至少保留一个超管。删除最后一个超管前需要先创建新的超管。

### 重置超管密码

```text
POST /api/admin/superadmins/reset
```

返回新的随机密码。也可以使用 CLI 命令：

```bash
picoaide reset-password <superadmin-username>
```

## 会话安全

### 会话 Cookie

登录成功后服务端写入 `session` Cookie：

```http
Cookie: session=<username>:<timestamp>:<signature>
```

- 格式：`用户名:时间戳:HMAC-SHA256签名`
- 有效期：24 小时
- 属性：`HttpOnly`、`SameSite=Lax`
- TLS 启用时设置 `Secure`

会话密钥持久化在 `settings` 表的 `internal.session_secret` 键中，重启服务后会话仍然有效。

### CSRF 保护

修改状态的表单 POST 请求需要携带 `csrf_token` 字段。Token 获取：

```text
GET /api/csrf
```

Token 机制：基于 `HMAC(小时窗口 + 用户名)` 生成，同一小时内同一用户的 Token 不变，前一小时的 Token 也有效（容忍时钟偏差）。

### 速率限制

登录接口限制为 10 次/5 分钟，基于内存计数器。超管和普通用户均受此限制。

### 安全 Header

| Header | 值 |
|--------|-----|
| `X-Frame-Options` | `DENY` |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |

## 认证模式切换的数据清理

这是一个重要的安全机制。当切换认证模式时（如从 LDAP 切换到 OIDC），系统会执行 `purgeOrdinaryAuthProviderStateForConfig()`：

1. 停止并删除所有普通用户的 Docker 容器
2. 清空 `containers` 表中的所有记录
3. 删除 `local_users` 表中所有普通用户（超管保留）
4. 清空 `groups` 和 `user_groups` 表
5. 归档用户目录到 `archive/` 下

设计理由：不同认证源的用户集、组结构和权限模型完全不同。保留旧数据会导致权限混乱和状态不一致。
