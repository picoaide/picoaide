<template>
  <div>
    <a-page-header title="技能中心" sub-title="管理已安装的技能" />
    <div style="margin-bottom: 16px">
      <a-radio-group v-model:value="filter">
        <a-radio-button value="all">全部</a-radio-button>
        <a-radio-button value="installed">已安装</a-radio-button>
        <a-radio-button value="available">可安装</a-radio-button>
      </a-radio-group>
    </div>
    <a-spin :spinning="loading">
      <a-list :data-source="filteredSkills" :grid="{ gutter: 16, column: 3 }">
        <template #renderItem="{ item }">
          <a-list-item>
            <a-card :title="item.name" size="small">
              <template #extra>
                <a-tag :color="item.installed ? 'green' : 'default'">
                  {{ item.installed ? '已安装' : item.source === 'group' ? '组分配' : '可安装' }}
                </a-tag>
              </template>
              <p style="color: #666; min-height: 40px">{{ item.description || '暂无描述' }}</p>
              <a-button
                v-if="!item.installed"
                type="primary"
                size="small"
                :loading="item._loading"
                @click="installSkill(item)"
              >安装</a-button>
              <a-button
                v-else
                danger
                size="small"
                :loading="item._loading"
                @click="uninstallSkill(item)"
              >卸载</a-button>
            </a-card>
          </a-list-item>
        </template>
      </a-list>
      <a-empty v-if="!loading && filteredSkills.length === 0" description="暂无技能" />
    </a-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

interface Skill {
  name: string
  description: string
  installed: boolean
  source: string
  _loading: boolean
}

const loading = ref(false)
const filter = ref('all')
const skills = ref<Skill[]>([])

const filteredSkills = computed(() => {
  if (filter.value === 'installed') return skills.value.filter(s => s.installed)
  if (filter.value === 'available') return skills.value.filter(s => !s.installed)
  return skills.value
})

const loadSkills = async () => {
  loading.value = true
  try {
    const data = await api.get('/user/skills')
    skills.value = (data.skills || data || []).map((s: any) => ({
      name: s.name || '',
      description: s.description || '',
      installed: s.install_status === 'installed' || s.install_status === 'group',
      source: s.install_status || '',
      _loading: false,
    }))
  } catch {
    message.error('网络错误')
  } finally {
    loading.value = false
  }
}

const installSkill = async (skill: Skill) => {
  skill._loading = true
  try {
    await api.post('/user/skills/install', { skill_name: skill.name })
    message.success('安装成功')
    skill.installed = true
  } catch {
    message.error('网络错误')
  } finally {
    skill._loading = false
  }
}

const uninstallSkill = async (skill: Skill) => {
  skill._loading = true
  try {
    await api.post('/user/skills/uninstall', { skill_name: skill.name })
    message.success('卸载成功')
    skill.installed = false
  } catch {
    message.error('网络错误')
  } finally {
    skill._loading = false
  }
}

onMounted(loadSkills)
</script>
