<template>
  <section class="space-y-6">
    <header class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('enterprise.usage.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.usage.description') }}</p>
      </div>
      <button class="btn btn-secondary btn-md" :disabled="loading" data-testid="usage-refresh" @click="load">
        <Icon name="refresh" size="sm" :class="loading && 'animate-spin'" />
        {{ t('enterprise.common.refresh') }}
      </button>
    </header>

    <div
      v-if="hasSourceIssue"
      class="card card-body border-yellow-200 text-sm text-yellow-700 dark:border-yellow-900/50 dark:text-yellow-500"
      role="status"
      data-testid="usage-source-issue"
    >
      <strong class="block font-semibold">{{ t('enterprise.usage.sourceIssueTitle') }}</strong>
      <span class="mt-1 block text-xs">{{ t('enterprise.usage.sourceIssueDescription', { sources: sourceIssueText }) }}</span>
    </div>

    <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <article class="card card-body">
        <div class="mb-4">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.usage.myQuotaTitle') }}</h2>
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('enterprise.usage.myQuotaWindow', { anchor: formatOptionalDate(usage?.window_anchor) }) }}
          </span>
        </div>
        <div
          v-if="usage?.source_status === 'unavailable'"
          class="grid h-32 place-items-center text-center text-sm text-gray-500 dark:text-dark-400"
          data-testid="usage-quota-unavailable"
        >
          {{ t('enterprise.usage.myQuotaUnavailable') }}
        </div>
        <dl v-else class="grid grid-cols-2 gap-x-6 gap-y-4">
          <div>
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.terms.allocation') }}</dt>
            <dd class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ usage?.allocation ?? '0' }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.terms.actualCost') }}</dt>
            <dd class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ usage?.actual_cost ?? '0' }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.terms.remaining') }}</dt>
            <dd class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ usage?.remaining ?? '0' }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.terms.overage') }}</dt>
            <dd
              class="mt-1 text-base font-semibold"
              :class="hasOverage ? 'text-red-600 dark:text-red-400' : 'text-gray-900 dark:text-white'"
            >
              {{ usage?.overage ?? '0' }}
            </dd>
          </div>
          <div class="col-span-2">
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.common.requests') }}</dt>
            <dd class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ usage?.requests ?? 0 }}</dd>
          </div>
        </dl>
      </article>

      <article class="card card-body">
        <div class="mb-4">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.usage.poolTitle') }}</h2>
          <span class="text-xs text-gray-500 dark:text-dark-400">{{ poolSourceLabel }}</span>
        </div>
        <div
          v-if="enterprisePool?.source_status === 'unavailable'"
          class="grid h-32 place-items-center text-center text-sm text-gray-500 dark:text-dark-400"
          data-testid="usage-pool-unavailable"
        >
          {{ t('enterprise.usage.poolUnavailable') }}
        </div>
        <template v-else>
          <dl class="grid grid-cols-2 gap-x-6 gap-y-4">
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.usage.poolLimit') }}</dt>
              <dd class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ enterprisePool?.pool_limit ?? '0' }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.usage.poolUsed') }}</dt>
              <dd class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ enterprisePool?.pool_used ?? '0' }}</dd>
            </div>
            <div class="col-span-2">
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.usage.poolRemaining') }}</dt>
              <dd
                class="mt-1 text-base font-semibold"
                :class="enterprisePool?.pool_exhausted ? 'text-red-600 dark:text-red-400' : 'text-gray-900 dark:text-white'"
              >
                {{ enterprisePool?.pool_remaining ?? '0' }}
              </dd>
            </div>
          </dl>
          <p v-if="enterprisePool?.pool_exhausted" class="mt-3 text-xs text-red-600 dark:text-red-400">
            {{ t('enterprise.usage.poolExhausted') }}
          </p>
        </template>
      </article>
    </div>

    <div class="card card-body">
      <div class="mb-4">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.usage.filterTitle') }}</h2>
        <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.usage.filterNote') }}</span>
      </div>
      <div class="flex flex-wrap items-end gap-3">
        <div class="min-w-[200px] flex-1">
          <Input
            :model-value="toLocalInput(dateRange[0])"
            type="datetime-local"
            :label="t('enterprise.usage.filterStart')"
            data-testid="usage-filter-start"
            @update:model-value="(value: string) => setRange(0, value)"
          />
        </div>
        <span class="pb-2.5 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.usage.filterSeparator') }}</span>
        <div class="min-w-[200px] flex-1">
          <Input
            :model-value="toLocalInput(dateRange[1])"
            type="datetime-local"
            :label="t('enterprise.usage.filterEnd')"
            data-testid="usage-filter-end"
            @update:model-value="(value: string) => setRange(1, value)"
          />
        </div>
        <button class="btn btn-primary btn-md" data-testid="usage-filter-apply" @click="applyFilter">
          <Icon name="search" size="sm" />
          {{ t('enterprise.usage.filterQuery') }}
        </button>
        <button
          v-if="dateRange.length"
          class="btn btn-secondary btn-md"
          data-testid="usage-filter-clear"
          @click="clearFilter"
        >
          {{ t('enterprise.usage.filterClear') }}
        </button>
      </div>
    </div>

    <div class="card card-body">
      <div class="mb-4 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.usage.trendTitle') }}</h2>
          <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.usage.trendNote') }}</span>
        </div>
        <span class="whitespace-nowrap text-xs text-gray-500 dark:text-dark-400">{{ trendSourceLabel }}</span>
      </div>
      <div v-if="trendLoading" class="grid h-48 place-items-center" data-testid="usage-trend-loading">
        <LoadingSpinner size="md" />
      </div>
      <div
        v-else-if="!trend.length"
        class="grid h-48 place-items-center text-center text-sm text-gray-500 dark:text-dark-400"
        data-testid="usage-trend-empty"
      >
        {{ t('enterprise.usage.trendEmpty') }}
      </div>
      <div
        v-else
        class="flex h-48 items-end gap-4 border-b border-l border-gray-200 px-3 pb-6 pt-4 dark:border-dark-700"
        :aria-label="t('enterprise.usage.trendTitle')"
      >
        <div v-for="point in trend" :key="point.at" class="flex h-full min-w-[18px] flex-1 flex-col items-center justify-end gap-2">
          <span
            class="w-full max-w-[44px] rounded-t bg-primary-500"
            :style="{ height: `${trendHeight(point.requests)}%` }"
            :title="t('enterprise.usage.trendTooltip', { date: formatDate(point.at), count: point.requests, cost: point.actual_cost })"
          />
          <small class="whitespace-nowrap text-[10px] text-gray-500 dark:text-dark-400">{{ trendLabel(point.at) }}</small>
        </div>
      </div>
    </div>

    <div class="card card-body">
      <div class="mb-4">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.usage.detailTitle') }}</h2>
        <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.usage.detailNote') }}</span>
      </div>
      <DataTable :columns="detailColumns" :data="records.items" :loading="detailLoading" :row-key="detailRowKey">
        <template #cell-request_at="{ row }">{{ formatDate(row.request_at) }}</template>
        <template #cell-window_anchor="{ row }">{{ formatDate(row.window_anchor) }}</template>
        <template #empty>
          <p class="text-sm text-gray-500 dark:text-dark-400" data-testid="usage-detail-empty">
            {{ t('enterprise.usage.detailEmpty') }}
          </p>
        </template>
      </DataTable>
      <Pagination
        v-if="records.total"
        class="mt-4"
        :total="records.total"
        :page="page"
        :page-size="pageSize"
        @update:page="onPageChange"
        @update:page-size="onPageSizeChange"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import DataTable from '@/components/common/DataTable.vue'
import Input from '@/components/common/Input.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import type { Column } from '@/components/common/types'
import { enterpriseAPI } from '@/api/enterprise'
import { useAppStore } from '@/stores/app'
import { sourceStatusLabel } from '@/utils/enterpriseDisplay'
import { formatDate } from '@/utils/format'
import type { EnterpriseEmployeeUsageRecord, EnterpriseEmployeeUsageSummary, EnterpriseEnterprisePoolStatus, EnterpriseEmployeeUsageTrendPoint, EnterprisePaginated } from '@/types/enterprise'

type SourceState = 'loading' | 'ready' | 'unavailable'
const emptyPage = <T,>(): EnterprisePaginated<T> => ({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })

const { t, locale } = useI18n()
const appStore = useAppStore()

const usage = ref<EnterpriseEmployeeUsageSummary>()
const enterprisePool = ref<EnterpriseEnterprisePoolStatus>()
const records = reactive(emptyPage<EnterpriseEmployeeUsageRecord>())
const trend = ref<EnterpriseEmployeeUsageTrendPoint[]>([])
const dateRange = ref<string[]>([])
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const detailLoading = ref(false)
const trendLoading = ref(false)
const sourceStates = reactive({ usage: 'loading' as SourceState, detail: 'loading' as SourceState, trend: 'loading' as SourceState })

const hasSourceIssue = computed(() => Object.values(sourceStates).some((state) => state === 'unavailable'))
const sourceIssueText = computed(() =>
  Object.entries(sourceStates)
    .filter(([, state]) => state === 'unavailable')
    .map(([name]) => t(`enterprise.usage.source${name.charAt(0).toUpperCase()}${name.slice(1)}`))
    .join('、'),
)
const poolSourceLabel = computed(() =>
  sourceStatusLabel(enterprisePool.value?.source_status === 'available' ? 'available' : 'unavailable', t),
)
const trendSourceLabel = computed(() =>
  sourceStates.trend === 'ready' ? t('enterprise.usage.trendSource') : sourceStatusLabel('unavailable', t),
)
const hasOverage = computed(() => Boolean(usage.value && usage.value.overage !== '0'))

const detailColumns = computed<Column[]>(() => [
  { key: 'request_at', label: t('enterprise.common.requestAt') },
  { key: 'api_key_masked', label: t('enterprise.common.key') },
  { key: 'generation', label: t('enterprise.common.generation') },
  { key: 'window_anchor', label: t('enterprise.common.windowAnchor') },
  { key: 'actual_cost', label: t('admin.enterprise.terms.actualCost') },
])
const detailRowKey = (row: EnterpriseEmployeeUsageRecord) => `${row.request_at}-${row.api_key_masked}-${row.generation}`

const filterParams = (): Record<string, string> => {
  const [start_at, end_at] = dateRange.value
  return Object.fromEntries(
    Object.entries({ start_at, end_at }).filter(([, value]) => value !== undefined && value !== ''),
  ) as Record<string, string>
}
const formatOptionalDate = (value?: string) => (value ? formatDate(value) : t('enterprise.common.notAvailable'))
const trendLabel = (value: string) =>
  new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', { month: 'numeric', day: 'numeric' }).format(new Date(value))
const trendHeight = (value: number) =>
  Math.max(8, Math.round((value / Math.max(...trend.value.map((point) => point.requests), 1)) * 100))

// datetime-local 控件只认本地时间字符串，查询参数仍保持 ISO 口径
const toLocalInput = (value?: string) => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (input: number) => String(input).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}
function setRange(index: 0 | 1, value: string) {
  const next = [...dateRange.value]
  next[index] = value ? new Date(value).toISOString() : ''
  dateRange.value = next.some((item) => item) ? next : []
}

async function loadHome() {
  sourceStates.usage = 'loading'
  try {
    const home = await enterpriseAPI.getEmployeeUsage({ suppressUnavailableRedirect: true })
    usage.value = home.usage
    enterprisePool.value = home.enterprise_pool
    sourceStates.usage = 'ready'
  } catch {
    sourceStates.usage = 'unavailable'
    throw new Error('usage')
  }
}
async function loadDetail() {
  detailLoading.value = true
  sourceStates.detail = 'loading'
  try {
    Object.assign(records, await enterpriseAPI.listEmployeeUsage({ ...filterParams(), page: page.value, page_size: pageSize.value }, { suppressUnavailableRedirect: true }))
    sourceStates.detail = 'ready'
  } catch {
    sourceStates.detail = 'unavailable'
    throw new Error('detail')
  } finally {
    detailLoading.value = false
  }
}
async function loadTrend() {
  trendLoading.value = true
  sourceStates.trend = 'loading'
  try {
    trend.value = await enterpriseAPI.getEmployeeUsageTrend(filterParams(), { suppressUnavailableRedirect: true })
    sourceStates.trend = 'ready'
  } catch {
    sourceStates.trend = 'unavailable'
    throw new Error('trend')
  } finally {
    trendLoading.value = false
  }
}
function resetData() {
  usage.value = undefined
  enterprisePool.value = undefined
  trend.value = []
  Object.assign(records, emptyPage<EnterpriseEmployeeUsageRecord>())
}
async function load() {
  loading.value = true
  resetData()
  const results = await Promise.allSettled([loadHome(), loadDetail(), loadTrend()])
  if (results.some((result) => result.status === 'rejected')) appStore.showError(t('enterprise.usage.loadFailed'), 5000)
  loading.value = false
}
async function applyFilter() {
  page.value = 1
  try {
    await Promise.all([loadDetail(), loadTrend()])
  } catch {
    appStore.showError(t('enterprise.usage.filterFailed'), 5000)
  }
}
async function clearFilter() {
  dateRange.value = []
  await applyFilter()
}
async function onPageChange(value: number) {
  page.value = value
  await loadDetail().catch(() => undefined)
}
async function onPageSizeChange(value: number) {
  pageSize.value = value
  page.value = 1
  await loadDetail().catch(() => undefined)
}
onMounted(load)
</script>
