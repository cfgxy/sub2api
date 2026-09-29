<template>
  <section class="mx-auto max-w-3xl space-y-6">
    <header>
      <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('enterprise.settings.title') }}</h1>
      <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.settings.description') }}</p>
    </header>

    <div class="card card-body">
      <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.settings.accountTitle') }}</h2>
      <dl class="mt-4 space-y-3">
        <div class="flex items-center justify-between gap-4">
          <dt class="text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.settings.accountEmail') }}</dt>
          <dd class="text-sm text-gray-900 dark:text-white">{{ profile?.email || t('enterprise.common.loading') }}</dd>
        </div>
        <div class="flex items-center justify-between gap-4">
          <dt class="text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.settings.accountStatus') }}</dt>
          <dd class="text-sm text-gray-900 dark:text-white">
            <StatusBadge v-if="profile" :status="profile.status" :label="employeeStatusLabel(profile.status, t)" />
            <span v-else>{{ t('enterprise.common.loading') }}</span>
          </dd>
        </div>
      </dl>
    </div>

    <div class="card card-body">
      <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.settings.passwordTitle') }}</h2>
      <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.settings.passwordHint') }}</p>
      <form class="mt-4 space-y-4" @submit.prevent="submitPasswordChange">
        <div data-testid="current-password">
          <Input
            v-model="form.current_password"
            type="password"
            :label="t('enterprise.settings.currentPassword')"
            autocomplete="current-password"
          />
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div data-testid="new-password">
            <Input
              v-model="form.new_password"
              type="password"
              :label="t('enterprise.settings.newPassword')"
              autocomplete="new-password"
            />
          </div>
          <div data-testid="confirm-password">
            <Input
              v-model="form.confirm_password"
              type="password"
              :label="t('enterprise.settings.confirmPassword')"
              autocomplete="new-password"
            />
          </div>
        </div>
        <p
          v-if="passwordValidationMessage"
          class="text-xs text-red-600 dark:text-red-400"
          data-testid="password-validation-error"
        >
          {{ passwordValidationMessage }}
        </p>
        <div class="flex items-center gap-3">
          <button
            type="submit"
            class="btn btn-primary btn-md"
            :disabled="!isPasswordFormFillable || saving"
            data-testid="submit-password"
          >
            {{ t('enterprise.settings.submitPassword') }}
          </button>
          <span
            v-if="isPasswordPolicySatisfied"
            class="text-xs font-bold text-green-600 dark:text-green-400"
            data-testid="password-policy-ok"
          >
            ✓ {{ t('enterprise.settings.policyOk') }}
          </span>
        </div>
      </form>
    </div>

    <div class="card card-body">
      <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.settings.sessionsTitle') }}</h2>
      <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.settings.sessionsHint') }}</p>

      <div v-if="sessionsLoading" class="flex justify-center py-10" data-testid="sessions-loading">
        <LoadingSpinner size="md" />
      </div>
      <div
        v-else-if="sessionsError"
        class="mt-4 flex items-center gap-3 text-sm text-red-600 dark:text-red-400"
        role="alert"
        data-testid="sessions-error"
      >
        <span>{{ t('enterprise.settings.sessionsError') }}</span>
        <button class="btn btn-secondary btn-sm" data-testid="sessions-retry" @click="loadSessions">
          {{ t('enterprise.common.retry') }}
        </button>
      </div>
      <template v-else>
        <p v-if="sessions.length === 0" class="mt-4 text-xs text-gray-500 dark:text-dark-400" data-testid="sessions-empty">
          {{ t('enterprise.settings.sessionsEmpty') }}
        </p>
        <div
          v-for="session in sessions"
          :key="session.id"
          class="flex items-center justify-between gap-4 border-b border-gray-200 py-4 last:border-b-0 dark:border-dark-700"
          data-testid="session-row"
        >
          <div class="flex min-w-0 items-center gap-3">
            <span class="grid h-9 w-9 flex-none place-items-center rounded-lg bg-gray-100 text-gray-500 dark:bg-dark-800 dark:text-dark-400">
              <Icon name="cpu" size="sm" />
            </span>
            <div class="min-w-0">
              <b class="block truncate text-sm text-gray-900 dark:text-white">
                {{ session.current ? t('enterprise.settings.currentBrowser') : describeUserAgent(session.user_agent) }}
              </b>
              <p class="mt-1 truncate text-xs text-gray-500 dark:text-dark-400">
                {{ session.current ? t('enterprise.settings.currentSession') : maskIpAddress(session.ip_address) }} ·
                {{ t('enterprise.settings.lastSeen', { time: relativeTime(session.last_seen_at) }) }}
              </p>
            </div>
          </div>
          <span
            v-if="session.current"
            class="badge badge-success flex-none"
            data-testid="session-current-tag"
          >
            {{ t('enterprise.settings.currentDevice') }}
          </span>
          <button
            v-else
            class="btn btn-secondary btn-sm flex-none"
            :disabled="revokingSessionId === session.id"
            data-testid="revoke-session"
            @click="askRevokeSession(session.id)"
          >
            {{ t('enterprise.settings.revokeSession') }}
          </button>
        </div>
        <div class="mt-4 rounded-lg bg-yellow-50 px-3 py-3 dark:bg-yellow-900/20">
          <b class="text-xs text-yellow-800 dark:text-yellow-500">{{ t('enterprise.settings.noticeTitle') }}</b>
          <p class="mt-1 text-xs text-yellow-700 dark:text-yellow-600">{{ t('enterprise.settings.noticeDescription') }}</p>
        </div>
      </template>
    </div>

    <div class="card card-body">
      <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.settings.dangerTitle') }}</h2>
      <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.settings.dangerHint') }}</p>
      <div class="mt-4 flex items-center justify-between gap-4 py-3">
        <div>
          <b class="block text-sm text-gray-900 dark:text-white">{{ t('enterprise.settings.logoutCurrent') }}</b>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.settings.logoutCurrentDescription') }}</p>
        </div>
        <button
          class="btn btn-secondary btn-md flex-none"
          :disabled="loggingOutCurrent"
          data-testid="logout-current"
          @click="pendingAction = 'logout-current'"
        >
          {{ t('enterprise.settings.logoutCurrent') }}
        </button>
      </div>
      <div class="flex items-center justify-between gap-4 border-t border-gray-200 py-3 dark:border-dark-700">
        <div>
          <b class="block text-sm text-gray-900 dark:text-white">{{ t('enterprise.settings.logoutAll') }}</b>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.settings.logoutAllDescription') }}</p>
        </div>
        <button
          class="btn btn-danger btn-md flex-none"
          :disabled="loggingOutAll"
          data-testid="logout-all"
          @click="pendingAction = 'logout-all'"
        >
          {{ t('enterprise.settings.logoutAll') }}
        </button>
      </div>
    </div>

    <ConfirmDialog
      :show="pendingAction !== null"
      :title="confirmTitle"
      :message="confirmMessage"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="runPendingAction"
      @cancel="pendingAction = null"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Input from '@/components/common/Input.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import { enterpriseAPI } from '@/api/enterprise'
import { useAppStore } from '@/stores/app'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'
import { employeeStatusLabel } from '@/utils/enterpriseDisplay'
import { describeUserAgent, maskIpAddress } from '@/utils/maskSessionDevice'
import type { EnterpriseEmployee, EnterpriseSession } from '@/types/enterprise'

type PendingAction = { kind: 'revoke'; id: string } | 'logout-current' | 'logout-all' | null

const { t } = useI18n()
const appStore = useAppStore()

const profile = ref<EnterpriseEmployee>()
const saving = ref(false)
const form = reactive({ current_password: '', new_password: '', confirm_password: '' })

const sessions = ref<EnterpriseSession[]>([])
const sessionsLoading = ref(true)
const sessionsError = ref(false)
const revokingSessionId = ref<string | null>(null)
const loggingOutCurrent = ref(false)
const loggingOutAll = ref(false)
const pendingAction = ref<PendingAction>(null)

const router = useRouter()
const auth = useEnterpriseAuthStore()

const isPasswordFormFillable = computed(
  () => form.current_password.length > 0 && form.new_password.length > 0 && form.confirm_password.length > 0,
)

const passwordValidationMessage = computed(() => {
  if (!isPasswordFormFillable.value) return ''
  if (form.new_password.length < 8) return t('enterprise.settings.policyTooShort')
  if (form.new_password !== form.confirm_password) return t('enterprise.settings.policyMismatch')
  return ''
})

const isPasswordPolicySatisfied = computed(() => passwordValidationMessage.value === '' && isPasswordFormFillable.value)

const confirmTitle = computed(() => {
  if (pendingAction.value === 'logout-current') return t('enterprise.settings.logoutCurrent')
  if (pendingAction.value === 'logout-all') return t('enterprise.settings.logoutAll')
  return t('enterprise.settings.revokeTitle')
})
const confirmMessage = computed(() => {
  if (pendingAction.value === 'logout-current') return t('enterprise.settings.logoutCurrentConfirm')
  if (pendingAction.value === 'logout-all') return t('enterprise.settings.logoutAllConfirm')
  return t('enterprise.settings.revokeConfirm')
})

function relativeTime(iso: string): string {
  const target = new Date(iso).getTime()
  if (Number.isNaN(target)) return t('enterprise.settings.timeUnknown')
  const diffMs = Date.now() - target
  if (diffMs < 60_000) return t('enterprise.settings.timeJustNow')
  const minutes = Math.floor(diffMs / 60_000)
  if (minutes < 60) return t('enterprise.settings.timeMinutes', { count: minutes })
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return t('enterprise.settings.timeHours', { count: hours })
  return t('enterprise.settings.timeDays', { count: Math.floor(hours / 24) })
}

async function loadProfile() {
  try {
    profile.value = await enterpriseAPI.getEmployeeProfile()
  } catch {
    // 只提示固定文案，不回显后端原始错误或内部标识
    appStore.showError(t('enterprise.settings.profileFailed'), 5000)
  }
}

async function loadSessions() {
  sessionsLoading.value = true
  sessionsError.value = false
  try {
    sessions.value = await enterpriseAPI.listSessions()
  } catch {
    sessionsError.value = true
  } finally {
    sessionsLoading.value = false
  }
}

async function submitPasswordChange() {
  if (!isPasswordFormFillable.value || passwordValidationMessage.value) return
  saving.value = true
  try {
    await enterpriseAPI.changePassword(form.current_password, form.new_password)
    form.current_password = ''
    form.new_password = ''
    form.confirm_password = ''
    auth.clear()
    appStore.showSuccess(t('enterprise.settings.passwordSuccess'), 3000)
    await router.replace('/enterprise/login')
  } catch {
    appStore.showError(t('enterprise.settings.passwordFailed'), 5000)
  } finally {
    saving.value = false
  }
}

function askRevokeSession(id: string) {
  pendingAction.value = { kind: 'revoke', id }
}

async function runPendingAction() {
  const action = pendingAction.value
  pendingAction.value = null
  if (action === 'logout-current') await logoutCurrent()
  else if (action === 'logout-all') await logoutAll()
  else if (action) await revokeSession(action.id)
}

async function revokeSession(id: string) {
  revokingSessionId.value = id
  try {
    await enterpriseAPI.revokeSession(id)
    appStore.showSuccess(t('enterprise.settings.revokeSuccess'), 3000)
    await loadSessions()
  } catch {
    appStore.showError(t('enterprise.settings.revokeFailed'), 5000)
  } finally {
    revokingSessionId.value = null
  }
}

async function logoutCurrent() {
  loggingOutCurrent.value = true
  try {
    await auth.logout()
    await router.replace('/enterprise/login')
  } finally {
    loggingOutCurrent.value = false
  }
}

async function logoutAll() {
  loggingOutAll.value = true
  try {
    await enterpriseAPI.revokeAllSessions()
    auth.clear()
    appStore.showSuccess(t('enterprise.settings.logoutAllSuccess'), 3000)
    await router.replace('/enterprise/login')
  } catch {
    appStore.showError(t('enterprise.settings.logoutAllFailed'), 5000)
  } finally {
    loggingOutAll.value = false
  }
}

onMounted(() => {
  loadProfile()
  loadSessions()
})
</script>
