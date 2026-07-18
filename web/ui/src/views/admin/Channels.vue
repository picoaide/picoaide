<template>
  <div>
    <a-page-header title="通讯渠道" sub-title="管理系统通讯渠道配置" />
    <a-table :columns="columns" :data-source="channels" :loading="loading" row-key="key" style="margin-top: 24px" :pagination="false">
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'fields'">
          <a-tag v-for="f in record.fields" :key="f.key">{{ f.label || f.key }}</a-tag>
        </template>
      </template>
    </a-table>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'

const loading = ref(false)
const channels = ref<any[]>([])

const columns = [
  { title: '渠道标识', dataIndex: 'key', key: 'key' },
  { title: '名称', dataIndex: 'label', key: 'label' },
  { title: '配置字段', key: 'fields' },
]

onMounted(async () => {
  loading.value = true
  try {
    const res = await fetch('/api/admin/channels')
    const data = await res.json()
    channels.value = data.channels || []
  } catch { message.error('获取渠道列表失败') }
  finally { loading.value = false }
})
</script>
