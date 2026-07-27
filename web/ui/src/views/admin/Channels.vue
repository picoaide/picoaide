<template>
  <div>
    <a-page-header title="通讯渠道" sub-title="管理系统通讯渠道的启用状态与用户权限" />
    <a-tabs v-model:activeKey="activeTab">
      <a-tab-pane key="config" tab="渠道开关">
        <a-spin :spinning="loading">
          <a-row :gutter="[24, 24]">
            <a-col v-for="ch in channels" :key="ch.key" :span="6">
              <a-card :title="ch.label">
                <template #extra>
                  <a-switch
                    :checked="ch.enabled"
                    @change="(v: boolean) => toggleChannel(ch.key, v)"
                  />
                </template>
                <p style="color: #888; font-size: 12px">
                  <a :href="ch.guide_url" target="_blank" rel="noopener">查看开发文档</a>
                </p>
              </a-card>
            </a-col>
          </a-row>
        </a-spin>
      </a-tab-pane>

      <a-tab-pane key="users" tab="用户权限">
        <a-spin :spinning="loadingUsers">
          <div style="margin-bottom: 16px; display: flex; gap: 16px; align-items: center">
            <a-select
              v-model:value="selectedUser"
              placeholder="选择用户"
              style="width: 200px"
              show-search
              :filter-option="(input:string, option:any) => option.value.toLowerCase().indexOf(input.toLowerCase()) >= 0"
              @change="onUserSelect"
            >
              <a-select-option v-for="u in userList" :key="u" :value="u">{{ u }}</a-select-option>
            </a-select>
            <a-button type="primary" :disabled="!selectedUser" @click="saveUserPermissions">保存权限</a-button>
          </div>

          <div v-if="selectedUser">
            <a-checkbox-group v-model:value="userPermChannels">
              <a-row>
                <a-col v-for="ch in channels" :key="ch.key" :span="12" style="margin-bottom: 8px">
                  <a-checkbox :value="ch.key">{{ ch.label }}</a-checkbox>
                </a-col>
              </a-row>
            </a-checkbox-group>
          </div>

          <a-divider />
          <h4>当前权限概览</h4>
          <a-table
            :columns="overviewColumns"
            :data-source="permissionOverview"
            row-key="username"
            :pagination="{ pageSize: 20 }"
          />
        </a-spin>
      </a-tab-pane>
    </a-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

interface ChannelDef {
  key: string
  label: string
  guide_url: string
  enabled: boolean
}

interface UserPermItem {
  username: string
  channel: string
  allowed: boolean
}

const activeTab = ref('config')
const loading = ref(false)
const channels = ref<ChannelDef[]>([])

const loadingUsers = ref(false)
const userList = ref<string[]>([])
const selectedUser = ref<string | undefined>(undefined)
const userPermChannels = ref<string[]>([])
const allPerms = ref<UserPermItem[]>([])

const overviewColumns = [
  { title: '用户', dataIndex: 'username', key: 'username' },
  { title: '已授权渠道', key: 'channels' },
]

const permissionOverview = computed(() => {
  const grouped: Record<string, string[]> = {}
  for (const p of allPerms.value) {
    if (!p.allowed) continue
    if (!grouped[p.username]) grouped[p.username] = []
    grouped[p.username].push(p.channel)
  }
  return Object.entries(grouped).map(([username, chs]) => ({
    username,
    channels: chs.map((ch: string) => {
      const c = channels.value.find(cc => cc.key === ch)
      return c ? c.label : ch
    }).join(', '),
  }))
})

const loadChannels = async () => {
  loading.value = true
  try {
    const data = await api.get('/admin/channels')
    channels.value = data.channels || []
  } catch {
    message.error('获取渠道列表失败')
  } finally {
    loading.value = false
  }
}

const loadUsers = async () => {
  loadingUsers.value = true
  try {
    const [userData, permData] = await Promise.all([
      api.get('/admin/users'),
      api.get('/admin/channels/users'),
    ])
    userList.value = (userData.users || []).map((u: any) => u.username || u)
    allPerms.value = (permData.items || []).map((item: any) => ({
      username: item.username,
      channel: item.channel,
      allowed: item.allowed,
    }))
  } catch {
    message.error('获取数据失败')
  } finally {
    loadingUsers.value = false
  }
}

const onUserSelect = () => {
  if (!selectedUser.value) {
    userPermChannels.value = []
    return
  }
  userPermChannels.value = allPerms.value
    .filter(p => p.username === selectedUser.value && p.allowed)
    .map(p => p.channel)
}

const saveUserPermissions = async () => {
  if (!selectedUser.value) return
  const chMap: Record<string, boolean> = {}
  for (const ch of channels.value) {
    chMap[ch.key] = userPermChannels.value.includes(ch.key)
  }
  try {
    await api.post('/admin/channels/users/permissions', {
      username: selectedUser.value,
      channels: chMap,
    })
    message.success('权限已保存')
    loadUsers()
  } catch {
    message.error('保存失败')
  }
}

const toggleChannel = async (key: string, enabled: boolean) => {
  try {
    await api.post('/admin/channels/toggle', { section: key, enabled })
    message.success(enabled ? '渠道已启用' : '渠道已禁用')
  } catch {
    message.error('操作失败')
    loadChannels()
  }
}

onMounted(() => {
  loadChannels()
  loadUsers()
})
</script>
