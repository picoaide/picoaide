# 用户组技能自动下发 — 测试流程

## 背景

用户加入组时，自动部署该组绑定的技能到用户目录。支持三种加入路径：

- 本地手动加组（`POST /api/admin/groups/members/add`）
- LDAP 同步（`SyncUserDirectory` → `SyncUserGroups`）
- LDAP/OIDC 组同步（`SyncGroups` → `ReplaceGroupMembersBySource`）

## 代码改动

| 文件 | 改动 |
|------|------|
| `internal/user/user.go` | 新增 `DeployGroupSkillsToUser(cfg, username)` 函数 |
| `internal/web/admin_groups.go` | `handleAdminGroupMembersAdd` 中新增成员后调用部署 |
| `internal/authsource/sync.go` | `SyncUserDirectory` 和 `SyncGroups` 中同步后调用部署 |

## 测试环境

- 测试服务器：`<测试服务器 IP>`
- 二进制路径：`/usr/sbin/picoaide`
- 服务名：`picoaide`
- 工作目录：`/data/picoaide/`

## 测试步骤

### 1. 准备测试数据

```bash
# 创建测试技能目录
ssh root@<测试服务器> "mkdir -p /data/picoaide/skill/web-dev-skill && echo 'Web开发技能包' > /data/picoaide/skill/web-dev-skill/README.md"

# 创建测试用户（通过 API）
curl -s -c /tmp/cookie http://<测试服务器>/api/login -d "username=admin&password=<password>"
CSRF=$(curl -s -b /tmp/cookie http://<测试服务器>/api/csrf | python3 -c "import sys,json;print(json.load(sys.stdin)['csrf_token'])")

# 创建组
curl -s -b /tmp/cookie "http://<测试服务器>/api/admin/groups/create" -d "name=test-group&csrf_token=$CSRF"

# 创建用户
curl -s -b /tmp/cookie "http://<测试服务器>/api/admin/users/create" -d "username=testuser&csrf_token=$CSRF"
```

### 2. 绑定技能到组

```bash
curl -s -b /tmp/cookie "http://<测试服务器>/api/admin/groups/skills/bind" \
  -d "group_name=test-group&skill_name=web-dev-skill&csrf_token=$CSRF"
```

预期返回：`"user_count":0`（尚无成员）

### 3. 添加用户到组（触发自动下发）

```bash
curl -s -b /tmp/cookie "http://<测试服务器>/api/admin/groups/members/add" \
  -d "group_name=test-group&usernames=testuser&csrf_token=$CSRF"
```

预期返回：`"success":true`

### 4. 验证技能已下发

```bash
ssh root@<测试服务器> "ls -la /data/picoaide/users/testuser/.picoclaw/workspace/skills/web-dev-skill/"
ssh root@<测试服务器> "cat /data/picoaide/users/testuser/.picoclaw/workspace/skills/web-dev-skill/README.md"
```

预期：目录存在，`README.md` 内容为 `Web开发技能包`

### 5. 浏览器 UI 测试

1. 打开 `http://<测试服务器>/admin/groups`
2. 点击目标组的「详情」按钮
3. 切换到「绑定技能」标签页
4. 选择技能 → 点击「绑定并部署」
5. 确认提示「已绑定到组 … 并部署到 N 个用户」
6. 切换到「成员」标签页
7. 点击可添加用户旁的 `+` 按钮
8. 确认用户出现在「已加入成员」列表中（成员数 +1）
9. 登录服务器验证技能目录已创建

## 测试结果（2026-05-13）

| 步骤 | 结果 |
|------|------|
| API: 绑定技能到组 | 通过 — 部署到 0 个用户（组空） |
| API: 添加用户到组 | 通过 — 自动触发部署 |
| 验证: 技能文件存在 | 通过 — `README.md` 内容匹配 |
| 浏览器: 绑定技能 | 通过 — 提示已部署到现有成员 |
| 浏览器: 添加成员 | 通过 — 成员数从 1 变为 2 |
| 浏览器: 验证下发 | 通过 — 新用户目录下有技能文件 |

## 覆盖路径

```
用户加入组 ─┬─ 本地手动 (handleAdminGroupMembersAdd)  ✅
            ├─ LDAP 用户同步 (SyncUserDirectory)       ✅
            └─ LDAP 组同步 (SyncGroups)                ✅
```
