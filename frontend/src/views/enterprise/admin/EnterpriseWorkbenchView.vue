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
        <small>weekly 上游硬额度 · {{ poolSourceLabel }}</small>
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
    </section>

    <section class="overview-grid">
      <article class="panel trend-panel">
        <div class="panel-heading"><div><h2>用量趋势</h2><span>按当前筛选范围聚合，单位：请求数</span></div><span class="source-label">{{ trendSourceLabel }}</span></div>
        <div v-if="!loading && !summary?.usage_trend?.length" class="empty-chart">当前筛选条件暂无趋势数据</div>
        <div v-else class="trend-chart" aria-label="用量趋势图">
          <div v-for="point in summary?.usage_trend || []" :key="point.at" class="trend-column">
            <span class="trend-bar" :style="{ height: `${trendHeight(point.requests)}%` }" :title="`${formatDate(point.at)}：${point.requests} 次`" />
            <small>{{ trendLabel(point.at) }}</small>
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

    <el-tabs v-model="activeTab" class="workbench-tabs" @tab-change="loadTab">
      <el-tab-pane label="员工汇总" name="summary">
        <div class="panel table-panel">
          <div class="table-heading"><div><h2>员工分配概况</h2><span>企业总池耗尽与个人 overage 分开处理</span></div></div>
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
      </el-tab-pane>
      <el-tab-pane label="企业用量明细" name="usage">
        <div class="panel table-panel">
          <div class="table-heading"><div><h2>调用明细</h2><span>历史 Key 保留员工归属，Key 值始终掩码；企业额度只按 weekly 口径</span></div><div class="filters-inline"><el-tag type="info">weekly</el-tag></div></div>
          <div class="table-wrap"><el-table v-loading="usageLoading" :data="usage.items" stripe>
            <el-table-column prop="request_at" label="请求时间" width="180"><template #default="{ row }">{{ formatDate(row.request_at) }}</template></el-table-column>
            <el-table-column prop="employee_email" label="员工" min-width="200" />
            <el-table-column prop="api_key_masked" label="Key" width="160" />
            <el-table-column prop="window_type" label="窗口" width="90" />
            <el-table-column prop="usage_credit" label="actual cost" width="130" />
            <el-table-column prop="configured_credit" label="allocation" width="130" />
            <el-table-column prop="classification" label="归属" width="120"><template #default="{ row }">{{ row.classification === 'employee' ? '员工' : '受控外部' }}</template></el-table-column>
          </el-table></div>
          <el-empty v-if="!usageLoading && !usage.items.length" description="当前筛选条件暂无明细" />
          <el-pagination v-if="usage.total" v-model:current-page="usagePage" v-model:page-size="pageSize" :total="usage.total" layout="total, sizes, prev, pager, next" @current-change="loadUsage" @size-change="loadUsage" />
        </div>
      </el-tab-pane>
      <el-tab-pane label="操作审计" name="audit">
        <div class="panel table-panel">
          <div class="audit-toolbar">
            <el-input v-model="auditFilters.search" clearable placeholder="操作、对象或 reason" @keyup.enter="searchAudit" />
            <el-input v-model="auditFilters.actor_ref" clearable placeholder="操作者" />
            <el-input v-model="auditFilters.event_type" clearable placeholder="动作类型" />
            <el-input v-model="auditFilters.entity_type" clearable placeholder="对象类型" />
            <el-input v-model="auditFilters.reason" clearable placeholder="reason" />
            <el-select v-model="auditFilters.result" clearable placeholder="全部结果"><el-option label="成功" value="success" /><el-option label="失败" value="failure" /><el-option label="拒绝" value="rejected" /></el-select>
            <el-date-picker v-model="auditRange" type="datetimerange" value-format="YYYY-MM-DDTHH:mm:ssZ" range-separator="至" start-placeholder="开始时间" end-placeholder="结束时间" />
            <el-button type="primary" :icon="Search" @click="searchAudit">查询</el-button>
          </div>
          <div class="table-wrap"><el-table v-loading="auditLoading" :data="audit.items" stripe>
            <el-table-column prop="created_at" label="时间" width="180"><template #default="{ row }">{{ formatDate(row.created_at) }}</template></el-table-column>
            <el-table-column label="操作者" width="180"><template #default="{ row }">{{ displayActorRef(row.actor_ref) }}</template></el-table-column>
            <el-table-column prop="event_type" label="动作" min-width="190" />
            <el-table-column prop="entity_type" label="对象" width="130" />
            <el-table-column label="结果" width="110"><template #default="{ row }"><el-tag :type="auditResultType(row)">{{ auditResult(row) }}</el-tag></template></el-table-column>
            <el-table-column label="reason" min-width="220"><template #default="{ row }">{{ auditReason(row) }}</template></el-table-column>
            <el-table-column label="详情" width="90" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="openAudit(row)">查看</el-button></template></el-table-column>
          </el-table></div>
          <el-empty v-if="!auditLoading && !audit.items.length" description="暂无符合条件的审计记录" />
          <el-pagination v-if="audit.total" v-model:current-page="auditPage" v-model:page-size="pageSize" :total="audit.total" layout="total, sizes, prev, pager, next" @current-change="loadAudit" @size-change="loadAudit" />
        </div>
      </el-tab-pane>
    </el-tabs>

    <el-drawer v-model="auditDrawerOpen" title="审计详情" size="440px">
      <template v-if="selectedAudit">
        <div class="audit-detail-status" :class="auditResultType(selectedAudit)">{{ auditResult(selectedAudit) }}</div>
        <dl class="audit-details">
          <div><dt>时间</dt><dd>{{ formatDate(selectedAudit.created_at) }}</dd></div>
          <div><dt>操作者</dt><dd>{{ displayActorRef(selectedAudit.actor_ref) }}</dd></div>
          <div><dt>动作</dt><dd>{{ selectedAudit.event_type }}</dd></div>
          <div><dt>对象</dt><dd>{{ selectedAudit.entity_type }}{{ selectedAudit.entity_id ? ` · ${selectedAudit.entity_id}` : '' }}</dd></div>
          <div><dt>reason</dt><dd>{{ auditReason(selectedAudit) }}</dd></div>
        </dl>
        <h3>脱敏事件字段</h3>
        <pre class="audit-payload">{{ payloadText(selectedAudit) }}</pre>
      </template>
    </el-drawer>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseEmployee, EnterprisePaginated, EnterpriseWorkbenchAuditEvent, EnterpriseWorkbenchEmployeeSummary, EnterpriseWorkbenchSummary, EnterpriseWorkbenchUsageRow } from '@/types/enterprise'

type SourceState = 'loading' | 'ready' | 'unavailable'
const emptyPage = <T,>(): EnterprisePaginated<T> => ({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
const departments = ref<EnterpriseDepartment[]>([])
const employees = ref<EnterpriseEmployee[]>([])
const summary = ref<EnterpriseWorkbenchSummary>()
const usage = reactive(emptyPage<EnterpriseWorkbenchUsageRow>())
const audit = reactive(emptyPage<EnterpriseWorkbenchAuditEvent>())
const filters = reactive<{ department_id?: number; employee_id?: number; api_key_id?: number; window_type?: string; window_anchor?: string; start_at?: string; end_at?: string }>({ window_type: 'week' })
const auditFilters = reactive({ search: '', actor_ref: '', event_type: '', entity_type: '', reason: '', result: '' })
const auditRange = ref<string[]>([])
const sourceStates = reactive({ summary: 'loading' as SourceState, usage: 'loading' as SourceState, audit: 'loading' as SourceState, directories: 'loading' as SourceState })
const activeTab = ref('summary'), loading = ref(false), usageLoading = ref(false), auditLoading = ref(false), usagePage = ref(1), auditPage = ref(1), pageSize = ref(20)
const auditDrawerOpen = ref(false)
const selectedAudit = ref<EnterpriseWorkbenchAuditEvent>()
const hasSourceIssue = computed(() => Object.values(sourceStates).some((state) => state === 'unavailable'))
const sourceIssueText = computed(() => Object.entries(sourceStates).filter(([, state]) => state === 'unavailable').map(([name]) => ({ summary: '汇总', usage: '用量明细', audit: '审计', directories: '组织目录' }[name])).join('、'))
const poolSourceLabel = computed(() => summary.value?.pool_source_status === 'available' ? '来源正常' : '来源不可用')
const trendSourceLabel = computed(() => sourceStates.summary === 'ready' ? '来源：企业 usage' : '来源不可用')
const topEmployees = computed(() => [...(summary.value?.employee_summaries || [])].sort((a, b) => Number(b.usage_credit) - Number(a.usage_credit)).slice(0, 4))

const params = (page?: number, includeWindow = true): Record<string, string | number> => Object.fromEntries(Object.entries({ ...filters, ...(includeWindow ? {} : { window_type: undefined }), page, page_size: pageSize.value }).filter(([, value]) => value !== undefined && value !== '')) as Record<string, string | number>
const auditParams = (page?: number): Record<string, string | number> => Object.fromEntries(Object.entries({ ...params(page), event_type: auditFilters.event_type, entity_type: auditFilters.entity_type, search: auditFilters.search, actor_ref: auditFilters.actor_ref, reason: auditFilters.reason, result: auditFilters.result, start_at: auditRange.value[0], end_at: auditRange.value[1] }).filter(([, value]) => value !== undefined && value !== '')) as Record<string, string | number>
const formatDate = (value: string | Date) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
const formatOptionalDate = (value?: string) => value ? formatDate(value) : '未获取'
const trendLabel = (value: string) => new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric' }).format(new Date(value))
const trendHeight = (value: number) => Math.max(8, Math.round((value / Math.max(...(summary.value?.usage_trend || []).map((item) => item.requests), 1)) * 100))
const usagePercentage = (item: EnterpriseWorkbenchEmployeeSummary) => Math.min(100, Math.round((Number(item.usage_credit) / Math.max(Number(item.configured_credit), 1)) * 100))
const auditResult = (row: EnterpriseWorkbenchAuditEvent) => row.result || String(row.payload.result || row.payload.status || 'success')
const auditResultType = (row: EnterpriseWorkbenchAuditEvent) => auditResult(row) === 'success' || auditResult(row) === '成功' ? 'success' : auditResult(row) === 'failure' || auditResult(row) === '失败' ? 'danger' : 'info'
const auditReason = (row: EnterpriseWorkbenchAuditEvent) => row.reason || String(row.payload.reason || '未提供')
const displayActorRef = (value: string) => value.toLowerCase().includes('session') ? 'enterprise_actor' : value
const payloadText = (row: EnterpriseWorkbenchAuditEvent) => JSON.stringify(row.payload, null, 2)
const resetData = () => { summary.value = undefined; Object.assign(usage, emptyPage<EnterpriseWorkbenchUsageRow>()); Object.assign(audit, emptyPage<EnterpriseWorkbenchAuditEvent>()); sourceStates.summary = 'loading'; sourceStates.usage = 'loading'; sourceStates.audit = 'loading' }

async function loadSummary() { try { summary.value = await enterpriseAPI.getWorkbenchSummary(params(undefined, false)); sourceStates.summary = 'ready' } catch { sourceStates.summary = 'unavailable'; throw new Error('summary') } }
async function loadUsage() { usageLoading.value = true; sourceStates.usage = 'loading'; try { Object.assign(usage, await enterpriseAPI.listWorkbenchUsage(params(usagePage.value))); sourceStates.usage = 'ready' } catch { sourceStates.usage = 'unavailable'; throw new Error('usage') } finally { usageLoading.value = false } }
async function loadAudit() { auditLoading.value = true; sourceStates.audit = 'loading'; try { Object.assign(audit, await enterpriseAPI.listWorkbenchAuditEvents(auditParams(auditPage.value))); sourceStates.audit = 'ready' } catch { sourceStates.audit = 'unavailable'; throw new Error('audit') } finally { auditLoading.value = false } }
async function loadDirectories() { try { [departments.value, employees.value] = await Promise.all([enterpriseAPI.listDepartments(), enterpriseAPI.listEmployees()]); sourceStates.directories = 'ready' } catch { sourceStates.directories = 'unavailable'; throw new Error('directories') } }
async function loadTab() { if (activeTab.value === 'usage') await loadUsage(); if (activeTab.value === 'audit') await loadAudit() }
async function load() { loading.value = true; resetData(); const results = await Promise.allSettled([loadSummary(), loadUsage(), loadAudit(), loadDirectories()]); if (results.some((result) => result.status === 'rejected')) ElMessage.error('部分工作台数据暂时不可用'); loading.value = false }
async function searchAudit() { auditPage.value = 1; try { await loadAudit() } catch { ElMessage.error('审计数据暂时不可用') } }
function openAudit(row: EnterpriseWorkbenchAuditEvent) { selectedAudit.value = row; auditDrawerOpen.value = true }
onMounted(load)
</script>

<style scoped>
.workspace{min-width:0;color:#111827}.page-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:20px}.eyebrow{margin-bottom:6px;color:#6b7280;font-size:11px;font-weight:700}.page-heading h1{margin:0 0 5px;font-size:24px;line-height:30px}.page-heading p{margin:0;color:#6b7280}.source-notice{display:flex;gap:10px;align-items:center;margin-bottom:16px;padding:11px 14px;border:1px solid #fde68a;border-radius:8px;background:#fffbeb;color:#92400e}.source-notice span{font-size:12px}.kpis{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px;margin-bottom:16px}.kpi-card{min-height:112px;padding:16px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.kpi-label{display:block;color:#6b7280;font-size:12px}.kpi-card strong{display:block;margin:9px 0 5px;font-size:24px;line-height:28px}.kpi-card small,.panel-heading span,.progress-note{color:#6b7280;font-size:11px}.danger-value,.overage{color:#b91c1c!important}.status-row{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin-bottom:16px}.status-chip{display:flex;align-items:flex-start;gap:10px;padding:12px 14px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.status-chip b,.status-chip small{display:block}.status-chip small{margin-top:4px;color:#6b7280;font-size:11px}.status-dot{width:8px;height:8px;flex:none;margin-top:4px;border-radius:50%;background:#9ca3af}.is-ok .status-dot{background:#15803d}.is-danger .status-dot{background:#b91c1c}.overview-grid{display:grid;grid-template-columns:minmax(0,1.45fr) minmax(320px,.75fr);gap:16px;margin-bottom:16px}.panel{min-width:0;padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.panel-heading,.table-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:16px}.panel-heading h2,.table-heading h2{margin:0 0 4px;font-size:14px}.source-label{white-space:nowrap}.trend-chart{display:flex;align-items:flex-end;gap:16px;height:205px;padding:16px 12px 26px;border-left:1px solid #d1d5db;border-bottom:1px solid #d1d5db;background:repeating-linear-gradient(to bottom,transparent 0,transparent 40px,#f0f1f3 41px)}.trend-column{display:flex;flex:1;min-width:18px;height:100%;flex-direction:column;align-items:center;justify-content:flex-end;gap:8px}.trend-bar{width:min(44px,100%);min-height:8%;border-radius:4px 4px 0 0;background:#2563eb}.trend-column small{color:#6b7280;font-size:10px;white-space:nowrap}.empty-chart{display:grid;place-items:center;height:205px;color:#6b7280}.progress-row{margin-bottom:17px}.progress-row:last-child{margin-bottom:0}.progress-top,.progress-note{display:flex;justify-content:space-between;gap:12px}.progress-top{margin-bottom:7px;font-size:12px}.progress-note{margin-top:5px}.progress-note .overage{font-weight:700}.workbench-tabs :deep(.el-tabs__item){height:44px}.table-panel{padding:0;overflow:hidden}.table-heading{padding:18px 18px 0;margin-bottom:14px}.table-wrap{width:100%;overflow-x:auto}.table-wrap :deep(.el-table){min-width:920px}.table-wrap :deep(.el-table th.el-table__cell){background:#f9fafb;color:#6b7280;font-size:11px}.table-wrap :deep(.el-table td.el-table__cell),.table-wrap :deep(.el-table th.el-table__cell){height:48px}.el-empty{padding:20px}.el-pagination{justify-content:flex-end;margin:16px 18px}.filters-inline{display:flex;gap:8px}.audit-toolbar{display:grid;grid-template-columns:minmax(180px,1.5fr) repeat(6,minmax(120px,1fr)) auto;gap:8px;padding:18px;border-bottom:1px solid #e5e7eb}.audit-detail-status{display:inline-flex;padding:5px 10px;border-radius:999px;background:#eff6ff;color:#2563eb;font-size:12px;font-weight:700}.audit-detail-status.success{background:#ecfdf5;color:#15803d}.audit-detail-status.danger{background:#fef2f2;color:#b91c1c}.audit-details{margin:20px 0}.audit-details div{display:grid;grid-template-columns:90px 1fr;gap:12px;padding:11px 0;border-bottom:1px solid #f0f1f3}.audit-details dt{color:#6b7280}.audit-details dd{margin:0;overflow-wrap:anywhere}.audit-details+h3{margin-top:24px;font-size:13px}.audit-payload{max-height:320px;overflow:auto;padding:12px;border:1px solid #e5e7eb;border-radius:6px;background:#f9fafb;color:#374151;font:12px/18px ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre-wrap;overflow-wrap:anywhere}@media(max-width:1000px){.kpis{grid-template-columns:repeat(2,minmax(0,1fr))}.overview-grid{grid-template-columns:1fr}.status-row{grid-template-columns:1fr}.audit-toolbar{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:640px){.page-heading{flex-direction:column}.page-heading .el-button{width:100%}.kpis{grid-template-columns:1fr}.audit-toolbar{grid-template-columns:1fr}.filters-inline{width:100%}.filters-inline>*{flex:1}.panel{padding:14px}.table-heading{padding:14px 14px 0}}
</style>
