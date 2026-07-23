<template>
  <div>
    <a-page-header title="知识库管理" sub-title="管理知识库、文件夹和访问权限" />

    <a-card title="知识库列表" style="margin-bottom: 16px">
      <template #extra>
        <a-space>
          <a-button :disabled="!selectedKB" type="primary" @click="showImport">导入文档</a-button>
          <a-button type="primary" @click="showCreateKB">新建知识库</a-button>
        </a-space>
      </template>
      <a-table :data-source="kbs" :columns="kbColumns" row-key="id" :loading="loading" :custom-row="kbRowClick">
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'actions'">
            <a-space>
              <a-button type="link" @click="editKB(record)">编辑</a-button>
              <a-popconfirm title="确定删除此知识库？所有文档将被删除" @confirm="deleteKB(record.id)">
                <a-button type="link" danger @click="kbModalOpen = false">删除</a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-row v-if="selectedKB" :gutter="16">
      <a-col :span="10">
        <a-card title="文件夹">
          <template #extra>
            <a-button type="primary" size="small" @click="showAddFolder">添加根文件夹</a-button>
          </template>
          <a-spin :spinning="treeLoading">
            <a-tree
              v-if="folderTreeData.length > 0"
              :tree-data="folderTreeData"
              :default-expand-all="true"
              @select="onFolderSelect"
            />
            <a-empty v-else description="暂无文件夹" />
          </a-spin>
        </a-card>
      </a-col>
      <a-col :span="14">
        <a-card v-if="selectedFolder" :title="'文件夹: ' + (selectedFolder.name || '')">
          <template #extra>
            <a-space>
              <a-button size="small" @click="showRenameFolder">重命名</a-button>
              <a-popconfirm title="确定删除此文件夹？" @confirm="deleteFolder">
                <a-button size="small" danger>删除</a-button>
              </a-popconfirm>
              <a-button size="small" @click="showAddSubFolder">添加子文件夹</a-button>
            </a-space>
          </template>
          <a-tabs v-model:activeKey="folderTab">
            <a-tab-pane key="docs" tab="文档">
              <a-table :data-source="documents" :columns="docColumns" row-key="id" :loading="docsLoading" size="small">
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'actions'">
                <a-popconfirm title="确定删除此文档？" @confirm="deleteDocument(record)">
                  <a-button type="link" size="small" danger>删除</a-button>
                </a-popconfirm>
              </template>
            </template>
          </a-table>
            </a-tab-pane>
            <a-tab-pane key="perms" tab="访问权限">
              <a-form layout="inline">
                <a-form-item label="独立权限（不继承父级）">
                  <a-switch v-model:checked="permissionsSet" @change="togglePermissionsSet" />
                </a-form-item>
              </a-form>
              <a-divider />
              <h4>用户授权</h4>
              <a-select mode="multiple" v-model:value="folderUsers" placeholder="选择用户" style="width: 100%" :options="userOptions" />
              <h4 style="margin-top: 12px">用户组授权</h4>
              <a-select mode="multiple" v-model:value="folderGroups" placeholder="选择用户组" style="width: 100%" :options="groupOptions" />
              <a-button type="primary" style="margin-top: 16px" @click="savePermissions">保存权限</a-button>
            </a-tab-pane>
          </a-tabs>
        </a-card>
        <a-card v-else title="文件夹">
          <a-empty description="选择一个文件夹查看文档和权限" />
        </a-card>
      </a-col>
    </a-row>

    <a-modal v-model:open="kbModalOpen" :title="kbModalTitle" @ok="saveKB" :confirm-loading="kbSaving">
      <a-form layout="vertical">
        <a-form-item label="名称">
          <a-input v-model:value="kbForm.name" />
        </a-form-item>
        <a-form-item label="描述">
          <a-textarea v-model:value="kbForm.description" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:open="folderModalOpen" :title="folderModalTitle" @ok="saveFolder" :confirm-loading="folderSaving">
      <a-form layout="vertical">
        <a-form-item label="文件夹名称">
          <a-input v-model:value="folderName" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:open="importModalOpen" title="导入文档" width="560px" :footer="null" :destroy-on-close="true">
      <a-tabs v-model:activeKey="importTab">
        <a-tab-pane key="upload" tab="文件上传">
          <a-upload-dragger
            :before-upload="handleFileSelect"
            :show-upload-list="false"
            :disabled="importing"
            accept=".md,.txt,.html,.pdf,.docx,.zip"
          >
            <a-button type="dashed" style="border:none;width:100%;height:130px">
              <p class="ant-upload-drag-icon">
                <file-outlined />
              </p>
              <p class="ant-upload-text">点击或拖拽文件到此区域</p>
              <p class="ant-upload-hint">支持 .md .txt .html .pdf .docx .zip 格式</p>
            </a-button>
          </a-upload-dragger>
          <div v-if="selectedFile" style="margin-top: 12px">
            <a-alert :message="'已选择: ' + selectedFile.name" type="info" show-icon style="margin-bottom: 12px" />
            <a-checkbox v-model:checked="autoClassify">自动分类（使用 LLM 拆分文档）</a-checkbox>
            <div style="margin-top: 12px">
              <a-button type="primary" :loading="importing" @click="startFileUpload">开始导入</a-button>
              <a-button style="margin-left: 8px" @click="selectedFile = null">重新选择</a-button>
            </div>
          </div>
        </a-tab-pane>
        <a-tab-pane key="url" tab="URL 导入">
          <a-form layout="vertical">
            <a-form-item label="网页 URL">
              <a-input v-model:value="importURL" placeholder="https://example.com/article" />
            </a-form-item>
            <a-form-item>
              <a-button type="primary" :loading="importing" @click="startURLImport">开始导入</a-button>
            </a-form-item>
          </a-form>
        </a-tab-pane>
      </a-tabs>

      <a-divider v-if="importTaskId" />
      <div v-if="importTaskId">
        <h4>导入进度</h4>
        <a-progress :percent="importProgress" :status="importStatus" />
        <p v-if="importError" style="color: red">{{ importError }}</p>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { message } from 'ant-design-vue'
import { FileOutlined } from '@ant-design/icons-vue'
import { api } from '../../composables/api'

const kbs = ref<any[]>([])
const loading = ref(false)
const kbModalOpen = ref(false)
const kbModalTitle = ref('')
const kbSaving = ref(false)
const kbForm = ref({ name: '', description: '' })
const editingKB = ref<any>(null)

const kbColumns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: '描述', dataIndex: 'description', key: 'description' },
  { title: '创建者', dataIndex: 'created_by', key: 'created_by' },
  { title: '创建时间', dataIndex: 'created_at', key: 'created_at' },
  { title: '操作', key: 'actions', width: 150 },
]

const loadKBs = async () => {
  loading.value = true
  const res = await api.get('/admin/knowledge-bases')
  kbs.value = res.data || []
  loading.value = false
}

const showCreateKB = () => {
  editingKB.value = null
  kbForm.value = { name: '', description: '' }
  kbModalTitle.value = '新建知识库'
  kbModalOpen.value = true
}

const editKB = (kb: any) => {
  selectKB(kb)
  editingKB.value = kb
  kbForm.value = { name: kb.name, description: kb.description }
  kbModalTitle.value = '编辑知识库'
  kbModalOpen.value = true
}

const saveKB = async () => {
  kbSaving.value = true
  try {
    if (editingKB.value) {
      await api.put('/admin/knowledge-bases/' + editingKB.value.id, kbForm.value)
      message.success('知识库已更新')
    } else {
      await api.post('/admin/knowledge-bases', kbForm.value)
      message.success('知识库已创建')
    }
    kbModalOpen.value = false
    loadKBs()
  } catch (e: any) {
    message.error(e.message || '操作失败')
  }
  kbSaving.value = false
}

const deleteKB = async (id: number) => {
  try {
    await api.delete('/admin/knowledge-bases/' + id)
    message.success('知识库已删除')
    if (selectedKB.value?.id === id) selectedKB.value = null
    loadKBs()
  } catch (e: any) {
    message.error(e.message || '删除失败')
  }
}

const selectedKB = ref<any>(null)
const folderTreeData = ref<any[]>([])
const treeLoading = ref(false)
const selectedFolder = ref<any>(null)
const folderTab = ref('docs')

const selectKB = async (kb: any) => {
  selectedKB.value = kb
  selectedFolder.value = null
  loadFolderTree()
}

const kbRowClick = (record: any) => ({
  onClick: () => selectKB(record),
})

const loadFolderTree = async () => {
  if (!selectedKB.value) return
  treeLoading.value = true
  const res = await api.get('/admin/knowledge-bases/' + selectedKB.value.id + '/folders')
  const folders = res.data || []
  folderTreeData.value = buildTree(folders)
  treeLoading.value = false
}

const buildTree = (folders: any[], parentId: number | null = null): any[] => {
  return folders
    .filter((f: any) => f.parent_id === parentId)
    .map((f: any) => ({
      key: String(f.id),
      title: f.name,
      id: f.id,
      children: buildTree(folders, f.id),
    }))
}

const permissionsSet = ref(false)
const folderUsers = ref<string[]>([])
const folderGroups = ref<number[]>([])
const allUsers = ref<any[]>([])
const allGroups = ref<any[]>([])

const userOptions = computed(() => allUsers.value.map((u: any) => ({ label: u.username, value: u.username })))
const groupOptions = computed(() => allGroups.value.map((g: any) => ({ label: g.name, value: g.id })))

const onFolderSelect = async (keys: any[], node?: any) => {
  if (keys.length === 0) { selectedFolder.value = null; documents.value = []; return }
  const folderId = parseInt(keys[0])
  const name = node?.node?.title || ''
  selectedFolder.value = { id: folderId, name }
  folderTab.value = 'docs'
  await Promise.all([
    loadFolderPermissions(folderId),
    loadDocuments(folderId),
  ])
}

const documents = ref<any[]>([])
const docsLoading = ref(false)
const docColumns = [
  { title: '标题', dataIndex: 'title', key: 'title', ellipsis: true },
  { title: '类型', dataIndex: 'file_type', key: 'file_type', width: 80 },
  { title: '大小', dataIndex: 'file_size', key: 'file_size', width: 100 },
  { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180 },
  { title: '操作', key: 'actions', width: 80 },
]

const loadDocuments = async (folderId: number) => {
  docsLoading.value = true
  try {
    const res = await api.get('/admin/knowledge-bases/folders/' + folderId + '/documents')
    documents.value = res.data || []
  } catch { documents.value = [] }
  docsLoading.value = false
}

const deleteDocument = async (record: any) => {
  try {
    await api.delete('/admin/knowledge-bases/documents/' + record.id)
    message.success('文档已删除')
    if (selectedFolder.value) loadDocuments(selectedFolder.value.id)
  } catch (e: any) {
    message.error(e.message || '删除失败')
  }
}

const loadFolderPermissions = async (folderId: number) => {
  const res = await api.get('/admin/knowledge-bases/folders/' + folderId + '/permissions')
  const data = res.data || {}
  permissionsSet.value = data.permissions_set === 1
  folderUsers.value = data.users || []
  folderGroups.value = data.groups || []
}

const togglePermissionsSet = async (val: boolean) => {
  if (!selectedFolder.value) return
  try {
    await api.put('/admin/knowledge-bases/folders/' + selectedFolder.value.id + '/permissions', {
      permissions_set: val ? 1 : 0,
    })
    message.success('权限模式已更新')
  } catch (e: any) {
    message.error(e.message || '更新失败')
  }
}

const savePermissions = async () => {
  if (!selectedFolder.value) return
  try {
    await api.put('/admin/knowledge-bases/folders/' + selectedFolder.value.id + '/permissions', {
      users: folderUsers.value,
      groups: folderGroups.value,
    })
    message.success('权限已保存')
  } catch (e: any) {
    message.error(e.message || '保存失败')
  }
}

const folderModalOpen = ref(false)
const folderModalTitle = ref('')
const folderName = ref('')
const folderSaving = ref(false)
const addingSubFolder = ref(false)

const showAddFolder = () => {
  folderModalTitle.value = '添加根文件夹'
  folderName.value = ''
  addingSubFolder.value = false
  folderModalOpen.value = true
}

const showAddSubFolder = () => {
  folderModalTitle.value = '添加子文件夹'
  folderName.value = ''
  addingSubFolder.value = true
  folderModalOpen.value = true
}

const showRenameFolder = () => {
  folderModalTitle.value = '重命名文件夹'
  folderName.value = selectedFolder.value?.name || ''
  folderModalOpen.value = true
}

const saveFolder = async () => {
  if (!folderName.value.trim()) { message.warning('请输入名称'); return }
  folderSaving.value = true
  try {
    const routeName = folderModalTitle.value
    if (routeName === '重命名文件夹') {
      await api.put('/admin/knowledge-bases/folders/' + selectedFolder.value.id, { name: folderName.value })
    } else if (addingSubFolder.value) {
      await api.post('/admin/knowledge-bases/' + selectedKB.value.id + '/folders', {
        parent_id: selectedFolder.value?.id,
        name: folderName.value,
      })
    } else {
      await api.post('/admin/knowledge-bases/' + selectedKB.value.id + '/folders', {
        name: folderName.value,
      })
    }
    message.success('操作成功')
    folderModalOpen.value = false
    loadFolderTree()
  } catch (e: any) {
    message.error(e.message || '操作失败')
  }
  folderSaving.value = false
}

const deleteFolder = async () => {
  if (!selectedFolder.value) return
  try {
    await api.delete('/admin/knowledge-bases/folders/' + selectedFolder.value.id)
    message.success('文件夹已删除')
    selectedFolder.value = null
    loadFolderTree()
  } catch (e: any) {
    message.error(e.message || '删除失败')
  }
}

const importModalOpen = ref(false)
const importTab = ref('upload')
const selectedFile = ref<File | null>(null)
const importURL = ref('')
const autoClassify = ref(false)
const importing = ref(false)
const importTaskId = ref('')
const importProgress = ref(0)
const importStatus = ref<'active' | 'success' | 'exception'>('active')
const importError = ref('')
let progressTimer: ReturnType<typeof setInterval> | null = null

const showImport = () => {
  selectedFile.value = null
  importURL.value = ''
  autoClassify.value = false
  importTaskId.value = ''
  importProgress.value = 0
  importStatus.value = 'active'
  importError.value = ''
  importModalOpen.value = true
}

const handleFileSelect = (file: File) => {
  selectedFile.value = file
  return false
}

const startFileUpload = async () => {
  if (!selectedFile.value || !selectedKB.value) return
  importing.value = true
  importTaskId.value = ''
  importProgress.value = 0
  importStatus.value = 'active'
  importError.value = ''
  try {
    const formData = new FormData()
    formData.append('file', selectedFile.value)
    if (autoClassify.value) formData.append('auto_classify', 'true')
    const res = await api.postForm('/admin/knowledge-bases/' + selectedKB.value.id + '/import/upload', formData)
    importTaskId.value = res.task_id
    importProgress.value = 10
    pollProgress(res.task_id)
  } catch (e: any) {
    message.error(e.message || '上传失败')
    importing.value = false
  }
}

const startURLImport = async () => {
  if (!importURL.value.trim() || !selectedKB.value) return
  importing.value = true
  importTaskId.value = ''
  importProgress.value = 0
  importStatus.value = 'active'
  importError.value = ''
  try {
    const res = await api.post('/admin/knowledge-bases/' + selectedKB.value.id + '/import/web', {
      url: importURL.value.trim(),
    })
    importTaskId.value = res.task_id
    importProgress.value = 10
    pollProgress(res.task_id)
  } catch (e: any) {
    message.error(e.message || '导入失败')
    importing.value = false
  }
}

const pollProgress = (taskId: string) => {
  progressTimer = setInterval(async () => {
    try {
      const res = await api.get('/admin/knowledge-bases/imports/' + taskId)
      const task = res.data
      if (task.status === 'ready') {
        importProgress.value = 100
        importStatus.value = 'success'
        importing.value = false
        clearTimer()
        message.success('导入完成')
        importModalOpen.value = false
        if (selectedFolder.value) loadDocuments(selectedFolder.value.id)
        else loadFolderTree()
      } else if (task.status === 'error') {
        importProgress.value = 100
        importStatus.value = 'exception'
        importError.value = task.error_msg || '导入失败'
        importing.value = false
        clearTimer()
      } else {
        importProgress.value = task.progress || 50
      }
    } catch {
      clearTimer()
      importing.value = false
    }
  }, 1500)
}

const clearTimer = () => {
  if (progressTimer) {
    clearInterval(progressTimer)
    progressTimer = null
  }
}

onMounted(async () => {
  await loadKBs()
  try {
    const usersRes = await api.get('/admin/users')
    allUsers.value = usersRes.users || []
  } catch {}
  try {
    const groupsRes = await api.get('/admin/groups')
    allGroups.value = groupsRes.groups || []
  } catch {}
})

onUnmounted(clearTimer)
</script>
