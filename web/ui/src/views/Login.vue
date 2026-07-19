<template>
  <div class="login-container">
    <div class="login-card">
      <div class="login-header">
        <img src="../assets/logo.svg" alt="" class="logo" />
        <h1>PicoAide</h1>
      </div>
      <a-form
        :model="form"
        @finish="handleLogin"
        layout="vertical"
        class="login-form"
      >
        <a-form-item
          name="username"
          :rules="[{ required: true, message: '请输入用户名' }]"
        >
          <a-input
            v-model:value="form.username"
            placeholder="用户名"
            size="large"
            @pressEnter="handleLogin"
          >
            <template #prefix><UserOutlined /></template>
          </a-input>
        </a-form-item>
        <a-form-item
          name="password"
          :rules="[{ required: true, message: '请输入密码' }]"
        >
          <a-input-password
            v-model:value="form.password"
            placeholder="密码"
            size="large"
            @pressEnter="handleLogin"
          >
            <template #prefix><LockOutlined /></template>
          </a-input-password>
        </a-form-item>
        <a-form-item>
          <a-button
            type="primary"
            html-type="submit"
            size="large"
            block
            :loading="loading"
          >
            登录
          </a-button>
        </a-form-item>
      </a-form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { UserOutlined, LockOutlined } from '@ant-design/icons-vue'

const router = useRouter()
const loading = ref(false)
const form = reactive({
  username: '',
  password: '',
})

const handleLogin = async () => {
  if (!form.username || !form.password) return
  loading.value = true
  try {
    const res = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form),
    })
    const data = await res.json()
    if (res.ok) {
      localStorage.setItem('session', '1')
      localStorage.setItem('username', form.username)
      const infoRes = await fetch('/api/user/info')
      const role = infoRes.ok ? (await infoRes.json()).role : 'user'
      localStorage.setItem('role', role || 'user')
      message.success('登录成功')
      router.push(role === 'superadmin' ? '/admin' : '/user/chat')
    } else {
      message.error(data.message || '登录失败')
    }
  } catch {
    message.error('网络错误')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-container {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}
.login-card {
  width: 400px;
  padding: 40px;
  background: #fff;
  border-radius: 12px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.15);
}
.login-header {
  text-align: center;
  margin-bottom: 32px;
}
.logo {
  width: 48px;
  height: 48px;
  margin-bottom: 12px;
}
.login-header h1 {
  font-size: 24px;
  font-weight: 600;
  color: #333;
  margin: 0;
}
.login-form {
  margin-top: 24px;
}
</style>
