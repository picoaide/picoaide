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

const stats = reactive({
  users: 0,
  containers: 0,
  groups: 0,
  skills: 0,
})

onMounted(async () => {
  try {
    const [usersRes, containersRes, groupsRes, skillsRes] = await Promise.all([
      fetch('/api/admin/users'),
      fetch('/api/admin/containers'),
      fetch('/api/admin/groups'),
      fetch('/api/admin/skills'),
    ])
    const [usersData, containersData, groupsData, skillsData] = await Promise.all([
      usersRes.json(),
      containersRes.json(),
      groupsRes.json(),
      skillsRes.json(),
    ])
    stats.users = usersData.total || 0
    stats.containers = containersData.filter?.((c: any) => c.status === 'running').length || 0
    stats.groups = groupsData.total || 0
    stats.skills = skillsData.total || 0
  } catch {
    // ignore
  }
})
</script>
