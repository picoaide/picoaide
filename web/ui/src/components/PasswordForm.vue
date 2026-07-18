<template>
  <a-card style="max-width: 480px">
    <a-form layout="vertical" @finish="handleSubmit" :model="form">
      <a-form-item label="当前密码" name="old_password" :rules="[{ required: true, message: '请输入当前密码' }]">
        <a-input-password v-model:value="form.old_password" />
      </a-form-item>
      <a-form-item label="新密码" name="new_password" :rules="[{ required: true, min: 6, message: '新密码至少 6 位' }]">
        <a-input-password v-model:value="form.new_password" />
      </a-form-item>
      <a-form-item label="确认新密码" name="confirm" :rules="[{ required: true, validator: validateConfirm }]">
        <a-input-password v-model:value="form.confirm" />
      </a-form-item>
      <a-form-item>
        <a-button type="primary" html-type="submit" :loading="loading">{{ buttonText }}</a-button>
      </a-form-item>
    </a-form>
  </a-card>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../composables/api'

const props = withDefaults(defineProps<{
  endpoint: string
  buttonText?: string
}>(), {
  buttonText: '修改密码',
})

const loading = ref(false)
const form = reactive({ old_password: '', new_password: '', confirm: '' })

const validateConfirm = (_rule: any, value: string) => {
  if (value !== form.new_password) return Promise.reject('两次密码不一致')
  return Promise.resolve()
}

const handleSubmit = async () => {
  loading.value = true
  try {
    const data = await api.post(props.endpoint, {
      old_password: form.old_password,
      new_password: form.new_password,
    })
    if (data.success !== undefined && !data.success) {
      message.error(data.error || '修改失败')
      return
    }
    message.success('密码修改成功')
    form.old_password = ''
    form.new_password = ''
    form.confirm = ''
  } catch (e: any) {
    try {
      const err = JSON.parse(e.message)
      message.error(err.error || err.message || '修改失败')
    } catch {
      message.error('网络错误')
    }
  } finally {
    loading.value = false
  }
}
</script>
