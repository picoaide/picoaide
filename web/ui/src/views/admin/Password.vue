<template>
  <div>
    <a-page-header title="修改密码" sub-title="修改超管账户密码" />
    <PasswordForm endpoint="/api/admin/password" button-text="修改密码" />
  </div>
</template>

<script setup lang="ts">
import PasswordForm from '../../components/PasswordForm.vue'
</script>
