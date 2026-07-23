<template>
  <div>
    <a-page-header title="渠道配置" sub-title="配置你的渠道" />
    <a-spin :spinning="loadingChannels">
      <a-list :data-source="channels" :grid="{ gutter: 16, column: 3 }">
        <template #renderItem="{ item }">
          <a-list-item>
            <a-card :title="item.name || item.channel" size="small" hoverable @click="openConfig(item)">
              <template #extra>
                <a-tag :color="item.enabled ? 'green' : 'default'">{{ item.enabled ? '已启用' : '未启用' }}</a-tag>
                <a-tag :color="item.configured ? 'blue' : 'default'">{{ item.configured ? '已配置' : '未配置' }}</a-tag>
              </template>
              <p style="color: #666">{{ item.description || '点击配置' }}</p>
            </a-card>
          </a-list-item>
        </template>
      </a-list>
      <a-empty v-if="!loadingChannels && channels.length === 0" description="暂无渠道" />
    </a-spin>

    <a-modal
      v-model:open="showConfig"
      :title="'配置: ' + currentChannel"
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
          <a-empty v-if="!loadingFields && fields.length === 0" description="无配置项" />
        </a-form>
      </a-spin>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

interface Channel {
  channel: string
  name: string
  description: string
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
const currentChannel = ref('')
const loadingFields = ref(false)
const fields = ref<FieldDef[]>([])
const formValues = ref<Record<string, any>>({})
const saving = ref(false)

const loadChannels = async () => {
  loadingChannels.value = true
  try {
    const data = await api.get('/channels')
    channels.value = (data.channels || data || []).map((c: any) => ({
      channel: c.channel || c.name || '',
      name: c.name || c.channel || '',
      description: c.description || '',
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
  currentChannel.value = ch.channel
  showConfig.value = true
  loadingFields.value = true
  fields.value = []
  formValues.value = {}
  try {
    const data = await api.get('/channels/config-fields', { section: ch.channel })
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
  } catch {
    message.error('网络错误')
  } finally {
    loadingFields.value = false
  }
}

const saveConfig = async () => {
  saving.value = true
  try {
    await api.post('/channels/config-fields', { section: currentChannel.value, values: formValues.value })
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
