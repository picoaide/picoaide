<template>
  <div>
    <a-page-header title="设置" sub-title="个人设置" />
    <a-spin :spinning="loading">
      <a-card title="用户信息" style="margin-top: 16px">
        <a-descriptions :column="1" bordered>
          <a-descriptions-item label="用户名">{{ userInfo.username }}</a-descriptions-item>
          <a-descriptions-item label="角色">
            <a-tag :color="userInfo.role === 'superadmin' ? 'red' : 'blue'">
              {{ userInfo.role === 'superadmin' ? '超管' : '用户' }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="来源">{{ userInfo.source || '本地' }}</a-descriptions-item>
        </a-descriptions>
      </a-card>

      <a-card title="MCP Token" style="margin-top: 16px">
        <p style="margin-bottom: 16px; color: #666">
          MCP Token 用于浏览器扩展和桌面代理连接
        </p>
        <a-input-group compact>
          <a-input :value="mcpToken" readonly style="width: calc(100% - 100px)" />
          <a-button type="primary" @click="copyToken" :disabled="!mcpToken">复制</a-button>
        </a-input-group>
      </a-card>

      <a-card title="安全设置" style="margin-top: 16px">
        <a-space>
          <a-button @click="$router.push('/user/authorization')">授权管理</a-button>
        </a-space>
      </a-card>
    </a-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'

const loading = ref(false)
const userInfo = ref<any>({})
const mcpToken = ref('')

const fetchUserInfo = async () => {
  try {
    const res = await fetch('/api/user/info')
    const data = await res.json()
    if (data.success) {
      userInfo.value = {
        username: data.username,
        role: data.role,
        source: data.source,
      }
    }
  } catch {
    message.error('获取用户信息失败')
  }
}

const fetchMCPToken = async () => {
  try {
    const res = await fetch('/api/mcp/token')
    const data = await res.json()
    if (data.success) {
      mcpToken.value = data.token
    }
  } catch {
    message.error('获取 MCP Token 失败')
  }
}

const copyToken = async () => {
  try {
    await navigator.clipboard.writeText(mcpToken.value)
    message.success('已复制到剪贴板')
  } catch {
    message.error('复制失败')
  }
}

onMounted(async () => {
  loading.value = true
  await Promise.all([fetchUserInfo(), fetchMCPToken()])
  loading.value = false
})
</script>
