<template>
  <TablePageLayout>
    <template #actions>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 class="text-xl font-bold text-gray-900 dark:text-white">管理审计</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">查看企业管理操作及其结果。</p>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">
          <Icon name="refresh" size="md" class="mr-2" :class="{ 'animate-spin': loading }" />刷新
        </button>
      </div>
    </template>

    <template #filters>
      <div class="card grid grid-cols-1 gap-3 p-4 sm:grid-cols-2 xl:grid-cols-4">
        <input v-model="filters.search" class="input" placeholder="操作、对象或原因" aria-label="搜索审计记录" @keyup.enter="searchAudit" />
        <input v-model="filters.actor_ref" class="input" placeholder="操作者" aria-label="操作者" @keyup.enter="searchAudit" />
        <input v-model="filters.event_type" class="input" placeholder="动作类型" aria-label="动作类型" @keyup.enter="searchAudit" />
        <input v-model="filters.entity_type" class="input" placeholder="对象类型" aria-label="对象类型" @keyup.enter="searchAudit" />
        <input v-model="filters.reason" class="input" placeholder="原因" aria-label="原因" @keyup.enter="searchAudit" />
        <Select v-model="filters.result" :options="resultOptions" placeholder="全部结果" aria-label="结果" clearable />
        <input v-model="startAt" type="datetime-local" class="input" aria-label="开始时间" />
        <input v-model="endAt" type="datetime-local" class="input" aria-label="结束时间" />
        <button type="button" class="btn btn-primary" :disabled="loading" @click="searchAudit">
          <Icon name="search" size="md" class="mr-2" />查询
        </button>
      </div>
    </template>

    <template #table>
      <div v-if="sourceUnavailable" role="status" class="p-6">
        <EmptyState icon="exclamationTriangle" title="审计数据源暂时不可用" action-text="重试" @action="load" />
      </div>
      <DataTable v-else :columns="columns" :data="audit.items" :loading="loading" row-key="id" :actions-count="1">
        <template #cell-created_at="{ value }">{{ formatDate(value) }}</template>
        <template #cell-actor_ref="{ value }"><span class="break-all">{{ displayActorRef(value) }}</span></template>
        <template #cell-event_type="{ row }">
          <span :title="row.event_type">{{ auditEventLabel(row.event_type, t) }}</span>
        </template>
        <template #cell-entity_type="{ row }">
          <span :title="row.entity_type">{{ auditEntityLabel(row.entity_type, t) }}</span>
        </template>
        <template #cell-result="{ row }"><StatusBadge :status="resultStatus(row)" :label="resultLabel(row)" /></template>
        <template #cell-reason="{ row }"><span class="break-all">{{ auditReason(row) }}</span></template>
        <template #cell-actions="{ row }"><button type="button" class="btn btn-ghost btn-sm" @click="selectedAudit = row">查看</button></template>
        <template #empty><EmptyState title="暂无符合条件的审计记录" /></template>
      </DataTable>
    </template>

    <template #pagination>
      <Pagination v-if="audit.total && !sourceUnavailable" :total="audit.total" :page="auditPage" :page-size="pageSize" @update:page="onPage" @update:pageSize="onPageSize" />
    </template>
  </TablePageLayout>

  <BaseDialog :show="!!selectedAudit" title="审计详情" width="normal" @close="selectedAudit = undefined">
    <template v-if="selectedAudit">
      <dl class="space-y-3 text-sm text-gray-900 dark:text-gray-100">
        <div><dt class="text-gray-500 dark:text-dark-400">时间</dt><dd>{{ formatDate(selectedAudit.created_at) }}</dd></div>
        <div><dt class="text-gray-500 dark:text-dark-400">操作者</dt><dd class="break-all">{{ displayActorRef(selectedAudit.actor_ref) }}</dd></div>
        <div><dt class="text-gray-500 dark:text-dark-400">动作</dt><dd>{{ auditEventLabel(selectedAudit.event_type, t) }}</dd></div>
        <div><dt class="text-gray-500 dark:text-dark-400">对象</dt><dd>{{ auditEntityLabel(selectedAudit.entity_type, t) }} {{ selectedAudit.entity_id ?? '' }}</dd></div>
        <div><dt class="text-gray-500 dark:text-dark-400">结果</dt><dd>{{ resultLabel(selectedAudit) }}</dd></div>
        <div><dt class="text-gray-500 dark:text-dark-400">原因</dt><dd class="break-all">{{ auditReason(selectedAudit) }}</dd></div>
        <div><dt class="text-gray-500 dark:text-dark-400">技术标识</dt><dd class="break-all font-mono text-xs">{{ selectedAudit.event_type }} · {{ selectedAudit.entity_type }}</dd></div>
      </dl>
      <h3 class="mt-6 text-sm font-semibold text-gray-900 dark:text-white">脱敏事件字段</h3>
      <dl v-if="whitelistedFields(selectedAudit).length" class="mt-2 space-y-3 text-sm">
        <div v-for="field in whitelistedFields(selectedAudit)" :key="field.key" class="border-b border-gray-200 py-2 dark:border-dark-700">
          <dt class="text-gray-500 dark:text-dark-400">{{ field.label }}</dt>
          <dd class="break-all text-gray-900 dark:text-gray-100">{{ field.value }}</dd>
        </div>
      </dl>
      <p v-else class="mt-2 text-sm text-gray-500 dark:text-dark-400">该事件无可展示的白名单字段。</p>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { enterpriseAPI } from '@/api/enterprise'
import { auditEntityLabel, auditEventLabel } from '@/utils/enterpriseDisplay'
import type { Column } from '@/components/common/types'
import type { EnterprisePaginated, EnterpriseWorkbenchAuditEvent } from '@/types/enterprise'

// 详情字段只消费后端允许展示的既有白名单，未知字段不渲染。
const auditPayloadFieldLabels: Record<string, string> = {
  result: '结果', reason: '原因', employee_id: '员工 ID', department_id: '部门 ID', api_key_id: 'API Key ID',
  subscription_id: '订阅 ID', upstream_subscription_id: '上游订阅 ID', upstream_group_id: '上游分组 ID',
  cancelled_subscription_id: '已取消订阅 ID', scheduled_subscription_id: '已排期订阅 ID',
  previous_assignment_id: '原分配 ID', new_assignment_id: '新分配 ID',
  previous_api_key_id: '原 Key ID', previous_masked_key: '原 Key（掩码）', masked_key: 'Key（掩码）',
  window_type: '窗口类型', window_anchor: '窗口锚点', previous_window_start: '原窗口起点',
  observed_window_start: '观测窗口起点', assignment_segment_boundary: '分配区间边界',
  expected_version: '期望版本', current_version: '当前版本', actual_version: '实际版本', version: '版本', credit: '调整后额度',
  name: '名称', status: '状态', affected_employees: '受影响员工数', initial: '是否初始', keys_disabled: '已停用 Key 数',
  fields: '变更字段', content_type: '内容类型', size_bytes: '大小（字节）', quota: '配额', quota_used: '已用配额',
  rate_limit_5h: '限流 5h', rate_limit_1d: '限流 1d', rate_limit_7d: '限流 7d',
  usage_5h: '用量 5h', usage_1d: '用量 1d', usage_7d: '用量 7d',
  window_5h_start: '窗口起点 5h', window_1d_start: '窗口起点 1d', window_7d_start: '窗口起点 7d'
}

const { t } = useI18n()
const appStore = useAppStore()
const columns: Column[] = [
  { key: 'created_at', label: '时间' }, { key: 'actor_ref', label: '操作者' },
  { key: 'event_type', label: '动作' }, { key: 'entity_type', label: '对象' },
  { key: 'result', label: '结果' }, { key: 'reason', label: '原因' }, { key: 'actions', label: '详情' }
]
const resultOptions = computed(() => ['success', 'failure', 'rejected'].map(value => ({ value, label: t(`admin.enterprise.audit.result.${value}`) })))
const emptyPage = (): EnterprisePaginated<EnterpriseWorkbenchAuditEvent> => ({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
const audit = reactive(emptyPage())
const filters = reactive({ search: '', actor_ref: '', event_type: '', entity_type: '', reason: '', result: '' })
const startAt = ref(''), endAt = ref('')
const loading = ref(false), sourceUnavailable = ref(false), auditPage = ref(1), pageSize = ref(20)
const selectedAudit = ref<EnterpriseWorkbenchAuditEvent>()

const auditParams = () => Object.fromEntries(Object.entries({
  page: auditPage.value, page_size: pageSize.value, ...filters,
  start_at: startAt.value ? new Date(startAt.value).toISOString() : undefined,
  end_at: endAt.value ? new Date(endAt.value).toISOString() : undefined
}).filter(([, value]) => value !== undefined && value !== '')) as Record<string, string | number>
const formatDate = (value: string | Date) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
const resultCode = (row: EnterpriseWorkbenchAuditEvent) => row.result || String(row.payload?.result || row.payload?.status || 'success')
const resultLabel = (row: EnterpriseWorkbenchAuditEvent) => t(`admin.enterprise.audit.result.${['success', 'failure', 'rejected'].includes(resultCode(row)) ? resultCode(row) : 'unknown'}`)
const resultStatus = (row: EnterpriseWorkbenchAuditEvent) => resultCode(row) === 'success' ? 'active' : 'error'
const auditReason = (row: EnterpriseWorkbenchAuditEvent) => row.reason || String(row.payload?.reason || '未提供')
const displayActorRef = (value: string) => value.toLowerCase().includes('session') ? 'enterprise_actor' : value
function whitelistedFields(row: EnterpriseWorkbenchAuditEvent) {
  return Object.entries(row.payload || {}).filter(([key]) => key in auditPayloadFieldLabels).map(([key, value]) => ({
    key, label: auditPayloadFieldLabels[key],
    value: key === 'result' ? t(`admin.enterprise.audit.result.${['success', 'failure', 'rejected'].includes(String(value)) ? value : 'unknown'}`) : typeof value === 'object' ? JSON.stringify(value) : String(value)
  }))
}

async function load() {
  loading.value = true
  sourceUnavailable.value = false
  Object.assign(audit, emptyPage())
  try {
    Object.assign(audit, await enterpriseAPI.listWorkbenchAuditEvents(auditParams(), { suppressUnavailableRedirect: true }))
  } catch {
    sourceUnavailable.value = true
    appStore.showError('审计数据暂时不可用')
  } finally {
    loading.value = false
  }
}
function searchAudit() { auditPage.value = 1; void load() }
function onPage(page: number) { auditPage.value = page; void load() }
function onPageSize(size: number) { pageSize.value = size; auditPage.value = 1; void load() }
onMounted(load)
</script>
