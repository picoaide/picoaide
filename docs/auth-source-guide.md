# 接入新的认证源

## 概述

PicoAide 使用认证源注册表模式。每种认证源独立一个文件，通过 `init()` 自动注册到全局 map。新增认证源只需写 Go 代码，无需修改前端。

认证源通过能力接口（interface）声明自己支持的功能，前端根据后端返回的能力描述自动渲染登录页面和配置表单。

相关代码位置：`internal/authsource/`

---

## 快速上手

在 `internal/authsource/` 下新建文件（如 `saml.go`），实现必要接口，用 `init()` 注册：

```go
package authsource

import "github.com/picoaide/picoaide/internal/config"

type SAMLProvider struct{}

func init() {
  Register("saml", SAMLProvider{})
}

func (SAMLProvider) DisplayName() string {
  return "SAML"
}
```

注册名（`"saml"`）会作为配置中 `web.auth_mode` 的值，也是 `ldap.host`、`oidc.issuer_url` 这类配置键的前缀。

---

## 能力接口

有三种核心能力接口，按需实现：

### 1. PasswordProvider — 用户名密码认证

```go
type PasswordProvider interface {
  Authenticate(cfg *config.GlobalConfig, username, password string) bool
}
```

| 方法 | 触发时机 | 说明 |
|------|----------|------|
| `Authenticate` | 用户在登录页输入用户名密码提交时 | 返回 true/false |

实现该接口后，前端登录页会自动显示用户名密码输入框。

### 2. BrowserProvider — 浏览器跳转/回调认证（OIDC 风格）

```go
type BrowserProvider interface {
  AuthURL(cfg *config.GlobalConfig, state string) (string, error)
  CompleteLogin(ctx context.Context, cfg *config.GlobalConfig, code string) (*Identity, error)
}
```

| 方法 | 触发时机 | 说明 |
|------|----------|------|
| `AuthURL` | 用户点击"SSO 登录"按钮 | 返回授权 URL，浏览器跳转 |
| `CompleteLogin` | 认证服务器回调时 | 返回 `*Identity{Username, Groups}` |

实现该接口后，前端登录页会自动显示 SSO 登录按钮。

### 3. DirectoryProvider — 用户/组目录同步

```go
type DirectoryProvider interface {
  FetchUsers(cfg *config.GlobalConfig) ([]string, error)
  FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error)
  FetchGroups(cfg *config.GlobalConfig) (GroupHierarchy, error)
}
```

| 方法 | 触发时机 | 说明 |
|------|----------|------|
| `FetchUsers` | 同步账号、搜索 LDAP 用户 | 返回所有用户名列表 |
| `FetchUserGroups` | 获取单个用户的组成员关系 | 返回组名列表 |
| `FetchGroups` | 同步用户组、预览组结构 | 返回 `map[组名]GroupNode{Members, SubGroups}` |

实现该接口后：
- 系统管理页显示同步账号和同步用户组按钮
- 白名单模块显示搜索用户功能
- 自动同步定时器生效

---

## 可选接口

### Describable — 自定义显示名

```go
type Describable interface {
  DisplayName() string
}
```

不实现则默认使用注册名作为显示名。

### Configurable — 声明配置字段

```go
type Configurable interface {
  ConfigFields() []FieldSection
}
```

认证源声明自己在全局配置中的字段（如 `ldap.host`、`oidc.client_id`），前端据此动态渲染配置表单。

字段类型支持：

| FieldType | HTML 元素 | 说明 |
|-----------|-----------|------|
| `FieldText` | `<input type="text">` | 文本输入 |
| `FieldPassword` | `<input type="password">` | 密码输入 |
| `FieldSelect` | `<select>` | 下拉选择，需提供 Options |

字段定义示例：

```go
func (P SAMLProvider) ConfigFields() []FieldSection {
  return []FieldSection{
    {
      Name: "SAML 配置",
      Fields: []FieldDefinition{
        {Key: "saml.idp_url", Label: "IdP URL", Type: FieldText, Placeholder: "https://idp.example.com", Required: true},
        {Key: "saml.client_id", Label: "Client ID", Type: FieldText, Required: true},
        {Key: "saml.client_secret", Label: "Client Secret", Type: FieldPassword, Required: true},
        {Key: "saml.username_attr", Label: "用户名属性", Type: FieldText, Default: "email"},
        {Key: "saml.scopes", Label: "Scopes", Type: FieldSelect, Default: "openid",
          Options: []FieldOption{
            {Value: "openid", Label: "OpenID"},
            {Value: "profile", Label: "Profile"},
          },
        },
      },
    },
    {
      Name: "同步",
      Fields: []FieldDefinition{
        {Key: "saml.sync_interval", Label: "自动同步间隔", Type: FieldSelect, Default: "30m",
          Options: []FieldOption{
            {Value: "0", Label: "禁用"},
            {Value: "5m", Label: "每 5 分钟"},
            {Value: "30m", Label: "每 30 分钟"},
            {Value: "1h", Label: "每 1 小时"},
            {Value: "24h", Label: "每天"},
          },
        },
      },
    },
  }
}
```

### Actionable — 自定义操作按钮

```go
type Actionable interface {
  Actions() []ActionDefinition
}
```

在配置表单的某个 Section 下方显示操作按钮。`Section` 字段必须与 `ConfigFields` 中某个 `FieldSection.Name` 匹配。

```go
func (P SAMLProvider) Actions() []ActionDefinition {
  return []ActionDefinition{
    {ID: "test-saml", Label: "测试连接", Section: "SAML 配置"},
  }
}
```

操作按钮的点击事件需要在 Web handler 中处理（见下文）。

---

## 配置键命名规范

认证源的配置键统一用 `{注册名}.{字段名}` 格式：

| 注册名 | 配置键示例 |
|--------|-----------|
| `ldap` | `ldap.host`、`ldap.filter`、`ldap.whitelist_enabled` |
| `oidc` | `oidc.issuer_url`、`oidc.client_id`、`oidc.whitelist_enabled` |
| `saml` | `saml.idp_url`、`saml.username_attr`、`saml.whitelist_enabled` |

`whitelist_enabled` 字段必须放在对应 provider 的命名空间下（如 `ldap.whitelist_enabled`），系统使用 `{mode}.whitelist_enabled` 读写。

---

## 完整的 Provider 示例

同时实现全部能力接口的 Provider：

```go
package authsource

import (
  "context"
  "github.com/picoaide/picoaide/internal/config"
)

type EnterpriseProvider struct{}

func init() {
  Register("enterprise", EnterpriseProvider{})
}

// --- 必要的基础接口 ---

func (EnterpriseProvider) DisplayName() string {
  return "企业微信"
}

// --- 能力接口：按需实现 ---

func (EnterpriseProvider) Authenticate(cfg *config.GlobalConfig, username, password string) bool {
  // 调用企业微信 API 验证密码
  return true
}

func (EnterpriseProvider) AuthURL(cfg *config.GlobalConfig, state string) (string, error) {
  return "https://open.work.weixin.qq.com/..." + state, nil
}

func (EnterpriseProvider) CompleteLogin(ctx context.Context, cfg *config.GlobalConfig, code string) (*Identity, error) {
  // 用 code 换取用户信息
  return &Identity{Username: code, Groups: []string{"group1"}}, nil
}

func (EnterpriseProvider) FetchUsers(cfg *config.GlobalConfig) ([]string, error) {
  return []string{"user1", "user2"}, nil
}

func (EnterpriseProvider) FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error) {
  return []string{"group1"}, nil
}

func (EnterpriseProvider) FetchGroups(cfg *config.GlobalConfig) (GroupHierarchy, error) {
  return GroupHierarchy{
    "group1": {Members: []string{"user1"}, SubGroups: []string{}},
  }, nil
}

// --- 配置表单声明 ---

func (EnterpriseProvider) ConfigFields() []FieldSection {
  return []FieldSection{
    {
      Name: "企业微信配置",
      Fields: []FieldDefinition{
        {Key: "enterprise.corp_id", Label: "企业 ID", Type: FieldText, Required: true},
        {Key: "enterprise.agent_id", Label: "应用 AgentID", Type: FieldText, Required: true},
        {Key: "enterprise.secret", Label: "应用 Secret", Type: FieldPassword, Required: true},
      },
    },
  }
}

func (EnterpriseProvider) Actions() []ActionDefinition {
  return []ActionDefinition{
    {ID: "test-enterprise", Label: "测试连接", Section: "企业微信配置"},
  }
}
```

---

## 前端处理操作按钮

如果 Provider 实现了 `Actionable`，需要在前端 `handleAction` 函数中添加对应的 handler（在 `internal/web/ui/admin/modules/auth.js` 中）：

```js
async function handleAction(ctx, actionId) {
  switch (actionId) {
    case 'test-ldap':
      await testLDAP(ctx);
      break;
    case 'test-enterprise':
      await testEnterprise(ctx);
      break;
    default:
      ctx.showMsg('#auth-msg', '未知操作', false);
  }
}
```

---

## 认证源切换清理逻辑

系统在切换 `web.auth_mode` 时会自动清理旧 provider 的所有数据：
- 清空 `local_users` 中 source 为旧 provider 的用户记录
- 清空组成员、容器记录
- 归档用户目录

这个逻辑是通用的，新增认证源无需额外处理。

---

## 自检清单

新增认证源后检查以下项目：

- [ ] `init()` 中调用了 `Register(name, ProviderType{})`
- [ ] `DisplayName()` 返回了中文显示名（若期望自定义名称）
- [ ] `ConfigFields()` 中的 Key 以 `{注册名}.` 为前缀
- [ ] 配置字段的 Default 值与 Go 结构体中的零值一致
- [ ] `handleAction` 中添加了新操作按钮的处理
- [ ] 补充了单元测试：provider 注册断言、字段定义断言
- [ ] 补充了集成测试：登录流程、同步流程

---

## 参考实现

- `internal/authsource/local.go` — 最简实现（仅密码认证，无配置字段）
- `internal/authsource/ldap.go` — 完整实现（密码 + 目录 + 配置字段 + 操作按钮）
- `internal/authsource/oidc.go` — 浏览器认证 + 配置字段（无目录能力）
