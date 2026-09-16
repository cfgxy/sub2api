<template>
  <section class="workspace">
    <div class="page-heading">
      <div>
        <div class="eyebrow">企业管理 / 用量</div>
        <h1>企业用量</h1>
        <p>按时间、员工、部门与模型筛选企业调用明细，核对企业总池与个人 overage。</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新数据</el-button>
    </div>

    <div v-if="hasSourceIssue" class="source-notice" role="status">
      <strong>部分数据源暂时不可用</strong>
      <span>{{ sourceIssueText }}。页面已清除本次查询前的数据，不以旧值伪装实时结果。</span>
    </div>

    <section class="kpis" aria-label="企业额度概览">
      <article class="kpi-card">
        <span class="kpi-label">企业总池</span>
        <strong>{{ summary?.enterprise_pool_limit || '0' }}</strong>
        <small>weekly 上游硬额度 · {{ poolSourceLabel }}</small>
      </article>
      <article class="kpi-card">
        <span class="kpi-label">总池剩余</span>
        <strong :class="{ 'danger-value': summary?.enterprise_pool_exhausted }">{{ summary?.enterprise_pool_remaining || '0' }}</strong>
        <small>{{ summary?.enterprise_pool_exhausted ? '总池已耗尽：企业级共享硬额度，与员工个人 overage 分开核对' : '上游订阅权威值，剩余不会为负' }}</small>
      </article>
      <article class="kpi-card">
        <span class="kpi-label">筛选用量</span>
        <strong>{{ summary?.total_usage_credit || '0' }}</strong>
        <small>{{ summary?.total_requests || 0 }} 次请求 · 当前筛选范围</small>
      </article>
      <article class="kpi-card">
        <span class="kpi-label">个人 overage 员工数</span>
        <strong :class="{ 'danger-value': overageEmployeeCount > 0 }">{{ overageEmployeeCount }}</strong>
        <small>个人 allocation 超用，独立于企业总池是否耗尽</small>
      </article>
    </section>

    <section class="panel filter-panel">
      <div class="filter-grid">
        <!-- 后端仅接受空值或 "week"（backend/internal/enterpriseidentity/workbench.go
             parseWorkbenchQuery 对非 week 的取值一律 400），底层数据模型也只有订阅结算周
             一种窗口粒度；此前提供的「按天/按月」选项后端必拒绝，导致整页降级为数据源
             不可用（B1）。真实的日/月聚合属于 R1 范围（不含范围），本单不做。 -->
        <el-select v-model="filters.window_type" placeholder="统计窗口" @change="applyFilters">
          <el-option label="按周" value="week" />
        </el-select>
        <el-select v-model="filters.department_id" clearable filterable placeholder="部门" @change="applyFilters">
          <el-option v-for="dept in departments" :key="dept.id" :label="dept.name" :value="dept.id" />
        </el-select>
        <el-select v-model="filters.employee_id" clearable filterable placeholder="员工" @change="applyFilters">
          <el-option v-for="employee in employees" :key="employee.id" :label="employee.email" :value="employee.id" />
        </el-select>
        <el-input v-model="filters.model" clearable placeholder="模型" @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-date-picker v-model="dateRange" type="datetimerange" value-format="YYYY-MM-DDTHH:mm:ssZ" range-separator="至" start-placeholder="开始时间" end-placeholder="结束时间" @change="applyFilters" />
        <el-button type="primary" :icon="Search" @click="applyFilters">查询</el-button>
      </div>
    </section>

    <section class="overview-grid">
      <article class="panel trend-panel">
        <div class="panel-heading"><div><h2>用量趋势</h2><span>按当前筛选范围聚合，单位：请求数</span></div><span class="source-label">{{ trendSourceLabel }}</span></div>
        <div v-if="sourceStates.summary === 'unavailable'" class="empty-chart">数据源不可用，请稍后重试</div>
        <div v-else-if="!loading && !summary?.usage_trend?.length" class="empty-chart">当前筛选条件暂无趋势数据</div>
        <div v-else class="trend-chart" aria-label="用量趋势图">
          <div v-for="point in summary?.usage_trend || []" :key="point.at" class="trend-column">
            <span class="trend-bar" :style="{ height: `${trendHeight(point.requests)}%` }" :title="`${formatDate(point.at)}：${point.requests} 次`" />
            <small>{{ trendLabel(point.at) }}</small>
          </div>
        </div>
      </article>
      <article class="panel allocation-panel">
        <div class="panel-heading"><div><h2>员工排名（Top 8）</h2><span>企业总池 vs. 个人 overage 分开处理</span></div></div>
        <div v-if="sourceStates.summary === 'unavailable'" class="empty-chart">数据源不可用，请稍后重试</div>
        <div v-else-if="!summary?.employee_summaries?.length" class="empty-chart">当前筛选条件暂无员工用量</div>
        <div v-for="item in topEmployees" :key="item.employee_id" class="progress-row">
          <div class="progress-top"><b>{{ item.email }}</b><span>{{ item.usage_credit }} / {{ item.configured_credit }}</span></div>
          <el-progress :percentage="usagePercentage(item)" :show-text="false" :status="item.overage_credit !== '0' ? 'exception' : undefined" />
          <div class="progress-note"><span>剩余 {{ item.remaining_credit }}</span><span v-if="item.overage_credit !== '0'" class="overage">超用 {{ item.overage_credit }}</span><span v-else>{{ item.recommendation }}</span></div>
        </div>
      </article>
    </section>

    <div class="panel table-panel">
      <div class="table-heading"><div><h2>调用明细</h2><span>历史 Key 保留员工归属，Key 值始终掩码</span></div></div>
      <div v-if="sourceStates.usage === 'unavailable'" class="empty-chart">用量明细数据源暂时不可用</div>
      <template v-else>
        <div class="table-wrap"><el-table v-loading="usageLoading" :data="usage.items" stripe>
          <el-table-column prop="request_at" label="请求时间" width="180"><template #default="{ row }">{{ formatDate(row.request_at) }}</template></el-table-column>
          <el-table-column prop="employee_email" label="员工" min-width="200" />
          <el-table-column prop="api_key_masked" label="Key" width="160" />
          <el-table-column prop="model" label="模型" min-width="140"><template #default="{ row }">{{ row.model || '未记录' }}</template></el-table-column>
          <el-table-column prop="window_type" label="窗口" width="90" />
          <el-table-column prop="usage_credit" label="actual cost" width="130" />
          <el-table-column prop="configured_credit" label="allocation" width="130" />
          <el-table-column prop="classification" label="归属" width="120"><template #default="{ row }">{{ row.classification === 'employee' ? '员工' : '受控外部' }}</template></el-table-column>
        </el-table></div>
        <el-empty v-if="!usageLoading && !usage.items.length" description="当前筛选条件暂无明细" />
        <el-pagination v-if="usage.total" v-model:current-page="usagePage" v-model:page-size="pageSize" :total="usage.total" layout="total, sizes, prev, pager, next" @current-change="loadUsage" @size-change="loadUsage" />
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseEmployee, EnterprisePaginated, EnterpriseWorkbenchEmployeeSummary, EnterpriseWorkbenchSummary, EnterpriseWorkbenchUsageRow } from '@/types/enterprise'

type SourceState = 'loading' | 'ready' | 'unavailable'
const emptyPage = <T,>(): EnterprisePaginated<T> => ({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })

const departments = ref<EnterpriseDepartment[]>([])
const employees = ref<EnterpriseEmployee[]>([])
const summary = ref<EnterpriseWorkbenchSummary>()
const usage = reactive(emptyPage<EnterpriseWorkbenchUsageRow>())
const filters = reactive<{ department_id?: number; employee_id?: number; model?: string; window_type: string }>({ window_type: 'week' })
const dateRange = ref<string[]>([])
const sourceStates = reactive({ summary: 'loading' as SourceState, usage: 'loading' as SourceState, directories: 'loading' as SourceState })
const loading = ref(false), usageLoading = ref(false), usagePage = ref(1), pageSize = ref(20)

const hasSourceIssue = computed(() => Object.values(sourceStates).some((state) => state === 'unavailable'))
const sourceIssueText = computed(() => Object.entries(sourceStates).filter(([, state]) => state === 'unavailable').map(([name]) => ({ summary: '汇总/趋势', usage: '用量明细', directories: '组织目录' }[name])).join('、'))
const poolSourceLabel = computed(() => summary.value?.pool_source_status === 'available' ? '来源正常' : '来源不可用')
const trendSourceLabel = computed(() => sourceStates.summary === 'ready' ? '来源：企业 usage' : '来源不可用')
const topEmployees = computed(() => [...(summary.value?.employee_summaries || [])].sort((a, b) => Number(b.usage_credit) - Number(a.usage_credit)).slice(0, 8))
const overageEmployeeCount = computed(() => (summary.value?.employee_summaries || []).filter((item) => item.overage_credit !== '0').length)

const params = (page?: number, includeWindow = true): Record<string, string | number> => Object.fromEntries(Object.entries({ ...filters, window_type: includeWindow ? filters.window_type : undefined, start_at: dateRange.value[0], end_at: dateRange.value[1], page, page_size: pageSize.value }).filter(([, value]) => value !== undefined && value !== '')) as Record<string, string | number>
const formatDate = (value: string | Date) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
const trendLabel = (value: string) => new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric' }).format(new Date(value))
const trendHeight = (value: number) => Math.max(8, Math.round((value / Math.max(...(summary.value?.usage_trend || []).map((item) => item.requests), 1)) * 100))
const usagePercentage = (item: EnterpriseWorkbenchEmployeeSummary) => Math.min(100, Math.round((Number(item.usage_credit) / Math.max(Number(item.configured_credit), 1)) * 100))
const resetData = () => { summary.value = undefined; Object.assign(usage, emptyPage<EnterpriseWorkbenchUsageRow>()); sourceStates.summary = 'loading'; sourceStates.usage = 'loading' }

async function loadSummary() { try { summary.value = await enterpriseAPI.getWorkbenchSummary(params(undefined, true), { suppressUnavailableRedirect: true }); sourceStates.summary = 'ready' } catch { sourceStates.summary = 'unavailable'; throw new Error('summary') } }
async function loadUsage() { usageLoading.value = true; sourceStates.usage = 'loading'; try { Object.assign(usage, await enterpriseAPI.listWorkbenchUsage(params(usagePage.value), { suppressUnavailableRedirect: true })); sourceStates.usage = 'ready' } catch { sourceStates.usage = 'unavailable'; throw new Error('usage') } finally { usageLoading.value = false } }
async function loadDirectories() { try { [departments.value, employees.value] = await Promise.all([enterpriseAPI.listDepartments({ suppressUnavailableRedirect: true }), enterpriseAPI.listEmployees({ suppressUnavailableRedirect: true })]); sourceStates.directories = 'ready' } catch { sourceStates.directories = 'unavailable'; throw new Error('directories') } }
async function load() { loading.value = true; resetData(); const results = await Promise.allSettled([loadSummary(), loadUsage(), loadDirectories()]); if (results.some((result) => result.status === 'rejected')) ElMessage.error('部分用量数据暂时不可用'); loading.value = false }
async function applyFilters() { usagePage.value = 1; loading.value = true; try { await Promise.all([loadSummary(), loadUsage()]) } catch { ElMessage.error('用量数据暂时不可用') } finally { loading.value = false } }

onMounted(load)
</script>

<style scoped>
.workspace{min-width:0;color:#111827}.page-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:20px}.eyebrow{margin-bottom:6px;color:#6b7280;font-size:11px;font-weight:700}.page-heading h1{margin:0 0 5px;font-size:24px;line-height:30px}.page-heading p{margin:0;color:#6b7280}.source-notice{display:flex;gap:10px;align-items:center;margin-bottom:16px;padding:11px 14px;border:1px solid #fde68a;border-radius:8px;background:#fffbeb;color:#92400e}.source-notice span{font-size:12px}.kpis{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px;margin-bottom:16px}.kpi-card{min-height:112px;padding:16px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.kpi-label{display:block;color:#6b7280;font-size:12px}.kpi-card strong{display:block;margin:9px 0 5px;font-size:24px;line-height:28px}.kpi-card small,.panel-heading span,.progress-note{color:#6b7280;font-size:11px}.danger-value,.overage{color:#b91c1c!important}.filter-panel{padding:16px;margin-bottom:16px}.filter-grid{display:grid;grid-template-columns:repeat(5,minmax(140px,1fr)) auto;gap:10px}.overview-grid{display:grid;grid-template-columns:minmax(0,1.45fr) minmax(320px,.75fr);gap:16px;margin-bottom:16px}.panel{min-width:0;padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.panel-heading,.table-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:16px}.panel-heading h2,.table-heading h2{margin:0 0 4px;font-size:14px}.source-label{white-space:nowrap}.trend-chart{display:flex;align-items:flex-end;gap:16px;height:205px;padding:16px 12px 26px;border-left:1px solid #d1d5db;border-bottom:1px solid #d1d5db;background:repeating-linear-gradient(to bottom,transparent 0,transparent 40px,#f0f1f3 41px)}.trend-column{display:flex;flex:1;min-width:18px;height:100%;flex-direction:column;align-items:center;justify-content:flex-end;gap:8px}.trend-bar{width:min(44px,100%);min-height:8%;border-radius:4px 4px 0 0;background:#2563eb}.trend-column small{color:#6b7280;font-size:10px;white-space:nowrap}.empty-chart{display:grid;place-items:center;height:205px;color:#6b7280}.progress-row{margin-bottom:17px}.progress-row:last-child{margin-bottom:0}.progress-top,.progress-note{display:flex;justify-content:space-between;gap:12px}.progress-top{margin-bottom:7px;font-size:12px}.progress-note{margin-top:5px}.progress-note .overage{font-weight:700}.table-panel{padding:0;overflow:hidden}.table-heading{padding:18px 18px 0;margin-bottom:14px}.table-panel .empty-chart{margin:0 18px 18px}.table-wrap{width:100%;overflow-x:auto}.table-wrap :deep(.el-table){min-width:920px}.table-wrap :deep(.el-table th.el-table__cell){background:#f9fafb;color:#6b7280;font-size:11px}.table-wrap :deep(.el-table td.el-table__cell),.table-wrap :deep(.el-table th.el-table__cell){height:48px}.el-empty{padding:20px}.el-pagination{justify-content:flex-end;margin:16px 18px}@media(max-width:1000px){.kpis{grid-template-columns:repeat(2,minmax(0,1fr))}.overview-grid{grid-template-columns:1fr}.filter-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:640px){.page-heading{flex-direction:column}.page-heading .el-button{width:100%}.kpis{grid-template-columns:1fr}.filter-grid{grid-template-columns:1fr}.panel{padding:14px}.table-heading{padding:14px 14px 0}}
</style>
