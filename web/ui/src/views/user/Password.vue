<template>
  <div>
    <a-page-header title="修改密码" sub-title="修改你的登录密码" />
    <PasswordForm endpoint="/api/user/password" button-text="保存" />
  </div>
</template>

<script setup lang="ts">
import PasswordForm from '../../components/PasswordForm.vue'
</script>
