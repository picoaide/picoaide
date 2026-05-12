# 登录页 + 认证模式

## 页面路由

`/login`

## 权限要求

无需登录，公开页面

## 功能概述

提供用户认证入口。根据后端认证模式自动切换 UI：

| 认证模式 | 登录方式 | provider.has_password | provider.has_browser |
|----------|----------|----------------------|---------------------|
| local | 用户名+密码 | true | false |
| ldap | 用户名+密码（委托 LDAP 验证） | true | false |
| oidc | SSO 按钮跳转 | false | true |

页面加载时调用 `GET /api/login/mode` 决定显示什么。

## 功能详细说明

### 1. 本地/LDAP 密码登录

**触发方式**：在用户名/密码输入框填写后点击"登录"按钮

**表单字段**：

| 字段 | 类型 | 说明 |
|------|------|------|
| username | string | 用户名 |
| password | string | 密码 |

**API 端点**：`POST /api/login`

**请求格式**（form 编码）：
```
username=admin&password=admin123
```

**成功响应**：
```json
{"success":true,"username":"admin"}
```
超管跳转 `/admin/dashboard`，普通用户跳转 `/manage` 或 `/initializing`。
统一认证模式下首次登录的外部用户会触发自动初始化（创建目录、容器记录等），此时 `initializing` 字段为 true。

**失败响应**：
```json
{"success":false,"error":"用户名或密码错误"}
```

**速率限制**：同一 IP 10 次/5 分钟，超出返回 429。

### 2. SSO 登录（仅 OIDC 模式）

**触发方式**：点击"SSO 登录"按钮，跳转到 OIDC Provider

**API 端点**：`GET /api/login/auth`
- 302 重定向到 OIDC Provider 授权页面
- 设置 `auth_state` cookie（10 分钟有效，CSRF 防护）

**回调**：`GET /api/login/callback?code=xxx&state=yyy`
- 验证 state 一致性
- 交换授权码获取身份信息
- 同步用户到本地
- 重定向到 `/manage` 或 `/initializing`

### 3. 退出登录

**触发方式**：点击右上角"退出"按钮

**API 端点**：`POST /api/logout`

**响应**：
```json
{"success":true,"message":"已登出"}
```
清除 session cookie，跳转到 `/login`。

### 4. 超管逃生通道

统一认证模式下（ldap/oidc），超管仍可使用本地密码登录。代码逻辑（`handleLogin`）：

```
authenticated = authsource.Authenticate(cfg, username, password)  // 走当前认证源
if !authenticated && isSuperadmin:
    ok, _, _ = auth.AuthenticateLocal(username, password)         // 尝试本地密码
    authenticated = ok
```

## 涉及的 API 端点

| 端点 | 方法 | 认证 | 说明 |
|------|------|------|------|
| `/api/login/mode` | GET | 无 | 返回当前认证模式和 provider 能力信息 |
| `/api/login` | POST | 限速 | 用户名密码登录 |
| `/api/login/auth` | GET | 无 | SSO 授权跳转 |
| `/api/login/callback` | GET | state cookie | SSO 回调处理 |
| `/api/logout` | POST | 无 | 登出 |

## 特殊说明

- 超管不能在浏览器扩展中登录（扩展只能使用 MCP token）
- 统一认证模式下修改密码不可用
- local 与 ldap 模式的前端登录界面完全相同，区别在后端认证逻辑
