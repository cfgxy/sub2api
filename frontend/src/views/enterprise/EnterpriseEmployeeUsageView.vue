<template>
  <section class="workspace">
    <div class="page-heading">
      <div>
        <h1>个人用量</h1>
        <p>查看本人 weekly 归因结果、企业总池对照与调用明细。</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </div>

    <div v-if="hasSourceIssue" class="source-notice" role="status">
      <strong>部分数据源暂时不可用</strong>
      <span>{{ sourceIssueText }}。页面不会用旧值或伪造数据冒充实时结果。</span>
    </div>

    <section class="compare-grid" aria-label="个人与企业状态对照">
      <article class="panel">
        <div class="panel-heading"><div><h2>我的额度</h2><span>weekly 窗口 · {{ formatOptionalDate(usage?.window_anchor) }}</span></div></div>
        <div v-if="usage?.source_status === 'unavailable'" class="empty-chart">当前没有可核验的订阅窗口，未显示伪造余额</div>
        <el-descriptions v-else :column="2" border>
          <el-descriptions-item label="allocation">{{ usage?.allocation ?? '0' }}</el-descriptions-item>
          <el-descriptions-item label="actual cost">{{ usage?.actual_cost ?? '0' }}</el-descriptions-item>
          <el-descriptions-item label="remaining">{{ usage?.remaining ?? '0' }}</el-descriptions-item>
          <el-descriptions-item label="overage"><span :class="{ overage: usage && usage.overage !== '0' }">{{ usage?.overage ?? '0' }}</span></el-descriptions-item>
          <el-descriptions-item label="请求数" :span="2">{{ usage?.requests ?? 0 }}</el-descriptions-item>
        </el-descriptions>
      </article>
      <article class="panel">
        <div class="panel-heading"><div><h2>企业总池</h2><span>{{ poolSourceLabel }}</span></div></div>
        <div v-if="enterprisePool?.source_status === 'unavailable'" class="empty-chart">企业总池来源暂不可用，未显示伪造余额</div>
        <el-descriptions v-else :column="2" border>
          <el-descriptions-item label="总池上限">{{ enterprisePool?.pool_limit ?? '0' }}</el-descriptions-item>
          <el-descriptions-item label="已用">{{ enterprisePool?.pool_used ?? '0' }}</el-descriptions-item>
          <el-descriptions-item label="剩余" :span="2"><span :class="{ 'danger-value': enterprisePool?.pool_exhausted }">{{ enterprisePool?.pool_remaining ?? '0' }}</span></el-descriptions-item>
        </el-descriptions>
        <p v-if="enterprisePool?.pool_exhausted" class="pool-warning">企业总池已耗尽，与个人 overage 分开处理，请联系企业管理员或等待订阅恢复。</p>
      </article>
    </section>

    <div class="panel filter-panel">
      <div class="panel-heading"><div><h2>时间筛选</h2><span>作用于调用趋势与明细，不影响以上 weekly 快照</span></div></div>
      <div class="filters-inline">
        <el-date-picker v-model="dateRange" type="datetimerange" value-format="YYYY-MM-DDTHH:mm:ssZ" range-separator="至" start-placeholder="开始时间" end-placeholder="结束时间" />
        <el-button type="primary" :icon="Search" @click="applyFilter">查询</el-button>
        <el-button v-if="dateRange.length" @click="clearFilter">清除</el-button>
      </div>
    </div>

    <div class="panel trend-panel">
      <div class="panel-heading"><div><h2>调用趋势</h2><span>按筛选窗口聚合的真实每日数据，无记录不补齐为 0</span></div><span class="source-label">{{ trendSourceLabel }}</span></div>
      <div v-if="!trendLoading && !trend.length" class="empty-chart">当前筛选范围暂无趋势数据</div>
      <div v-else class="trend-chart" aria-label="调用趋势图">
        <div v-for="point in trend" :key="point.at" class="trend-column">
          <span class="trend-bar" :style="{ height: `${trendHeight(point.requests)}%` }" :title="`${formatDate(point.at)}：${point.requests} 次，actual cost ${point.actual_cost}`" />
          <small>{{ trendLabel(point.at) }}</small>
        </div>
      </div>
    </div>

    <div class="panel table-panel">
      <div class="table-heading"><div><h2>调用明细</h2><span>含 assignment generation，用于 Key 轮换后的归属追溯</span></div></div>
      <div class="table-wrap"><el-table v-loading="detailLoading" :data="records.items" stripe>
        <el-table-column label="请求时间" width="180"><template #default="{ row }">{{ formatDate(row.request_at) }}</template></el-table-column>
        <el-table-column prop="api_key_masked" label="Key" width="160" />
        <el-table-column prop="generation" label="generation" width="110" />
        <el-table-column label="窗口锚点" width="180"><template #default="{ row }">{{ formatDate(row.window_anchor) }}</template></el-table-column>
        <el-table-column prop="actual_cost" label="actual cost" width="130" />
      </el-table></div>
      <el-empty v-if="!detailLoading && !records.items.length" description="当前筛选范围暂无调用明细" />
      <el-pagination v-if="records.total" v-model:current-page="page" v-model:page-size="pageSize" :total="records.total" layout="total, sizes, prev, pager, next" @current-change="loadDetail" @size-change="loadDetail" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseEmployeeUsageRecord, EnterpriseEmployeeUsageSummary, EnterpriseEnterprisePoolStatus, EnterpriseEmployeeUsageTrendPoint, EnterprisePaginated } from '@/types/enterprise'

type SourceState = 'loading' | 'ready' | 'unavailable'
const emptyPage = <T,>(): EnterprisePaginated<T> => ({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })

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
const sourceIssueText = computed(() => Object.entries(sourceStates).filter(([, state]) => state === 'unavailable').map(([name]) => ({ usage: '个人额度', detail: '调用明细', trend: '调用趋势' }[name])).join('、'))
const poolSourceLabel = computed(() => enterprisePool.value?.source_status === 'available' ? '来源正常' : '来源不可用')
const trendSourceLabel = computed(() => sourceStates.trend === 'ready' ? '来源：个人归因记录' : '来源不可用')

const filterParams = (): Record<string, string> => {
  const [start_at, end_at] = dateRange.value
  return Object.fromEntries(Object.entries({ start_at, end_at }).filter(([, value]) => value !== undefined && value !== '')) as Record<string, string>
}
const formatDate = (value: string | Date) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
const formatOptionalDate = (value?: string) => value ? formatDate(value) : '未获取'
const trendLabel = (value: string) => new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric' }).format(new Date(value))
const trendHeight = (value: number) => Math.max(8, Math.round((value / Math.max(...trend.value.map((point) => point.requests), 1)) * 100))

async function loadHome() {
  sourceStates.usage = 'loading'
  try {
    const home = await enterpriseAPI.getEmployeeUsage()
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
    Object.assign(records, await enterpriseAPI.listEmployeeUsage({ ...filterParams(), page: page.value, page_size: pageSize.value }))
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
    trend.value = await enterpriseAPI.getEmployeeUsageTrend(filterParams())
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
  if (results.some((result) => result.status === 'rejected')) ElMessage.error('部分个人用量数据暂时不可用')
  loading.value = false
}
async function applyFilter() {
  page.value = 1
  try {
    await Promise.all([loadDetail(), loadTrend()])
  } catch {
    ElMessage.error('筛选查询失败，数据暂时不可用')
  }
}
async function clearFilter() {
  dateRange.value = []
  await applyFilter()
}
onMounted(load)
</script>

<style scoped>
.workspace{max-width:1100px;min-width:0;color:#111827}
.page-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:20px}
.page-heading h1{margin:0 0 6px;font-size:26px}
.page-heading p{margin:0;color:#6b7280}
.source-notice{display:flex;gap:10px;align-items:center;margin-bottom:16px;padding:11px 14px;border:1px solid #fde68a;border-radius:8px;background:#fffbeb;color:#92400e}
.source-notice span{font-size:12px}
.compare-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px;margin-bottom:16px}
.panel{min-width:0;margin-top:0;padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}
.filter-panel,.trend-panel,.table-panel{margin-top:16px}
.panel-heading,.table-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:16px}
.panel-heading h2,.table-heading h2{margin:0 0 4px;font-size:14px}
.panel-heading span,.table-heading span{color:#6b7280;font-size:11px}
.source-label{white-space:nowrap;color:#6b7280;font-size:11px}
.danger-value,.overage{color:#b91c1c!important}
.pool-warning{margin:12px 0 0;color:#b91c1c;font-size:12px}
.filters-inline{display:flex;gap:8px;flex-wrap:wrap}
.trend-chart{display:flex;align-items:flex-end;gap:16px;height:205px;padding:16px 12px 26px;border-left:1px solid #d1d5db;border-bottom:1px solid #d1d5db;background:repeating-linear-gradient(to bottom,transparent 0,transparent 40px,#f0f1f3 41px)}
.trend-column{display:flex;flex:1;min-width:18px;height:100%;flex-direction:column;align-items:center;justify-content:flex-end;gap:8px}
.trend-bar{width:min(44px,100%);min-height:8%;border-radius:4px 4px 0 0;background:#2563eb}
.trend-column small{color:#6b7280;font-size:10px;white-space:nowrap}
.empty-chart{display:grid;place-items:center;height:120px;color:#6b7280;text-align:center}
.trend-panel .empty-chart{height:205px}
.table-panel{padding:0;overflow:hidden}
.table-heading{padding:18px 18px 0;margin-bottom:14px}
.table-wrap{width:100%;overflow-x:auto}
.table-wrap :deep(.el-table){min-width:760px}
.table-wrap :deep(.el-table th.el-table__cell){background:#f9fafb;color:#6b7280;font-size:11px}
.el-empty{padding:20px}
.el-pagination{justify-content:flex-end;margin:16px 18px}
@media(max-width:1000px){.compare-grid{grid-template-columns:1fr}}
@media(max-width:640px){.page-heading{flex-direction:column}.page-heading .el-button{width:100%}.panel{padding:14px}.table-heading{padding:14px 14px 0}.filters-inline{width:100%}.filters-inline>*{flex:1}}
</style>
