<template>
  <section class="workspace">
    <div class="page-heading">
      <div>
        <div class="eyebrow">企业管理 / 审计</div>
        <h1>管理审计</h1>
        <p>按操作者、对象、动作、时间、结果与 reason 六个维度检索管理操作记录；详情按字段白名单展示，不展示原始事件负载。</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新数据</el-button>
    </div>

    <div v-if="sourceStates.audit === 'unavailable'" class="source-notice" role="status">
      <strong>审计数据源暂时不可用</strong>
      <span>页面已清除本次查询前的数据，不以旧值伪装实时结果。</span>
    </div>

    <div class="panel table-panel">
      <div class="audit-toolbar">
        <el-input v-model="auditFilters.search" clearable placeholder="操作、对象或 reason" @keyup.enter="searchAudit" />
        <el-input v-model="auditFilters.actor_ref" clearable placeholder="操作者" @keyup.enter="searchAudit" />
        <el-input v-model="auditFilters.event_type" clearable placeholder="动作类型" @keyup.enter="searchAudit" />
        <el-input v-model="auditFilters.entity_type" clearable placeholder="对象类型" @keyup.enter="searchAudit" />
        <el-input v-model="auditFilters.reason" clearable placeholder="reason" @keyup.enter="searchAudit" />
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
        <dl v-if="whitelistedFields(selectedAudit).length" class="audit-details audit-payload-fields">
          <div v-for="field in whitelistedFields(selectedAudit)" :key="field.key"><dt>{{ field.label }}</dt><dd>{{ field.value }}</dd></div>
        </dl>
        <p v-else class="audit-payload-empty">该事件无可展示的白名单字段。</p>
      </template>
    </el-drawer>
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterprisePaginated, EnterpriseWorkbenchAuditEvent } from '@/types/enterprise'

// auditPayloadFieldLabels mirrors the backend's auditPayloadFieldWhitelist in
// backend/internal/enterpriseidentity/workbench.go: the detail drawer must
// never render a payload field that is not on this list, even if the API
// response contains one — this keeps the frontend fail-closed in lockstep
// with the backend whitelist rather than trusting a raw payload dump.
const auditPayloadFieldLabels: Record<string, string> = {
  result: '结果', reason: 'reason',
  employee_id: '员工 ID', department_id: '部门 ID', api_key_id: 'API Key ID',
  subscription_id: '订阅 ID', upstream_subscription_id: '上游订阅 ID', upstream_group_id: '上游分组 ID',
  cancelled_subscription_id: '已取消订阅 ID', scheduled_subscription_id: '已排期订阅 ID',
  previous_assignment_id: '原分配 ID', new_assignment_id: '新分配 ID',
  previous_api_key_id: '原 Key ID', previous_masked_key: '原 Key（掩码）', masked_key: 'Key（掩码）',
  window_type: '窗口类型', window_anchor: '窗口锚点',
  previous_window_start: '原窗口起点', observed_window_start: '观测窗口起点',
  assignment_segment_boundary: '分配区间边界',
  expected_version: '期望版本', current_version: '当前版本', actual_version: '实际版本', version: '版本', credit: '调整后额度',
  name: '名称', status: '状态', affected_employees: '受影响员工数',
  initial: '是否初始', keys_disabled: '已停用 Key 数', fields: '变更字段',
  content_type: '内容类型', size_bytes: '大小（字节）',
  quota: '配额', quota_used: '已用配额',
  rate_limit_5h: '限流 5h', rate_limit_1d: '限流 1d', rate_limit_7d: '限流 7d',
  usage_5h: '用量 5h', usage_1d: '用量 1d', usage_7d: '用量 7d',
  window_5h_start: '窗口起点 5h', window_1d_start: '窗口起点 1d', window_7d_start: '窗口起点 7d',
}

type SourceState = 'loading' | 'ready' | 'unavailable'
const emptyPage = <T,>(): EnterprisePaginated<T> => ({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })

const audit = reactive(emptyPage<EnterpriseWorkbenchAuditEvent>())
const auditFilters = reactive({ search: '', actor_ref: '', event_type: '', entity_type: '', reason: '', result: '' })
const auditRange = ref<string[]>([])
const sourceStates = reactive({ audit: 'loading' as SourceState })
const loading = ref(false), auditLoading = ref(false), auditPage = ref(1), pageSize = ref(20)
const auditDrawerOpen = ref(false)
const selectedAudit = ref<EnterpriseWorkbenchAuditEvent>()

const auditParams = (page?: number): Record<string, string | number> => Object.fromEntries(Object.entries({ page, page_size: pageSize.value, event_type: auditFilters.event_type, entity_type: auditFilters.entity_type, search: auditFilters.search, actor_ref: auditFilters.actor_ref, reason: auditFilters.reason, result: auditFilters.result, start_at: auditRange.value[0], end_at: auditRange.value[1] }).filter(([, value]) => value !== undefined && value !== '')) as Record<string, string | number>
const formatDate = (value: string | Date) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
const auditResult = (row: EnterpriseWorkbenchAuditEvent) => row.result || String(row.payload.result || row.payload.status || 'success')
const auditResultType = (row: EnterpriseWorkbenchAuditEvent) => auditResult(row) === 'success' || auditResult(row) === '成功' ? 'success' : auditResult(row) === 'failure' || auditResult(row) === '失败' ? 'danger' : 'info'
const auditReason = (row: EnterpriseWorkbenchAuditEvent) => row.reason || String(row.payload.reason || '未提供')
const displayActorRef = (value: string) => value.toLowerCase().includes('session') ? 'enterprise_actor' : value
function whitelistedFields(row: EnterpriseWorkbenchAuditEvent) {
  return Object.entries(row.payload || {})
    .filter(([key]) => key in auditPayloadFieldLabels)
    .map(([key, value]) => ({ key, label: auditPayloadFieldLabels[key], value: typeof value === 'object' ? JSON.stringify(value) : String(value) }))
}
const resetData = () => { Object.assign(audit, emptyPage<EnterpriseWorkbenchAuditEvent>()); sourceStates.audit = 'loading' }

async function loadAudit() { auditLoading.value = true; sourceStates.audit = 'loading'; try { Object.assign(audit, await enterpriseAPI.listWorkbenchAuditEvents(auditParams(auditPage.value))); sourceStates.audit = 'ready' } catch { sourceStates.audit = 'unavailable'; throw new Error('audit') } finally { auditLoading.value = false } }
async function load() { loading.value = true; resetData(); try { await loadAudit() } catch { ElMessage.error('审计数据暂时不可用') } finally { loading.value = false } }
async function searchAudit() { auditPage.value = 1; try { await loadAudit() } catch { ElMessage.error('审计数据暂时不可用') } }
function openAudit(row: EnterpriseWorkbenchAuditEvent) { selectedAudit.value = row; auditDrawerOpen.value = true }

onMounted(load)
</script>

<style scoped>
.workspace{min-width:0;color:#111827}.page-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:20px}.eyebrow{margin-bottom:6px;color:#6b7280;font-size:11px;font-weight:700}.page-heading h1{margin:0 0 5px;font-size:24px;line-height:30px}.page-heading p{margin:0;color:#6b7280}.source-notice{display:flex;gap:10px;align-items:center;margin-bottom:16px;padding:11px 14px;border:1px solid #fde68a;border-radius:8px;background:#fffbeb;color:#92400e}.source-notice span{font-size:12px}.panel{min-width:0;padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.table-panel{padding:0;overflow:hidden}.table-wrap{width:100%;overflow-x:auto}.table-wrap :deep(.el-table){min-width:920px}.table-wrap :deep(.el-table th.el-table__cell){background:#f9fafb;color:#6b7280;font-size:11px}.table-wrap :deep(.el-table td.el-table__cell),.table-wrap :deep(.el-table th.el-table__cell){height:48px}.el-empty{padding:20px}.el-pagination{justify-content:flex-end;margin:16px 18px}.audit-toolbar{display:grid;grid-template-columns:minmax(180px,1.5fr) repeat(6,minmax(120px,1fr)) auto;gap:8px;padding:18px;border-bottom:1px solid #e5e7eb}.audit-detail-status{display:inline-flex;padding:5px 10px;border-radius:999px;background:#eff6ff;color:#2563eb;font-size:12px;font-weight:700}.audit-detail-status.success{background:#ecfdf5;color:#15803d}.audit-detail-status.danger{background:#fef2f2;color:#b91c1c}.audit-details{margin:20px 0}.audit-details div{display:grid;grid-template-columns:120px 1fr;gap:12px;padding:11px 0;border-bottom:1px solid #f0f1f3}.audit-details dt{color:#6b7280}.audit-details dd{margin:0;overflow-wrap:anywhere}.audit-details+h3{margin-top:24px;font-size:13px}.audit-payload-empty{color:#6b7280;font-size:12px}@media(max-width:1000px){.audit-toolbar{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:640px){.page-heading{flex-direction:column}.page-heading .el-button{width:100%}.audit-toolbar{grid-template-columns:1fr}.panel{padding:14px}}
</style>
