<template>
  <div class="space-y-6">
    <!-- 页头 -->
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0">
        <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-dark-400">
          企业管理 / 运营概览
        </p>
        <h2 class="mt-1 text-xl font-bold text-gray-900 dark:text-white">管理员工作台</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          查看{{ terms.pool }}、员工{{ terms.allocation }}与关键运营状态。
        </p>
      </div>
      <button
        type="button"
        class="btn btn-secondary w-full sm:w-auto"
        :disabled="loading"
        data-testid="workbench-refresh"
        @click="load"
      >
        <Icon name="refresh" size="md" class="mr-2" :class="{ 'animate-spin': loading }" />
        刷新数据
      </button>
    </div>

    <!-- 错误态：数据源不可用 -->
    <div
      v-if="hasSourceIssue"
      role="status"
      class="card flex flex-col gap-1 border-l-4 border-l-amber-500 p-4"
    >
      <span class="text-sm font-semibold text-gray-900 dark:text-white">部分数据源暂时不可用</span>
      <span class="text-xs text-gray-500 dark:text-dark-400">
        {{ sourceIssueText }}。页面已清除本次查询前的数据，不以旧值伪装实时结果。
      </span>
      <div>
        <button type="button" class="btn btn-secondary btn-sm mt-2" :disabled="loading" @click="load">
          重试
        </button>
      </div>
    </div>

    <!-- 加载态 -->
    <div v-if="loading && !summary" class="flex items-center justify-center py-16">
      <LoadingSpinner />
    </div>

    <template v-else>
      <!-- 概览指标 -->
      <section class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4" aria-label="企业额度概览">
        <div class="card p-4">
          <p class="text-xs font-medium text-gray-500 dark:text-dark-400">当前订阅</p>
          <p class="mt-2 truncate text-xl font-bold text-gray-900 dark:text-white">
            {{ summary?.subscription_plan || '未获取' }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ subscriptionStatusText }}
          </p>
        </div>
        <div class="card p-4">
          <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ terms.pool }}</p>
          <p class="mt-2 truncate text-xl font-bold text-gray-900 dark:text-white">
            {{ summary?.enterprise_pool_limit || '0' }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            每周上游硬{{ terms.quota }} · {{ poolSourceLabel }}{{ poolObservedAtLabel ? ` · ${poolObservedAtLabel}` : '' }}
          </p>
        </div>
        <div class="card p-4">
          <p class="text-xs font-medium text-gray-500 dark:text-dark-400">总池{{ terms.remaining }}</p>
          <p
            class="mt-2 truncate text-xl font-bold"
            :class="summary?.enterprise_pool_exhausted ? 'text-red-600 dark:text-red-400' : 'text-gray-900 dark:text-white'"
          >
            {{ summary?.enterprise_pool_remaining || '0' }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ summary?.enterprise_pool_exhausted ? '总池已耗尽：联系平台或等待订阅恢复' : '上游订阅权威值，剩余不会为负' }}
          </p>
        </div>
        <div class="card p-4">
          <p class="text-xs font-medium text-gray-500 dark:text-dark-400">筛选用量</p>
          <p class="mt-2 truncate text-xl font-bold text-gray-900 dark:text-white">
            {{ summary?.total_usage_credit || '0' }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ summary?.total_requests || 0 }} 次请求 · 当前筛选范围
          </p>
        </div>
      </section>

      <!-- 状态摘要 -->
      <section class="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div class="card flex items-start gap-3 p-4">
          <StatusBadge
            :status="summary?.enterprise_pool_exhausted ? 'error' : 'active'"
            label=""
            class="mt-1"
          />
          <div class="min-w-0">
            <p class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ summary?.enterprise_pool_exhausted ? `${terms.pool}已耗尽` : `${terms.pool}可用` }}
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ summary?.enterprise_pool_exhausted
                ? `不等同于个人${terms.overage}，请先处理订阅来源`
                : `企业硬${terms.quota}与个人${terms.allocation}分开核对` }}
            </p>
          </div>
        </div>
        <div class="card flex items-start gap-3 p-4">
          <StatusBadge status="neutral" label="" class="mt-1" />
          <div class="min-w-0">
            <p class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ summary?.employee_count || 0 }} 名员工有用量
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              在职员工 {{ summary?.active_employee_count || 0 }} 名
            </p>
          </div>
        </div>
        <div class="card flex items-start gap-3 p-4">
          <StatusBadge status="neutral" label="" class="mt-1" />
          <div class="min-w-0">
            <p class="text-sm font-semibold text-gray-900 dark:text-white">统计窗口：每周</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              窗口锚点 {{ formatOptionalDate(summary?.pool_window_anchor) }}
            </p>
          </div>
        </div>
        <div v-if="summary?.scheduled_subscription_since" class="card flex items-start gap-3 p-4">
          <StatusBadge status="neutral" label="" class="mt-1" />
          <div class="min-w-0">
            <p class="text-sm font-semibold text-gray-900 dark:text-white">
              已排期订阅：{{ summary.scheduled_subscription_plan || '未知计划' }}
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              创建于 {{ formatOptionalDate(summary.scheduled_subscription_since) }}，尚未生效
            </p>
          </div>
        </div>
      </section>

      <!-- 趋势与员工额度 -->
      <section class="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <div class="card p-4 xl:col-span-2">
          <div class="mb-4 flex flex-wrap items-start justify-between gap-2">
            <div>
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">用量趋势</h3>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                按当前筛选范围聚合，单位：请求数
              </p>
            </div>
            <span class="whitespace-nowrap text-xs text-gray-500 dark:text-dark-400">
              {{ trendSourceLabel }}
            </span>
          </div>
          <div
            v-if="!trendLabels.length"
            class="flex h-56 items-center justify-center text-sm text-gray-500 dark:text-dark-400"
          >
            当前筛选条件暂无趋势数据
          </div>
          <EnterpriseUsageTrendChart
            v-else
            :labels="trendLabels"
            :values="trendValues"
            dataset-label="请求数"
          />
        </div>

        <div class="card p-4">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">员工{{ terms.allocation }}使用</h3>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
            {{ terms.allocation }}仅为配置与计量口径
          </p>
          <div
            v-if="!topEmployees.length"
            class="flex h-48 items-center justify-center text-sm text-gray-500 dark:text-dark-400"
          >
            当前筛选条件暂无员工用量
          </div>
          <div v-else class="mt-4 space-y-4">
            <div v-for="item in topEmployees" :key="item.employee_id">
              <div class="flex items-baseline justify-between gap-3 text-xs">
                <span class="truncate font-medium text-gray-900 dark:text-white" :title="item.email">
                  {{ item.email }}
                </span>
                <span class="whitespace-nowrap text-gray-500 dark:text-dark-400">
                  {{ item.usage_credit }} / {{ item.configured_credit }}
                </span>
              </div>
              <div
                class="progress mt-2"
                role="progressbar"
                :aria-valuenow="usagePercentage(item)"
                aria-valuemin="0"
                aria-valuemax="100"
                :aria-label="`${item.email} ${terms.allocation}使用率`"
              >
                <div
                  class="progress-bar"
                  :class="{ 'from-red-500 to-red-400': isPositiveAmount(item.overage_credit) }"
                  :style="{ width: `${usagePercentage(item)}%` }"
                ></div>
              </div>
              <div class="mt-1.5 flex justify-between gap-3 text-xs text-gray-500 dark:text-dark-400">
                <span>{{ terms.remaining }} {{ item.remaining_credit }}</span>
                <span
                  v-if="isPositiveAmount(item.overage_credit)"
                  class="font-semibold text-red-600 dark:text-red-400"
                >
                  {{ terms.overage }} {{ item.overage_credit }}
                </span>
                <span v-else class="truncate">{{ t('admin.enterprise.terms.withinAllocation') }}</span>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- 最近操作 -->
      <section class="card p-4">
        <div class="mb-4 flex flex-wrap items-start justify-between gap-2">
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">最近操作</h3>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">近期企业管理审计事件</p>
          </div>
          <RouterLink
            to="/enterprise/admin/audit"
            class="whitespace-nowrap text-xs font-semibold text-primary-600 hover:text-primary-700 dark:text-primary-400"
          >
            查看审计
          </RouterLink>
        </div>
        <div
          v-if="activityState === 'unavailable'"
          class="flex h-32 items-center justify-center text-sm text-gray-500 dark:text-dark-400"
        >
          审计数据源暂时不可用
        </div>
        <div
          v-else-if="!recentAuditEvents.length"
          class="flex h-32 items-center justify-center text-sm text-gray-500 dark:text-dark-400"
        >
          暂无最近操作
        </div>
        <ul v-else class="space-y-4 border-l border-gray-200 pl-5 dark:border-dark-700">
          <li v-for="event in recentAuditEvents" :key="event.id" class="relative">
            <span
              class="absolute -left-[23px] top-1.5 h-2 w-2 rounded-full bg-primary-500 ring-2 ring-white dark:ring-dark-900"
              aria-hidden="true"
            ></span>
            <p class="text-xs font-semibold text-gray-900 dark:text-white" :title="`${event.event_type} · ${event.entity_type}`">
              {{ auditEventLabel(event.event_type, t) }}
              <span v-if="event.entity_type" class="font-normal text-gray-500 dark:text-dark-400">
                · {{ auditEntityLabel(event.entity_type, t) }}
              </span>
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ auditActorLabel(event.actor_ref, t) }} · {{ formatDate(event.created_at) }}
            </p>
          </li>
        </ul>
      </section>

      <!-- 员工分配概况 -->
      <section class="card p-4">
        <div class="mb-4">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">员工分配概况</h3>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
            {{ terms.pool }}耗尽与个人{{ terms.overage }}分开处理；完整用量明细见「企业用量」页，操作审计见「管理审计」页
          </p>
        </div>
        <DataTable
          :columns="employeeColumns"
          :data="employeeSummaries"
          :loading="loading"
          row-key="employee_id"
        >
          <template #cell-overage_credit="{ value }">
            <span
              :class="isPositiveAmount(value) ? 'font-semibold text-red-600 dark:text-red-400' : 'text-gray-500 dark:text-dark-400'"
            >
              {{ value }}
            </span>
          </template>
          <template #cell-recommendation="{ row }">
            <span :class="isPositiveAmount(row.overage_credit) ? 'text-red-600 dark:text-red-400' : 'text-gray-500 dark:text-dark-400'">
              {{ isPositiveAmount(row.overage_credit) ? t('admin.enterprise.terms.reviewOverage') : t('admin.enterprise.terms.withinAllocation') }}
            </span>
          </template>
          <template #empty>
            <EmptyState title="当前筛选条件暂无用量" description="调整筛选范围或等待员工产生调用后再查看。" />
          </template>
        </DataTable>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import EnterpriseUsageTrendChart from '@/components/charts/EnterpriseUsageTrendChart.vue'
import { useAppStore } from '@/stores/app'
import { enterpriseAPI } from '@/api/enterprise'
import { auditActorLabel, auditEntityLabel, auditEventLabel, sourceStatusLabel, subscriptionStatusLabel } from '@/utils/enterpriseDisplay'
import type { Column } from '@/components/common/types'
import type {
  EnterpriseDepartment,
  EnterpriseEmployee,
  EnterpriseWorkbenchAuditEvent,
  EnterpriseWorkbenchEmployeeSummary,
  EnterpriseWorkbenchSummary
} from '@/types/enterprise'

const { t } = useI18n()
const appStore = useAppStore()

const terms = computed(() => ({
  allocation: t('admin.enterprise.terms.allocation'),
  remaining: t('admin.enterprise.terms.remaining'),
  overage: t('admin.enterprise.terms.overage'),
  quota: t('admin.enterprise.terms.quota'),
  actualCost: t('admin.enterprise.terms.actualCost'),
  pool: t('admin.enterprise.terms.pool')
}))

type SourceState = 'loading' | 'ready' | 'unavailable'
const departments = ref<EnterpriseDepartment[]>([])
const employees = ref<EnterpriseEmployee[]>([])
const summary = ref<EnterpriseWorkbenchSummary>()
const recentAuditEvents = ref<EnterpriseWorkbenchAuditEvent[]>([])
const activityState = ref<SourceState>('loading')
const sourceStates = reactive({ summary: 'loading' as SourceState, directories: 'loading' as SourceState })
const loading = ref(false)

const hasSourceIssue = computed(() => Object.values(sourceStates).some((state) => state === 'unavailable'))
const sourceIssueText = computed(() =>
  Object.entries(sourceStates)
    .filter(([, state]) => state === 'unavailable')
    .map(([name]) => ({ summary: '汇总', directories: '组织目录' }[name]))
    .join('、')
)
const subscriptionStatusText = computed(() =>
  summary.value?.subscription_status
    ? subscriptionStatusLabel(summary.value.subscription_status, t)
    : '上游订阅来源不可用'
)
const poolSourceLabel = computed(
  () => `来源${sourceStatusLabel(summary.value?.pool_source_status || 'unavailable', t)}`
)
const poolObservedAtLabel = computed(() =>
  summary.value?.pool_observed_at
    ? `更新于 ${new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(summary.value.pool_observed_at))}`
    : ''
)
const trendSourceLabel = computed(() =>
  sourceStates.summary === 'ready' ? '来源：企业用量' : '来源不可用'
)

const employeeSummaries = computed(() => summary.value?.employee_summaries || [])
const topEmployees = computed(() =>
  [...employeeSummaries.value].sort((a, b) => Number(b.usage_credit) - Number(a.usage_credit)).slice(0, 4)
)

const employeeColumns = computed<Column[]>(() => [
  { key: 'email', label: '员工' },
  { key: 'requests', label: '请求数' },
  { key: 'configured_credit', label: terms.value.allocation },
  { key: 'usage_credit', label: terms.value.actualCost },
  { key: 'remaining_credit', label: terms.value.remaining },
  { key: 'overage_credit', label: terms.value.overage },
  { key: 'recommendation', label: '处理建议' }
])

// 后端以 NUMERIC(20,8) 定长字符串返回（零值为 "0.00000000"），按数值判断而非字符串比较
const isPositiveAmount = (value?: string | null) => Number(value ?? 0) > 0

// 概览汇总有意不传 window_type：它反映整体快照，与用量页的窗口筛选口径不同
const params = (): Record<string, string | number> => ({})
const formatDate = (value: string | Date) =>
  new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
const formatOptionalDate = (value?: string) => (value ? formatDate(value) : '未获取')
const trendLabel = (value: string) =>
  new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric' }).format(new Date(value))
const trendLabels = computed(() => (summary.value?.usage_trend || []).map((point) => trendLabel(point.at)))
const trendValues = computed(() => (summary.value?.usage_trend || []).map((point) => point.requests))

const usagePercentage = (item: EnterpriseWorkbenchEmployeeSummary) =>
  Math.min(100, Math.round((Number(item.usage_credit) / Math.max(Number(item.configured_credit), 1)) * 100))
const resetData = () => {
  summary.value = undefined
  sourceStates.summary = 'loading'
}

async function loadSummary() {
  try {
    summary.value = await enterpriseAPI.getWorkbenchSummary(params(), { suppressUnavailableRedirect: true })
    sourceStates.summary = 'ready'
  } catch {
    sourceStates.summary = 'unavailable'
    throw new Error('summary')
  }
}

async function loadDirectories() {
  try {
    [departments.value, employees.value] = await Promise.all([
      enterpriseAPI.listDepartments({ suppressUnavailableRedirect: true }),
      enterpriseAPI.listEmployees({ suppressUnavailableRedirect: true })
    ])
    sourceStates.directories = 'ready'
  } catch {
    sourceStates.directories = 'unavailable'
    throw new Error('directories')
  }
}

async function loadRecentActivity() {
  activityState.value = 'loading'
  try {
    const result = await enterpriseAPI.listWorkbenchAuditEvents(
      { page: 1, page_size: 5 },
      { suppressUnavailableRedirect: true }
    )
    recentAuditEvents.value = result.items
    activityState.value = 'ready'
  } catch {
    recentAuditEvents.value = []
    activityState.value = 'unavailable'
  }
}

async function load() {
  loading.value = true
  resetData()
  const results = await Promise.allSettled([loadSummary(), loadDirectories()])
  if (results.some((result) => result.status === 'rejected')) {
    appStore.showError('部分工作台数据暂时不可用')
  }
  loading.value = false
}

onMounted(() => {
  load()
  loadRecentActivity()
})
</script>
