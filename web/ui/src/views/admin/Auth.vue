<template>
  <div>
    <a-page-header title="认证配置" sub-title="管理认证源和白名单" />

    <a-card style="margin-bottom: 16px">
      <a-form layout="inline">
        <a-form-item label="认证模式">
          <a-radio-group :value="authMode" @change="handleModeChange">
            <a-radio-button v-for="p in providers" :key="p.name" :value="p.name">
              {{ p.display_name || p.name }}
            </a-radio-button>
          </a-radio-group>
        </a-form-item>
      </a-form>
    </a-card>

    <a-tabs v-model:activeKey="activeTab">
      <a-tab-pane key="config" tab="配置">
        <a-spin :spinning="loading">
          <template v-for="section in currentFields" :key="section.name">
            <a-card :title="section.name" style="margin-bottom: 16px">
              <a-row :gutter="24">
                <a-col
                  v-for="field in section.fields"
                  :key="field.key"
                  :span="field.type === 'select' ? 12 : 12"
                >
                  <a-form-item :label="field.label" style="margin-bottom: 16px">
                    <a-input
                      v-if="field.type === 'text'"
                      v-model:value="configValues[field.key]"
                      :placeholder="field.placeholder || ''"
                    />
                    <a-input-password
                      v-if="field.type === 'password'"
                      v-model:value="configValues[field.key]"
                      :placeholder="field.placeholder || ''"
                    />
                    <a-select
                      v-if="field.type === 'select'"
                      v-model:value="configValues[field.key]"
                      :placeholder="field.placeholder || ''"
                      style="width: 100%"
                    >
                      <a-select-option
                        v-for="opt in field.options"
                        :key="opt.value"
                        :value="opt.value"
                      >
                        {{ opt.label }}
                      </a-select-option>
                    </a-select>
                  </a-form-item>
                </a-col>
              </a-row>
            </a-card>
          </template>

          <template v-if="currentActions.length > 0">
            <a-card title="操作" style="margin-bottom: 16px">
              <a-space>
                <a-button
                  v-for="act in currentActions"
                  :key="act.id"
                  :loading="actionLoading[act.id]"
                  @click="handleAction(act.id)"
                >
                  {{ act.label }}
                </a-button>
              </a-space>
            </a-card>
          </template>

          <a-button type="primary" :loading="saving" @click="handleSave">保存配置</a-button>
        </a-spin>
      </a-tab-pane>

      <a-tab-pane key="whitelist" tab="白名单">
        <div style="margin-bottom: 12px">
          <a-space>
            <a-input v-model:value="whitelistInput" placeholder="输入用户名" style="width: 240px" />
            <a-button type="primary" @click="handleAddWhitelist" :loading="wlLoading">添加</a-button>
          </a-space>
        </div>
        <a-table
          :columns="wlColumns"
          :data-source="whitelist"
          :loading="wlTableLoading"
          row-key="username"
          size="small"
          :pagination="false"
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
    </a-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

const activeTab = ref('config')
const authMode = ref('local')
const providers = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const configValues = reactive<Record<string, any>>({})
const actionLoading = reactive<Record<string, boolean>>({})

const whitelist = ref<any[]>([])
const whitelistInput = ref('')
const wlLoading = ref(false)
const wlTableLoading = ref(false)
const wlColumns = [
  { title: '用户名', dataIndex: 'username', key: 'username' },
  { title: '操作', key: 'action', width: 100 },
]

const currentProvider = computed(() =>
  providers.value.find(p => p.name === authMode.value)
)

const currentFields = computed(() =>
  currentProvider.value?.fields || []
)

const currentActions = computed(() =>
  currentProvider.value?.actions || []
)

const handleModeChange = async (e: any) => {
  const mode = typeof e === 'string' ? e : e?.target?.value
  if (!mode || mode === authMode.value) return
  try {
    await api.post('/config', { config: JSON.stringify({ web: { auth_mode: mode } }) })
    message.success('认证模式已切换')
    authMode.value = mode
  } catch {
    message.error('切换失败')
  }
}

const currentConfigKey = computed(() => {
  if (authMode.value === 'local') return 'local'
  if (authMode.value === 'ldap') return 'ldap'
  if (authMode.value === 'oidc') return 'oidc'
  return authMode.value
})

const fetchData = async () => {
  loading.value = true
  try {
    const [modeData, provData, cfgData, wlData] = await Promise.all([
      api.get('/login/mode'),
      api.get('/admin/auth/providers'),
      api.get('/config'),
      api.get('/admin/whitelist'),
    ])
    authMode.value = modeData.auth_mode || 'local'
    providers.value = provData.providers || []

    const sectionCfg = cfgData[currentConfigKey.value] || {}
    configValues['host'] = sectionCfg.host || ''
    configValues['bind_dn'] = sectionCfg.bind_dn || ''
    configValues['bind_password'] = ''
    configValues['base_dn'] = sectionCfg.base_dn || ''
    configValues['filter'] = sectionCfg.filter || ''
    configValues['username_attribute'] = sectionCfg.username_attribute || ''
    configValues['group_search_mode'] = sectionCfg.group_search_mode || ''
    configValues['group_base_dn'] = sectionCfg.group_base_dn || ''
    configValues['group_filter'] = sectionCfg.group_filter || ''
    configValues['group_member_attribute'] = sectionCfg.group_member_attribute || ''
    configValues['issuer_url'] = sectionCfg.issuer_url || ''
    configValues['client_id'] = sectionCfg.client_id || ''
    configValues['client_secret'] = ''
    configValues['redirect_url'] = sectionCfg.redirect_url || ''
    configValues['scopes'] = sectionCfg.scopes || ''
    configValues['username_claim'] = sectionCfg.username_claim || ''
    configValues['groups_claim'] = sectionCfg.groups_claim || ''
    configValues['sync_interval'] = sectionCfg.sync_interval || ''

    whitelist.value = (wlData.users || []).map((u: string) => ({ username: u }))
  } catch {
    message.error('加载配置失败')
  } finally {
    loading.value = false
  }
}

const handleSave = async () => {
  saving.value = true
  try {
    const provider = currentConfigKey.value
    await api.post('/config', { config: JSON.stringify({ [provider]: { ...configValues } }) })
    message.success('保存成功')
  } catch {
    message.error('保存失败')
  } finally {
    saving.value = false
  }
}

const handleAction = async (actionId: string) => {
  actionLoading[actionId] = true
  try {
    const endpoint = {
      'test-ldap': '/admin/auth/test-ldap',
      'sync-users': '/admin/auth/sync-users',
      'sync-groups': '/admin/auth/sync-groups',
    }[actionId]
    if (!endpoint) {
      message.warning('未知操作')
      return
    }
    await api.post(endpoint)
    message.success('操作成功')
  } catch {
    message.error('操作失败')
  } finally {
    actionLoading[actionId] = false
  }
}

const handleAddWhitelist = async () => {
  if (!whitelistInput.value.trim()) { message.warning('请输入用户名'); return }
  wlLoading.value = true
  try {
    await api.post('/admin/whitelist', { add: whitelistInput.value.trim() })
    message.success('添加成功')
    whitelistInput.value = ''
    fetchData()
  } catch { message.error('添加失败') }
  finally { wlLoading.value = false }
}

const handleRemoveWhitelist = async (username: string) => {
  try {
    await api.post('/admin/whitelist', { remove: username })
    message.success('移除成功')
    fetchData()
  } catch { message.error('移除失败') }
}

onMounted(fetchData)
</script>
