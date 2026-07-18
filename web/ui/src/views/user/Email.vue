<template>
  <div>
    <a-page-header title="邮箱配置" sub-title="配置你的邮箱账户" />
    <a-card style="max-width: 600px">
      <a-form :model="form" :label-col="{ span: 6 }" :wrapper-col="{ span: 16 }">
        <a-form-item label="邮箱地址">
          <a-input v-model:value="form.email" placeholder="user@example.com" />
        </a-form-item>
        <a-divider>SMTP 发信</a-divider>
        <a-form-item label="SMTP 主机">
          <a-input v-model:value="form.smtpHost" placeholder="smtp.example.com" />
        </a-form-item>
        <a-form-item label="SMTP 端口">
          <a-input-number v-model:value="form.smtpPort" :min="1" :max="65535" style="width: 100%" />
        </a-form-item>
        <a-form-item label="SMTP TLS">
          <a-switch v-model:checked="form.smtpTls" />
        </a-form-item>
        <a-divider>IMAP 收信</a-divider>
        <a-form-item label="IMAP 主机">
          <a-input v-model:value="form.imapHost" placeholder="imap.example.com" />
        </a-form-item>
        <a-form-item label="IMAP 端口">
          <a-input-number v-model:value="form.imapPort" :min="1" :max="65535" style="width: 100%" />
        </a-form-item>
        <a-form-item label="IMAP TLS">
          <a-switch v-model:checked="form.imapTls" />
        </a-form-item>
        <a-divider>登录凭据</a-divider>
        <a-form-item label="用户名">
          <a-input v-model:value="form.loginUser" />
        </a-form-item>
        <a-form-item label="密码">
          <a-input-password v-model:value="form.loginPassword" />
        </a-form-item>
        <a-form-item :wrapper-col="{ offset: 6 }">
          <a-space>
            <a-button type="primary" :loading="saving" @click="saveEmail">保存</a-button>
            <a-button :loading="testing" @click="testEmail">测试</a-button>
            <a-popconfirm title="确定删除邮箱配置？" @confirm="deleteEmail">
              <a-button danger :loading="deleting">删除</a-button>
            </a-popconfirm>
          </a-space>
        </a-form-item>
      </a-form>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'

const form = ref({
  email: '',
  smtpHost: '',
  smtpPort: 465,
  smtpTls: true,
  imapHost: '',
  imapPort: 993,
  imapTls: true,
  loginUser: '',
  loginPassword: '',
})

const saving = ref(false)
const testing = ref(false)
const deleting = ref(false)

const loadEmail = async () => {
  try {
    const data = await api.get('/user/email')
    if (data.email) Object.assign(form.value, data)
  } catch { /* ignore */ }
}

const buildParams = () => {
  const params: Record<string, string> = {}
  for (const [k, v] of Object.entries(form.value)) {
    params[k] = String(v)
  }
  return params
}

const saveEmail = async () => {
  saving.value = true
  try {
    await api.post('/user/email', buildParams())
    message.success('保存成功')
  } catch {
    message.error('网络错误')
  } finally {
    saving.value = false
  }
}

const testEmail = async () => {
  testing.value = true
  try {
    await api.post('/user/email/test', buildParams())
    message.success('测试成功')
  } catch {
    message.error('网络错误')
  } finally {
    testing.value = false
  }
}

const deleteEmail = async () => {
  deleting.value = true
  try {
    await api.post('/user/email/delete')
    message.success('删除成功')
    form.value = { email: '', smtpHost: '', smtpPort: 465, smtpTls: true, imapHost: '', imapPort: 993, imapTls: true, loginUser: '', loginPassword: '' }
  } catch {
    message.error('网络错误')
  } finally {
    deleting.value = false
  }
}

onMounted(loadEmail)
</script>
