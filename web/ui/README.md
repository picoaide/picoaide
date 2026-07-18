# PicoAide 前端

基于 Vue 3 + TypeScript + Ant Design Vue 的管理界面。

## 目录结构

```
web/ui/
├── src/
│   ├── layouts/          # 布局组件
│   │   ├── AdminLayout.vue
│   │   └── UserLayout.vue
│   ├── views/
│   │   ├── admin/        # 管理后台页面
│   │   └── user/         # 用户端页面
│   ├── router/           # 路由配置
│   ├── styles/           # 全局样式
│   └── main.ts           # 入口文件
├── package.json
└── vite.config.ts
```

## 开发

```bash
cd web/ui
npm install
npm run dev     # 启动开发服务器 (localhost:5173)
```

开发服务器会自动代理 `/api` 请求到 `http://localhost:8080`。

## 构建

前端构建产物输出到 `internal/web/dist/`，然后通过 `go:embed` 嵌入到 Go 二进制中。

```bash
# 仅构建前端
make build-ui

# 完整构建（前端 + 后端）
make build
```

**注意**：直接运行 `go build` 需要先构建前端，否则会因 `internal/web/dist/` 为空而编译失败。

## 技术栈

- **框架**: Vue 3 + TypeScript
- **UI 库**: Ant Design Vue 4
- **路由**: Vue Router 4
- **状态管理**: Pinia
- **构建工具**: Vite
- **Markdown**: markdown-it + DOMPurify
- **HTTP**: 原生 fetch API
