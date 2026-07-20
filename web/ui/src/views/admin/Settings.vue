<template>
  <div>
    <a-page-header title="系统配置" sub-title="管理没有独立页面的全局配置项">
      <template #extra>
        <a-space>
          <a-badge v-if="linkedSections.length > 0" count="!" :offset="[-8, 0]" size="small">
            <a-dropdown>
              <a-button>已有配置页面</a-button>
              <template #overlay>
                <a-menu>
                  <a-menu-item v-for="s in linkedSections" :key="s.path">
                    <a :href="s.path" target="_self">{{ s.label }}</a>
                  </a-menu-item>
                </a-menu>
              </template>
            </a-dropdown>
          </a-badge>
          <a-button @click="handleApply" :loading="applyLoading">下发配置</a-button>
          <a-button type="primary" @click="handleSave" :loading="saveLoading">保存</a-button>
        </a-space>
      </template>
    </a-page-header>

    <a-spin :spinning="loading" style="margin-top: 16px">
      <a-collapse v-model:activeKey="activeKeys">
        <a-collapse-panel
          v-for="(values, section) in filteredConfig"
          :key="section"
          :header="sectionLabels[section] || section"
        >
          <a-form layout="vertical">
            <a-row :gutter="24">
              <a-col
                v-for="(val, key) in values"
                :key="key"
                :span="typeof val === 'object' && val !== null ? 24 : 12"
              >
                <a-form-item :label="key">
                  <a-textarea
                    v-if="typeof val === 'object' && val !== null"
                    v-model:value="filteredConfig[section][key]"
                    :rows="4"
                    style="font-family: monospace; font-size: 13px"
                  />
                  <a-input
                    v-else
                    v-model:value="filteredConfig[section][key]"
                    style="font-family: monospace"
                  />
                </a-form-item>
              </a-col>
            </a-row>
          </a-form>
        </a-collapse-panel>
      </a-collapse>
    </a-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

const loading = ref(false)
const saveLoading = ref(false)
const applyLoading = ref(false)
const activeKeys = ref<string[]>([])

const sectionLabels: Record<string, string> = {
  picoclaw: 'PicoClaw 网关',
  security: '安全设置',
  tools: '工具配置',
  model: '模型',
}

const linkedSections = [
  { key: 'web', label: 'Web / 认证配置', path: '/admin/auth' },
  { key: 'ldap', label: 'LDAP 配置', path: '/admin/auth' },
  { key: 'oidc', label: 'OIDC 配置', path: '/admin/auth' },
  { key: 'tls', label: 'HTTPS 证书', path: '/admin/tls' },
  { key: 'skills', label: '技能库', path: '/admin/skills' },
  { key: 'skill', label: '技能库', path: '/admin/skills' },
  { key: 'channel', label: '通讯渠道', path: '/admin/channels' },
]

const excludedKeys = new Set(['web', 'ldap', 'oidc', 'tls', 'skills', 'skill', 'channel'])

const filteredConfig = reactive<Record<string, any>>({})

const fetchConfig = async () => {
  loading.value = true
  try {
    const data = await api.get('/config')
    activeKeys.value = []
    for (const [key, val] of Object.entries(data)) {
      if (excludedKeys.has(key)) continue
      if (typeof val === 'object' && val !== null && !Array.isArray(val)) {
        filteredConfig[key] = { ...val as any }
        activeKeys.value.push(key)
      } else {
        if (!filteredConfig['general']) filteredConfig['general'] = {}
        filteredConfig['general'][key] = val
      }
    }
    if (filteredConfig['general']) activeKeys.value.unshift('general')
  } catch {
    message.error('获取配置失败')
  } finally {
    loading.value = false
  }
}

const collectConfig = () => {
  const cfg: Record<string, any> = {}
  for (const [section, values] of Object.entries(filteredConfig)) {
    if (section === 'general') {
      Object.assign(cfg, values)
    } else {
      cfg[section] = { ...values }
    }
  }
  return cfg
}

const handleSave = async () => {
  saveLoading.value = true
  try {
    await api.post('/config', { config: JSON.stringify(collectConfig()) })
    message.success('保存成功')
  } catch {
    message.error('保存失败')
  } finally {
    saveLoading.value = false
  }
}

const handleApply = async () => {
  applyLoading.value = true
  try {
    await api.post('/admin/config/apply')
    message.success('配置已下发')
  } catch {
    message.error('下发失败')
  } finally {
    applyLoading.value = false
  }
}

onMounted(fetchConfig)
</script>
