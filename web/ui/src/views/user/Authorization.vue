<template>
  <div>
    <a-page-header title="授权管理" sub-title="管理已授权的 Cookie 域名" />
    <a-table
      :columns="columns"
      :data-source="domains"
      :loading="loading"
      row-key="domain"
      style="margin-top: 16px"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'action'">
          <a-popconfirm title="确定取消该域名的授权？" @confirm="handleDelete(record.domain)">
            <a-button type="link" size="small" danger>取消授权</a-button>
          </a-popconfirm>
        </template>
      </template>
    </a-table>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

const loading = ref(false)
const domains = ref<any[]>([])

const columns = [
  { title: '域名', dataIndex: 'domain', key: 'domain' },
  { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at' },
  { title: '操作', key: 'action', width: 120 },
]

const fetchDomains = async () => {
  loading.value = true
  try {
    const data = await api.get('/user/cookies')
    domains.value = data.list || []
  } catch {
    message.error('获取授权域名列表失败')
  } finally {
    loading.value = false
  }
}

const handleDelete = async (domain: string) => {
  try {
    const data = await api.post('/user/cookies/delete', { domain })
    if (data.success) {
      message.success('已取消授权')
      fetchDomains()
    } else {
      message.error(data.message || '取消授权失败')
    }
  } catch {
    message.error('取消授权失败')
  }
}

onMounted(fetchDomains)
</script>
