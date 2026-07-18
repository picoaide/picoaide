<template>
  <a-layout class="user-layout">
    <a-layout-header class="user-header">
      <div class="header-left">
        <img src="../assets/logo.svg" alt="" class="logo-icon" />
        <span class="title">PicoAide</span>
      </div>
      <a-tabs v-model:activeKey="activeTab" class="header-tabs" @change="handleTabChange">
        <a-tab-pane key="chat" tab="对话" />
        <a-tab-pane key="skills" tab="技能中心" />
        <a-tab-pane key="channels" tab="通讯渠道" />
        <a-tab-pane key="email" tab="邮箱" />
        <a-tab-pane key="files" tab="文件管理" />
        <a-tab-pane key="teamspace" tab="团队空间" />
        <a-tab-pane key="authorization" tab="AI 授权" />
        <a-tab-pane key="cron" tab="定时任务" />
        <a-tab-pane key="password" tab="修改密码" />
      </a-tabs>
      <div class="header-right">
        <span class="username">{{ username }}</span>
        <a-button type="link" @click="handleLogout">退出</a-button>
      </div>
    </a-layout-header>
    <a-layout-content class="user-content">
      <router-view />
    </a-layout-content>
  </a-layout>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'

const router = useRouter()
const route = useRoute()
const activeTab = ref('chat')
const username = ref(localStorage.getItem('username') || '')

watch(() => route.name, (name) => {
  if (name && typeof name === 'string') {
    activeTab.value = name.replace('user-', '')
  }
}, { immediate: true })

const handleTabChange = (key: string) => {
  router.push({ name: `user-${key}` })
}

const handleLogout = async () => {
  await fetch('/api/logout', { method: 'POST' })
  localStorage.removeItem('session')
  localStorage.removeItem('username')
  router.push('/login')
}
</script>

<style scoped>
.user-layout {
  min-height: 100vh;
  background: #f5f5f5;
}
.user-header {
  background: #fff;
  display: flex;
  align-items: center;
  padding: 0 24px;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.08);
  height: 56px;
  line-height: 56px;
}
.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-right: 32px;
}
.logo-icon {
  width: 28px;
  height: 28px;
}
.title {
  font-size: 18px;
  font-weight: 600;
  color: #333;
}
.header-tabs {
  flex: 1;
  margin-bottom: 0;
  overflow: hidden;
}
.header-tabs :deep(.ant-tabs-nav) {
  margin-bottom: 0;
}
.header-tabs :deep(.ant-tabs-tab) {
  padding: 16px 0;
  font-size: 13px;
}
.header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.username {
  color: #666;
}
.user-content {
  padding: 24px;
}
</style>
