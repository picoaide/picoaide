<template>
  <div>
    <a-page-header title="用户组" sub-title="管理用户组及成员">
      <template #extra>
        <a-button type="primary" @click="openCreate">创建组</a-button>
      </template>
    </a-page-header>

    <div style="margin-bottom: 16px">
      <a-input-search
        v-model:value="search"
        placeholder="搜索组名"
        style="width: 300px"
        @search="handleSearch"
        allow-clear
      />
    </div>

    <a-table
      :columns="groupColumns"
      :data-source="groups"
      :loading="loading"
      :pagination="pagination"
      @change="handleTableChange"
      row-key="name"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'name'">
          <a @click="openMembers(record)">{{ record.name }}</a>
        </template>
        <template v-if="column.key === 'parent'">
          {{ record.parent_name || record.parent_id || '-' }}
        </template>
        <template v-if="column.key === 'action'">
          <a-popconfirm title="确定删除该组？" @confirm="handleDelete(record.name)">
            <a-button type="link" size="small" danger>删除</a-button>
          </a-popconfirm>
        </template>
      </template>
    </a-table>

    <a-modal
      v-model:open="showCreate"
      title="创建用户组"
      @ok="handleCreate"
      :confirm-loading="createLoading"
    >
      <a-form layout="vertical">
        <a-form-item label="组名" required>
          <a-input v-model:value="createForm.name" placeholder="请输入组名" />
        </a-form-item>
        <a-form-item label="描述">
          <a-input v-model:value="createForm.description" placeholder="请输入描述" />
        </a-form-item>
        <a-form-item label="父组">
          <a-select
            v-model:value="createForm.parent_id"
            placeholder="选择父组（可选）"
            allow-clear
            :options="parentOptions"
          />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-drawer
      v-model:open="showMembers"
      :title="`成员管理 - ${currentGroup}`"
      width="640"
      @close="closeMembers"
    >
      <a-tabs v-model:activeKey="memberTab">
        <a-tab-pane key="members" tab="成员列表">
          <div style="margin-bottom: 12px">
            <a-space>
              <a-input-search
                v-model:value="memberSearch"
                placeholder="搜索成员"
                style="width: 240px"
                @search="fetchMembers"
                allow-clear
              />
              <a-button type="primary" @click="showAddMembers = true">添加成员</a-button>
            </a-space>
          </div>
          <a-table
            :columns="memberColumns"
            :data-source="members"
            :loading="membersLoading"
            :pagination="memberPagination"
            @change="handleMemberTableChange"
            row-key="username"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'action'">
                <a-popconfirm title="确定移除该成员？" @confirm="handleRemoveMember(record.username)">
                  <a-button type="link" size="small" danger>移除</a-button>
                </a-popconfirm>
              </template>
            </template>
          </a-table>
        </a-tab-pane>
        <a-tab-pane key="skills" tab="技能绑定">
          <div style="margin-bottom: 12px">
            <a-space>
              <a-select
                v-model:value="selectedSkill"
                placeholder="选择技能"
                style="width: 240px"
                show-search
                :filter-option="filterSkillOption"
                :options="availableSkills"
              />
              <a-button type="primary" @click="handleBindSkill" :disabled="!selectedSkill">绑定</a-button>
            </a-space>
          </div>
          <a-spin :spinning="skillsLoading">
            <a-tag
              v-for="s in boundSkills"
              :key="s"
              closable
              color="blue"
              style="margin-bottom: 8px"
              @close="handleUnbindSkill(s)"
            >
              {{ s }}
            </a-tag>
            <a-empty v-if="!boundSkills.length" description="暂无绑定技能" />
          </a-spin>
        </a-tab-pane>
      </a-tabs>
    </a-drawer>

    <a-modal
      v-model:open="showAddMembers"
      title="添加成员"
      @ok="handleAddMembers"
      :confirm-loading="addMembersLoading"
    >
      <a-form layout="vertical">
        <a-form-item label="用户名列表" required>
          <a-textarea
            v-model:value="addMemberUsernames"
            placeholder="每行一个用户名"
            :rows="6"
          />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, watch } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'
import { usePagination } from '../../composables/usePagination'

const { pagination } = usePagination()
const loading = ref(false)
const groups = ref<any[]>([])
const search = ref('')
const showCreate = ref(false)
const createLoading = ref(false)

const groupColumns = [
  { title: '组名', key: 'name' },
  { title: '描述', dataIndex: 'description', key: 'description' },
  { title: '来源', dataIndex: 'source', key: 'source' },
  { title: '父组', key: 'parent' },
  { title: '操作', key: 'action', width: 100 },
]

const showMembers = ref(false)
const currentGroup = ref('')
const memberTab = ref('members')
const memberSearch = ref('')
const members = ref<any[]>([])
const membersLoading = ref(false)
const memberPagination = reactive({ current: 1, pageSize: 20, total: 0 })
const memberColumns = [
  { title: '用户名', dataIndex: 'username', key: 'username' },
  { title: '操作', key: 'action', width: 100 },
]
const showAddMembers = ref(false)
const addMembersLoading = ref(false)
const addMemberUsernames = ref('')

const selectedSkill = ref('')
const boundSkills = ref<string[]>([])
const skillsLoading = ref(false)
const availableSkills = ref<{ label: string; value: string }[]>([])

const createForm = reactive({ name: '', description: '', parent_id: undefined as number | undefined })
const parentOptions = ref<{ label: string; value: number }[]>([])

const fetchGroups = async () => {
  loading.value = true
  try {
    const data = await api.get('/admin/groups', { page: String(pagination.current), page_size: String(pagination.pageSize), search: search.value })
    groups.value = data.groups || []
    pagination.total = data.total || 0
    parentOptions.value = groups.value.map((g: any) => ({ label: g.name, value: g.id }))
  } catch {
    message.error('获取组列表失败')
  } finally {
    loading.value = false
  }
}

const handleTableChange = (pag: any) => {
  pagination.current = pag.current
  pagination.pageSize = pag.pageSize
  fetchGroups()
}

const handleSearch = () => {
  pagination.current = 1
  fetchGroups()
}

const openCreate = () => {
  createForm.name = ''
  createForm.description = ''
  createForm.parent_id = undefined
  showCreate.value = true
}

const handleCreate = async () => {
  if (!createForm.name.trim()) {
    message.warning('请输入组名')
    return
  }
  createLoading.value = true
  try {
    const params: Record<string, string> = { name: createForm.name.trim() }
    if (createForm.description) params.description = createForm.description
    if (createForm.parent_id) (params as any).parent_id = createForm.parent_id
    await api.post('/admin/groups/create', params)
    message.success('创建成功')
    showCreate.value = false
    fetchGroups()
  } catch {
    message.error('创建失败')
  } finally {
    createLoading.value = false
  }
}

const handleDelete = async (name: string) => {
  try {
    await api.post('/admin/groups/delete', { name })
    message.success('删除成功')
    fetchGroups()
  } catch {
    message.error('删除失败')
  }
}

const openMembers = async (record: any) => {
  currentGroup.value = record.name
  showMembers.value = true
  memberTab.value = 'members'
  memberPagination.current = 1
  memberSearch.value = ''
  await fetchMembers()
  fetchAllSkills()
}

const closeMembers = () => {
  showMembers.value = false
  currentGroup.value = ''
}

const fetchMembers = async () => {
  membersLoading.value = true
  try {
    const data = await api.get('/admin/groups/members', { name: currentGroup.value, page: String(memberPagination.current), page_size: String(memberPagination.pageSize), search: memberSearch.value })
    members.value = (data.members || []).map((u: string) => ({ username: u }))
    memberPagination.total = data.total || 0
    boundSkills.value = data.skills || []
  } catch {
    message.error('获取成员列表失败')
  } finally {
    membersLoading.value = false
  }
}

const handleMemberTableChange = (pag: any) => {
  memberPagination.current = pag.current
  memberPagination.pageSize = pag.pageSize
  fetchMembers()
}

const handleAddMembers = async () => {
  const names = addMemberUsernames.value.split('\n').map(s => s.trim()).filter(Boolean)
  if (!names.length) {
    message.warning('请输入用户名')
    return
  }
  addMembersLoading.value = true
  try {
    await api.post('/admin/groups/members/add', { group_name: currentGroup.value, usernames: names })
    message.success('添加成功')
    showAddMembers.value = false
    addMemberUsernames.value = ''
    fetchMembers()
  } catch {
    message.error('添加失败')
  } finally {
    addMembersLoading.value = false
  }
}

const handleRemoveMember = async (username: string) => {
  try {
    await api.post('/admin/groups/members/remove', { group_name: currentGroup.value, username })
    message.success('移除成功')
    fetchMembers()
  } catch {
    message.error('移除失败')
  }
}

const fetchAllSkills = async () => {
  try {
    const data = await api.get('/admin/skills')
    availableSkills.value = (data.skills || []).map((s: any) => ({ label: s.name, value: s.name }))
  } catch {
    // ignore
  }
}

const filterSkillOption = (input: string, option: any) => {
  return option.label.toLowerCase().includes(input.toLowerCase())
}

const handleBindSkill = async () => {
  if (!selectedSkill.value) return
  try {
    await api.post('/admin/groups/skills/bind', { group_name: currentGroup.value, skill_name: selectedSkill.value })
    message.success('绑定成功')
    selectedSkill.value = ''
    fetchMembers()
  } catch {
    message.error('绑定失败')
  }
}

const handleUnbindSkill = async (skillName: string) => {
  try {
    await api.post('/admin/groups/skills/unbind', { group_name: currentGroup.value, skill_name: skillName })
    message.success('解绑成功')
    fetchMembers()
  } catch {
    message.error('解绑失败')
  }
}

watch(memberTab, (val) => {
  if (val === 'skills') {
    fetchMembers()
    fetchAllSkills()
  }
})

onMounted(fetchGroups)
</script>
