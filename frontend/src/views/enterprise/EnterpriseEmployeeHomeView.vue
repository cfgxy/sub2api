<template>
  <section class="space-y-6">
    <header class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('enterprise.home.title') }}</h1>
      </div>
      <button class="btn btn-secondary btn-md" :disabled="loading" data-testid="home-refresh" @click="load">
        <Icon name="refresh" size="sm" :class="loading && 'animate-spin'" />
        {{ t('enterprise.common.refresh') }}
      </button>
    </header>

    <div
      v-if="loadError"
      class="card card-body flex flex-col gap-3 border-red-200 text-sm text-red-600 dark:border-red-900/50 dark:text-red-400 sm:flex-row sm:items-center sm:justify-between"
      role="alert"
      data-testid="home-load-error"
    >
      <span>{{ t('enterprise.home.loadFailed') }}</span>
      <button class="btn btn-secondary btn-sm self-start" data-testid="home-load-retry" @click="load">
        {{ t('enterprise.common.retry') }}
      </button>
    </div>

    <div v-else-if="loading" class="flex justify-center py-16" data-testid="home-loading">
      <LoadingSpinner size="lg" />
    </div>

    <template v-else>
      <div
        v-if="usage?.source_status === 'unavailable'"
        class="card card-body border-yellow-200 text-sm text-yellow-700 dark:border-yellow-900/50 dark:text-yellow-500"
        role="status"
        data-testid="home-source-unavailable"
      >
        {{ t('enterprise.home.sourceUnavailable') }}
      </div>

      <div class="grid grid-cols-1 gap-6 lg:grid-cols-2" data-testid="home-quota-comparison">
        <article class="card card-body">
          <div class="mb-4">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.home.personalQuotaTitle') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.home.quotaWindow') }}</p>
          </div>
          <div
            v-if="usage?.source_status !== 'available'"
            class="grid h-32 place-items-center text-center text-sm text-gray-500 dark:text-dark-400"
            data-testid="home-personal-quota-unavailable"
          >
            {{ unavailableMessage(usage?.unavailable_reason) }}
          </div>
          <dl v-else class="grid grid-cols-3 gap-x-4 gap-y-4">
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.home.personalQuota') }}</dt>
              <dd class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ formatEnterpriseAmount(usage.allocation) }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.home.used') }}</dt>
              <dd class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ formatEnterpriseAmount(usage.actual_cost) }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.home.available') }}</dt>
              <dd class="mt-1 text-base font-semibold" :class="personalAvailable ? 'text-gray-900 dark:text-white' : 'text-red-600 dark:text-red-400'">
                {{ formatEnterpriseAmount(usage.remaining) }}
              </dd>
            </div>
          </dl>
        </article>

        <article class="card card-body">
          <div class="mb-4">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.home.poolTitle') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.home.quotaWindow') }}</p>
          </div>
          <div
            v-if="pool?.source_status !== 'available'"
            class="grid h-32 place-items-center text-center text-sm text-gray-500 dark:text-dark-400"
            data-testid="home-pool-unavailable"
          >
            {{ unavailableMessage(pool?.unavailable_reason) }}
          </div>
          <dl v-else class="grid grid-cols-3 gap-x-4 gap-y-4">
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.home.poolLimit') }}</dt>
              <dd class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ formatEnterpriseAmount(pool.pool_limit) }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.home.used') }}</dt>
              <dd class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ formatEnterpriseAmount(pool.pool_used) }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.home.available') }}</dt>
              <dd class="mt-1 text-base font-semibold" :class="pool.pool_exhausted ? 'text-red-600 dark:text-red-400' : 'text-gray-900 dark:text-white'">
                {{ formatEnterpriseAmount(pool.pool_remaining) }}
              </dd>
            </div>
          </dl>
        </article>
      </div>

      <div
        v-if="quotaStatus"
        class="card card-body text-sm"
        :class="quotaStatus.tone"
        role="status"
        data-testid="home-quota-status"
      >
        {{ quotaStatus.message }}
      </div>

      <div class="card card-body">
        <div class="mb-4 flex items-start justify-between gap-4">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.home.trendTitle') }}</h2>
          <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.home.trendNote') }}</span>
        </div>
        <div
          v-if="!recentTrend.length"
          class="grid h-32 place-items-center text-sm text-gray-500 dark:text-dark-400"
          data-testid="home-trend-empty"
        >
          {{ t('enterprise.home.trendEmpty') }}
        </div>
        <div
          v-else
          class="flex h-40 items-end gap-4 border-b border-l border-gray-200 px-3 pb-6 pt-4 dark:border-dark-700"
          :aria-label="t('enterprise.home.trendTitle')"
        >
          <div v-for="point in recentTrend" :key="point.at" class="flex h-full min-w-[18px] flex-1 flex-col items-center justify-end gap-2">
            <span
              class="w-full max-w-[36px] rounded-t bg-primary-500"
              :style="{ height: `${trendHeight(point.requests)}%` }"
              :title="t('enterprise.home.trendTooltip', { date: formatDate(point.at), count: point.requests })"
            />
            <small class="whitespace-nowrap text-[10px] text-gray-500 dark:text-dark-400">{{ trendLabel(point.at) }}</small>
          </div>
        </div>
      </div>

      <div class="grid grid-cols-1 gap-6 lg:grid-cols-[1.3fr_1fr]">
        <div class="card card-body">
          <div class="mb-4 flex items-start justify-between gap-4">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.home.recentTitle') }}</h2>
            <RouterLink to="/enterprise/usage" class="whitespace-nowrap text-xs font-semibold text-primary-600 hover:underline dark:text-primary-400">
              {{ t('enterprise.common.viewAll') }}
            </RouterLink>
          </div>
          <div
            v-if="recentCallsState === 'unavailable'"
            class="grid h-32 place-items-center text-sm text-gray-500 dark:text-dark-400"
            data-testid="home-recent-unavailable"
          >
            {{ t('enterprise.home.recentUnavailable') }}
          </div>
          <DataTable
            v-else
            :columns="recentColumns"
            :data="recentCalls"
            :loading="recentCallsState === 'loading'"
            :row-key="recentRowKey"
          >
            <template #cell-request_at="{ row }">{{ formatDate(row.request_at) }}</template>
            <template #empty>
              <p class="text-sm text-gray-500 dark:text-dark-400" data-testid="home-recent-empty">
                {{ t('enterprise.home.recentEmpty') }}
              </p>
            </template>
          </DataTable>
        </div>

        <div class="card card-body">
          <div class="mb-4 flex items-start justify-between gap-4">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.home.reminderTitle') }}</h2>
            <span class="badge" :class="reminderBadgeClass">{{ reminderLabel }}</span>
          </div>
          <template v-if="usage?.source_status === 'available'">
            <b class="block text-base text-gray-900 dark:text-white">
              {{ t('enterprise.home.reminderHeadline', { percentage: usagePercentage }) }}
            </b>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('enterprise.home.reminderRemaining', { value: formatEnterpriseAmount(usage.remaining) }) }}
              {{ hasOverage ? t('enterprise.home.reminderOverage', { value: formatEnterpriseAmount(usage.overage) }) : t('enterprise.home.reminderNoOverage') }}
            </p>
            <div class="mt-3 h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
              <i class="block h-full rounded-full bg-primary-500" :style="{ width: `${usagePercentage}%` }" />
            </div>
            <div class="mt-1.5 flex justify-between text-[11px] text-gray-500 dark:text-dark-400">
              <span>0</span>
              <span>{{ formatEnterpriseAmount(usage.allocation) }} {{ t('admin.enterprise.terms.allocation') }}</span>
            </div>
          </template>
          <p v-else class="text-sm text-gray-500 dark:text-dark-400">{{ unavailableMessage(usage?.unavailable_reason) }}</p>
        </div>
      </div>

      <div class="card card-body">
        <h2 class="mb-2 text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.home.accessTitle') }}</h2>
        <p class="text-sm text-gray-500 dark:text-dark-400">
          {{ home?.key
            ? t('enterprise.home.accessKeyStatus', { status: keyStatusLabel(home.key.status, t) })
            : t('enterprise.home.accessNoKey') }}
        </p>
        <RouterLink to="/enterprise/keys" class="mt-3 inline-block text-sm font-semibold text-primary-600 hover:underline dark:text-primary-400">
          {{ t('enterprise.home.manageKey') }}
        </RouterLink>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import DataTable from '@/components/common/DataTable.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { Column } from '@/components/common/types'
import { enterpriseAPI } from '@/api/enterprise'
import { formatEnterpriseAmount } from '@/utils/enterpriseAmounts'
import { keyStatusLabel } from '@/utils/enterpriseDisplay'
import { formatDate } from '@/utils/format'
import type { EnterpriseEmployeeHome, EnterpriseEmployeeUsageSummary, EnterpriseEnterprisePoolStatus, EnterpriseEmployeeUsageTrendPoint, EnterpriseEmployeeUsageRecord } from '@/types/enterprise'

type SourceState = 'loading' | 'ready' | 'unavailable'

const { t, locale } = useI18n()

const loading = ref(false)
const loadError = ref(false)
const home = ref<EnterpriseEmployeeHome>()
const usage = ref<EnterpriseEmployeeUsageSummary>()
const pool = ref<EnterpriseEnterprisePoolStatus>()
const recentTrend = ref<EnterpriseEmployeeUsageTrendPoint[]>([])
const recentCalls = ref<EnterpriseEmployeeUsageRecord[]>([])
const recentCallsState = ref<SourceState>('loading')

// This home view only renders the two authoritative values when their
// backend-provided weekly anchors match. It does not use client-side cache as
// a fallback for quota information.
const personalAvailable = computed(() => Number(usage.value?.remaining ?? 0) > 0)
const poolAvailable = computed(() => pool.value?.pool_exhausted === false && Number(pool.value?.pool_remaining ?? 0) > 0)
const quotaStatus = computed(() => {
  const reason = usage.value?.unavailable_reason || pool.value?.unavailable_reason
  if (usage.value?.source_status !== 'available' || pool.value?.source_status !== 'available') {
    return { tone: 'border-yellow-200 text-yellow-700 dark:border-yellow-900/50 dark:text-yellow-500', message: unavailableMessage(reason) }
  }
  if (!poolAvailable.value && personalAvailable.value) {
    return { tone: 'border-red-200 text-red-600 dark:border-red-900/50 dark:text-red-400', message: t('enterprise.home.statusEnterpriseExhausted') }
  }
  if (poolAvailable.value && !personalAvailable.value) {
    return { tone: 'border-yellow-200 text-yellow-700 dark:border-yellow-900/50 dark:text-yellow-500', message: t('enterprise.home.statusPersonalExhausted') }
  }
  if (!poolAvailable.value && !personalAvailable.value) {
    return { tone: 'border-red-200 text-red-600 dark:border-red-900/50 dark:text-red-400', message: t('enterprise.home.statusBothExhausted') }
  }
  return { tone: 'border-green-200 text-green-700 dark:border-green-900/50 dark:text-green-400', message: t('enterprise.home.statusAvailable') }
})

function unavailableMessage(reason?: string) {
  if (reason === 'subscription_expired') return t('enterprise.home.subscriptionExpired')
  if (reason === 'no_active_subscription') return t('enterprise.home.noActiveSubscription')
  if (reason === 'window_mismatch') return t('enterprise.home.windowMismatch')
  return t('enterprise.home.dataUnavailable')
}

const recentColumns = computed<Column[]>(() => [
  { key: 'request_at', label: t('enterprise.common.time') },
  { key: 'api_key_masked', label: t('enterprise.common.key') },
  { key: 'actual_cost', label: t('admin.enterprise.terms.actualCost') },
])
const recentRowKey = (row: EnterpriseEmployeeUsageRecord) => `${row.request_at}-${row.api_key_masked}-${row.generation}`

const trendLabel = (value: string) =>
  new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', { month: 'numeric', day: 'numeric' }).format(new Date(value))
const trendHeight = (value: number) =>
  Math.max(8, Math.round((value / Math.max(...recentTrend.value.map((point) => point.requests), 1)) * 100))

const hasOverage = computed(() => {
  const value = usage.value?.overage
  return value !== undefined && Number(value) > 0
})

const usagePercentage = computed(() => {
  if (!usage.value) return 0
  const allocation = Number(usage.value.allocation)
  const cost = Number(usage.value.actual_cost)
  if (!allocation) return 0
  return Math.min(100, Math.round((cost / allocation) * 100))
})
const reminderBadgeClass = computed(() =>
  usagePercentage.value >= 90 ? 'badge-danger' : usagePercentage.value >= 70 ? 'badge-warning' : 'badge-success',
)
const reminderLabel = computed(() =>
  usagePercentage.value >= 90
    ? t('enterprise.home.reminderCritical')
    : usagePercentage.value >= 70
      ? t('enterprise.home.reminderHigh')
      : t('enterprise.home.reminderNormal'),
)

async function load() {
  loading.value = true
  loadError.value = false
  try {
    home.value = await enterpriseAPI.getEmployeeHome({ suppressUnavailableRedirect: true })
    usage.value = home.value.usage
    pool.value = home.value.enterprise_pool
    recentTrend.value = home.value.recent_trend
  } catch {
    loadError.value = true
  } finally {
    loading.value = false
  }
}

async function loadRecentCalls() {
  recentCallsState.value = 'loading'
  try {
    const result = await enterpriseAPI.listEmployeeUsage({ page: 1, page_size: 5 }, { suppressUnavailableRedirect: true })
    recentCalls.value = result.items
    recentCallsState.value = 'ready'
  } catch {
    recentCalls.value = []
    recentCallsState.value = 'unavailable'
  }
}

onMounted(() => {
  load()
  loadRecentCalls()
})
</script>
