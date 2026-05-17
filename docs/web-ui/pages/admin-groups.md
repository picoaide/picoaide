# 用户组页面

## 页面路由

`/admin/groups`

## 权限要求

超管（`superadmin`）登录

## 功能概述

管理用户组，支持树形结构、组成员管理、组-技能绑定。子组自动继承父组的技能绑定。

## 功能详细说明

### 1. 用户组列表

**功能**：展示所有用户组，含成员预览和树形层级关系

**API 端点**：`GET /api/admin/groups`

**响应格式**：
```json
{
  "success": true,
  "groups": [
    {
      "id": 1,
      "name": "研发部",
      "parent_id": null,
      "source": "local",
      "member_count": 5,
      "members_preview": ["user1", "user2", "user3"],
      "skills": ["code-review", "deploy"],
      "children": [
        {
          "id": 2,
          "name": "前端组",
          "parent_id": 1,
          "source": "local",
          "member_count": 3,
          "members_preview": ["user4", "user5"],
          "skills": ["code-review"],
          "children": []
        }
      ]
    }
  ]
}
```

### 2. 创建组

**触发方式**：点击"创建组"按钮 → 输入组名 → 提交

**API 端点**：`POST /api/admin/groups/create`

**请求格式**：
```json
{
  "name": "新用户组",
  "parent_id": null
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| name | string | 是 | 组名 |
| parent_id | int | 否 | 父组 ID，null 为顶级组 |

**响应格式（成功）**：
```json
{
  "success": true,
  "message": "创建成功",
  "id": 3,
  "name": "新用户组"
}
```

**响应格式（失败）**：
```json
{
  "success": false,
  "error": "组名已存在"
}
```

### 3. 删除组

**触发方式**：点击组行的"删除"按钮 → 确认删除

**API 端点**：`POST /api/admin/groups/delete`

**请求格式**：
```json
{
  "id": 1
}
```

**注意事项**：删除组时，子组会被一并删除。组内的成员关系也会被清除。

### 4. 管理组成员

#### 添加组成员（仅 local 模式，统一认证模式返回 403）

**API 端点**：`POST /api/admin/groups/members/add`

**请求格式**（form）：
```
group_name=组名&usernames=user1,user2&csrf_token=xxx
```

**响应格式**：
```json
{
  "success": true,
  "message": "已添加 2 个用户到组 组名"
}
```

#### 移除组成员（仅 local 模式，统一认证模式返回 403）

**API 端点**：`POST /api/admin/groups/members/remove`

**请求格式**（form）：
```
group_name=组名&username=user1&csrf_token=xxx
```

### 5. 绑定/解绑技能

#### 绑定技能到组

**API 端点**：`POST /api/admin/groups/skills/bind`

**请求格式**（form）：
```
group_name=组名&skill_name=技能名&csrf_token=xxx
```

#### 解绑技能

**API 端点**：`POST /api/admin/groups/skills/unbind`

**请求格式**（form）：
```
group_name=组名&skill_name=技能名&csrf_token=xxx
```

**继承规则**：子组自动继承父组的所有技能。如果子组自身也绑定了技能，则合并生效。解绑只影响当前组，不影响子组继承。

### 6. LDAP 组同步

当认证模式为 `ldap` 且 `ldap.group_search_mode` 启用时，支持 LDAP 组同步。

LDAP 组同步通过认证源相关端点完成，详情见认证管理页面文档。

**API 端点**：`POST /api/admin/auth/sync-groups`

**请求格式**：空

**响应格式**：
```json
{
  "success": true,
  "message": "同步完成"
}
```

LDAP 连接测试通过 `POST /api/admin/auth/test-ldap` 端点进行，该端点为 LDAP 的整体连接验证（包括用户查询和组查询），不单独提供组测试端点。

## 涉及的 API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/admin/groups` | GET | 组列表 |
| `/api/admin/groups/create` | POST | 创建组 |
| `/api/admin/groups/delete` | POST | 删除组 |
| `/api/admin/groups/members` | GET | 组成员列表（参数 `name`） |
| `/api/admin/groups/members/add` | POST | 添加组成员（仅 local） |
| `/api/admin/groups/members/remove` | POST | 移除组成员（仅 local） |
| `/api/admin/groups/skills/bind` | POST | 绑定技能到组 |
| `/api/admin/groups/skills/unbind` | POST | 解绑组技能 |
| `/api/admin/auth/sync-groups` | POST | 同步 LDAP 组 |
| `/api/admin/auth/test-ldap` | POST | 测试 LDAP 连接（含组查询） |
