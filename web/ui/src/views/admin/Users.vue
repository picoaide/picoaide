<template>
  <div>
    <a-page-header title="用户管理" sub-title="管理系统用户">
      <template #extra>
        <a-space>
          <a-button @click="showBatchCreate = true">批量创建</a-button>
          <a-button type="primary" @click="showCreate = true">创建用户</a-button>
        </a-space>
      </template>
    </a-page-header>

    <div style="margin-bottom: 16px">
      <a-input-search
        v-model:value="search"
        placeholder="搜索用户名"
        style="width: 300px"
        @search="handleSearch"
        allow-clear
      />
    </div>

    <a-table
      :columns="columns"
      :data-source="users"
      :loading="loading"
      :pagination="pagination"
      @change="handleTableChange"
      row-key="username"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'role'">
          <a-tag :color="record.role === 'superadmin' ? 'red' : 'blue'">
            {{ record.role === 'superadmin' ? '超管' : '用户' }}
          </a-tag>
        </template>
        <template v-if="column.key === 'groups'">
          <template v-if="record.groups && record.groups.length">
            <a-tag v-for="g in record.groups" :key="g">{{ g }}</a-tag>
          </template>
          <span v-else style="color: #999">无</span>
        </template>
        <template v-if="column.key === 'action'">
          <a-popconfirm title="确定删除该用户？" @confirm="handleDelete(record.username)">
            <a-button type="link" size="small" danger>删除</a-button>
          </a-popconfirm>
        </template>
      </template>
    </a-table>

    <a-modal
      v-model:open="showCreate"
      title="创建用户"
      @ok="handleCreate"
      :confirm-loading="createLoading"
    >
      <a-form layout="vertical">
        <a-form-item label="用户名" required>
          <a-input v-model:value="createForm.username" placeholder="请输入用户名" />
        </a-form-item>
      </a-form>
      <template v-if="createdPassword">
        <a-alert type="success" message="用户创建成功" style="margin-top: 16px" />
        <a-descriptions :column="1" size="small" style="margin-top: 8px">
          <a-descriptions-item label="用户名">{{ createdUsername }}</a-descriptions-item>
          <a-descriptions-item label="密码">
            <span style="font-family: monospace">{{ createdPassword }}</span>
          </a-descriptions-item>
        </a-descriptions>
      </template>
    </a-modal>

    <a-modal
      v-model:open="showBatchCreate"
      title="批量创建用户"
      @ok="handleBatchCreate"
      :confirm-loading="batchLoading"
    >
      <a-form layout="vertical">
        <a-form-item label="用户名列表" required>
          <a-textarea
            v-model:value="batchUsernames"
            placeholder="每行一个用户名"
            :rows="6"
          />
        </a-form-item>
      </a-form>
      <template v-if="batchResult">
        <a-alert type="info" message="批量创建完成" style="margin-top: 16px" />
        <pre style="margin-top: 8px; font-size: 12px; max-height: 200px; overflow: auto">{{ batchResult }}</pre>
      </template>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'
import { usePagination } from '../../composables/usePagination'

const { pagination } = usePagination()
const loading = ref(false)
const users = ref<any[]>([])
const search = ref('')
const showCreate = ref(false)
const showBatchCreate = ref(false)
const createLoading = ref(false)
const batchLoading = ref(false)
const createdPassword = ref('')
const createdUsername = ref('')
const batchUsernames = ref('')
const batchResult = ref('')
const createForm = ref({ username: '' })

const columns = [
  { title: '用户名', dataIndex: 'username', key: 'username' },
  { title: '角色', dataIndex: 'role', key: 'role' },
  { title: '来源', dataIndex: 'source', key: 'source' },
  { title: 'IP', dataIndex: 'ip', key: 'ip' },
  { title: '所属组', key: 'groups' },
  { title: '操作', key: 'action', width: 100 },
]

const fetchUsers = async () => {
  loading.value = true
  try {
    const data = await api.get('/admin/users', { page: String(pagination.current), page_size: String(pagination.pageSize), search: search.value })
    users.value = data.users || []
    pagination.total = data.total || 0
  } catch {
    message.error('获取用户列表失败')
  } finally {
    loading.value = false
  }
}

const handleTableChange = (pag: any) => {
  pagination.current = pag.current
  pagination.pageSize = pag.pageSize
  fetchUsers()
}

const handleSearch = () => {
  pagination.current = 1
  fetchUsers()
}

const handleCreate = async () => {
  if (!createForm.value.username.trim()) {
    message.warning('请输入用户名')
    return
  }
  createLoading.value = true
  createdPassword.value = ''
  try {
    const data = await api.post('/admin/users/create', { username: createForm.value.username.trim() })
    message.success('创建成功')
    createdUsername.value = createForm.value.username.trim()
    createdPassword.value = data.password || ''
    createForm.value.username = ''
    fetchUsers()
  } catch {
    message.error('创建失败')
  } finally {
    createLoading.value = false
  }
}

const handleBatchCreate = async () => {
  const names = batchUsernames.value.split('\n').map(s => s.trim()).filter(Boolean)
  if (!names.length) {
    message.warning('请输入用户名')
    return
  }
  batchLoading.value = true
  batchResult.value = ''
  try {
    const data = await api.post('/admin/users/batch-create', { usernames: names.join('\n') })
    batchResult.value = data.result || JSON.stringify(data, null, 2)
    fetchUsers()
  } catch {
    message.error('批量创建失败')
  } finally {
    batchLoading.value = false
  }
}

const handleDelete = async (username: string) => {
  try {
    await api.post('/admin/users/delete', { username })
    message.success('删除成功')
    fetchUsers()
  } catch {
    message.error('删除失败')
  }
}

onMounted(fetchUsers)
</script>
