# 前端

基于 Vue 3 + TypeScript + Ant Design Vue 4 的 SPA。

## 目录结构

```
web/ui/
├── src/
│   ├── composables/         # 组合式函数
│   │   ├── api.ts           # API 抽象层（get/post，自动 CSRF）
│   │   ├── useCsrf.ts       # CSRF token 获取
│   │   └── usePagination.ts # 共享分页状态
│   ├── components/          # 共享组件
│   │   └── PasswordForm.vue # 密码修改组件
│   ├── layouts/             # 布局组件
│   │   ├── AdminLayout.vue  # 管理后台布局（侧边栏菜单）
│   │   └── UserLayout.vue   # 用户端布局（Tab 导航）
│   ├── views/
│   │   ├── admin/           # 13 个管理页面
│   │   ├── user/            # 10 个用户页面
│   │   └── Login.vue        # 登录页
│   ├── router/index.ts      # 路由配置
│   ├── styles/global.css    # 全局样式
│   ├── main.ts              # 入口（注册 Ant Design Vue + Router）
│   └── App.vue              # 根组件
├── index.html
├── vite.config.ts
└── package.json
```

## API 层

`composables/api.ts` 提供统一的 HTTP 客户端：

```typescript
import { api } from '../../composables/api'

// GET 请求
const data = await api.get('/admin/users', { page: '1', search: '' })

// POST 请求（自动添加 X-CSRF-Token header）
const result = await api.post('/admin/users/create', { username: 'newuser' })
```

所有 API 统一使用：
- `Content-Type: application/json`
- X-CSRF-Token 从 `/api/csrf` 获取，自动添加到请求头
- `credentials: 'include'` 发送 session cookie

## 路由守卫

`router/index.ts` 中的 `beforeEach` 守卫：
- 检查 `localStorage.getItem('session')`
- 未登录 → 重定向到 `/login`
- 已登录 → 放行

Go 服务端也有双重守卫：
- `/user/*`: 普通用户可访问（超管重定向到 `/admin/dashboard`）
- `/admin/*`: 仅超管可访问（普通用户重定向到 `/user/chat`）

## 关键页面

### 管理后台（13 页）

| 路径 | 组件 | 说明 |
|------|------|------|
| `/admin/dashboard` | Dashboard.vue | 统计概览卡 |
| `/admin/users` | Users.vue | 用户 CRUD、批量创建、搜索分页 |
| `/admin/groups` | Groups.vue | 用户组管理、成员管理、技能绑定 |
| `/admin/skills` | Skills.vue | 技能库、来源管理、注册中心 |
| `/admin/superadmins` | Superadmins.vue | 超管账户管理 |
| `/admin/auth` | Auth.vue | 白名单、LDAP 配置、同步 |
| `/admin/mcp-servers` | MCPServers.vue | MCP 服务管理 |
| `/admin/tls` | TLS.vue | HTTPS 证书管理 |
| `/admin/settings` | Settings.vue | 全局配置 JSON 编辑器 |
| `/admin/teamspace` | Teamspace.vue | 共享文件夹管理 |
| `/admin/channels` | Channels.vue | 通讯渠道配置 |
| `/admin/models` | Models.vue | 模型配置与测试 |
| `/admin/password` | Password.vue | 超管密码修改 |

### 用户端（10 页）

| 路径 | 组件 | 说明 |
|------|------|------|
| `/user/chat` | Chat.vue | AI 对话（多会话、流式输出） |
| `/user/skills` | Skills.vue | 技能安装/卸载 |
| `/user/files` | Files.vue | 文件管理（上传/编辑/删除/新建目录） |
| `/user/channels` | Channels.vue | 渠道配置 |
| `/user/email` | Email.vue | 邮箱配置 |
| `/user/teamspace` | Teamspace.vue | 共享文件夹列表 |
| `/user/authorization` | Authorization.vue | Cookie 授权管理 |
| `/user/cron` | Cron.vue | 定时任务管理 |
| `/user/password` | Password.vue | 修改密码 |
| `/user/settings` | Settings.vue | 用户信息、MCP Token |
