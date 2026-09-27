<template>
  <EnterpriseAuthShell auth-layout>
    <div class="space-y-6">
      <header class="text-center">
        <div class="mx-auto mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
          <Icon name="mail" size="md" />
        </div>
        <h2 class="text-2xl font-bold text-gray-900 dark:text-gray-100">找回密码</h2>
        <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-dark-400">重置链接将在一小时内有效。</p>
      </header>

      <div v-if="submitted" class="space-y-5">
        <div class="rounded-md border border-green-200 bg-green-50 p-4 dark:border-green-800/50 dark:bg-green-900/20">
          <div class="flex items-start gap-3">
            <Icon name="checkCircle" size="lg" class="shrink-0 text-green-600 dark:text-green-400" />
            <div>
              <h3 class="font-semibold text-green-800 dark:text-green-200">请求已提交</h3>
              <p class="mt-1 text-sm leading-6 text-green-700 dark:text-green-300">如果账号存在，重置邮件会发送到该邮箱。</p>
            </div>
          </div>
        </div>
        <RouterLink to="/enterprise/login" class="btn btn-primary flex w-full items-center justify-center gap-2">
          <Icon name="arrowLeft" size="md" />
          返回登录
        </RouterLink>
      </div>

      <form v-else class="space-y-5" :aria-busy="loading" @submit.prevent="submit">
        <p v-if="error" role="alert" class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
          {{ error }}
        </p>
        <div>
          <label for="enterprise-forgot-email" class="input-label">企业邮箱</label>
          <div class="relative">
            <Icon name="mail" size="md" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500" />
            <input
              id="enterprise-forgot-email"
              v-model="email"
              type="email"
              required
              autofocus
              autocomplete="email"
              class="input pl-11"
              :class="{ 'input-error': emailError }"
              :disabled="loading"
              @blur="validateEmail"
            />
          </div>
          <p v-if="emailError" class="mt-1 text-xs text-red-600 dark:text-red-400">{{ emailError }}</p>
        </div>
        <button type="submit" class="btn btn-primary flex w-full items-center justify-center gap-2" :disabled="loading">
          <Icon :name="loading ? 'refresh' : 'mail'" size="md" :class="{ 'animate-spin': loading }" />
          {{ loading ? '发送中' : '发送重置邮件' }}
        </button>
      </form>
    </div>

    <template #footer>
      <p class="text-gray-500 dark:text-dark-400">
        记起密码了？
        <RouterLink to="/enterprise/login" class="font-medium text-primary-600 hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300">返回登录</RouterLink>
      </p>
    </template>
  </EnterpriseAuthShell>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import EnterpriseAuthShell from '@/components/enterprise/EnterpriseAuthShell.vue'
import { enterpriseAPI } from '@/api/enterprise'

const email = ref('')
const emailError = ref('')
const error = ref('')
const loading = ref(false)
const submitted = ref(false)

function validateEmail(): boolean {
  emailError.value = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.value.trim()) ? '' : '请输入有效邮箱'
  return !emailError.value
}

async function submit() {
  if (loading.value || !validateEmail()) return
  loading.value = true
  error.value = ''
  try {
    await enterpriseAPI.forgotPassword(email.value.trim())
    submitted.value = true
  } catch {
    error.value = '请求未能完成，请稍后重试'
  } finally {
    loading.value = false
  }
}
</script>
