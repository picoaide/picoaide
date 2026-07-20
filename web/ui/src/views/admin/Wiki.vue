<template>
  <div>
    <a-page-header title="知识库管理" sub-title="管理知识库、文件夹和访问权限" />

    <a-card title="知识库列表" style="margin-bottom: 16px">
      <template #extra>
        <a-button type="primary" @click="showCreateKB">新建知识库</a-button>
      </template>
      <a-table :data-source="kbs" :columns="kbColumns" row-key="id" :loading="loading" @row-click="selectKB">
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'actions'">
            <a-space>
              <a-button type="link" @click="editKB(record)">编辑</a-button>
              <a-popconfirm title="确定删除此知识库？所有文档将被删除" @confirm="deleteKB(record.id)">
                <a-button type="link" danger>删除</a-button>
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
        <a-card title="文件夹权限">
          <template #extra>
            <a-space v-if="selectedFolder">
              <a-button size="small" @click="showRenameFolder">重命名</a-button>
              <a-popconfirm title="确定删除此文件夹？" @confirm="deleteFolder">
                <a-button size="small" danger>删除</a-button>
              </a-popconfirm>
              <a-button size="small" @click="showAddSubFolder">添加子文件夹</a-button>
            </a-space>
          </template>

          <div v-if="!selectedFolder">
            <a-empty description="选择一个文件夹管理权限" />
          </div>

          <div v-else>
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
          </div>
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
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { message } from 'ant-design-vue'
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

const selectKB = async (kb: any) => {
  selectedKB.value = kb
  selectedFolder.value = null
  loadFolderTree()
}

const loadFolderTree = async () => {
  if (!selectedKB.value) return
  treeLoading.value = true
  const res = await api.get('/admin/knowledge-bases/' + selectedKB.value.id + '/folders')
  const folders = res.data || []
  folderTreeData.value = buildTree(folders)
  treeLoading.value = false
}

const buildTree = (folders: any[], parentId?: number | null): any[] => {
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

const onFolderSelect = async (keys: any[]) => {
  if (keys.length === 0) { selectedFolder.value = null; return }
  const folderId = parseInt(keys[0])
  selectedFolder.value = { id: folderId }
  await loadFolderPermissions(folderId)
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
</script>
