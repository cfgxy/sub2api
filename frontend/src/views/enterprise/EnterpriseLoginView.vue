<template>
  <EnterpriseAuthShell auth-layout>
    <div class="space-y-6">
      <header class="text-center">
        <p class="mb-1 text-xs font-semibold text-primary-600 dark:text-primary-400">企业登录</p>
        <h2 class="text-2xl font-bold text-gray-900 dark:text-gray-100">登录到您的工作区</h2>
        <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-dark-400">
          管理员与员工使用同一企业账户入口，继续访问已获授权的 Sub2API 服务。
        </p>
      </header>

      <p v-if="error" role="alert" class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
        {{ error }}
      </p>
      <p v-if="success" role="status" class="text-center text-sm text-primary-700 dark:text-primary-300">登录成功，正在进入工作区</p>

      <form class="space-y-5" :aria-busy="loading" @submit.prevent="submit">
        <div>
          <label for="enterprise-email" class="input-label">企业邮箱</label>
          <div class="relative">
            <Icon name="mail" size="md" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500" />
            <input id="enterprise-email" v-model="form.email" class="input pl-11" type="email" required autocomplete="username" :disabled="loading" />
          </div>
        </div>
        <div>
          <label for="enterprise-password" class="input-label">密码</label>
          <div class="relative">
            <Icon name="lock" size="md" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500" />
            <input id="enterprise-password" v-model="form.password" class="input pl-11 pr-11" :type="showPassword ? 'text' : 'password'" required autocomplete="current-password" :disabled="loading" />
            <button type="button" class="absolute inset-y-0 right-0 flex items-center px-3.5 text-gray-400 hover:text-gray-600 focus-visible:outline-primary-500 dark:hover:text-dark-300" :disabled="loading" :aria-label="showPassword ? '隐藏密码' : '显示密码'" @click="showPassword = !showPassword">
              <Icon :name="showPassword ? 'eyeOff' : 'eye'" size="md" />
            </button>
          </div>
          <div class="mt-2 text-right">
            <RouterLink to="/enterprise/forgot-password" class="text-sm font-medium text-primary-600 hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300">忘记密码</RouterLink>
          </div>
        </div>
        <button type="submit" class="btn btn-primary w-full" :disabled="loading">
          <Icon :name="loading ? 'refresh' : 'login'" size="md" :class="{ 'animate-spin': loading }" />
          {{ success ? '正在进入工作区' : loading ? '登录中' : '登录' }}
        </button>
      </form>
    </div>
  </EnterpriseAuthShell>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import EnterpriseAuthShell from '@/components/enterprise/EnterpriseAuthShell.vue'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'
import { enterpriseHome } from '@/router/enterpriseGuard'

const auth = useEnterpriseAuthStore()
const route = useRoute()
const router = useRouter()
const form = reactive({ email: '', password: '' })
const loading = ref(false)
const success = ref(false)
const showPassword = ref(false)
const error = ref('')

function loginError(failure: unknown): string {
  const reason = (failure as { reason?: string } | null)?.reason
  if (reason === 'INVALID_CREDENTIALS') return '邮箱或密码错误'
  if (reason === 'ENTERPRISE_DISABLED') return '企业账户暂不可用，请联系管理员'
  if (reason === 'ENTERPRISE_PRINCIPAL_INACTIVE') return '账户暂不可用，请联系企业管理员'
  return '登录失败，请稍后重试'
}

async function submit() {
  if (loading.value) return
  loading.value = true
  success.value = false
  error.value = ''
  try {
    const principal = await auth.login(form)
    success.value = true
    const redirect = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/enterprise/')
      ? route.query.redirect
      : enterpriseHome(principal.role)
    await router.replace(principal.force_password_change ? '/enterprise/change-password' : redirect)
  } catch (failure) {
    success.value = false
    error.value = loginError(failure)
  } finally {
    loading.value = false
  }
}
</script>
