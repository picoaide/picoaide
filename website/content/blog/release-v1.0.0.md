---
title: "PicoAide v1.0.0 正式发布"
description: "PicoAide 首个正式版本发布，提供完整的企业级 AI PaaS 解决方案"
date: 2026-05-06
draft: false
weight: 1
tags: ["发布"]
---

今天我们很高兴地宣布，PicoAide v1.0.0 正式发布。作为企业级 AI PaaS 工作平台，PicoAide 致力于让每位员工拥有自己的 AI 操作助手，同时确保企业数据的边界得到守护。

PicoAide 的核心理念是「AI 的力量应当被释放，但数据的边界必须被守护」。通过私有化部署、容器隔离和权限继承，企业可以在不将数据暴露给外部的情况下，让 AI 真正进入日常工作流程。

## 核心功能

PicoAide v1.0.0 提供了完整的平台功能。**容器化管理**方面，每个用户拥有独立的 PicoClaw AI 代理容器，通过 Docker Engine SDK 管理容器生命周期，自动创建和维护 `picoaide-net` 私有网络，确保容器间完全隔离。**MCP 中继层**实现了 AI 与外部工具的桥梁，通过 SSE + WebSocket 双协议架构，支持浏览器标签页控制和桌面环境操作，AI 可以自主完成网页浏览、表单填写、文件处理等复杂任务。**权限体系**支持本地认证和 LDAP 认证，通过用户组实现技能和配置的差异化下发，CSRF 保护和 HMAC 签名会话确保安全。

## 技术架构

PicoAide 采用 Go 语言开发，使用 Docker Engine Go SDK 直接管理容器，SQLite 记录状态，不依赖 docker-compose 等外部编排工具。网络方面使用 100.64.0.0/16 CGNAT 地址空间，ICC 设为 false 禁止容器间通信。配置管理采用分层合并策略，全局配置通过 `util.MergeMap()` 合并到各用户配置中。MCP 协议层支持标准的 `initialize`、`tools/list`、`tools/call` JSON-RPC 方法，协议版本为 `2024-11-05`。

## 客户端生态

浏览器扩展（PicoAide Helper）让 AI 能够控制 Chrome 标签页，提供导航、点击、输入、截图等 11 个浏览器操作工具。桌面客户端让 AI 能够控制用户的桌面环境，提供屏幕截图、鼠标控制、键盘输入、文件操作等 15 个桌面工具，支持 6 个权限组和白名单目录机制。两项客户端均通过 WebSocket 与 PicoAide Server 建立长连接，实现实时的指令转发和结果返回。

## 开始使用

现在就开始体验 PicoAide。访问 [快速开始](/docs/quick-start/) 指南，几分钟内即可完成部署。源码和安装包可在 [GitHub](https://github.com/picoaide/picoaide) 获取。如果你在使用过程中遇到问题，请查阅 [文档](/docs/) 或在 GitHub 上提交 Issue。
