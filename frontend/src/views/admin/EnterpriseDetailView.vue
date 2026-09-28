<template>
  <PlatformEnterpriseShell>
    <section class="space-y-6">
      <header class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <span class="block text-xs text-gray-400 dark:text-dark-500">{{ t('admin.enterprise.detail.breadcrumb') }}</span>
          <h1 class="mt-1 text-2xl font-bold text-gray-900 dark:text-white">{{ item?.name || t('admin.enterprise.detail.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.detail.description') }}</p>
        </div>
        <div class="flex gap-3">
          <button class="btn btn-secondary btn-md" data-testid="enterprise-detail-back" @click="router.back()">
            {{ t('admin.enterprise.common.back') }}
          </button>
          <button
            v-if="item?.status === 'active'"
            class="btn btn-danger btn-md"
            data-testid="enterprise-detail-disable"
            @click="askDisable"
          >
            {{ t('admin.enterprise.common.disable') }}
          </button>
        </div>
      </header>

      <div v-if="loading" class="card" data-testid="enterprise-detail-loading">
        <div class="card-body space-y-3">
          <Skeleton v-for="row in 5" :key="row" height="1.25rem" />
        </div>
      </div>

      <div v-else-if="sourceUnavailable" class="card" role="alert" data-testid="enterprise-detail-unavailable">
        <div class="card-body text-sm text-red-600 dark:text-red-400">
          <strong class="block text-base font-semibold">{{ t('admin.enterprise.detail.unavailableTitle') }}</strong>
          <p class="mt-1">{{ t('admin.enterprise.detail.unavailableDescription') }}</p>
          <button class="btn btn-secondary btn-sm mt-4" data-testid="enterprise-detail-retry" @click="load">
            {{ t('admin.enterprise.list.refresh') }}
          </button>
        </div>
      </div>

      <template v-else-if="item">
        <div class="border-b border-gray-200 dark:border-dark-700" role="tablist">
          <button
            v-for="tab in tabs"
            :key="tab.name"
            type="button"
            role="tab"
            :aria-selected="activeTab === tab.name"
            :data-testid="`enterprise-detail-tab-${tab.name}`"
            :class="[
              '-mb-px border-b-2 px-4 py-2 text-sm font-medium transition-colors',
              activeTab === tab.name
                ? 'border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400'
                : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200',
            ]"
            @click="activeTab = tab.name"
          >
            {{ tab.label }}
          </button>
        </div>

        <div v-if="activeTab === 'overview'" class="space-y-6" data-testid="enterprise-detail-overview">
          <div class="card">
            <dl class="card-body grid gap-4 sm:grid-cols-2">
              <div>
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.name') }}</dt>
                <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ item.name }}</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.status') }}</dt>
                <dd class="mt-1">
                  <StatusBadge :status="item.status" :label="enterpriseStatusLabel(item.status, t)" />
                </dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.portalHost') }}</dt>
                <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ item.portal_host }}</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.createdAt') }}</dt>
                <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ formatDate(item.created_at) }}</dd>
              </div>
            </dl>
          </div>

          <div class="card">
            <div class="card-body">
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.enterprise.detail.impact') }}</h2>
              <dl class="mt-4 grid gap-4 sm:grid-cols-2">
                <div>
                  <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.employeeCount') }}</dt>
                  <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ item.employee_count }}</dd>
                </div>
                <div>
                  <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.activeEmployees') }}</dt>
                  <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ item.active_employee_count }}</dd>
                </div>
                <div>
                  <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.activeSessions') }}</dt>
                  <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ item.active_session_count }}</dd>
                </div>
                <div>
                  <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.activeKeys') }}</dt>
                  <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ item.active_key_count }}</dd>
                </div>
                <div class="sm:col-span-2">
                  <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.subscriptions') }}</dt>
                  <dd class="mt-1 grid gap-1.5 text-sm font-medium text-gray-900 dark:text-white">
                    <template v-if="item.subscriptions.length">
                      <span v-for="subscription in item.subscriptions" :key="subscription.id">{{ subscriptionLabel(subscription) }}</span>
                    </template>
                    <span v-else data-testid="enterprise-detail-no-subscriptions">{{ t('admin.enterprise.common.noActiveSubscriptions') }}</span>
                  </dd>
                </div>
              </dl>
            </div>
          </div>
        </div>

        <div v-else class="card" data-testid="enterprise-detail-access">
          <dl class="card-body grid gap-4">
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.detail.adminEmail') }}</dt>
              <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ item.admin_email || t('admin.enterprise.common.notConfigured') }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.upstreamUserId') }}</dt>
              <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ item.dedicated_upstream_user_id }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.activeSessions') }}</dt>
              <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ item.active_session_count }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.activeKeys') }}</dt>
              <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ item.active_key_count }}</dd>
            </div>
          </dl>
        </div>
      </template>

      <ConfirmDialog
        :show="confirmVisible"
        :title="t('admin.enterprise.list.disableTitle')"
        :message="confirmMessage"
        danger
        @confirm="confirmDisable"
        @cancel="confirmVisible = false"
      />
    </section>
  </PlatformEnterpriseShell>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import PlatformEnterpriseShell from '@/components/admin/PlatformEnterpriseShell.vue'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'
import { useAppStore } from '@/stores/app'
import { formatDate } from '@/utils/format'
import { enterpriseStatusLabel, subscriptionStatusLabel } from '@/utils/enterpriseDisplay'

const route = useRoute()
const router = useRouter()
const { t } = useI18n({ useScope: 'global' })
const appStore = useAppStore()
const item = ref<PlatformEnterprise>()
const loading = ref(false)
const sourceUnavailable = ref(false)
const activeTab = ref('overview')
const confirmVisible = ref(false)
const confirmMessage = ref('')

const tabs = computed(() => [
  { name: 'overview', label: t('admin.enterprise.detail.overview') },
  { name: 'access', label: t('admin.enterprise.detail.access') },
])

function subscriptionLabel(subscription: PlatformEnterprise['subscriptions'][number]) {
  return t('admin.enterprise.detail.subscriptionValue', {
    plan: subscription.plan,
    status: subscriptionStatusLabel(subscription.status, t),
    limit: subscription.weekly_limit || t('admin.enterprise.common.notConfigured'),
    expiresAt: formatDate(subscription.expires_at),
  })
}

async function load() {
  loading.value = true
  item.value = undefined
  sourceUnavailable.value = false
  try {
    item.value = await enterprisePlatformAPI.get(Number(route.params.id))
  } catch {
    sourceUnavailable.value = true
    appStore.showError(t('admin.enterprise.detail.unavailableMessage'))
  } finally {
    loading.value = false
  }
}

// 停用前重新拉取当前影响面，确保确认框展示的是实时数据而非页面旧值
async function askDisable() {
  let current: PlatformEnterprise
  try {
    current = await enterprisePlatformAPI.get(Number(route.params.id))
  } catch {
    item.value = undefined
    sourceUnavailable.value = true
    appStore.showError(t('admin.enterprise.detail.unavailableMessage'))
    return
  }
  item.value = current
  if (current.status !== 'active') return
  const subscriptionLabelText = current.subscriptions.length
    ? current.subscriptions.map(subscriptionLabel).join('；')
    : t('admin.enterprise.common.noActiveSubscriptions')
  confirmMessage.value = t('admin.enterprise.list.disableConfirm', {
    name: current.name,
    subscriptions: subscriptionLabelText,
    employees: current.employee_count,
    activeEmployees: current.active_employee_count,
    sessions: current.active_session_count,
    keys: current.active_key_count,
  })
  confirmVisible.value = true
}

async function confirmDisable() {
  const current = item.value
  confirmVisible.value = false
  if (!current) return
  try {
    await enterprisePlatformAPI.disable(current.id, t('admin.enterprise.list.disableReason'))
    appStore.showSuccess(t('admin.enterprise.list.disableSuccess'))
    await load()
  } catch {
    appStore.showError(t('admin.enterprise.list.disableFailed'))
  }
}

onMounted(load)
</script>
