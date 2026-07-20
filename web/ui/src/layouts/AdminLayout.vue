<template>
  <a-layout class="admin-layout">
    <a-layout-sider v-model:collapsed="collapsed" :trigger="null" collapsible class="admin-sider">
      <div class="logo">
        <img src="../assets/logo.svg" alt="" class="logo-icon" />
        <span v-if="!collapsed">PicoAide</span>
      </div>
      <a-menu
        v-model:selectedKeys="selectedKeys"
        theme="dark"
        mode="inline"
        @click="handleMenuClick"
      >
        <a-menu-item key="dashboard">
          <template #icon><DashboardOutlined /></template>
          <span>概览</span>
        </a-menu-item>
        <a-menu-item key="users">
          <template #icon><UserOutlined /></template>
          <span>用户管理</span>
        </a-menu-item>
        <a-menu-item key="groups">
          <template #icon><TeamOutlined /></template>
          <span>用户组</span>
        </a-menu-item>
        <a-menu-item key="teamspace">
          <template #icon><FolderOutlined /></template>
          <span>团队空间</span>
        </a-menu-item>
        <a-menu-item key="wiki">
          <template #icon><BookOutlined /></template>
          <span>知识库</span>
        </a-menu-item>
        <a-menu-divider />
        <a-menu-item key="skills">
          <template #icon><ThunderboltOutlined /></template>
          <span>技能库</span>
        </a-menu-item>
        <a-menu-divider />
        <a-menu-item key="channels">
          <template #icon><MessageOutlined /></template>
          <span>通讯渠道</span>
        </a-menu-item>
        <a-menu-item key="models">
          <template #icon><RobotOutlined /></template>
          <span>模型配置</span>
        </a-menu-item>
        <a-menu-divider />
        <a-menu-item key="superadmins">
          <template #icon><SafetyCertificateOutlined /></template>
          <span>超管账户</span>
        </a-menu-item>
        <a-menu-item key="auth">
          <template #icon><LockOutlined /></template>
          <span>认证配置</span>
        </a-menu-item>
        <a-menu-divider />
        <a-menu-item key="password">
          <template #icon><KeyOutlined /></template>
          <span>修改密码</span>
        </a-menu-item>
        <a-menu-divider />
        <a-menu-item key="mcp-servers">
          <template #icon><ApiOutlined /></template>
          <span>MCP 服务</span>
        </a-menu-item>
        <a-menu-divider />
        <a-menu-item key="tls">
          <template #icon><SafetyOutlined /></template>
          <span>HTTPS 证书</span>
        </a-menu-item>
      </a-menu>
    </a-layout-sider>
    <a-layout>
      <a-layout-header class="admin-header">
        <MenuUnfoldOutlined
          v-if="collapsed"
          class="trigger"
          @click="collapsed = false"
        />
        <MenuFoldOutlined
          v-else
          class="trigger"
          @click="collapsed = true"
        />
        <div class="header-right">
          <span class="username">{{ username }}</span>
          <a-button type="link" @click="handleLogout">退出</a-button>
        </div>
      </a-layout-header>
      <a-layout-content class="admin-content">
        <router-view />
      </a-layout-content>
    </a-layout>
  </a-layout>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  DashboardOutlined,
  UserOutlined,
  TeamOutlined,
  FolderOutlined,
  BookOutlined,
  ThunderboltOutlined,
  MessageOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  LockOutlined,
  KeyOutlined,
  ApiOutlined,
  SafetyOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
} from '@ant-design/icons-vue'

const router = useRouter()
const route = useRoute()
const collapsed = ref(false)
const selectedKeys = ref<string[]>(['dashboard'])
const username = ref(localStorage.getItem('username') || '')

watch(() => route.name, (name) => {
  if (name && typeof name === 'string') {
    selectedKeys.value = [name.replace('admin-', '')]
  }
}, { immediate: true })

const handleMenuClick = ({ key }: { key: string }) => {
  router.push({ name: `admin-${key}` })
}

const handleLogout = async () => {
  await fetch('/api/logout', { method: 'POST' })
  localStorage.removeItem('session')
  localStorage.removeItem('username')
  router.push('/login')
}
</script>

<style scoped>
.admin-layout {
  min-height: 100vh;
}
.admin-sider {
  background: #001529;
}
.logo {
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: #fff;
  font-size: 18px;
  font-weight: 600;
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
}
.logo-icon {
  width: 28px;
  height: 28px;
}
.admin-header {
  background: #fff;
  padding: 0 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.08);
}
.trigger {
  font-size: 18px;
  cursor: pointer;
  transition: color 0.3s;
}
.trigger:hover {
  color: #1890ff;
}
.header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.username {
  color: #666;
}
.admin-content {
  margin: 24px;
  padding: 24px;
  background: #fff;
  border-radius: 8px;
  min-height: 280px;
}
</style>
