<template>
  <div>
    <a-page-header title="文件管理" sub-title="管理你的文件">
      <template #extra>
        <a-button type="primary" @click="showUpload = true">上传文件</a-button>
        <a-button @click="showMkdir = true">新建文件夹</a-button>
      </template>
    </a-page-header>

    <a-breadcrumb style="margin-bottom: 16px">
      <a-breadcrumb-item v-for="(seg, i) in pathSegments" :key="i">
        <a @click="navigateTo(i)">{{ seg || '根目录' }}</a>
      </a-breadcrumb-item>
    </a-breadcrumb>

    <a-spin :spinning="loading">
      <a-table
        :data-source="files"
        :columns="columns"
        :pagination="false"
        row-key="name"
        size="small"
        @row-click="handleRowClick"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <span style="cursor: pointer" @click="handleRowClick(record)">
              <FolderOutlined v-if="record.is_dir" style="color: #faad14; margin-right: 6px" />
              <FileOutlined v-else style="color: #999; margin-right: 6px" />
              {{ record.name }}
            </span>
          </template>
          <template v-if="column.key === 'size'">
            {{ record.is_dir ? '-' : formatSize(record.size) }}
          </template>
          <template v-if="column.key === 'action'">
            <a-space>
              <a-button v-if="!record.is_dir" type="link" size="small" @click.stop="downloadFile(record)">下载</a-button>
              <a-button v-if="!record.is_dir && isTextFile(record.name)" type="link" size="small" @click.stop="editFile(record)">编辑</a-button>
              <a-popconfirm title="确定删除？" @confirm="deleteFile(record)">
                <a-button type="link" size="small" danger @click.stop>删除</a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-spin>

    <a-modal v-model:open="showUpload" title="上传文件" @ok="doUpload" :confirm-loading="uploading">
      <a-upload
        :file-list="uploadFiles"
        :before-upload="beforeUpload"
        @remove="uploadFiles = []"
        :max-count="1"
      >
        <a-button><UploadOutlined /> 选择文件</a-button>
      </a-upload>
      <p style="color: #999; margin-top: 8px">最大 32MB</p>
    </a-modal>

    <a-modal v-model:open="showMkdir" title="新建文件夹" @ok="doMkdir">
      <a-input v-model:value="newDirName" placeholder="文件夹名称" />
    </a-modal>

    <a-modal
      v-model:open="showEditor"
      :title="'编辑: ' + editFileName"
      :width="720"
      @ok="doSaveEdit"
      :confirm-loading="saving"
    >
      <a-textarea v-model:value="editContent" :auto-size="{ minRows: 10, maxRows: 30 }" style="font-family: monospace" />
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { message } from 'ant-design-vue'
import { FolderOutlined, FileOutlined, UploadOutlined } from '@ant-design/icons-vue'
import { api } from '../../composables/api'
import { getCsrf } from '../../composables/useCsrf'

const route = useRoute()
const loading = ref(false)
const currentPath = ref('')
const files = ref<any[]>([])

const pathSegments = computed(() => {
  const parts = currentPath.value.split('/').filter(Boolean)
  return ['根目录', ...parts]
})

const columns = [
  { title: '名称', key: 'name', dataIndex: 'name' },
  { title: '大小', key: 'size', dataIndex: 'size', width: 100 },
  { title: '修改时间', key: 'mod_time', dataIndex: 'mod_time', width: 180 },
  { title: '操作', key: 'action', width: 200 },
]

const formatSize = (bytes: number) => {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

const isTextFile = (name: string) => {
  const exts = ['.txt', '.md', '.json', '.yaml', '.yml', '.js', '.ts', '.vue', '.py', '.sh', '.css', '.html', '.xml', '.csv', '.log', '.conf', '.cfg', '.ini', '.toml', '.go', '.rs', '.java', '.c', '.h', '.cpp', '.hpp']
  return exts.some(e => name.toLowerCase().endsWith(e))
}

const navigateTo = (idx: number) => {
  if (idx === 0) { currentPath.value = '' }
  else {
    const parts = currentPath.value.split('/').filter(Boolean)
    currentPath.value = parts.slice(0, idx).join('/')
  }
}

const loadFiles = async () => {
  loading.value = true
  try {
    const params = currentPath.value ? `?path=${encodeURIComponent(currentPath.value)}` : ''
    const res = await fetch(`/api/files${params}`)
    if (!res.ok) { message.error('加载文件列表失败'); return }
    const data = await res.json()
    files.value = (data.files || data.entries || data || []).map((f: any) => ({
      name: f.name || '',
      size: f.size || 0,
      mod_time: f.mod_time || f.modified || '',
      is_dir: !!f.is_dir,
    }))
  } catch {
    message.error('网络错误')
  } finally {
    loading.value = false
  }
}

const handleRowClick = (record: any) => {
  if (record.is_dir) {
    currentPath.value = currentPath.value ? `${currentPath.value}/${record.name}` : record.name
  }
}

const downloadFile = (record: any) => {
  const path = currentPath.value ? `${currentPath.value}/${record.name}` : record.name
  window.open(`/api/files/download?path=${encodeURIComponent(path)}`, '_blank')
}

const deleteFile = async (record: any) => {
  try {
    const path = currentPath.value ? `${currentPath.value}/${record.name}` : record.name
    await api.post('/files/delete', { path })
    message.success('删除成功')
    loadFiles()
  } catch (e: any) {
    message.error(e.message || '网络错误')
  }
}

const showUpload = ref(false)
const uploading = ref(false)
const uploadFiles = ref<any[]>([])

const beforeUpload = (file: any) => {
  uploadFiles.value = [file]
  return false
}

const doUpload = async () => {
  if (!uploadFiles.value.length) { message.warning('请选择文件'); return }
  uploading.value = true
  try {
    const csrf_token = await getCsrf()
    const fd = new FormData()
    fd.append('file', uploadFiles.value[0])
    fd.append('path', currentPath.value)
    fd.append('csrf_token', csrf_token)
    const res = await fetch('/api/files/upload', { method: 'POST', body: fd })
    if (!res.ok) { const e = await res.json().catch(() => ({})); message.error(e.message || '上传失败'); return }
    message.success('上传成功')
    showUpload.value = false
    uploadFiles.value = []
    loadFiles()
  } catch {
    message.error('网络错误')
  } finally {
    uploading.value = false
  }
}

const showMkdir = ref(false)
const newDirName = ref('')

const doMkdir = async () => {
  if (!newDirName.value.trim()) { message.warning('请输入文件夹名称'); return }
  try {
    await api.post('/files/mkdir', { path: currentPath.value, name: newDirName.value.trim() })
    message.success('创建成功')
    showMkdir.value = false
    newDirName.value = ''
    loadFiles()
  } catch (e: any) {
    message.error(e.message || '网络错误')
  }
}

const showEditor = ref(false)
const editFileName = ref('')
const editContent = ref('')
const editPath = ref('')
const saving = ref(false)

const editFile = async (record: any) => {
  const path = currentPath.value ? `${currentPath.value}/${record.name}` : record.name
  try {
    const res = await fetch(`/api/files/edit?path=${encodeURIComponent(path)}`)
    if (!res.ok) { message.error('读取文件失败'); return }
    const data = await res.json()
    editFileName.value = record.name
    editContent.value = data.content || ''
    editPath.value = path
    showEditor.value = true
  } catch {
    message.error('网络错误')
  }
}

const doSaveEdit = async () => {
  saving.value = true
  try {
    await api.post('/files/edit', { path: editPath.value, content: editContent.value })
    message.success('保存成功')
    showEditor.value = false
  } catch (e: any) {
    message.error(e.message || '网络错误')
  } finally {
    saving.value = false
  }
}

watch(currentPath, () => loadFiles())

onMounted(() => {
  const p = route.query.path as string
  if (p) currentPath.value = p
  loadFiles()
})
</script>
