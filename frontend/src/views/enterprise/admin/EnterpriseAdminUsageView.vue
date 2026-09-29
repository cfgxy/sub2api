<template>
  <TablePageLayout>
    <template #actions>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 class="text-xl font-bold text-gray-900 dark:text-white">企业用量</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">核对企业总池、员工额度与调用明细。</p>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">
          <Icon name="refresh" size="md" class="mr-2" :class="{ 'animate-spin': loading }" />刷新数据
        </button>
      </div>
    </template>

    <template #filters>
      <div class="space-y-4">
        <div v-if="hasSourceIssue" role="status" class="card border-l-4 border-l-amber-500 p-4 text-sm text-gray-900 dark:text-white">
          部分数据源暂时不可用：{{ sourceIssueText }}
        </div>
        <div class="card grid grid-cols-1 gap-3 p-4 sm:grid-cols-2 xl:grid-cols-4">
          <Select v-model="filters.window_type" :options="windowOptions" aria-label="统计窗口" @update:model-value="applyFilters" />
          <Select v-model="filters.department_id" :options="departmentOptions" placeholder="部门" aria-label="部门" clearable @update:model-value="applyFilters" />
          <Select v-model="filters.employee_id" :options="employeeOptions" placeholder="员工" aria-label="员工" clearable @update:model-value="applyFilters" />
          <input v-model="filters.model" class="input" placeholder="模型" aria-label="模型" @keyup.enter="applyFilters" />
          <input v-model="startAt" type="datetime-local" class="input" aria-label="开始时间" />
          <input v-model="endAt" type="datetime-local" class="input" aria-label="结束时间" />
          <button type="button" class="btn btn-primary" :disabled="loading" @click="applyFilters"><Icon name="search" size="md" class="mr-2" />查询</button>
        </div>
      </div>
    </template>

    <template #table>
      <div class="space-y-5 pb-4">
        <section v-if="sourceStates.summary === 'unavailable'" class="p-6">
          <EmptyState icon="exclamationTriangle" title="汇总与趋势数据源不可用" action-text="重试" @action="load" />
        </section>
        <template v-else>
          <section class="grid grid-cols-1 gap-3 px-4 pt-4 sm:grid-cols-2 xl:grid-cols-4" aria-label="企业额度概览">
            <div class="card p-4"><p class="text-xs text-gray-500 dark:text-dark-400">{{ terms.pool }}</p><p class="mt-2 text-xl font-bold text-gray-900 dark:text-white">{{ summary?.enterprise_pool_limit ?? '0' }}</p><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ poolSourceLabel }}</p></div>
            <div class="card p-4"><p class="text-xs text-gray-500 dark:text-dark-400">总池{{ terms.remaining }}</p><p class="mt-2 text-xl font-bold text-gray-900 dark:text-white">{{ summary?.enterprise_pool_remaining ?? '0' }}</p><p v-if="summary?.enterprise_pool_exhausted" class="mt-1 text-xs text-red-600 dark:text-red-400">总池已耗尽</p></div>
            <div class="card p-4"><p class="text-xs text-gray-500 dark:text-dark-400">筛选用量</p><p class="mt-2 text-xl font-bold text-gray-900 dark:text-white">{{ summary?.total_usage_credit ?? '0' }}</p><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ summary?.total_requests ?? 0 }} 次请求</p></div>
            <div class="card p-4"><p class="text-xs text-gray-500 dark:text-dark-400">个人{{ terms.overage }}员工数</p><p class="mt-2 text-xl font-bold text-gray-900 dark:text-white">{{ overageEmployeeCount }}</p></div>
          </section>
          <div class="grid grid-cols-1 gap-4 px-4 lg:grid-cols-2">
            <section class="card p-4">
              <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">用量趋势</h3>
              <EnterpriseUsageTrendChart v-if="summary?.usage_trend?.length" :labels="trendLabels" :values="trendValues" dataset-label="请求数" />
              <EmptyState v-else title="当前筛选条件暂无趋势数据" />
            </section>
            <section class="card p-4">
              <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">员工排名（Top 8）</h3>
              <div v-if="topEmployees.length" class="space-y-3">
                <div v-for="item in topEmployees" :key="item.employee_id" class="min-w-0 border-b border-gray-200 pb-2 text-sm dark:border-dark-700">
                  <div class="flex flex-wrap justify-between gap-2"><span class="break-all font-medium text-gray-900 dark:text-white">{{ item.email }}</span><span class="text-gray-500 dark:text-dark-400">{{ item.usage_credit }} / {{ item.configured_credit }}</span></div>
                  <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ terms.remaining }} {{ item.remaining_credit }}<span v-if="item.overage_credit !== '0'" class="ml-2 text-red-600 dark:text-red-400">{{ terms.overage }} {{ item.overage_credit }}</span></p>
                </div>
              </div>
              <EmptyState v-else title="当前筛选条件暂无员工用量" />
            </section>
          </div>
        </template>

        <section aria-label="调用明细">
          <h3 class="px-4 pb-3 text-sm font-semibold text-gray-900 dark:text-white">调用明细</h3>
          <div v-if="sourceStates.usage === 'unavailable'" class="p-6"><EmptyState icon="exclamationTriangle" title="用量明细数据源暂时不可用" action-text="重试" @action="load" /></div>
          <DataTable v-else :columns="columns" :data="usage.items" :loading="usageLoading" row-key="attribution_id">
            <template #cell-request_at="{ value }">{{ formatDate(value) }}</template>
            <template #cell-employee_email="{ value }"><span class="break-all">{{ value || '未归属员工' }}</span></template>
            <template #cell-model="{ value }">{{ value || '未记录' }}</template>
            <template #cell-window_type="{ value }">{{ windowTypeLabel(value, t) }}</template>
            <template #cell-classification="{ value }">{{ value === 'employee' ? '员工' : value === 'controlled_external' ? '受控外部' : '未知归属' }}</template>
            <template #empty><EmptyState title="当前筛选条件暂无明细" /></template>
          </DataTable>
        </section>
      </div>
    </template>

    <template #pagination>
      <Pagination v-if="usage.total && sourceStates.usage !== 'unavailable'" :total="usage.total" :page="usagePage" :page-size="pageSize" @update:page="onPage" @update:pageSize="onPageSize" />
    </template>
  </TablePageLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'
import EnterpriseUsageTrendChart from '@/components/charts/EnterpriseUsageTrendChart.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { enterpriseAPI } from '@/api/enterprise'
import { sourceStatusLabel, windowTypeLabel } from '@/utils/enterpriseDisplay'
import type { Column } from '@/components/common/types'
import type { EnterpriseDepartment, EnterpriseEmployee, EnterprisePaginated, EnterpriseWorkbenchSummary, EnterpriseWorkbenchUsageRow } from '@/types/enterprise'

type SourceState = 'loading' | 'ready' | 'unavailable'
const { t } = useI18n()
const appStore = useAppStore()
const terms = computed(() => ({ pool: t('admin.enterprise.terms.pool'), remaining: t('admin.enterprise.terms.remaining'), overage: t('admin.enterprise.terms.overage'), allocation: t('admin.enterprise.terms.allocation'), actualCost: t('admin.enterprise.terms.actualCost') }))
const columns = computed<Column[]>(() => [
  { key: 'request_at', label: '请求时间' }, { key: 'employee_email', label: '员工邮箱' }, { key: 'api_key_masked', label: 'Key' },
  { key: 'model', label: '模型' }, { key: 'window_type', label: '窗口' }, { key: 'usage_credit', label: terms.value.actualCost },
  { key: 'configured_credit', label: terms.value.allocation }, { key: 'classification', label: '归属' }
])
const windowOptions = [{ value: 'week', label: '按周' }]
const departments = ref<EnterpriseDepartment[]>([]), employees = ref<EnterpriseEmployee[]>([])
const departmentOptions = computed(() => departments.value.map(item => ({ value: item.id, label: item.name })))
const employeeOptions = computed(() => employees.value.map(item => ({ value: item.id, label: item.email })))
const summary = ref<EnterpriseWorkbenchSummary>()
const emptyPage = (): EnterprisePaginated<EnterpriseWorkbenchUsageRow> => ({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
const usage = reactive(emptyPage())
const filters = reactive<{ department_id?: number; employee_id?: number; model?: string; window_type: string }>({ window_type: 'week' })
const startAt = ref(''), endAt = ref('')
const sourceStates = reactive({ summary: 'loading' as SourceState, usage: 'loading' as SourceState, directories: 'loading' as SourceState })
const loading = ref(false), usageLoading = ref(false), usagePage = ref(1), pageSize = ref(20)
const hasSourceIssue = computed(() => Object.values(sourceStates).some(value => value === 'unavailable'))
const sourceIssueText = computed(() => Object.entries(sourceStates).filter(([, value]) => value === 'unavailable').map(([key]) => ({ summary: '汇总/趋势', usage: '用量明细', directories: '组织目录' }[key])).join('、'))
const poolSourceLabel = computed(() => `来源${sourceStatusLabel(summary.value?.pool_source_status || 'unavailable', t)}`)
const topEmployees = computed(() => [...(summary.value?.employee_summaries || [])].sort((a, b) => Number(b.usage_credit) - Number(a.usage_credit)).slice(0, 8))
const overageEmployeeCount = computed(() => (summary.value?.employee_summaries || []).filter(item => Number(item.overage_credit) > 0).length)
const trendLabels = computed(() => (summary.value?.usage_trend || []).map(item => new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric' }).format(new Date(item.at))))
const trendValues = computed(() => (summary.value?.usage_trend || []).map(item => item.requests))
const formatDate = (value: string) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
const params = (page?: number): Record<string, string | number> => Object.fromEntries(Object.entries({
  ...filters, start_at: startAt.value ? new Date(startAt.value).toISOString() : undefined,
  end_at: endAt.value ? new Date(endAt.value).toISOString() : undefined, page, page_size: pageSize.value
}).filter(([, value]) => value !== undefined && value !== '')) as Record<string, string | number>

async function loadSummary() {
  summary.value = undefined
  sourceStates.summary = 'loading'
  try { summary.value = await enterpriseAPI.getWorkbenchSummary(params(), { suppressUnavailableRedirect: true }); sourceStates.summary = 'ready' }
  catch { sourceStates.summary = 'unavailable' }
}
async function loadUsage() {
  usageLoading.value = true
  sourceStates.usage = 'loading'
  Object.assign(usage, emptyPage())
  try { Object.assign(usage, await enterpriseAPI.listWorkbenchUsage(params(usagePage.value), { suppressUnavailableRedirect: true })); sourceStates.usage = 'ready' }
  catch { sourceStates.usage = 'unavailable' }
  finally { usageLoading.value = false }
}
async function loadDirectories() {
  sourceStates.directories = 'loading'
  try {
    const [loadedDepartments, loadedEmployees] = await Promise.all([enterpriseAPI.listDepartments({ suppressUnavailableRedirect: true }), enterpriseAPI.listEmployees({ suppressUnavailableRedirect: true })])
    departments.value = loadedDepartments
    employees.value = loadedEmployees
    sourceStates.directories = 'ready'
  } catch { departments.value = []; employees.value = []; sourceStates.directories = 'unavailable' }
}
async function load() {
  loading.value = true
  await Promise.all([loadSummary(), loadUsage(), loadDirectories()])
  if (hasSourceIssue.value) appStore.showError('部分用量数据暂时不可用')
  loading.value = false
}
async function applyFilters() { usagePage.value = 1; loading.value = true; await Promise.all([loadSummary(), loadUsage()]); if (hasSourceIssue.value) appStore.showError('用量数据暂时不可用'); loading.value = false }
function onPage(page: number) { usagePage.value = page; void loadUsage() }
function onPageSize(size: number) { pageSize.value = size; usagePage.value = 1; void loadUsage() }
onMounted(load)
</script>
