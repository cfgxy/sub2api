<template>
  <EnterpriseAuthShell auth-layout>
    <div class="space-y-6">
      <header class="text-center">
        <div class="mx-auto mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
          <Icon name="lock" size="md" />
        </div>
        <p class="mb-1 text-xs font-semibold text-primary-600 dark:text-primary-400">首次登录安全设置</p>
        <h2 class="text-2xl font-bold text-gray-900 dark:text-gray-100">设置您的登录密码</h2>
        <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-dark-400">
          初始密码仅用于首次登录，设置完成后将直接进入工作台。
        </p>
      </header>

      <p v-if="error" role="alert" class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
        {{ error }}
      </p>
      <p v-if="success" role="status" class="rounded-md border border-green-200 bg-green-50 px-3 py-2 text-sm text-green-700 dark:border-green-800 dark:bg-green-900/20 dark:text-green-300">
        密码已更新，正在进入工作台。
      </p>

      <form class="space-y-5" :aria-busy="loading" @submit.prevent="submit">
        <div>
          <label for="enterprise-new-password" class="input-label">新密码</label>
          <div class="relative">
            <Icon name="lock" size="md" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500" />
            <input
              id="enterprise-new-password"
              v-model="form.next"
              :type="showPassword ? 'text' : 'password'"
              class="input pl-11 pr-11"
              :class="{ 'input-error': errors.next }"
              required
              autofocus
              autocomplete="new-password"
              :disabled="loading"
              @blur="validateForm"
            />
            <button
              type="button"
              class="absolute inset-y-0 right-0 flex items-center px-3.5 text-gray-400 transition-colors hover:text-gray-600 focus-visible:outline-primary-500 dark:hover:text-dark-300"
              :disabled="loading"
              :aria-label="showPassword ? '隐藏密码' : '显示密码'"
              @click="showPassword = !showPassword"
            >
              <Icon :name="showPassword ? 'eyeOff' : 'eye'" size="md" />
            </button>
          </div>
          <p v-if="errors.next" class="mt-1 text-xs text-red-600 dark:text-red-400">{{ errors.next }}</p>
        </div>

        <div>
          <label for="enterprise-confirm-password" class="input-label">确认新密码</label>
          <div class="relative">
            <Icon name="lock" size="md" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500" />
            <input
              id="enterprise-confirm-password"
              v-model="form.confirm"
              :type="showConfirmPassword ? 'text' : 'password'"
              class="input pl-11 pr-11"
              :class="{ 'input-error': errors.confirm }"
              required
              autocomplete="new-password"
              :disabled="loading"
              @blur="validateForm"
            />
            <button
              type="button"
              class="absolute inset-y-0 right-0 flex items-center px-3.5 text-gray-400 transition-colors hover:text-gray-600 focus-visible:outline-primary-500 dark:hover:text-dark-300"
              :disabled="loading"
              :aria-label="showConfirmPassword ? '隐藏密码' : '显示密码'"
              @click="showConfirmPassword = !showConfirmPassword"
            >
              <Icon :name="showConfirmPassword ? 'eyeOff' : 'eye'" size="md" />
            </button>
          </div>
          <p v-if="errors.confirm" class="mt-1 text-xs text-red-600 dark:text-red-400">{{ errors.confirm }}</p>
        </div>

        <div class="rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-600 dark:border-dark-700 dark:bg-dark-700/60 dark:text-dark-300">
          密码至少需要 8 个字符，不限制字符组成。
        </div>
        <button type="submit" class="btn btn-primary flex w-full items-center justify-center gap-2" :disabled="loading || success">
          <Icon :name="loading ? 'refresh' : 'checkCircle'" size="md" :class="{ 'animate-spin': loading }" />
          {{ loading ? '保存中' : success ? '已完成' : '完成设置并继续' }}
        </button>
      </form>

      <p class="text-center text-xs text-gray-400 dark:text-dark-500">请勿与他人共享您的密码。</p>
    </div>
  </EnterpriseAuthShell>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import EnterpriseAuthShell from '@/components/enterprise/EnterpriseAuthShell.vue'
import { enterpriseHome } from '@/router/enterpriseGuard'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'
import { validatePassword } from '@/features/enterprise/validation'

const auth = useEnterpriseAuthStore()
const router = useRouter()
const loading = ref(false)
const success = ref(false)
const error = ref('')
const showPassword = ref(false)
const showConfirmPassword = ref(false)
const form = reactive({ next: '', confirm: '' })
const errors = reactive({ next: '', confirm: '' })

function validateForm(): boolean {
  errors.next = validatePassword(form.next)
  errors.confirm = form.confirm === form.next ? '' : '两次输入的密码不一致'
  return !errors.next && !errors.confirm
}

function actionError(failure: unknown): string {
  const reason = (failure as { reason?: string } | null)?.reason
  if (reason === 'PASSWORD_POLICY') return '密码不符合要求，请重新设置'
  if (reason === 'SESSION_EXPIRED') return '登录会话已过期，请重新登录'
  return '密码更新失败，请稍后重试'
}

async function submit() {
  if (loading.value || success.value || !validateForm()) return
  loading.value = true
  error.value = ''
  try {
    await auth.changeInitialPassword(form.next)
    success.value = true
    await router.replace(enterpriseHome(auth.principal?.role))
  } catch (failure) {
    error.value = actionError(failure)
  } finally {
    loading.value = false
  }
}
</script>
