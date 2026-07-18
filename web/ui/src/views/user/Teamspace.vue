<template>
  <div>
    <a-page-header title="团队空间" sub-title="可访问的共享文件夹" />
    <a-spin :spinning="loading">
      <a-list
        :data-source="folders"
        :grid="{ gutter: 16, column: 2 }"
        style="margin-top: 16px"
      >
        <template #renderItem="{ item }">
          <a-list-item>
            <a-card>
              <template #title>
                <div style="display: flex; align-items: center; gap: 8px">
                  <span>{{ item.name }}</span>
                  <a-tag v-if="item.is_public" color="green">公开</a-tag>
                </div>
              </template>
              <p>{{ item.description || '暂无描述' }}</p>
              <p style="color: #999; font-size: 12px; margin: 0">
                成员数: {{ item.member_count }}
              </p>
            </a-card>
          </a-list-item>
        </template>
      </a-list>
      <a-empty v-if="!loading && folders.length === 0" description="暂无可访问的共享文件夹" />
    </a-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'

const loading = ref(false)
const folders = ref<any[]>([])

const fetchFolders = async () => {
  loading.value = true
  try {
    const res = await fetch('/api/shared-folders')
    const data = await res.json()
    folders.value = data.folders || []
  } catch {
    message.error('获取共享文件夹列表失败')
  } finally {
    loading.value = false
  }
}

onMounted(fetchFolders)
</script>
