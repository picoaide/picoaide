<template>
  <div>
    <a-page-header title="认证配置" sub-title="管理认证源和白名单" />

    <a-card style="margin-bottom: 16px">
      <a-form layout="inline">
        <a-form-item label="认证模式">
          <a-radio-group v-model:value="authMode" @change="handleModeChange">
            <a-radio-button value="local">本地</a-radio-button>
            <a-radio-button value="ldap">LDAP</a-radio-button>
            <a-radio-button value="oidc">OIDC</a-radio-button>
          </a-radio-group>
        </a-form-item>
      </a-form>
    </a-card>

    <a-tabs v-model:activeKey="activeTab" style="margin-top: 16px">
      <a-tab-pane key="whitelist" tab="白名单">
        <div style="margin-bottom: 12px">
          <a-space>
            <a-input
              v-model:value="whitelistInput"
              placeholder="输入用户名"
              style="width: 240px"
            />
            <a-button type="primary" @click="handleAddWhitelist" :loading="wlLoading">添加</a-button>
          </a-space>
        </div>
        <a-table
          :columns="wlColumns"
          :data-source="whitelist"
          :loading="wlTableLoading"
          :pagination="wlPagination"
          @change="handleWlTableChange"
          row-key="username"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'action'">
              <a-popconfirm title="确定移除？" @confirm="handleRemoveWhitelist(record.username)">
                <a-button type="link" size="small" danger>移除</a-button>
              </a-popconfirm>
            </template>
          </template>
        </a-table>
      </a-tab-pane>

      <a-tab-pane key="ldap" tab="LDAP 配置" v-if="authMode === 'ldap'">
        <a-card>
          <a-form layout="vertical">
            <a-row :gutter="24">
              <a-col :span="12">
                <a-form-item label="LDAP 主机">
                  <a-input v-model:value="ldapConfig.host" placeholder="ldap://host:389" />
                </a-form-item>
              </a-col>
              <a-col :span="12">
                <a-form-item label="绑定 DN">
                  <a-input v-model:value="ldapConfig.bind_dn" placeholder="cn=admin,dc=example,dc=com" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-row :gutter="24">
              <a-col :span="12">
                <a-form-item label="绑定密码">
                  <a-input-password v-model:value="ldapConfig.bind_password" placeholder="请输入密码" />
                </a-form-item>
              </a-col>
              <a-col :span="12">
                <a-form-item label="Base DN">
                  <a-input v-model:value="ldapConfig.base_dn" placeholder="dc=example,dc=com" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-row :gutter="24">
              <a-col :span="12">
                <a-form-item label="用户过滤器">
                  <a-input v-model:value="ldapConfig.filter" placeholder="(objectClass=person)" />
                </a-form-item>
              </a-col>
              <a-col :span="12">
                <a-form-item label="用户名属性">
                  <a-input v-model:value="ldapConfig.username_attribute" placeholder="uid" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-row :gutter="24">
              <a-col :span="12">
                <a-form-item label="组搜索模式">
                  <a-select v-model:value="ldapConfig.group_search_mode" placeholder="选择模式">
                    <a-select-option value="member_of">member_of</a-select-option>
                    <a-select-option value="group_search">group_search</a-select-option>
                  </a-select>
                </a-form-item>
              </a-col>
              <a-col :span="12">
                <a-form-item label="组 Base DN">
                  <a-input v-model:value="ldapConfig.group_base_dn" placeholder="ou=groups,dc=example,dc=com" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-row :gutter="24">
              <a-col :span="12">
                <a-form-item label="组过滤器">
                  <a-input v-model:value="ldapConfig.group_filter" placeholder="(objectClass=groupOfNames)" />
                </a-form-item>
              </a-col>
              <a-col :span="12">
                <a-form-item label="组成员属性">
                  <a-input v-model:value="ldapConfig.group_member_attribute" placeholder="member" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-form-item>
              <a-space>
                <a-button type="primary" @click="handleSaveLdap" :loading="ldapSaving">保存配置</a-button>
                <a-button @click="handleTestLdap" :loading="ldapTesting">测试连接</a-button>
                <a-button @click="handleSyncUsers" :loading="syncingUsers">同步用户</a-button>
                <a-button @click="handleSyncGroups" :loading="syncingGroups">同步组</a-button>
              </a-space>
            </a-form-item>
          </a-form>
        </a-card>
      </a-tab-pane>

      <a-tab-pane key="providers" tab="认证源">
        <a-table
          :columns="providerColumns"
          :data-source="providers"
          :loading="providersLoading"
          :pagination="false"
          row-key="name"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'capabilities'">
              <a-tag v-for="c in record.capabilities" :key="c" color="blue">{{ c }}</a-tag>
            </template>
          </template>
        </a-table>
      </a-tab-pane>
    </a-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

const activeTab = ref('whitelist')
const authMode = ref('local')

const whitelist = ref<any[]>([])
const whitelistInput = ref('')
const wlLoading = ref(false)
const wlTableLoading = ref(false)
const wlPagination = reactive({ current: 1, pageSize: 20, total: 0, showSizeChanger: true, showTotal: (t: number) => `共 ${t} 条` })
const wlColumns = [
  { title: '用户名', dataIndex: 'username', key: 'username' },
  { title: '添加者', dataIndex: 'added_by', key: 'added_by' },
  { title: '操作', key: 'action', width: 100 },
]

const ldapConfig = reactive({
  host: '',
  bind_dn: '',
  bind_password: '',
  base_dn: '',
  filter: '',
  username_attribute: '',
  group_search_mode: '',
  group_base_dn: '',
  group_filter: '',
  group_member_attribute: '',
})
const ldapSaving = ref(false)
const ldapTesting = ref(false)
const syncingUsers = ref(false)
const syncingGroups = ref(false)

const providers = ref<any[]>([])
const providersLoading = ref(false)
const providerColumns = [
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: '类型', dataIndex: 'type', key: 'type' },
  { title: '能力', key: 'capabilities' },
]

const fetchWhitelist = async () => {
  wlTableLoading.value = true
  try {
    const data = await api.get('/admin/whitelist')
    whitelist.value = (data.users || []).map((u: string) => ({ username: u, added_by: '' }))
    wlPagination.total = whitelist.value.length
  } catch {
    message.error('获取白名单失败')
  } finally {
    wlTableLoading.value = false
  }
}

const handleWlTableChange = (pag: any) => {
  wlPagination.current = pag.current
  wlPagination.pageSize = pag.pageSize
}

const handleAddWhitelist = async () => {
  if (!whitelistInput.value.trim()) {
    message.warning('请输入用户名')
    return
  }
  wlLoading.value = true
  try {
    await api.post('/admin/whitelist', { add: whitelistInput.value.trim() })
    message.success('添加成功')
    whitelistInput.value = ''
    fetchWhitelist()
  } catch {
    message.error('添加失败')
  } finally {
    wlLoading.value = false
  }
}

const handleRemoveWhitelist = async (username: string) => {
  try {
    await api.post('/admin/whitelist', { remove: username })
    message.success('移除成功')
    fetchWhitelist()
  } catch {
    message.error('移除失败')
  }
}

const fetchAuthMode = async () => {
  try {
    const data = await api.get('/login/mode')
    authMode.value = data.auth_mode || 'local'
    if (authMode.value === 'ldap') {
      fetchLdapConfig()
    }
  } catch {
    // ignore
  }
}

const handleModeChange = async (e: any) => {
  const mode = typeof e === 'string' ? e : e?.target?.value
  if (!mode || mode === authMode.value) return
  try {
    const cfg = await api.get('/config')
    cfg.web = cfg.web || {}
    cfg.web.auth_mode = mode
    await api.post('/config', { config: JSON.stringify(cfg) })
    message.success('认证模式已切换，请重新登录')
  } catch {
    message.error('切换失败，请通过系统配置修改')
  }
}

const fetchLdapConfig = async () => {
  try {
    const data = await api.get('/config')
    const ldap = data.ldap || data.config?.ldap || {}
    Object.keys(ldapConfig).forEach(key => {
      if (ldap[key] !== undefined) {
        ;(ldapConfig as any)[key] = ldap[key]
      }
    })
  } catch {
    // ignore
  }
}

const handleSaveLdap = async () => {
  ldapSaving.value = true
  try {
    const cfg = await api.get('/config')
    cfg.ldap = { ...ldapConfig }
    await api.post('/config', { config: JSON.stringify(cfg) })
    message.success('保存成功')
  } catch {
    message.error('保存失败')
  } finally {
    ldapSaving.value = false
  }
}

const handleTestLdap = async () => {
  ldapTesting.value = true
  try {
    await api.post('/admin/auth/test-ldap')
    message.success('连接成功')
  } catch {
    message.error('连接失败')
  } finally {
    ldapTesting.value = false
  }
}

const handleSyncUsers = async () => {
  syncingUsers.value = true
  try {
    await api.post('/admin/auth/sync-users')
    message.success('同步成功')
  } catch {
    message.error('同步失败')
  } finally {
    syncingUsers.value = false
  }
}

const handleSyncGroups = async () => {
  syncingGroups.value = true
  try {
    await api.post('/admin/auth/sync-groups')
    message.success('同步成功')
  } catch {
    message.error('同步失败')
  } finally {
    syncingGroups.value = false
  }
}

const fetchProviders = async () => {
  providersLoading.value = true
  try {
    const data = await api.get('/admin/auth/providers')
    providers.value = data.providers || []
  } catch {
    message.error('获取认证源失败')
  } finally {
    providersLoading.value = false
  }
}

onMounted(() => {
  fetchAuthMode()
  fetchWhitelist()
  fetchProviders()
})
</script>
