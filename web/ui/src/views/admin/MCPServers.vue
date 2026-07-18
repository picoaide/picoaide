<template>
  <div>
    <a-page-header title="MCP 服务" sub-title="管理 MCP 服务器">
      <template #extra>
        <a-space>
          <a-button @click="handleReload" :loading="reloading">重新加载</a-button>
          <a-button type="primary" @click="openCreate">创建服务</a-button>
        </a-space>
      </template>
    </a-page-header>

    <a-table
      :columns="columns"
      :data-source="servers"
      :loading="loading"
      :pagination="false"
      row-key="id"
      style="margin-top: 16px"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'enabled'">
          <a-tag :color="record.enabled ? 'green' : 'default'">{{ record.enabled ? '启用' : '禁用' }}</a-tag>
        </template>
        <template v-if="column.key === 'connection'">
          {{ record.transport === 'stdio' ? record.command : record.url }}
        </template>
        <template v-if="column.key === 'action'">
          <a-space>
            <a-button type="link" size="small" @click="openEdit(record)">编辑</a-button>
            <a-button type="link" size="small" @click="openTools(record)">工具</a-button>
            <a-button type="link" size="small" @click="openGrants(record)">授权</a-button>
            <a-button type="link" size="small" danger @click="handleDelete(record)">删除</a-button>
          </a-space>
        </template>
      </template>
    </a-table>

    <a-modal
      v-model:open="showForm"
      :title="editingId ? '编辑服务' : '创建服务'"
      @ok="handleSave"
      :confirm-loading="saveLoading"
      width="600px"
    >
      <a-form layout="vertical">
        <a-form-item label="名称">
          <a-input v-model:value="form.name" />
        </a-form-item>
        <a-form-item label="传输方式">
          <a-select v-model:value="form.transport">
            <a-select-option value="stdio">stdio</a-select-option>
            <a-select-option value="http">http</a-select-option>
            <a-select-option value="sse">sse</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item v-if="form.transport === 'stdio'" label="命令">
          <a-input v-model:value="form.command" placeholder="如: npx" />
        </a-form-item>
        <a-form-item v-if="form.transport === 'stdio'" label="参数 (JSON 数组)">
          <a-textarea v-model:value="form.args" :rows="2" placeholder='["@modelcontextprotocol/server-xxx"]' />
        </a-form-item>
        <a-form-item v-if="form.transport !== 'stdio'" label="URL">
          <a-input v-model:value="form.url" placeholder="http://..." />
        </a-form-item>
        <a-form-item label="环境变量 (JSON)">
          <a-textarea v-model:value="form.env" :rows="2" placeholder='{"KEY": "value"}' />
        </a-form-item>
        <a-form-item v-if="form.transport !== 'stdio'" label="请求头 (JSON)">
          <a-textarea v-model:value="form.headers" :rows="2" placeholder='{"Authorization": "Bearer ..."}' />
        </a-form-item>
        <a-form-item label="启用">
          <a-switch v-model:checked="form.enabled" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-drawer
      v-model:open="showGrants"
      :title="`授权管理 - ${grantServerName}`"
      width="500"
    >
      <a-row justify="end" style="margin-bottom: 16px">
        <a-button type="primary" size="small" @click="showAddGrant = true">添加授权</a-button>
      </a-row>
      <a-table
        :columns="grantColumns"
        :data-source="grants"
        :loading="grantsLoading"
        :pagination="false"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'action'">
            <a-button type="link" size="small" danger @click="handleRemoveGrant(record.id)">移除</a-button>
          </template>
        </template>
      </a-table>
    </a-drawer>

    <a-modal v-model:open="showAddGrant" title="添加授权" @ok="handleAddGrant" :confirm-loading="addGrantLoading">
      <a-form layout="vertical">
        <a-form-item label="授权类型">
          <a-select v-model:value="grantForm.grant_type">
            <a-select-option value="user">指定用户</a-select-option>
            <a-select-option value="*">所有用户</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item v-if="grantForm.grant_type === 'user'" label="用户名">
          <a-input v-model:value="grantForm.grant_value" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:open="showTools" title="工具列表" :footer="null" width="600px">
      <a-table
        :columns="toolColumns"
        :data-source="tools"
        :loading="toolsLoading"
        :pagination="false"
        row-key="name"
        size="small"
      />
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { message, Modal } from 'ant-design-vue'
import { api } from '../../composables/api'

const servers = ref<any[]>([])
const loading = ref(false)
const reloading = ref(false)

const columns = [
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: '传输方式', dataIndex: 'transport', key: 'transport' },
  { title: '命令/URL', key: 'connection' },
  { title: '状态', key: 'enabled', width: 80 },
  { title: '工具数', dataIndex: 'tool_count', key: 'tool_count', width: 80 },
  { title: '操作', key: 'action', width: 240 },
]

const fetchServers = async () => {
  loading.value = true
  try {
    const data = await api.get('/admin/mcp/servers')
    servers.value = data.servers || []
  } catch {
    message.error('获取服务列表失败')
  } finally {
    loading.value = false
  }
}

const handleReload = async () => {
  reloading.value = true
  try {
    await api.post('/admin/mcp/servers/reload')
    message.success('重新加载成功')
    fetchServers()
  } catch {
    message.error('重新加载失败')
  } finally {
    reloading.value = false
  }
}

const showForm = ref(false)
const editingId = ref<number | null>(null)
const saveLoading = ref(false)
const form = reactive({
  name: '',
  transport: 'stdio' as string,
  command: '',
  args: '',
  url: '',
  env: '',
  headers: '',
  enabled: true,
})

const openCreate = () => {
  editingId.value = null
  Object.assign(form, { name: '', transport: 'stdio', command: '', args: '', url: '', env: '', headers: '', enabled: true })
  showForm.value = true
}

const openEdit = (record: any) => {
  editingId.value = record.id
  Object.assign(form, {
    name: record.name || '',
    transport: record.transport || 'stdio',
    command: record.command || '',
    args: Array.isArray(record.args) ? JSON.stringify(record.args) : (record.args || ''),
    url: record.url || '',
    env: typeof record.env === 'object' ? JSON.stringify(record.env) : (record.env || ''),
    headers: typeof record.headers === 'object' ? JSON.stringify(record.headers) : (record.headers || ''),
    enabled: !!record.enabled,
  })
  showForm.value = true
}

const handleSave = async () => {
  if (!form.name) {
    message.warning('请填写名称')
    return
  }
  saveLoading.value = true
  try {
    const params: Record<string, string> = { name: form.name, transport: form.transport }
    if (form.command) params.command = form.command
    if (form.args) params.args = form.args
    if (form.url) params.url = form.url
    if (form.env) params.env = form.env
    if (form.headers) params.headers = form.headers
    params.enabled = String(form.enabled)

    const path = editingId.value ? `/admin/mcp/servers/update/${editingId.value}` : '/admin/mcp/servers/create'
    await api.post(path, params)
    message.success(editingId.value ? '更新成功' : '创建成功')
    showForm.value = false
    fetchServers()
  } catch {
    message.error('操作失败')
  } finally {
    saveLoading.value = false
  }
}

const handleDelete = (record: any) => {
  Modal.confirm({
    title: '确认删除',
    content: `确定要删除服务「${record.name}」吗？`,
    okText: '确认',
    cancelText: '取消',
    onOk: async () => {
      try {
        await api.post(`/admin/mcp/servers/delete/${record.id}`)
        message.success('删除成功')
        fetchServers()
      } catch {
        message.error('删除失败')
      }
    },
  })
}

const showGrants = ref(false)
const grantServerId = ref(0)
const grantServerName = ref('')
const grants = ref<any[]>([])
const grantsLoading = ref(false)

const grantColumns = [
  { title: '类型', dataIndex: 'grant_type', key: 'grant_type' },
  { title: '值', dataIndex: 'grant_value', key: 'grant_value' },
  { title: '操作', key: 'action', width: 80 },
]

const openGrants = async (record: any) => {
  grantServerId.value = record.id
  grantServerName.value = record.name
  showGrants.value = true
  fetchGrants()
}

const fetchGrants = async () => {
  grantsLoading.value = true
  try {
    const data = await api.get('/admin/mcp/servers/grants', { server_id: String(grantServerId.value) })
    grants.value = data.grants || []
  } catch {
    message.error('获取授权列表失败')
  } finally {
    grantsLoading.value = false
  }
}

const showAddGrant = ref(false)
const addGrantLoading = ref(false)
const grantForm = reactive({ grant_type: 'user', grant_value: '' })

const handleAddGrant = async () => {
  addGrantLoading.value = true
  try {
    await api.post('/admin/mcp/servers/grants/add', {
      server_id: String(grantServerId.value),
      grant_type: grantForm.grant_type,
      grant_value: grantForm.grant_type === '*' ? '*' : grantForm.grant_value,
    })
    message.success('添加成功')
    showAddGrant.value = false
    grantForm.grant_value = ''
    fetchGrants()
  } catch {
    message.error('添加失败')
  } finally {
    addGrantLoading.value = false
  }
}

const handleRemoveGrant = async (id: number) => {
  try {
    await api.post(`/admin/mcp/servers/grants/remove/${id}`)
    message.success('移除成功')
    fetchGrants()
  } catch {
    message.error('移除失败')
  }
}

const showTools = ref(false)
const tools = ref<any[]>([])
const toolsLoading = ref(false)

const toolColumns = [
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: '描述', dataIndex: 'description', key: 'description' },
]

const openTools = async (record: any) => {
  showTools.value = true
  toolsLoading.value = true
  try {
    const data = await api.get('/admin/mcp/servers/tools', { name: record.name })
    tools.value = data.tools || []
  } catch {
    message.error('获取工具列表失败')
  } finally {
    toolsLoading.value = false
  }
}

onMounted(fetchServers)
</script>
