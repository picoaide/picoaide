<template>
  <div>
    <a-page-header title="超管账户" sub-title="管理超级管理员账户">
      <template #extra>
        <a-button type="primary" @click="showCreate = true">创建超管</a-button>
      </template>
    </a-page-header>

    <a-table
      :columns="columns"
      :data-source="admins"
      :loading="loading"
      :pagination="false"
      row-key="username"
      style="margin-top: 16px"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'action'">
          <a-space>
            <a-button type="link" size="small" @click="handleReset(record.username)">重置密码</a-button>
            <a-popconfirm
              :title="admins.length <= 1 ? '至少保留一个超管，无法删除' : '确定删除该超管？'"
              :disabled="admins.length <= 1"
              @confirm="handleDelete(record.username)"
            >
              <a-button type="link" size="small" danger :disabled="admins.length <= 1">删除</a-button>
            </a-popconfirm>
          </a-space>
        </template>
      </template>
    </a-table>

    <a-modal
      v-model:open="showCreate"
      title="创建超管"
      @ok="handleCreate"
      :confirm-loading="createLoading"
    >
      <a-form layout="vertical">
        <a-form-item label="用户名" required>
          <a-input v-model:value="createForm.username" placeholder="请输入用户名" />
        </a-form-item>
      </a-form>
      <template v-if="createdPassword">
        <a-alert type="success" message="超管创建成功" style="margin-top: 16px" />
        <a-descriptions :column="1" size="small" style="margin-top: 8px">
          <a-descriptions-item label="用户名">{{ createdUsername }}</a-descriptions-item>
          <a-descriptions-item label="密码">
            <span style="font-family: monospace">{{ createdPassword }}</span>
          </a-descriptions-item>
        </a-descriptions>
      </template>
    </a-modal>

    <a-modal
      v-model:open="showReset"
      title="重置密码"
    >
      <template v-if="resetPassword">
        <a-alert type="success" message="密码重置成功" />
        <a-descriptions :column="1" size="small" style="margin-top: 16px">
          <a-descriptions-item label="用户名">{{ resetUsername }}</a-descriptions-item>
          <a-descriptions-item label="新密码">
            <span style="font-family: monospace">{{ resetPassword }}</span>
          </a-descriptions-item>
        </a-descriptions>
      </template>
      <template #footer>
        <a-button @click="showReset = false">关闭</a-button>
      </template>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

const loading = ref(false)
const admins = ref<any[]>([])
const showCreate = ref(false)
const createLoading = ref(false)
const createdPassword = ref('')
const createdUsername = ref('')
const showReset = ref(false)
const resetPassword = ref('')
const resetUsername = ref('')
const createForm = ref({ username: '' })
let closeTimer: ReturnType<typeof setTimeout> | null = null

const columns = [
  { title: '用户名', dataIndex: 'username', key: 'username' },
  { title: '操作', key: 'action', width: 200 },
]

const fetchAdmins = async () => {
  loading.value = true
  try {
    const data = await api.get('/admin/superadmins')
    admins.value = (data.admins || []).map((u: string) => ({ username: u }))
  } catch {
    message.error('获取超管列表失败')
  } finally {
    loading.value = false
  }
}

const handleCreate = async () => {
  if (!createForm.value.username.trim()) {
    message.warning('请输入用户名')
    return
  }
  createLoading.value = true
  createdPassword.value = ''
  try {
    const data = await api.post('/admin/superadmins/create', { username: createForm.value.username.trim() })
    message.success('创建成功')
    createdUsername.value = createForm.value.username.trim()
    createdPassword.value = data.password || ''
    createForm.value.username = ''
    fetchAdmins()
    if (closeTimer) clearTimeout(closeTimer)
    closeTimer = setTimeout(() => {
      showCreate.value = false
      createdPassword.value = ''
    }, 5000)
  } catch {
    message.error('创建失败')
  } finally {
    createLoading.value = false
  }
}

const handleDelete = async (username: string) => {
  try {
    await api.post('/admin/superadmins/delete', { username })
    message.success('删除成功')
    fetchAdmins()
  } catch {
    message.error('删除失败')
  }
}

const handleReset = async (username: string) => {
  try {
    const data = await api.post('/admin/superadmins/reset', { username })
    resetUsername.value = username
    resetPassword.value = data.password || ''
    showReset.value = true
  } catch {
    message.error('重置失败')
  }
}

onMounted(fetchAdmins)
</script>
