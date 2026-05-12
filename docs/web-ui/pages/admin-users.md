# 用户管理页面

## 页面路由

`/admin/users`

## 权限要求

超管（`superadmin`）登录

## 功能概述

管理系统用户。页面根据当前认证模式呈现不同 UI：

- **local 模式**：显示"创建用户/批量创建/删除"等操作按钮，用户可手工管理
- **LDAP/OIDC 模式**：显示"普通用户由当前认证源同步，不支持手动新建或删除"提示，创建/删除按钮不可见，用户由认证源自动同步

## 功能详细说明

### 1. 用户列表

**功能**：分页展示所有用户，显示用户名、来源、所属组、容器状态、镜像版本、IP 地址

**API 端点**：`GET /api/admin/users`

**查询参数**：

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| page | int | 1 | 页码 |
| page_size | int | 20 | 每页数量（可选 20/50/100） |
| search | string | — | 搜索关键字 |

**响应格式**：
```json
{
  "success": true,
  "users": [
    {
      "username": "daichenglong",
      "source": "ldap",
      "groups": ["confluence-users", "jira-software-users"],
      "container_status": "running",
      "image": "v0.2.8",
      "ip": "100.64.0.5",
      "ready": true
    }
  ],
  "total": 3,
  "page": 1,
  "page_size": 20,
  "total_pages": 1
}
```

**每行操作按钮**：启动、重启、下发配置、日志、更多

### 2. 创建用户（仅 local 模式）

**触发方式**：点击"创建用户"按钮 → 弹出表单

**API 端点**：`POST /api/admin/users/create`

**请求格式**（form）：
```
username=newuser&password=userpassword
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| username | string | 是 | 需符合 `^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`，最长 64 字符 |
| password | string | 否 | 不填则自动生成 |

**成功响应**：
```json
{"success":true,"username":"newuser","password":"autoGenPass123"}
```

**失败响应**（重复用户名）：
```json
{"success":false,"error":"用户名已存在"}
```

### 3. 删除用户（仅 local 模式）

**触发方式**：点击用户行的"删除"按钮

**API 端点**：`POST /api/admin/users/delete`

**请求格式**（form）：
```
username=xxx&csrf_token=yyy
```

**删除后处理**：用户目录归档到 `archive/`，容器被停止并删除，数据库记录清除。

### 4. 批量创建用户（仅 local 模式）

**API 端点**：`POST /api/admin/users/batch-create`

**请求格式**（form）：
```
usernames=user1,user2,user3&csrf_token=yyy
```

**响应**：
```json
{"success":true,"message":"批量创建完成","users":[{"username":"user1","password":"xxx"},...]}
```

## 涉及的 API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/admin/users` | GET | 用户列表（分页+搜索） |
| `/api/admin/users/create` | POST | 创建用户（仅 local） |
| `/api/admin/users/batch-create` | POST | 批量创建（仅 local） |
| `/api/admin/users/delete` | POST | 删除用户（仅 local） |
| `/api/admin/container/start` | POST | 启动用户容器 |
| `/api/admin/container/restart` | POST | 重启用户容器 |
| `/api/admin/container/logs` | GET | 查看容器日志 |
| `/api/admin/config/apply` | POST | 下发配置到用户 |
