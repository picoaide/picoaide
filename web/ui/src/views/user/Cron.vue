<template>
  <div>
    <a-page-header title="定时任务" sub-title="管理定时执行的任务">
      <template #extra>
        <a-button type="primary" @click="showCreate = true">创建任务</a-button>
      </template>
    </a-page-header>
    <a-table
      :columns="columns"
      :data-source="jobs"
      :loading="loading"
      row-key="id"
      style="margin-top: 16px"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'enabled'">
          <a-switch
            :checked="record.enabled"
            @change="handleToggle(record)"
            checked-children="启用"
            un-checked-children="禁用"
          />
        </template>
        <template v-if="column.key === 'action'">
          <a-space>
            <a-button type="link" size="small" @click="handleEdit(record)">编辑</a-button>
            <a-popconfirm title="确定删除该任务？" @confirm="handleDelete(record.id)">
              <a-button type="link" size="small" danger>删除</a-button>
            </a-popconfirm>
          </a-space>
        </template>
      </template>
    </a-table>

    <a-modal
      v-model:open="showCreate"
      title="创建定时任务"
      @ok="handleCreate"
      :confirm-loading="submitting"
    >
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 16 }">
        <a-form-item label="调度表达式" required>
          <a-input v-model:value="form.schedule" placeholder="如: cron 0 9 * * * 或 every 3600000" />
        </a-form-item>
        <a-form-item label="任务提示" required>
          <a-textarea v-model:value="form.prompt" :rows="4" placeholder="任务执行时的提示内容" />
        </a-form-item>
        <a-form-item label="渠道 ID">
          <a-input v-model:value="form.channel_id" placeholder="可选" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal
      v-model:open="showEdit"
      title="编辑定时任务"
      @ok="handleUpdate"
      :confirm-loading="submitting"
    >
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 16 }">
        <a-form-item label="调度表达式" required>
          <a-input v-model:value="editForm.schedule" placeholder="如: cron 0 9 * * * 或 every 3600000" />
        </a-form-item>
        <a-form-item label="任务提示" required>
          <a-textarea v-model:value="editForm.prompt" :rows="4" placeholder="任务执行时的提示内容" />
        </a-form-item>
        <a-form-item label="渠道 ID">
          <a-input v-model:value="editForm.channel_id" placeholder="可选" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

const loading = ref(false)
const submitting = ref(false)
const jobs = ref<any[]>([])
const showCreate = ref(false)
const showEdit = ref(false)
const form = ref({ schedule: '', prompt: '', channel_id: '' })
const editForm = ref({ id: 0, schedule: '', prompt: '', channel_id: '' })

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 80 },
  { title: '调度表达式', dataIndex: 'schedule', key: 'schedule' },
  { title: '任务提示', dataIndex: 'prompt', key: 'prompt', ellipsis: true },
  { title: '渠道', dataIndex: 'channel_id', key: 'channel_id', width: 120 },
  { title: '状态', key: 'enabled', width: 100 },
  { title: '下次执行', dataIndex: 'next_run_at', key: 'next_run_at', width: 180 },
  { title: '操作', key: 'action', width: 150 },
]

const fetchJobs = async () => {
  loading.value = true
  try {
    const data = await api.get('/cron')
    jobs.value = data.jobs || []
  } catch {
    message.error('获取任务列表失败')
  } finally {
    loading.value = false
  }
}

const handleCreate = async () => {
  if (!form.value.schedule || !form.value.prompt) {
    message.error('调度表达式和任务提示不能为空')
    return
  }
  submitting.value = true
  try {
    const data = await api.post('/cron/create', { schedule: form.value.schedule, prompt: form.value.prompt, channel_id: form.value.channel_id })
    if (data.success) {
      message.success('创建成功')
      showCreate.value = false
      form.value = { schedule: '', prompt: '', channel_id: '' }
      fetchJobs()
    } else {
      message.error(data.message || '创建失败')
    }
  } catch {
    message.error('创建失败')
  } finally {
    submitting.value = false
  }
}

const handleEdit = (record: any) => {
  editForm.value = {
    id: record.id,
    schedule: record.schedule,
    prompt: record.prompt,
    channel_id: record.channel_id || '',
  }
  showEdit.value = true
}

const handleUpdate = async () => {
  if (!editForm.value.schedule || !editForm.value.prompt) {
    message.error('调度表达式和任务提示不能为空')
    return
  }
  submitting.value = true
  try {
    const data = await api.post('/cron/update', { id: editForm.value.id, schedule: editForm.value.schedule, prompt: editForm.value.prompt, channel_id: editForm.value.channel_id })
    if (data.success) {
      message.success('更新成功')
      showEdit.value = false
      fetchJobs()
    } else {
      message.error(data.message || '更新失败')
    }
  } catch {
    message.error('更新失败')
  } finally {
    submitting.value = false
  }
}

const handleDelete = async (id: number) => {
  try {
    const data = await api.post('/cron/delete', { id })
    if (data.success) {
      message.success('删除成功')
      fetchJobs()
    } else {
      message.error(data.message || '删除失败')
    }
  } catch {
    message.error('删除失败')
  }
}

const handleToggle = async (record: any) => {
  try {
    const data = await api.post('/cron/toggle', { id: record.id })
    if (data.success) {
      message.success(data.enabled ? '已启用' : '已禁用')
      fetchJobs()
    } else {
      message.error(data.message || '操作失败')
    }
  } catch {
    message.error('操作失败')
  }
}

onMounted(fetchJobs)
</script>
