<template>
  <div>
    <a-page-header title="TLS 证书" sub-title="管理 HTTPS 证书" />

    <a-card title="证书状态" style="margin-top: 16px" :loading="statusLoading">
      <a-descriptions :column="2" bordered size="small">
        <a-descriptions-item label="HTTPS 启用">
          <a-tag :color="status.enabled ? 'green' : 'default'">{{ status.enabled ? '是' : '否' }}</a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="已配置证书">
          <a-tag :color="status.has_cert ? 'green' : 'default'">{{ status.has_cert ? '是' : '否' }}</a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="证书有效">
          <a-tag :color="status.valid ? 'green' : 'red'">{{ status.valid ? '是' : '否' }}</a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="已过期">
          <a-tag :color="status.expired ? 'red' : 'green'">{{ status.expired ? '是' : '否' }}</a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="主题" :span="2">{{ status.subject || '-' }}</a-descriptions-item>
        <a-descriptions-item label="SAN" :span="2">{{ status.sans?.join(', ') || '-' }}</a-descriptions-item>
        <a-descriptions-item label="签发者">{{ status.issuer || '-' }}</a-descriptions-item>
        <a-descriptions-item label="主机匹配">
          <a-tag :color="status.host_match ? 'green' : 'orange'">{{ status.host_match ? '匹配' : '不匹配' }}</a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="生效时间">{{ status.not_before || '-' }}</a-descriptions-item>
        <a-descriptions-item label="过期时间">{{ status.not_after || '-' }}</a-descriptions-item>
      </a-descriptions>
    </a-card>

    <a-card title="HTTPS 开关" style="margin-top: 16px">
      <a-space>
        <span>启用 HTTPS：</span>
        <a-switch :checked="status.enabled" @change="handleToggle" :loading="toggleLoading" />
      </a-space>
    </a-card>

    <a-card title="上传证书" style="margin-top: 16px">
      <a-form layout="vertical">
        <a-form-item label="证书 PEM">
          <a-textarea v-model:value="certPem" :rows="6" placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----" />
        </a-form-item>
        <a-form-item label="私钥 PEM">
          <a-textarea v-model:value="keyPem" :rows="6" placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----" />
        </a-form-item>
        <a-form-item>
          <a-space>
            <a-button @click="handleVerify" :loading="verifyLoading">验证证书</a-button>
            <a-button type="primary" @click="handleSave" :loading="saveLoading">保存并启用</a-button>
            <a-button danger @click="handleClear" :loading="clearLoading">清除证书</a-button>
          </a-space>
        </a-form-item>
      </a-form>
      <a-descriptions v-if="verifyResult" title="验证结果" :column="2" bordered size="small" style="margin-top: 16px">
        <a-descriptions-item label="有效">
          <a-tag :color="verifyResult.valid ? 'green' : 'red'">{{ verifyResult.valid ? '是' : '否' }}</a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="主题">{{ verifyResult.subject || '-' }}</a-descriptions-item>
        <a-descriptions-item label="SAN">{{ verifyResult.sans?.join(', ') || '-' }}</a-descriptions-item>
        <a-descriptions-item label="过期时间">{{ verifyResult.not_after || '-' }}</a-descriptions-item>
        <a-descriptions-item v-if="verifyResult.error" label="错误" :span="2">
          <span style="color: red">{{ verifyResult.error }}</span>
        </a-descriptions-item>
      </a-descriptions>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { message, Modal } from 'ant-design-vue'
import { api } from '../../composables/api'

const statusLoading = ref(false)
const status = reactive({
  enabled: false,
  has_cert: false,
  valid: false,
  subject: '',
  sans: [] as string[],
  issuer: '',
  not_before: '',
  not_after: '',
  expired: false,
  host_match: false,
})

const fetchStatus = async () => {
  statusLoading.value = true
  try {
    const data = await api.get('/admin/tls/status')
    Object.assign(status, data.data)
  } catch {
    message.error('获取证书状态失败')
  } finally {
    statusLoading.value = false
  }
}

const certPem = ref('')
const keyPem = ref('')
const verifyLoading = ref(false)
const saveLoading = ref(false)
const clearLoading = ref(false)
const toggleLoading = ref(false)
const verifyResult = ref<any>(null)

const handleVerify = async () => {
  if (!certPem.value || !keyPem.value) {
    message.warning('请填写证书和私钥')
    return
  }
  verifyLoading.value = true
  verifyResult.value = null
  try {
    const data = await api.post('/admin/tls/verify', { cert_pem: certPem.value, key_pem: keyPem.value })
    verifyResult.value = data.data
  } catch {
    message.error('验证请求失败')
  } finally {
    verifyLoading.value = false
  }
}

const handleSave = async () => {
  if (!certPem.value || !keyPem.value) {
    message.warning('请填写证书和私钥')
    return
  }
  saveLoading.value = true
  try {
    await api.post('/admin/tls/save', { cert_pem: certPem.value, key_pem: keyPem.value, enabled: true })
    message.success('保存成功')
    certPem.value = ''
    keyPem.value = ''
    verifyResult.value = null
    fetchStatus()
  } catch {
    message.error('保存失败')
  } finally {
    saveLoading.value = false
  }
}

const handleToggle = async (checked: boolean) => {
  toggleLoading.value = true
  try {
    await api.post('/admin/tls/toggle', { enabled: checked })
    message.success(checked ? '已启用 HTTPS' : '已禁用 HTTPS')
    fetchStatus()
  } catch {
    message.error('操作失败')
  } finally {
    toggleLoading.value = false
  }
}

const handleClear = () => {
  Modal.confirm({
    title: '确认清除',
    content: '确定要清除当前证书吗？此操作不可恢复。',
    okText: '确认',
    cancelText: '取消',
    onOk: async () => {
      clearLoading.value = true
      try {
        await api.post('/admin/tls/clear')
        message.success('证书已清除')
        fetchStatus()
      } catch {
        message.error('清除失败')
      } finally {
        clearLoading.value = false
      }
    },
  })
}

onMounted(fetchStatus)
</script>
