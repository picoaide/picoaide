<template>
  <div>
    <a-page-header title="模型配置" sub-title="配置 AI 模型参数并测试连接">
      <template #extra>
        <a-button type="primary" :loading="saving" @click="handleSave">保存配置</a-button>
      </template>
    </a-page-header>

    <a-spin :spinning="loading">
      <a-card title="模型参数" style="margin-top: 16px">
        <a-form layout="vertical" style="max-width: 600px">
          <a-form-item label="提供商">
            <a-select v-model:value="form.provider">
              <a-select-option value="openai">OpenAI</a-select-option>
              <a-select-option value="anthropic">Anthropic</a-select-option>
              <a-select-option value="deepseek">DeepSeek</a-select-option>
              <a-select-option value="qwen">通义千问</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="模型 ID">
            <a-input v-model:value="form.model_id" placeholder="如 deepseek-v4-flash" />
          </a-form-item>
          <a-form-item label="Base URL">
            <a-input v-model:value="form.base_url" placeholder="https://api.deepseek.com" />
          </a-form-item>
          <a-form-item label="API Key">
            <a-input-password v-model:value="form.api_key" placeholder="sk-..." />
          </a-form-item>
          <a-row :gutter="24">
            <a-col :span="8">
              <a-form-item label="最大 Token">
                <a-input-number v-model:value="form.max_tokens" :min="0" style="width: 100%" />
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item label="上下文窗口">
                <a-input-number v-model:value="form.context_window" :min="0" style="width: 100%" />
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item label="最大迭代">
                <a-input-number v-model:value="form.max_iter" :min="1" style="width: 100%" />
              </a-form-item>
            </a-col>
          </a-row>
          <a-row :gutter="24">
            <a-col :span="8">
              <a-form-item label="温度">
                <a-input-number v-model:value="form.temperature" :min="0" :max="2" :step="0.1" style="width: 100%" />
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item label="超时时间 (秒)">
                <a-input-number v-model:value="form.request_timeout" :min="10" style="width: 100%" />
              </a-form-item>
            </a-col>
          </a-row>
          <a-form-item>
            <a-button type="default" @click="handleTest" :loading="testing">测试连接</a-button>
          </a-form-item>
        </a-form>
      </a-card>
    </a-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

const loading = ref(false)
const saving = ref(false)
const testing = ref(false)

const form = reactive({
  provider: 'deepseek',
  model_id: '',
  base_url: '',
  api_key: '',
  max_tokens: 0,
  context_window: 35000,
  max_iter: 500,
  temperature: 0.7,
  request_timeout: 600,
})

const fetchConfig = async () => {
  loading.value = true
  try {
    const cfg = await api.get('/config')
    const m = cfg.model || {}
    form.provider = m.provider || 'deepseek'
    form.model_id = m.model_id || ''
    form.base_url = m.base_url || ''
    form.api_key = m.api_key || ''
    form.max_tokens = m.max_tokens ?? 0
    form.context_window = m.context_window ?? 35000
    form.max_iter = m.max_iter ?? 500
    form.temperature = m.temperature ?? 0.7
    form.request_timeout = m.request_timeout ?? 600
  } catch {
    message.error('获取配置失败')
  } finally {
    loading.value = false
  }
}

const handleSave = async () => {
  if (!form.model_id || !form.api_key) {
    message.warning('请填写模型 ID 和 API Key')
    return
  }
  saving.value = true
  try {
    await api.post('/config', {
      config: JSON.stringify({
        model: {
          provider: form.provider,
          model_id: form.model_id,
          base_url: form.base_url,
          api_key: form.api_key,
          max_tokens: form.max_tokens,
          context_window: form.context_window,
          max_iter: form.max_iter,
          temperature: form.temperature,
          request_timeout: form.request_timeout,
        },
      }),
    })
    message.success('保存成功')
  } catch {
    message.error('保存失败')
  } finally {
    saving.value = false
  }
}

const handleTest = async () => {
  if (!form.model_id || !form.api_key) {
    message.warning('请填写模型 ID 和 API Key')
    return
  }
  testing.value = true
  try {
    const data = await api.post('/admin/model/test', {
      provider: form.provider,
      model_id: form.model_id,
      base_url: form.base_url,
      api_key: form.api_key,
    })
    if (data.success) {
      message.success('连接成功')
    } else {
      message.error(data.error || data.message || '连接失败')
    }
  } catch (e: any) {
    message.error(e.message || '请求失败')
  } finally {
    testing.value = false
  }
}

onMounted(fetchConfig)
</script>
