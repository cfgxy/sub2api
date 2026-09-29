<template>
  <section class="space-y-6">
    <header class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('enterprise.keys.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.keys.description') }}</p>
      </div>
      <div class="flex gap-3">
        <button
          v-if="!key"
          class="btn btn-primary btn-md"
          :disabled="mutating"
          data-testid="create-key"
          @click="createKey"
        >
          <Icon name="plus" size="sm" />
          {{ t('enterprise.keys.create') }}
        </button>
        <template v-else>
          <button class="btn btn-secondary btn-md" :disabled="mutating" data-testid="rotate-key" @click="askRotate">
            <Icon name="sync" size="sm" />
            {{ t('enterprise.keys.rotate') }}
          </button>
          <button class="btn btn-danger btn-md" :disabled="mutating" data-testid="disable-key" @click="askDisable">
            <Icon name="ban" size="sm" />
            {{ t('enterprise.keys.disable') }}
          </button>
        </template>
      </div>
    </header>

    <div
      v-if="!loading && loadError"
      class="card card-body flex flex-col gap-3 border-red-200 text-sm text-red-600 dark:border-red-900/50 dark:text-red-400 sm:flex-row sm:items-center sm:justify-between"
      role="alert"
      data-testid="keys-load-error"
    >
      <span>{{ t('enterprise.keys.loadError') }}</span>
      <button class="btn btn-secondary btn-sm self-start" data-testid="keys-load-retry" @click="load">
        {{ t('enterprise.common.retry') }}
      </button>
    </div>

    <div v-else-if="loading" class="flex justify-center py-16" data-testid="keys-loading">
      <LoadingSpinner size="lg" />
    </div>

    <EmptyState v-else-if="!key" :title="t('enterprise.keys.empty')" :description="t('enterprise.keys.description')" data-testid="keys-empty" />

    <div v-else class="card card-body space-y-6">
      <div class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-200 pb-5 dark:border-dark-700">
        <div class="min-w-0">
          <span class="block text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.keys.currentKey') }}</span>
          <code class="mt-1 block break-all text-base text-gray-900 dark:text-white">{{ key.masked_key }}</code>
        </div>
        <StatusBadge :status="key.status" :label="keyStatusLabel(key.status, t)" />
      </div>

      <div class="max-w-md space-y-2">
        <span class="block text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.keys.quotaTitle') }}</span>
        <strong class="block break-all text-xl text-gray-900 dark:text-white">{{ formatLimit(key.quota) }}</strong>
        <small class="block text-xs text-gray-500 dark:text-dark-400">
          {{ t('enterprise.keys.quotaUsed', { value: formatUsage(key.quota_used) }) }}
        </small>
        <div class="h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
          <i class="block h-full rounded-full bg-primary-500" :style="{ width: `${percentage(key.quota_used, key.quota)}%` }" />
        </div>
      </div>
    </div>

    <BaseDialog :show="secretVisible" :title="t('enterprise.keys.secretTitle')" width="normal" @close="clearSecret">
      <div class="space-y-4">
        <p class="rounded-lg bg-yellow-50 px-4 py-3 text-sm text-yellow-700 dark:bg-yellow-900/20 dark:text-yellow-500">
          {{ t('enterprise.keys.secretWarning') }}
        </p>
        <div data-testid="plaintext-key" class="font-mono">
          <Input :model-value="secret" readonly />
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end">
          <button class="btn btn-primary btn-md" data-testid="close-secret" @click="clearSecret">
            {{ t('enterprise.keys.secretConfirm') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="pendingAction !== null"
      :title="pendingAction === 'rotate' ? t('enterprise.keys.rotateTitle') : t('enterprise.keys.disableTitle')"
      :message="pendingAction === 'rotate' ? t('enterprise.keys.rotateConfirm') : t('enterprise.keys.disableConfirm')"
      :confirm-text="pendingAction === 'rotate' ? t('enterprise.keys.rotate') : t('enterprise.keys.disable')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmPendingAction"
      @cancel="pendingAction = null"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { onBeforeRouteLeave } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Input from '@/components/common/Input.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import { enterpriseAPI, isEnterpriseKeyMutationStateConflict } from '@/api/enterprise'
import { useAppStore } from '@/stores/app'
import { keyStatusLabel } from '@/utils/enterpriseDisplay'
import type { EnterpriseEmployeeKey } from '@/types/enterprise'

const { t } = useI18n()
const appStore = useAppStore()

const key = ref<EnterpriseEmployeeKey | null>(null)
const loading = ref(false)
const loadError = ref(false)
const mutating = ref(false)
const secret = ref('')
const secretVisible = ref(false)
const pendingAction = ref<'disable' | 'rotate' | null>(null)

const formatUsage = (value: number) => `$${value.toFixed(2)}`
const formatLimit = (value: number) => (value > 0 ? formatUsage(value) : t('enterprise.common.unlimited'))
const percentage = (usage: number, limit: number) => (limit > 0 ? Math.min(100, Math.round((usage / limit) * 100)) : 0)

async function load() {
  loading.value = true
  loadError.value = false
  try {
    key.value = await enterpriseAPI.getCurrentKey()
  } catch {
    loadError.value = true
    appStore.showError(t('enterprise.keys.loadFailed'), 5000)
  } finally {
    loading.value = false
  }
}

async function handleMutationFailure(error: unknown, message: string) {
  // 失败提示只使用本地固定文案，不回显后端原始错误，避免泄露明文或内部标识
  if (isEnterpriseKeyMutationStateConflict(error)) {
    await load()
    appStore.showWarning(t('enterprise.keys.stateConflict'), 4000)
    return
  }
  appStore.showError(message, 5000)
}

function showSecret(plaintext?: string, replayed = false) {
  clearSecret()
  if (replayed || !plaintext) return
  secret.value = plaintext
  secretVisible.value = true
}

function clearSecret() {
  secret.value = ''
  secretVisible.value = false
}

async function createKey() {
  if (mutating.value) return
  mutating.value = true
  try {
    const result = await enterpriseAPI.createKey()
    key.value = result.key
    showSecret(result.plaintext, result.replayed)
    if (result.replayed) appStore.showWarning(t('enterprise.keys.replayed'), 4000)
    else appStore.showSuccess(t('enterprise.keys.createSuccess'), 3000)
  } catch (error) {
    await handleMutationFailure(error, t('enterprise.keys.createFailed'))
  } finally {
    mutating.value = false
  }
}

function askDisable() {
  if (!key.value || mutating.value) return
  pendingAction.value = 'disable'
}

function askRotate() {
  if (!key.value || mutating.value) return
  pendingAction.value = 'rotate'
}

async function confirmPendingAction() {
  const action = pendingAction.value
  pendingAction.value = null
  if (action === 'disable') await disableKey()
  else if (action === 'rotate') await rotateKey()
}

async function disableKey() {
  if (!key.value || mutating.value) return
  mutating.value = true
  try {
    await enterpriseAPI.disableKey(key.value.id)
    key.value = null
    clearSecret()
    appStore.showSuccess(t('enterprise.keys.disableSuccess'), 3000)
  } catch (error) {
    await handleMutationFailure(error, t('enterprise.keys.disableFailed'))
  } finally {
    mutating.value = false
  }
}

async function rotateKey() {
  if (!key.value || mutating.value) return
  mutating.value = true
  try {
    const result = await enterpriseAPI.rotateKey(key.value.id)
    key.value = result.key
    showSecret(result.plaintext, result.replayed)
    if (result.replayed) appStore.showWarning(t('enterprise.keys.replayed'), 4000)
    else appStore.showSuccess(t('enterprise.keys.rotateSuccess'), 3000)
  } catch (error) {
    await handleMutationFailure(error, t('enterprise.keys.rotateFailed'))
  } finally {
    mutating.value = false
  }
}

onBeforeRouteLeave(() => {
  clearSecret()
})
onUnmounted(clearSecret)
onMounted(load)
</script>
