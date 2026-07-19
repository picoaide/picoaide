<template>
  <div>
    <a-page-header title="系统配置" sub-title="全局 Picoclaw 配置">
      <template #extra>
        <a-space>
          <a-button @click="handleApply" :loading="applyLoading">下发配置</a-button>
          <a-button type="primary" @click="handleSave" :loading="saveLoading">保存</a-button>
        </a-space>
      </template>
    </a-page-header>

    <a-card style="margin-top: 16px">
      <a-spin :spinning="loading">
        <a-textarea
          v-model:value="configText"
          :rows="24"
          style="font-family: monospace; font-size: 13px"
          placeholder="JSON 格式的全局配置"
        />
        <div v-if="parseError" style="color: red; margin-top: 8px">
          JSON 格式错误: {{ parseError }}
        </div>
      </a-spin>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

const configText = ref('')
const loading = ref(false)
const saveLoading = ref(false)
const applyLoading = ref(false)
const parseError = ref('')

watch(configText, (val) => {
  try {
    if (val.trim()) JSON.parse(val)
    parseError.value = ''
  } catch (e: any) {
    parseError.value = e.message
  }
})

const fetchConfig = async () => {
  loading.value = true
  try {
    const data = await api.get('/config')
    configText.value = JSON.stringify(data.config || data, null, 2)
  } catch {
    message.error('获取配置失败')
  } finally {
    loading.value = false
  }
}

const handleSave = async () => {
  if (parseError.value) {
    message.warning('请先修正 JSON 格式错误')
    return
  }
  saveLoading.value = true
  try {
    await api.post('/config', { config: JSON.parse(configText.value) })
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
