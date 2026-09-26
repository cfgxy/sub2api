<template>
  <EnterpriseAuthShell auth-layout>
    <div class="space-y-6">
      <header class="text-center">
        <div class="mx-auto mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
          <Icon name="key" size="md" />
        </div>
        <h2 class="text-2xl font-bold text-gray-900 dark:text-gray-100">设置新的登录密码</h2>
        <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-dark-400">设置完成后可使用新密码重新登录企业工作区。</p>
      </header>

      <div v-if="!token" class="space-y-5">
        <div class="rounded-md border border-amber-200 bg-amber-50 p-4 dark:border-amber-800/50 dark:bg-amber-900/20">
          <div class="flex items-start gap-3">
            <Icon name="exclamationCircle" size="lg" class="shrink-0 text-amber-600 dark:text-amber-400" />
            <div>
              <h3 class="font-semibold text-amber-800 dark:text-amber-200">重置链接缺少令牌</h3>
              <p class="mt-1 text-sm leading-6 text-amber-700 dark:text-amber-300">请使用邮件中的完整链接，或重新申请密码重置指引。</p>
            </div>
          </div>
        </div>
        <RouterLink to="/enterprise/forgot-password" class="btn btn-primary flex w-full items-center justify-center gap-2">
          <Icon name="arrowLeft" size="md" />
          重新申请
        </RouterLink>
      </div>

      <div v-else-if="success" class="space-y-5">
        <div class="rounded-md border border-green-200 bg-green-50 p-4 dark:border-green-800/50 dark:bg-green-900/20">
          <div class="flex items-start gap-3">
            <Icon name="checkCircle" size="lg" class="shrink-0 text-green-600 dark:text-green-400" />
            <div>
              <h3 class="font-semibold text-green-800 dark:text-green-200">密码已重置</h3>
              <p class="mt-1 text-sm leading-6 text-green-700 dark:text-green-300">请使用新密码登录企业工作区。</p>
            </div>
          </div>
        </div>
        <RouterLink to="/enterprise/login" class="btn btn-primary flex w-full items-center justify-center gap-2">
          <Icon name="login" size="md" />
          返回登录
        </RouterLink>
      </div>

      <form v-else class="space-y-5" :aria-busy="loading" @submit.prevent="submit">
        <p v-if="error" role="alert" class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
          {{ error }}
        </p>
        <div>
          <label for="enterprise-reset-password" class="input-label">新密码</label>
          <div class="relative">
            <Icon name="lock" size="md" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500" />
            <input id="enterprise-reset-password" v-model="password" :type="showPassword ? 'text' : 'password'" required autocomplete="new-password" class="input pl-11 pr-11" :class="{ 'input-error': passwordError }" :disabled="loading" @blur="validateForm" />
            <button type="button" class="absolute inset-y-0 right-0 flex items-center px-3.5 text-gray-400 hover:text-gray-600 dark:hover:text-dark-300" :disabled="loading" :aria-label="showPassword ? '隐藏密码' : '显示密码'" @click="showPassword = !showPassword"><Icon :name="showPassword ? 'eyeOff' : 'eye'" size="md" /></button>
          </div>
          <p v-if="passwordError" class="mt-1 text-xs text-red-600 dark:text-red-400">{{ passwordError }}</p>
        </div>
        <div>
          <label for="enterprise-reset-confirm" class="input-label">确认新密码</label>
          <div class="relative">
            <Icon name="lock" size="md" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500" />
            <input id="enterprise-reset-confirm" v-model="confirmPassword" :type="showConfirmPassword ? 'text' : 'password'" required autocomplete="new-password" class="input pl-11 pr-11" :class="{ 'input-error': confirmError }" :disabled="loading" @blur="validateForm" />
            <button type="button" class="absolute inset-y-0 right-0 flex items-center px-3.5 text-gray-400 hover:text-gray-600 dark:hover:text-dark-300" :disabled="loading" :aria-label="showConfirmPassword ? '隐藏密码' : '显示密码'" @click="showConfirmPassword = !showConfirmPassword"><Icon :name="showConfirmPassword ? 'eyeOff' : 'eye'" size="md" /></button>
          </div>
          <p v-if="confirmError" class="mt-1 text-xs text-red-600 dark:text-red-400">{{ confirmError }}</p>
        </div>
        <div class="rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm leading-6 text-gray-600 dark:border-dark-700 dark:bg-dark-700/60 dark:text-dark-300">
          密码至少需要 8 个字符，不限制字符组成。
        </div>
        <button type="submit" class="btn btn-primary flex w-full items-center justify-center gap-2" :disabled="loading">
          <Icon :name="loading ? 'refresh' : 'checkCircle'" size="md" :class="{ 'animate-spin': loading }" />
          {{ loading ? '重置中' : '确认重置' }}
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
import { computed, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import EnterpriseAuthShell from '@/components/enterprise/EnterpriseAuthShell.vue'
import { enterpriseAPI } from '@/api/enterprise'
import { validatePassword } from '@/features/enterprise/validation'

const route = useRoute()
const router = useRouter()
const token = computed(() => typeof route.query.token === 'string' ? route.query.token : '')
const password = ref('')
const confirmPassword = ref('')
const showPassword = ref(false)
const showConfirmPassword = ref(false)
const passwordError = ref('')
const confirmError = ref('')
const error = ref('')
const loading = ref(false)
const success = ref(false)

function validateForm(): boolean {
  passwordError.value = validatePassword(password.value)
  confirmError.value = confirmPassword.value === password.value ? '' : '两次输入的密码不一致'
  return !passwordError.value && !confirmError.value
}

async function submit() {
  if (loading.value || !validateForm()) return
  loading.value = true
  error.value = ''
  try {
    await enterpriseAPI.resetPassword(token.value, password.value)
    success.value = true
  } catch (failure) {
    const reason = (failure as { reason?: string } | null)?.reason
    if (reason === 'PASSWORD_RESET_INVALID') {
      await router.replace('/enterprise/session-states?state=reset-link-invalid')
      return
    }
    error.value = '密码重置失败，请稍后重试'
  } finally {
    loading.value = false
  }
}
</script>
