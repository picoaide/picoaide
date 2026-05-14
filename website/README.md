# PicoAide 官网

基于 [Hugo](https://gohugo.io/) 构建的项目官网，部署在 Cloudflare Pages。

## 本地开发

```bash
# 安装 Hugo Extended (v0.147+)
# macOS: brew install hugo
# Linux: 下载 https://github.com/gohugoio/hugo/releases

# 本地预览
hugo server -D

# 构建
hugo --minify
```

## Cloudflare Pages 配置

- **构建命令**: `cd website && mkdir -p static/rules && cp -r ../internal/user/picoclaw_rules static/rules/picoclaw && hugo --minify`
- **输出目录**: `website/public`
- **环境变量**: `HUGO_VERSION = 0.147.5`
