<template>
  <div>
    <a-page-header title="概览" sub-title="系统状态总览" />
    <a-row :gutter="24" style="margin-top: 24px">
      <a-col :span="6">
        <a-card>
          <a-statistic title="用户总数" :value="stats.users" />
        </a-card>
      </a-col>
      <a-col :span="6">
        <a-card>
          <a-statistic title="在线容器" :value="stats.containers" />
        </a-card>
      </a-col>
      <a-col :span="6">
        <a-card>
          <a-statistic title="用户组" :value="stats.groups" />
        </a-card>
      </a-col>
      <a-col :span="6">
        <a-card>
          <a-statistic title="技能数" :value="stats.skills" />
        </a-card>
      </a-col>
    </a-row>
  </div>
</template>

<script setup lang="ts">
import { reactive, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

const stats = reactive({
  users: 0,
  containers: 0,
  groups: 0,
  skills: 0,
})

onMounted(async () => {
  try {
    const [usersData, groupsData, skillsData] = await Promise.all([
      api.get('/admin/users'),
      api.get('/admin/groups'),
      api.get('/admin/skills'),
    ])
    stats.users = usersData.total || 0
    stats.groups = groupsData.total || 0
    stats.skills = skillsData.total || 0
    // ponytail: no /api/admin/containers endpoint exists
    stats.containers = 0
  } catch {
    message.error('获取概况失败')
  }
})
</script>
