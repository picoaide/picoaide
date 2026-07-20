<template>
  <div>
    <a-page-header title="系统配置" sub-title="管理没有独立页面的全局配置项">
      <template #extra>
        <a-space>
          <a-dropdown>
            <a-button>已有配置页面</a-button>
            <template #overlay>
              <a-menu>
                <a-menu-item v-for="s in linkedSections" :key="s.path" @click="router.push(s.path)">
                  {{ s.label }}
                </a-menu-item>
              </a-menu>
            </template>
          </a-dropdown>
          <a-button @click="handleApply" :loading="applyLoading">下发配置</a-button>
          <a-button type="primary" @click="handleSave" :loading="saveLoading">保存</a-button>
        </a-space>
      </template>
    </a-page-header>

    <a-spin :spinning="loading" style="margin-top: 16px">
      <a-collapse v-model:activeKey="activeKeys">
        <a-collapse-panel
          v-for="(values, section) in displayConfig"
          :key="section"
          :header="sectionLabels[section] || section"
        >
          <a-form layout="vertical">
            <a-row :gutter="24">
              <a-col v-for="(item, key) in values" :key="key" :span="item.isObj ? 24 : 12">
                <a-form-item :label="key">
                  <a-textarea
                    v-if="item.isObj"
                    v-model:value="item.text"
                    :rows="4"
                    style="font-family: monospace; font-size: 13px"
                  />
                  <a-input
                    v-else
                    v-model:value="item.text"
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
import { useRouter } from 'vue-router'
import { api } from '../../composables/api'

const router = useRouter()

const loading = ref(false)
const saveLoading = ref(false)
const applyLoading = ref(false)
const activeKeys = ref<string[]>([])

const sectionLabels: Record<string, string> = {
  picoclaw: 'PicoClaw 网关',
  security: '安全设置',
  tools: '工具配置',
  general: '通用',
}

const linkedSections = [
  { key: 'auth', label: '认证配置 (LDAP / OIDC)', path: '/admin/auth' },
  { key: 'model', label: '模型配置', path: '/admin/models' },
  { key: 'tls', label: 'HTTPS 证书', path: '/admin/tls' },
  { key: 'skills', label: '技能库', path: '/admin/skills' },
  { key: 'channel', label: '通讯渠道', path: '/admin/channels' },
]

const excludedKeys = new Set(['ldap', 'oidc', 'web', 'model', 'tls', 'skills', 'channel'])

interface DisplayItem {
  text: string
  isObj: boolean
}

const displayConfig = reactive<Record<string, Record<string, DisplayItem>>>({})

const formatValue = (v: any): string => {
  if (v === null || v === undefined) return ''
  if (typeof v === 'object') return JSON.stringify(v, null, 2)
  return String(v)
}

const fetchConfig = async () => {
  loading.value = true
  try {
    const data = await api.get('/config')
    activeKeys.value = []
    for (const [key, val] of Object.entries(data)) {
      if (excludedKeys.has(key)) continue
      const section = typeof val === 'object' && val !== null && !Array.isArray(val) ? key : 'general'
      if (!displayConfig[section]) {
        displayConfig[section] = {}
        activeKeys.value.push(section)
      }
      if (section === 'general') {
        displayConfig['general'][key] = { text: formatValue(val), isObj: typeof val === 'object' && val !== null }
      } else {
        for (const [k, v] of Object.entries(val as Record<string, any>)) {
          displayConfig[section][k] = { text: formatValue(v), isObj: typeof v === 'object' && v !== null }
        }
      }
    }
  } catch {
    message.error('获取配置失败')
  } finally {
    loading.value = false
  }
}

const parseValue = (item: DisplayItem): any => {
  if (!item.text.trim()) return ''
  if (item.isObj) {
    try { return JSON.parse(item.text) } catch { message.warning('JSON 格式无效，将按字符串保存'); return item.text }
  }
  return item.text
}

const collectConfig = () => {
  const cfg: Record<string, any> = {}
  for (const [section, items] of Object.entries(displayConfig)) {
    if (section === 'general') {
      for (const [key, item] of Object.entries(items)) {
        cfg[key] = parseValue(item)
      }
    } else {
      cfg[section] = {}
      for (const [key, item] of Object.entries(items)) {
        cfg[section][key] = parseValue(item)
      }
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
