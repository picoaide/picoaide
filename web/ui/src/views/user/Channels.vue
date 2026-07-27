<template>
  <div>
    <a-page-header title="通讯渠道" sub-title="配置你的通讯渠道凭据" />
    <a-spin :spinning="loadingChannels">
      <a-list :data-source="channels" :grid="{ gutter: 16, column: 3 }">
        <template #renderItem="{ item }">
          <a-list-item>
            <a-card :title="item.label" size="small" hoverable @click="openConfig(item)">
              <template #extra>
                <a-tag :color="item.enabled ? 'green' : 'default'">{{ item.enabled ? '已启用' : '未启用' }}</a-tag>
                <a-tag :color="item.configured ? 'blue' : 'default'">{{ item.configured ? '已配置' : '未配置' }}</a-tag>
              </template>
              <p style="color: #666; min-height: 40px">点击配置渠道凭据</p>
            </a-card>
          </a-list-item>
        </template>
      </a-list>
      <a-empty v-if="!loadingChannels && channels.length === 0" description="暂无可用渠道，请联系管理员" />
    </a-spin>

    <a-modal
      v-model:open="showConfig"
      :title="'配置: ' + currentLabel"
      @ok="saveConfig"
      :confirm-loading="saving"
      :width="560"
    >
      <a-spin :spinning="loadingFields">
        <a-form :label-col="{ span: 6 }">
          <a-form-item v-for="field in fields" :key="field.name" :label="field.label || field.name">
            <a-input
              v-if="field.type === 'password'"
              v-model:value="formValues[field.name]"
              type="password"
              :placeholder="field.description"
            />
            <a-input-number
              v-else-if="field.type === 'number'"
              v-model:value="formValues[field.name]"
              :placeholder="field.description"
              style="width: 100%"
            />
            <a-switch
              v-else-if="field.type === 'boolean'"
              v-model:checked="formValues[field.name]"
            />
            <a-input
              v-else
              v-model:value="formValues[field.name]"
              :placeholder="field.description"
            />
          </a-form-item>
        </a-form>
        <a-form-item label="启用">
          <a-switch v-model:checked="formEnabled" />
        </a-form-item>
      </a-spin>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

interface Channel {
  key: string
  label: string
  enabled: boolean
  configured: boolean
}

interface FieldDef {
  name: string
  label: string
  type: string
  description: string
}

const loadingChannels = ref(false)
const channels = ref<Channel[]>([])
const showConfig = ref(false)
const currentKey = ref('')
const currentLabel = ref('')
const loadingFields = ref(false)
const fields = ref<FieldDef[]>([])
const formValues = ref<Record<string, any>>({})
const formEnabled = ref(false)
const saving = ref(false)

const loadChannels = async () => {
  loadingChannels.value = true
  try {
    const data = await api.get('/channels')
    channels.value = (data.channels || []).map((c: any) => ({
      key: c.key || '',
      label: c.label || '',
      enabled: !!c.enabled,
      configured: !!c.configured,
    }))
  } catch {
    message.error('网络错误')
  } finally {
    loadingChannels.value = false
  }
}

const openConfig = async (ch: Channel) => {
  currentKey.value = ch.key
  currentLabel.value = ch.label
  showConfig.value = true
  loadingFields.value = true
  fields.value = []
  formValues.value = {}
  formEnabled.value = ch.enabled
  try {
    const data = await api.get('/channels/config-fields', { section: ch.key })
    fields.value = (data.fields || []).map((f: any) => ({
      name: f.field?.key || '',
      label: f.field?.label || f.field?.key || '',
      type: f.field?.type || 'string',
      description: f.field?.hint || '',
    }))
    const vals: Record<string, any> = {}
    ;(data.fields || []).forEach((f: any) => {
      vals[f.field?.key] = f.value
    })
    formValues.value = vals
    formEnabled.value = !!data.enabled
  } catch {
    message.error('网络错误')
  } finally {
    loadingFields.value = false
  }
}

const saveConfig = async () => {
  saving.value = true
  try {
    const values = { ...formValues.value, enabled: formEnabled.value }
    await api.post('/channels/config-fields', { section: currentKey.value, values })
    message.success('保存成功')
    showConfig.value = false
    loadChannels()
  } catch {
    message.error('网络错误')
  } finally {
    saving.value = false
  }
}

onMounted(loadChannels)
</script>
