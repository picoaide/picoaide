<template>
  <div>
    <a-page-header title="团队空间" sub-title="管理共享文件夹">
      <template #extra>
        <a-button type="primary" @click="showCreate = true">创建文件夹</a-button>
      </template>
    </a-page-header>

    <a-row :gutter="16" style="margin-top: 24px">
      <a-col :span="8" v-for="folder in folders" :key="folder.id">
        <a-card :title="folder.name" style="margin-bottom: 16px">
          <template #extra>
            <a-space>
              <a-tag v-if="folder.is_public" color="green">公开</a-tag>
              <a-tag v-else>私有</a-tag>
              <a-dropdown>
                <a-button type="text" size="small"><EllipsisOutlined /></a-button>
                <template #overlay>
                  <a-menu>
                    <a-menu-item @click="handleEdit(folder)">编辑</a-menu-item>
                    <a-menu-item @click="handleSetGroups(folder)">设置可见范围</a-menu-item>
                    <a-menu-item @click="handleTest(folder)">测试挂载</a-menu-item>
                    <a-menu-item @click="handleMount(folder)">手动挂载</a-menu-item>
                    <a-menu-item danger @click="handleDelete(folder)">删除</a-menu-item>
                  </a-menu>
                </template>
              </a-dropdown>
            </a-space>
          </template>
          <p style="color: #666; margin-bottom: 8px">{{ folder.description || '暂无描述' }}</p>
          <a-tag>{{ folder.member_count || 0 }} 成员</a-tag>
        </a-card>
      </a-col>
    </a-row>

    <a-empty v-if="folders.length === 0 && !loading" description="暂无共享文件夹" style="margin-top: 48px" />

    <a-modal v-model:open="showCreate" title="创建共享文件夹" @ok="handleCreate" @cancel="form.name = ''; form.description = ''; form.is_public = false" :confirmLoading="creating">
      <a-form layout="vertical">
        <a-form-item label="名称" required>
          <a-input v-model:value="form.name" placeholder="文件夹名称" />
        </a-form-item>
        <a-form-item label="描述">
          <a-textarea v-model:value="form.description" placeholder="描述" :rows="2" />
        </a-form-item>
        <a-form-item label="公开">
          <a-switch v-model:checked="form.is_public" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:open="showEdit" title="编辑共享文件夹" @ok="submitEdit" @cancel="editForm.name = ''; editForm.description = ''; editForm.is_public = false" :confirmLoading="creating">
      <a-form layout="vertical">
        <a-form-item label="名称" required>
          <a-input v-model:value="editForm.name" />
        </a-form-item>
        <a-form-item label="描述">
          <a-textarea v-model:value="editForm.description" :rows="2" />
        </a-form-item>
        <a-form-item label="公开">
          <a-switch v-model:checked="editForm.is_public" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:open="showTestModal" title="测试挂载" @ok="doTest" :confirmLoading="testLoading">
      <a-form layout="vertical">
        <a-form-item label="用户名（留空则测试所有用户）">
          <a-input v-model:value="testUsername" placeholder="输入用户名" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:open="showGroups" title="设置可见范围" @ok="submitGroups" :confirmLoading="creating">
      <a-form layout="vertical">
        <a-form-item label="可见用户组">
          <a-select v-model:value="selectedGroupIds" mode="multiple" placeholder="选择用户组">
            <a-select-option v-for="g in allGroups" :key="g.id" :value="String(g.id)">{{ g.name }}</a-select-option>
          </a-select>
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { message, Modal } from 'ant-design-vue'
import { EllipsisOutlined } from '@ant-design/icons-vue'
import { api } from '../../composables/api'

const loading = ref(false)
const creating = ref(false)
const folders = ref<any[]>([])
const allGroups = ref<any[]>([])
const showCreate = ref(false)
const showEdit = ref(false)
const showGroups = ref(false)
const showTestModal = ref(false)
const testLoading = ref(false)
const testUsername = ref('')
const testFolder = ref<any>(null)
const currentFolder = ref<any>(null)
const selectedGroupIds = ref<string[]>([])

const form = reactive({ name: '', description: '', is_public: false })
const editForm = reactive({ name: '', description: '', is_public: false })

const fetchFolders = async () => {
  loading.value = true
  try {
    const data = await api.get('/admin/shared-folders')
    folders.value = data.folders || []
  } catch { message.error('获取共享文件夹失败') }
  finally { loading.value = false }
}

const fetchGroups = async () => {
  try {
    const data = await api.get('/admin/groups')
    allGroups.value = data.groups || []
  } catch { /* ignore */ }
}

const handleCreate = async () => {
  if (!form.name) { message.warning('请输入名称'); return }
  creating.value = true
  try {
    const data = await api.post('/admin/shared-folders/create', { name: form.name, description: form.description, is_public: form.is_public })
    if (data.success) { message.success('创建成功'); showCreate.value = false; form.name = ''; form.description = ''; form.is_public = false; fetchFolders() }
    else message.error(data.error || '创建失败')
  } catch { message.error('创建失败') }
  finally { creating.value = false }
}

const handleEdit = (folder: any) => {
  currentFolder.value = folder
  editForm.name = folder.name
  editForm.description = folder.description || ''
  editForm.is_public = folder.is_public
  showGroups.value = false
  showEdit.value = true
}

const submitEdit = async () => {
  if (!editForm.name) { message.warning('请输入名称'); return }
  creating.value = true
  try {
    const data = await api.post('/admin/shared-folders/update', { id: currentFolder.value.id, name: editForm.name, description: editForm.description, is_public: editForm.is_public })
    if (data.success) { message.success('更新成功'); showEdit.value = false; fetchFolders() }
    else message.error(data.error || '更新失败')
  } catch { message.error('更新失败') }
  finally { creating.value = false }
}

const handleSetGroups = async (folder: any) => {
  currentFolder.value = folder
  await fetchGroups()
  selectedGroupIds.value = []
  showEdit.value = false
  showGroups.value = true
}

const submitGroups = async () => {
  creating.value = true
  try {
    const data = await api.post('/admin/shared-folders/groups/set', { folder_id: currentFolder.value.id, group_ids: selectedGroupIds.value.map(Number) })
    if (data.success) { message.success('设置成功'); showGroups.value = false; fetchFolders() }
    else message.error(data.error || '设置失败')
  } catch { message.error('设置失败') }
  finally { creating.value = false }
}

const handleTest = (folder: any) => {
  testFolder.value = folder
  testUsername.value = ''
  showTestModal.value = true
}

const doTest = async () => {
  testLoading.value = true
  try {
    const data = await api.post('/admin/shared-folders/test', { folder_id: testFolder.value.id, username: testUsername.value || undefined })
    if (data.mounted) message.success('挂载正常')
    else message.warning(data.message || '挂载异常')
    showTestModal.value = false
  } catch { message.error('测试失败') }
  finally { testLoading.value = false }
}

const handleMount = async (folder: any) => {
  try {
    const data = await api.post('/admin/shared-folders/mount', { folder_id: folder.id })
    if (data.success) message.success('挂载完成')
    else message.error(data.error || '挂载失败')
  } catch { message.error('挂载失败') }
}

const handleDelete = (folder: any) => {
  Modal.confirm({
    title: '确定删除此共享文件夹？',
    content: `操作将删除"${folder.name}"，是否继续？`,
    onOk: async () => {
      try {
        const data = await api.post('/admin/shared-folders/delete', { id: folder.id })
        if (data.success) { message.success('删除成功'); fetchFolders() }
        else message.error(data.error || '删除失败')
      } catch { message.error('删除失败') }
    },
  })
}

onMounted(() => { fetchFolders(); fetchGroups() })
</script>
