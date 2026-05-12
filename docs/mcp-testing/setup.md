# 环境搭建

## 前置条件
- PicoAide 服务已运行在 {SERVER_URL}
- AI 可以通过 MCP Browser 工具控制浏览器
- 浏览器已安装 PicoAide Helper 扩展
- 测试环境信息：admin/admin123（服务器地址等详见 [docs/ai-guide.md](../ai-guide.md)）

## 验证服务是否正常
1. 访问 {SERVER_URL}/api/health -> 返回 {"status":"ok","version":"..."}
2. 访问 {SERVER_URL}/login -> 页面正常加载

## 登录测试服务器
1. 通过 MCP Browser 工具 navigate 到 {SERVER_URL}/login
2. 使用 admin/admin123 登录

## LDAP 说明
- 测试服务器的 LDAP 已经预先配置好
- 可以通过浏览器登录测试 LDAP 用户
- 详细 LDAP 配置见 docs/web-ui/pages/admin-auth.md

## 可以执行的测试
- 完整的功能测试（docs/mcp-testing/scenarios/）
- 认证模式切换测试（重点：local <-> ldap）
- 用户 CRUD 测试
- 容器操作测试

## 测试前准备
每次测试前，确保：
1. 当前没有已登录的会话（或者先 logout）
2. 测试用的用户数据可以创建和删除
