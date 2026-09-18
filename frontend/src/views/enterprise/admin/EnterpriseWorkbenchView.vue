<template>
  <section class="workspace">
    <div class="page-heading">
      <div>
        <div class="eyebrow">企业管理 / 运营概览</div>
        <h1>管理员工作台</h1>
        <p>查看企业总池、员工分配与关键运营状态。</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新数据</el-button>
    </div>

    <div v-if="hasSourceIssue" class="source-notice" role="status">
      <strong>部分数据源暂时不可用</strong>
      <span>{{ sourceIssueText }}。页面已清除本次查询前的数据，不以旧值伪装实时结果。</span>
    </div>

    <section class="kpis" aria-label="企业额度概览">
      <article class="kpi-card">
        <span class="kpi-label">当前订阅</span>
        <strong>{{ summary?.subscription_plan || '未获取' }}</strong>
        <small>{{ summary?.subscription_status || '上游订阅来源不可用' }}</small>
      </article>
      <article class="kpi-card">
        <span class="kpi-label">企业总池</span>
        <strong>{{ summary?.enterprise_pool_limit || '0' }}</strong>
        <small>weekly 上游硬额度 · {{ poolSourceLabel }}{{ poolObservedAtLabel ? ` · ${poolObservedAtLabel}` : '' }}</small>
      </article>
      <article class="kpi-card">
        <span class="kpi-label">总池剩余</span>
        <strong :class="{ 'danger-value': summary?.enterprise_pool_exhausted }">{{ summary?.enterprise_pool_remaining || '0' }}</strong>
        <small>{{ summary?.enterprise_pool_exhausted ? '总池已耗尽：联系平台或等待订阅恢复' : '上游订阅权威值，剩余不会为负' }}</small>
      </article>
      <article class="kpi-card">
        <span class="kpi-label">筛选用量</span>
        <strong>{{ summary?.total_usage_credit || '0' }}</strong>
        <small>{{ summary?.total_requests || 0 }} 次请求 · 当前筛选范围</small>
      </article>
    </section>

    <section class="status-row">
      <div class="status-chip" :class="summary?.enterprise_pool_exhausted ? 'is-danger' : 'is-ok'">
        <span class="status-dot" />
        <div><b>{{ summary?.enterprise_pool_exhausted ? '企业总池已耗尽' : '企业总池可用' }}</b><small>{{ summary?.enterprise_pool_exhausted ? '不等同于个人超用，请先处理订阅来源' : '企业硬额度与个人 allocation 分开核对' }}</small></div>
      </div>
      <div class="status-chip is-neutral"><span class="status-dot" /><div><b>{{ summary?.employee_count || 0 }} 名员工有用量</b><small>在职员工 {{ summary?.active_employee_count || 0 }} 名</small></div></div>
      <div class="status-chip is-neutral"><span class="status-dot" /><div><b>统计窗口：weekly</b><small>窗口锚点 {{ formatOptionalDate(summary?.pool_window_anchor) }}</small></div></div>
      <div v-if="summary?.scheduled_subscription_since" class="status-chip is-neutral"><span class="status-dot" /><div><b>已排期订阅：{{ summary.scheduled_subscription_plan || '未知计划' }}</b><small>创建于 {{ formatOptionalDate(summary.scheduled_subscription_since) }}，尚未生效</small></div></div>
    </section>

    <section class="overview-grid">
      <article class="panel trend-panel">
        <div class="panel-heading"><div><h2>用量趋势</h2><span>按当前筛选范围聚合，单位：请求数</span></div><span class="source-label">{{ trendSourceLabel }}</span></div>
        <div v-if="!loading && !summary?.usage_trend?.length" class="empty-chart">当前筛选条件暂无趋势数据</div>
        <div v-else class="trend-chart">
          <div class="trend-ylabels"><span v-for="label in trendYLabels" :key="label">{{ label }}</span></div>
          <div class="trend-plot">
            <svg viewBox="0 0 720 174" preserveAspectRatio="none" aria-label="用量趋势图">
              <path :d="trendPathD" fill="none" stroke="#2563eb" stroke-width="3" stroke-linejoin="round" />
              <circle v-if="trendPoints.length" :cx="trendPoints[trendPoints.length - 1].x" :cy="trendPoints[trendPoints.length - 1].y" r="5" fill="#fff" stroke="#2563eb" stroke-width="3" />
            </svg>
            <div class="trend-xlabels"><span v-for="point in trendPoints" :key="point.at" :title="`${formatDate(point.at)}：${point.requests} 次`">{{ point.label }}</span></div>
          </div>
        </div>
      </article>
      <article class="panel allocation-panel">
        <div class="panel-heading"><div><h2>员工额度使用</h2><span>allocation 仅为配置与计量口径</span></div></div>
        <div v-if="!summary?.employee_summaries?.length" class="empty-chart">当前筛选条件暂无员工用量</div>
        <div v-for="item in topEmployees" :key="item.employee_id" class="progress-row">
          <div class="progress-top"><b>{{ item.email }}</b><span>{{ item.usage_credit }} / {{ item.configured_credit }}</span></div>
          <el-progress :percentage="usagePercentage(item)" :show-text="false" :status="item.overage_credit !== '0' ? 'exception' : undefined" />
          <div class="progress-note"><span>剩余 {{ item.remaining_credit }}</span><span v-if="item.overage_credit !== '0'" class="overage">超用 {{ item.overage_credit }}</span><span v-else>{{ item.recommendation }}</span></div>
        </div>
      </article>
    </section>

    <article class="panel activity-panel">
      <div class="panel-heading"><div><h2>最近操作</h2><span>近期企业管理审计事件</span></div><RouterLink class="panel-link" to="/enterprise/admin/audit">查看审计</RouterLink></div>
      <div v-if="activityState === 'unavailable'" class="empty-chart">审计数据源暂时不可用</div>
      <div v-else-if="!recentAuditEvents.length" class="empty-chart">暂无最近操作</div>
      <ul v-else class="activity-timeline">
        <li v-for="event in recentAuditEvents" :key="event.id" class="activity-event">
          <b>{{ event.event_type }}{{ event.entity_type ? ` · ${event.entity_type}` : '' }}</b>
          <p>{{ displayActor(event.actor_ref) }} · {{ formatDate(event.created_at) }}</p>
        </li>
      </ul>
    </article>

    <div class="panel table-panel">
      <div class="table-heading"><div><h2>员工分配概况</h2><span>企业总池耗尽与个人 overage 分开处理；完整用量明细见「企业用量」页，操作审计见「管理审计」页</span></div></div>
      <div class="table-wrap"><el-table :data="summary?.employee_summaries || []" stripe>
        <el-table-column prop="email" label="员工" min-width="220" />
        <el-table-column prop="requests" label="请求数" width="100" />
        <el-table-column prop="configured_credit" label="allocation" width="130" />
        <el-table-column prop="usage_credit" label="actual cost" width="130" />
        <el-table-column prop="remaining_credit" label="remaining" width="130" />
        <el-table-column label="overage" width="130"><template #default="{ row }"><span :class="row.overage_credit !== '0' ? 'overage' : 'muted'">{{ row.overage_credit }}</span></template></el-table-column>
        <el-table-column prop="recommendation" label="处理建议" min-width="250" />
      </el-table></div>
      <el-empty v-if="!loading && !(summary?.employee_summaries?.length)" description="当前筛选条件暂无用量" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseEmployee, EnterpriseWorkbenchAuditEvent, EnterpriseWorkbenchEmployeeSummary, EnterpriseWorkbenchSummary } from '@/types/enterprise'

type SourceState = 'loading' | 'ready' | 'unavailable'
const departments = ref<EnterpriseDepartment[]>([])
const employees = ref<EnterpriseEmployee[]>([])
const summary = ref<EnterpriseWorkbenchSummary>()
const recentAuditEvents = ref<EnterpriseWorkbenchAuditEvent[]>([])
const activityState = ref<SourceState>('loading')
const sourceStates = reactive({ summary: 'loading' as SourceState, directories: 'loading' as SourceState })
const loading = ref(false)
const hasSourceIssue = computed(() => Object.values(sourceStates).some((state) => state === 'unavailable'))
const sourceIssueText = computed(() => Object.entries(sourceStates).filter(([, state]) => state === 'unavailable').map(([name]) => ({ summary: '汇总', directories: '组织目录' }[name])).join('、'))
const poolSourceLabel = computed(() => summary.value?.pool_source_status === 'available' ? '来源正常' : '来源不可用')
const poolObservedAtLabel = computed(() => summary.value?.pool_observed_at ? `更新于 ${new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(summary.value.pool_observed_at))}` : '')
const trendSourceLabel = computed(() => sourceStates.summary === 'ready' ? '来源：企业 usage' : '来源不可用')
const topEmployees = computed(() => [...(summary.value?.employee_summaries || [])].sort((a, b) => Number(b.usage_credit) - Number(a.usage_credit)).slice(0, 4))

// The overview summary intentionally omits window_type: it reports the
// overall trend/allocation snapshot regardless of window, unlike the
// window-scoped filters on the standalone usage page.
const params = (): Record<string, string | number> => ({})
const formatDate = (value: string | Date) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
const formatOptionalDate = (value?: string) => value ? formatDate(value) : '未获取'
const trendLabel = (value: string) => new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric' }).format(new Date(value))
const trendMaxValue = computed(() => Math.max(...(summary.value?.usage_trend || []).map((item) => item.requests), 1))
const trendPoints = computed(() => {
  const trend = summary.value?.usage_trend || []
  const width = 720, height = 174
  const stepX = trend.length > 1 ? width / (trend.length - 1) : 0
  return trend.map((point, index) => ({
    x: trend.length > 1 ? index * stepX : width / 2,
    y: height - (point.requests / trendMaxValue.value) * height,
    at: point.at,
    requests: point.requests,
    label: trendLabel(point.at),
  }))
})
const trendPathD = computed(() => trendPoints.value.map((point, index) => `${index === 0 ? 'M' : 'L'}${point.x.toFixed(1)} ${point.y.toFixed(1)}`).join(' '))
const trendYLabels = computed(() => [1, 0.75, 0.5, 0.25, 0].map((fraction) => Math.round(trendMaxValue.value * fraction)))
const usagePercentage = (item: EnterpriseWorkbenchEmployeeSummary) => Math.min(100, Math.round((Number(item.usage_credit) / Math.max(Number(item.configured_credit), 1)) * 100))
const displayActor = (value: string) => value.toLowerCase().includes('session') ? 'enterprise_actor' : value
const resetData = () => { summary.value = undefined; sourceStates.summary = 'loading' }

async function loadSummary() { try { summary.value = await enterpriseAPI.getWorkbenchSummary(params()); sourceStates.summary = 'ready' } catch { sourceStates.summary = 'unavailable'; throw new Error('summary') } }
async function loadDirectories() { try { [departments.value, employees.value] = await Promise.all([enterpriseAPI.listDepartments(), enterpriseAPI.listEmployees()]); sourceStates.directories = 'ready' } catch { sourceStates.directories = 'unavailable'; throw new Error('directories') } }
async function loadRecentActivity() {
  activityState.value = 'loading'
  try {
    const result = await enterpriseAPI.listWorkbenchAuditEvents({ page: 1, page_size: 5 }, { suppressUnavailableRedirect: true })
    recentAuditEvents.value = result.items
    activityState.value = 'ready'
  } catch {
    recentAuditEvents.value = []
    activityState.value = 'unavailable'
  }
}
async function load() { loading.value = true; resetData(); const results = await Promise.allSettled([loadSummary(), loadDirectories()]); if (results.some((result) => result.status === 'rejected')) ElMessage.error('部分工作台数据暂时不可用'); loading.value = false }
onMounted(() => { load(); loadRecentActivity() })
</script>

<style scoped>
.workspace{min-width:0;color:#111827}.page-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:20px}.eyebrow{margin-bottom:6px;color:#6b7280;font-size:11px;font-weight:700}.page-heading h1{margin:0 0 5px;font-size:24px;line-height:30px}.page-heading p{margin:0;color:#6b7280}.source-notice{display:flex;gap:10px;align-items:center;margin-bottom:16px;padding:11px 14px;border:1px solid #fde68a;border-radius:8px;background:#fffbeb;color:#92400e}.source-notice span{font-size:12px}.kpis{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px;margin-bottom:16px}.kpi-card{min-height:112px;padding:16px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.kpi-label{display:block;color:#6b7280;font-size:12px}.kpi-card strong{display:block;margin:9px 0 5px;font-size:24px;line-height:28px}.kpi-card small,.panel-heading span,.progress-note{color:#6b7280;font-size:11px}.danger-value,.overage{color:#b91c1c!important}.status-row{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin-bottom:16px}.status-chip{display:flex;align-items:flex-start;gap:10px;padding:12px 14px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.status-chip b,.status-chip small{display:block}.status-chip small{margin-top:4px;color:#6b7280;font-size:11px}.status-dot{width:8px;height:8px;flex:none;margin-top:4px;border-radius:50%;background:#9ca3af}.is-ok .status-dot{background:#15803d}.is-danger .status-dot{background:#b91c1c}.overview-grid{display:grid;grid-template-columns:minmax(0,1.45fr) minmax(320px,.75fr);gap:16px;margin-bottom:16px}.panel{min-width:0;padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.panel-heading,.table-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:16px}.panel-heading h2,.table-heading h2{margin:0 0 4px;font-size:14px}.source-label{white-space:nowrap}.trend-chart{display:flex;gap:8px;height:205px}.trend-ylabels{display:flex;flex-direction:column;justify-content:space-between;min-width:30px;padding:4px 0 24px;color:#6b7280;font-size:10px;text-align:right}.trend-plot{position:relative;flex:1;padding:8px 8px 24px;border-left:1px solid #d1d5db;border-bottom:1px solid #d1d5db;background:repeating-linear-gradient(to bottom,transparent 0,transparent 40px,#f0f1f3 41px)}.trend-plot svg{display:block;width:100%;height:calc(100% - 24px);overflow:visible}.trend-xlabels{position:absolute;left:8px;right:8px;bottom:2px;display:flex;justify-content:space-between;color:#6b7280;font-size:10px}.empty-chart{display:grid;place-items:center;height:205px;color:#6b7280}.activity-panel{margin-bottom:16px}.panel-link{color:#2563eb;font-size:12px;text-decoration:none;font-weight:600;white-space:nowrap}.activity-timeline{margin:0;padding:0 0 0 20px;list-style:none;border-left:1px solid #d1d5db}.activity-event{position:relative;margin-bottom:17px}.activity-event:last-child{margin-bottom:0}.activity-event:before{content:"";position:absolute;left:-25px;top:3px;width:8px;height:8px;border-radius:50%;background:#2563eb;border:2px solid #fff;box-shadow:0 0 0 1px #93c5fd}.activity-event b{font-size:12px}.activity-event p{margin:4px 0 0;color:#6b7280;font-size:11px;line-height:17px}.progress-row{margin-bottom:17px}.progress-row:last-child{margin-bottom:0}.progress-top,.progress-note{display:flex;justify-content:space-between;gap:12px}.progress-top{margin-bottom:7px;font-size:12px}.progress-note{margin-top:5px}.progress-note .overage{font-weight:700}.workbench-tabs :deep(.el-tabs__item){height:44px}.table-panel{padding:0;overflow:hidden}.table-heading{padding:18px 18px 0;margin-bottom:14px}.table-wrap{width:100%;overflow-x:auto}.table-wrap :deep(.el-table){min-width:920px}.table-wrap :deep(.el-table th.el-table__cell){background:#f9fafb;color:#6b7280;font-size:11px}.table-wrap :deep(.el-table td.el-table__cell),.table-wrap :deep(.el-table th.el-table__cell){height:48px}.el-empty{padding:20px}.el-pagination{justify-content:flex-end;margin:16px 18px}.filters-inline{display:flex;gap:8px}.audit-toolbar{display:grid;grid-template-columns:minmax(180px,1.5fr) repeat(6,minmax(120px,1fr)) auto;gap:8px;padding:18px;border-bottom:1px solid #e5e7eb}.audit-detail-status{display:inline-flex;padding:5px 10px;border-radius:999px;background:#eff6ff;color:#2563eb;font-size:12px;font-weight:700}.audit-detail-status.success{background:#ecfdf5;color:#15803d}.audit-detail-status.danger{background:#fef2f2;color:#b91c1c}.audit-details{margin:20px 0}.audit-details div{display:grid;grid-template-columns:90px 1fr;gap:12px;padding:11px 0;border-bottom:1px solid #f0f1f3}.audit-details dt{color:#6b7280}.audit-details dd{margin:0;overflow-wrap:anywhere}.audit-details+h3{margin-top:24px;font-size:13px}.audit-payload{max-height:320px;overflow:auto;padding:12px;border:1px solid #e5e7eb;border-radius:6px;background:#f9fafb;color:#374151;font:12px/18px ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre-wrap;overflow-wrap:anywhere}@media(max-width:1000px){.kpis{grid-template-columns:repeat(2,minmax(0,1fr))}.overview-grid{grid-template-columns:1fr}.status-row{grid-template-columns:1fr}.audit-toolbar{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:640px){.page-heading{flex-direction:column}.page-heading .el-button{width:100%}.kpis{grid-template-columns:1fr}.audit-toolbar{grid-template-columns:1fr}.filters-inline{width:100%}.filters-inline>*{flex:1}.panel{padding:14px}.table-heading{padding:14px 14px 0}}
</style>
