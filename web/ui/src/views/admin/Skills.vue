<template>
  <div>
    <a-page-header title="技能库" sub-title="管理 AI 技能" />
    <a-tabs v-model:activeKey="activeTab" style="margin-top: 16px">
      <a-tab-pane key="installed" tab="已安装技能">
        <a-row justify="space-between" align="middle" style="margin-bottom: 16px">
          <a-input-search
            v-model:value="search"
            placeholder="搜索技能"
            style="width: 300px"
            @search="fetchSkills"
          />
        </a-row>
        <a-table
          :columns="skillColumns"
          :data-source="skills"
          :loading="skillsLoading"
          :pagination="skillsPagination"
          @change="handleSkillsTableChange"
          row-key="name"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'is_default'">
              <a-switch
                :checked="record.is_default"
                @change="toggleDefault(record.name)"
                size="small"
              />
            </template>
            <template v-if="column.key === 'action'">
              <a-space>
                <a-button type="link" size="small" @click="openDeploy(record)">部署</a-button>
                <a-button type="link" size="small" danger @click="handleRemove(record.name)">移除</a-button>
              </a-space>
            </template>
          </template>
        </a-table>
      </a-tab-pane>

      <a-tab-pane key="sources" tab="技能来源">
        <a-row justify="end" style="margin-bottom: 16px">
          <a-button type="primary" @click="showAddSource = true">添加 Git 来源</a-button>
        </a-row>
        <a-table
          :columns="sourceColumns"
          :data-source="sources"
          :loading="sourcesLoading"
          :pagination="false"
          row-key="name"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'last_pull'">
              {{ record.last_pull ? new Date(record.last_pull).toLocaleString() : '-' }}
            </template>
            <template v-if="column.key === 'action'">
              <a-space>
                <a-button type="link" size="small" :loading="pullingName === record.name" @click="handlePull(record.name)">拉取</a-button>
                <a-button type="link" size="small" danger @click="handleRemoveSource(record.name)">移除</a-button>
              </a-space>
            </template>
          </template>
        </a-table>
      </a-tab-pane>

      <a-tab-pane key="registry" tab="注册中心">
        <a-row justify="space-between" align="middle" style="margin-bottom: 16px">
          <a-input-search
            v-model:value="registryQuery"
            placeholder="搜索技能"
            style="width: 300px"
            @search="fetchRegistry"
          />
        </a-row>
        <a-table
          :columns="registryColumns"
          :data-source="registryItems"
          :loading="registryLoading"
          :pagination="registryPagination"
          @change="handleRegistryTableChange"
          row-key="slug"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'action'">
              <a-button type="link" size="small" :loading="installingSlug === record.slug" @click="handleInstall(record)">安装</a-button>
            </template>
          </template>
        </a-table>
      </a-tab-pane>
    </a-tabs>

    <a-modal v-model:open="showDeploy" title="部署技能" @ok="handleDeploy" :confirm-loading="deployLoading">
      <a-form layout="vertical">
        <a-form-item label="技能">
          <a-input :value="deploySkillName" disabled />
        </a-form-item>
        <a-form-item label="部署目标类型">
          <a-radio-group v-model:value="deployTargetType">
            <a-radio value="user">用户</a-radio>
            <a-radio value="group">用户组</a-radio>
          </a-radio-group>
        </a-form-item>
        <a-form-item :label="deployTargetType === 'user' ? '用户名' : '用户组名'">
          <a-input v-model:value="deployTargetValue" :placeholder="deployTargetType === 'user' ? '输入用户名' : '输入用户组名'" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:open="showAddSource" title="添加 Git 技能来源" @ok="handleAddSource" :confirm-loading="addSourceLoading">
      <a-form layout="vertical">
        <a-form-item label="名称">
          <a-input v-model:value="sourceForm.name" />
        </a-form-item>
        <a-form-item label="URL">
          <a-input v-model:value="sourceForm.url" placeholder="https://..." />
        </a-form-item>
        <a-form-item label="引用">
          <a-input v-model:value="sourceForm.ref" placeholder="main" />
        </a-form-item>
        <a-form-item label="引用类型">
          <a-select v-model:value="sourceForm.ref_type">
            <a-select-option value="branch">分支</a-select-option>
            <a-select-option value="tag">标签</a-select-option>
            <a-select-option value="commit">提交</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="用户名（可选）">
          <a-input v-model:value="sourceForm.username" />
        </a-form-item>
        <a-form-item label="密码（可选）">
          <a-input-password v-model:value="sourceForm.password" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, watch } from 'vue'
import { message, Modal } from 'ant-design-vue'
import { api } from '../../composables/api'
import { usePagination } from '../../composables/usePagination'

const activeTab = ref('installed')
const { pagination: skillsPagination } = usePagination()
const { pagination: registryPagination } = usePagination()

const search = ref('')
const skills = ref<any[]>([])
const skillsLoading = ref(false)

const skillColumns = [
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: '描述', dataIndex: 'description', key: 'description' },
  { title: '来源', dataIndex: 'source', key: 'source' },
  { title: '默认', key: 'is_default', width: 80 },
  { title: '操作', key: 'action', width: 150 },
]

const fetchSkills = async () => {
  skillsLoading.value = true
  try {
    const data = await api.get('/admin/skills', { page: String(skillsPagination.current), page_size: String(skillsPagination.pageSize), search: search.value })
    skills.value = data.skills || []
    skillsPagination.total = data.total || 0
  } catch {
    message.error('获取技能列表失败')
  } finally {
    skillsLoading.value = false
  }
}

const handleSkillsTableChange = (pag: any) => {
  skillsPagination.current = pag.current
  skillsPagination.pageSize = pag.pageSize || 20
  fetchSkills()
}

const showDeploy = ref(false)
const deploySkillName = ref('')
const deployTargetType = ref<'user' | 'group'>('user')
const deployTargetValue = ref('')
const deployLoading = ref(false)

const openDeploy = (record: any) => {
  deploySkillName.value = record.name
  deployTargetType.value = 'user'
  deployTargetValue.value = ''
  showDeploy.value = true
}

const handleDeploy = async () => {
  if (!deployTargetValue.value) {
    message.warning('请输入目标名称')
    return
  }
  deployLoading.value = true
  try {
    const params: Record<string, string> = { skill_name: deploySkillName.value }
    if (deployTargetType.value === 'user') {
      params.username = deployTargetValue.value
    } else {
      params.group_name = deployTargetValue.value
    }
    await api.post('/admin/skills/deploy', params)
    message.success('部署成功')
    showDeploy.value = false
  } catch {
    message.error('部署失败')
  } finally {
    deployLoading.value = false
  }
}

const handleRemove = (name: string) => {
  Modal.confirm({
    title: '确认移除',
    content: `确定要移除技能「${name}」吗？`,
    okText: '确认',
    cancelText: '取消',
    onOk: async () => {
      try {
        await api.post('/admin/skills/remove', { name })
        message.success('移除成功')
        fetchSkills()
      } catch {
        message.error('移除失败')
      }
    },
  })
}

const toggleDefault = async (skillName: string) => {
  try {
    await api.post('/admin/skills/defaults/toggle', { skill_name: skillName })
    message.success('已切换')
    fetchSkills()
  } catch {
    message.error('操作失败')
  }
}

const sources = ref<any[]>([])
const sourcesLoading = ref(false)
const pullingName = ref('')

const sourceColumns = [
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: '类型', dataIndex: 'type', key: 'type' },
  { title: 'URL', dataIndex: 'url', key: 'url' },
  { title: '上次拉取', key: 'last_pull' },
  { title: '技能数', dataIndex: 'skill_count', key: 'skill_count' },
  { title: '操作', key: 'action', width: 150 },
]

const fetchSources = async () => {
  sourcesLoading.value = true
  try {
    const data = await api.get('/admin/skills/sources')
    sources.value = data.sources || []
  } catch {
    message.error('获取来源列表失败')
  } finally {
    sourcesLoading.value = false
  }
}

const showAddSource = ref(false)
const addSourceLoading = ref(false)
const sourceForm = reactive({
  name: '',
  url: '',
  ref: 'main',
  ref_type: 'branch',
  username: '',
  password: '',
})

const handleAddSource = async () => {
  if (!sourceForm.name || !sourceForm.url) {
    message.warning('请填写名称和 URL')
    return
  }
  addSourceLoading.value = true
  try {
    const params: Record<string, string> = { name: sourceForm.name, url: sourceForm.url, ref: sourceForm.ref, ref_type: sourceForm.ref_type }
    if (sourceForm.username) params.username = sourceForm.username
    if (sourceForm.password) params.password = sourceForm.password
    await api.post('/admin/skills/sources/git', params)
    message.success('添加成功')
    showAddSource.value = false
    Object.assign(sourceForm, { name: '', url: '', ref: 'main', ref_type: 'branch', username: '', password: '' })
    fetchSources()
  } catch {
    message.error('添加失败')
  } finally {
    addSourceLoading.value = false
  }
}

const handlePull = async (name: string) => {
  pullingName.value = name
  try {
    await api.post('/admin/skills/sources/pull', { name })
    message.success('拉取成功')
    fetchSources()
  } catch {
    message.error('拉取失败')
  } finally {
    pullingName.value = ''
  }
}

const handleRemoveSource = (name: string) => {
  Modal.confirm({
    title: '确认移除',
    content: `确定要移除来源「${name}」吗？`,
    okText: '确认',
    cancelText: '取消',
    onOk: async () => {
      try {
        await api.post('/admin/skills/sources/remove', { name })
        message.success('移除成功')
        fetchSources()
      } catch {
        message.error('移除失败')
      }
    },
  })
}

const registryItems = ref<any[]>([])
const registryLoading = ref(false)
const registryQuery = ref('')
const installingSlug = ref('')

const registryColumns = [
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: '标识', dataIndex: 'slug', key: 'slug' },
  { title: '描述', dataIndex: 'description', key: 'description' },
  { title: '来源', dataIndex: 'source', key: 'source' },
  { title: '操作', key: 'action', width: 100 },
]

const fetchRegistry = async () => {
  registryLoading.value = true
  try {
    if (sources.value.length === 0) {
      try {
        const data = await api.get('/admin/skills/sources')
        sources.value = data.sources || []
      } catch {}
    }
    const src = sources.value.length > 0 ? sources.value[0].name : 'skillhub.cn'
    const params: Record<string, string> = { source: src, page: String(registryPagination.current), page_size: String(registryPagination.pageSize) }
    if (registryQuery.value) params.q = registryQuery.value
    const data = await api.get('/admin/skills/registry/list', params)
    registryItems.value = data.skills || []
    registryPagination.total = data.total || 0
  } catch {
    message.error('获取注册中心失败')
  } finally {
    registryLoading.value = false
  }
}

const handleRegistryTableChange = (pag: any) => {
  registryPagination.current = pag.current
  registryPagination.pageSize = pag.pageSize || 20
  fetchRegistry()
}

const handleInstall = async (record: any) => {
  installingSlug.value = record.slug
  try {
    await api.post('/admin/skills/registry/install', { source: record.source || '', slug: record.slug })
    message.success('安装成功')
    fetchSkills()
  } catch {
    message.error('安装失败')
  } finally {
    installingSlug.value = ''
  }
}

watch(activeTab, (tab) => {
  if (tab === 'installed') fetchSkills()
  else if (tab === 'sources') fetchSources()
  else if (tab === 'registry') fetchRegistry()
})

onMounted(fetchSkills)
</script>
