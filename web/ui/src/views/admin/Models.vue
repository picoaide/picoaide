<template>
  <div>
    <a-page-header title="模型配置" sub-title="配置和测试 AI 模型" />

    <a-card title="模型测试" style="margin-top: 16px">
      <a-form layout="vertical" style="max-width: 600px">
        <a-form-item label="提供商">
          <a-select v-model:value="form.provider" placeholder="选择提供商">
            <a-select-option value="openai">OpenAI</a-select-option>
            <a-select-option value="anthropic">Anthropic</a-select-option>
            <a-select-option value="deepseek">DeepSeek</a-select-option>
            <a-select-option value="qwen">通义千问</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="模型 ID">
          <a-input v-model:value="form.model_id" placeholder="如 gpt-4o, claude-3-5-sonnet-20241022" />
        </a-form-item>
        <a-form-item label="Base URL">
          <a-input v-model:value="form.base_url" placeholder="https://api.openai.com/v1" />
        </a-form-item>
        <a-form-item label="API Key">
          <a-input-password v-model:value="form.api_key" placeholder="sk-..." />
        </a-form-item>
        <a-form-item>
          <a-button type="primary" @click="handleTest" :loading="testing">测试连接</a-button>
        </a-form-item>
      </a-form>

      <a-result v-if="testResult !== null" :status="testResult ? 'success' : 'error'" :title="testResult ? '连接成功' : '连接失败'" style="margin-top: 16px">
        <template #extra v-if="testError">
          <a-alert type="error" :message="testError" show-icon />
        </template>
      </a-result>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

const form = reactive({
  provider: 'openai',
  model_id: '',
  base_url: '',
  api_key: '',
})

const testing = ref(false)
const testResult = ref<boolean | null>(null)
const testError = ref('')

const handleTest = async () => {
  if (!form.model_id || !form.api_key) {
    message.warning('请填写模型 ID 和 API Key')
    return
  }
  testing.value = true
  testResult.value = null
  testError.value = ''
  try {
    const params: Record<string, string> = { provider: form.provider, model_id: form.model_id }
    if (form.base_url) params.base_url = form.base_url
    if (form.api_key) params.api_key = form.api_key
    const data = await api.post('/admin/model/test', params)
    if (data.success) {
      testResult.value = true
    } else {
      testResult.value = false
      testError.value = data.error || data.message || '连接失败'
    }
  } catch {
    testResult.value = false
    testError.value = '请求失败'
  } finally {
    testing.value = false
  }
}
</script>
