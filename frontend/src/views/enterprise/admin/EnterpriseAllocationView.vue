<template>
  <section class="workspace">
    <div class="page-heading">
      <div>
        <div class="eyebrow">企业管理 / 订阅与额度</div>
        <h1>订阅与额度</h1>
        <p>从企业总池向员工分配 allocation；部门不参与分配，只作组织归属展示。</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新数据</el-button>
    </div>

    <div v-if="sourceState === 'unavailable'" class="source-notice" role="status">
      <strong>数据来源暂时不可用</strong>
      <span>无法获取权威额度或订阅信息，页面已清除本次查询前的数据，不以旧值伪装实时结果。</span>
    </div>

    <div v-else-if="!loading && !subscriptionId" class="source-notice" role="status">
      <strong>暂无生效订阅</strong>
      <span>企业当前没有 active 订阅，无法进行 allocation 分配。</span>
    </div>

    <template v-else>
      <section class="kpis" aria-label="企业总池分配概览">
        <article class="kpi-card">
          <span class="kpi-label">企业总池</span>
          <strong>{{ result?.authoritative_limit ?? '未获取' }}</strong>
          <small>weekly 本周期 · {{ result?.pool_source_status === 'available' ? '来源正常' : '来源不可用' }}</small>
        </article>
        <article class="kpi-card">
          <span class="kpi-label">员工 allocation 合计</span>
          <strong>{{ result?.allocated_total || '0' }}</strong>
          <small>{{ result?.items?.length || 0 }} 名员工已分配</small>
        </article>
        <article class="kpi-card">
          <span class="kpi-label">企业池未分配额度</span>
          <strong>{{ result?.unallocated_total || '0' }}</strong>
          <small>最小为 0，不显示负数</small>
        </article>
        <article class="kpi-card">
          <span class="kpi-label">独立 overage</span>
          <strong :class="{ 'danger-value': hasOverallocation }">{{ result?.overallocated_by || '0' }}</strong>
          <small>超用独立记录，不冲减员工个人 remaining</small>
        </article>
      </section>

      <div v-if="result?.warning" class="source-notice" role="status">
        <strong>企业池已超分配</strong>
        <span>{{ result.warning }}：当前仅作提示，不代表请求级硬阻断，员工调用仍以上游硬额度为准。</span>
      </div>

      <div class="panel table-panel">
        <div class="table-heading">
          <div><h2>员工 allocation</h2><span>allocation 不代表请求级硬阻断；部门仅组织归属，不参与分配</span></div>
          <el-button type="primary" :icon="Plus" :disabled="!subscriptionId" @click="openDialog()">调整员工额度</el-button>
        </div>
        <div class="table-wrap">
          <el-table v-loading="loading" :data="result?.items || []" stripe>
            <el-table-column prop="email" label="员工" min-width="200" />
            <el-table-column label="部门归属" width="140"><template #default="{ row }">{{ departmentName(row.department_id) }}</template></el-table-column>
            <el-table-column prop="configured_credit" label="allocation" width="120" />
            <el-table-column prop="usage_credit" label="已使用" width="120" />
            <el-table-column prop="remaining_credit" label="remaining" width="120" />
            <el-table-column label="overage" width="120"><template #default="{ row }"><span :class="row.overage_credit !== '0' ? 'overage' : 'muted'">{{ row.overage_credit }}</span></template></el-table-column>
            <el-table-column label="状态" width="100"><template #default="{ row }"><el-tag :type="row.status === 'overage' ? 'danger' : 'success'">{{ row.status === 'overage' ? '有超用' : '正常' }}</el-tag></template></el-table-column>
            <el-table-column label="操作" width="90" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="openDialog(row)">调整</el-button></template></el-table-column>
          </el-table>
        </div>
        <el-empty v-if="!loading && !(result?.items?.length)" description="当前订阅暂无员工 allocation" />
      </div>
    </template>

    <el-dialog v-model="dialogOpen" title="调整员工额度" width="440px">
      <el-form :model="form" label-position="top">
        <el-form-item label="员工">
          <el-select v-model="form.employee_id" filterable placeholder="选择员工" :disabled="!!editingRow">
            <el-option v-for="employee in employees" :key="employee.id" :label="employee.email" :value="employee.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="credit">
          <el-input v-model="form.credit" placeholder="非负数值字符串" />
        </el-form-item>
        <el-form-item label="调整原因（必填）">
          <el-input v-model="form.reason" type="textarea" :rows="2" maxlength="200" show-word-limit placeholder="不得包含密钥、Token、Cookie 或邮箱等敏感信息" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogOpen = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseAllocationListResult, EnterpriseAllocationListItem, EnterpriseDepartment, EnterpriseEmployee } from '@/types/enterprise'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'

const auth = useEnterpriseAuthStore()
const enterpriseId = computed(() => auth.principal?.enterprise_id || 0)

type SourceState = 'loading' | 'ready' | 'unavailable'
const loading = ref(false)
const saving = ref(false)
const sourceState = ref<SourceState>('loading')
const subscriptionId = ref<number>()
const windowAnchor = ref<string>()
const result = ref<EnterpriseAllocationListResult>()
const departments = ref<EnterpriseDepartment[]>([])
const employees = ref<EnterpriseEmployee[]>([])
const dialogOpen = ref(false)
const editingRow = ref<EnterpriseAllocationListItem>()
const form = reactive({ employee_id: 0, credit: '0', reason: '' })

const hasOverallocation = computed(() => !!result.value?.overallocated_by && result.value.overallocated_by !== '0')
const departmentName = (id?: number | null) => departments.value.find((department) => department.id === id)?.name || '未分配部门'

async function loadDirectories() {
  try {
    [departments.value, employees.value] = await Promise.all([enterpriseAPI.listDepartments(), enterpriseAPI.listEmployees()])
  } catch {
    // 组织目录仅用于展示部门名称，加载失败不阻断 allocation 主流程
  }
}

async function load() {
  loading.value = true
  sourceState.value = 'loading'
  result.value = undefined
  try {
    const summary = await enterpriseAPI.getWorkbenchSummary()
    subscriptionId.value = summary.subscription_id ?? undefined
    windowAnchor.value = summary.pool_window_anchor
    if (subscriptionId.value && windowAnchor.value) {
      result.value = await enterpriseAPI.listSubscriptionAllocations(subscriptionId.value, {
        enterprise_id: enterpriseId.value,
        window_type: 'week',
        window_anchor: windowAnchor.value,
      })
    }
    sourceState.value = 'ready'
  } catch {
    sourceState.value = 'unavailable'
    ElMessage.error('allocation 数据暂时不可用')
  } finally {
    loading.value = false
  }
}

function openDialog(row?: EnterpriseAllocationListItem) {
  editingRow.value = row
  form.employee_id = row?.employee_id || 0
  form.credit = row?.configured_credit || '0'
  form.reason = ''
  dialogOpen.value = true
}

async function save() {
  if (!subscriptionId.value || !windowAnchor.value || !form.employee_id || !form.reason.trim()) {
    ElMessage.warning('请完整填写员工、credit 与调整原因')
    return
  }
  saving.value = true
  try {
    const current = await enterpriseAPI.getAllocationSummary(subscriptionId.value, form.employee_id, {
      enterprise_id: enterpriseId.value,
      window_type: 'week',
      window_anchor: windowAnchor.value,
    }).catch(() => undefined)
    await enterpriseAPI.setAllocation(subscriptionId.value, form.employee_id, {
      enterprise_id: enterpriseId.value,
      window_type: 'week',
      window_anchor: windowAnchor.value,
      credit: form.credit,
      expected_version: current?.allocation_version || 0,
      reason: form.reason.trim(),
    })
    ElMessage.success('allocation 已保存')
    dialogOpen.value = false
    await load()
  } catch {
    ElMessage.error('allocation 保存失败，请核对 credit、版本号或企业订阅来源')
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await loadDirectories()
  await load()
})
</script>

<style scoped>
.workspace{min-width:0;color:#111827}
.page-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:20px}
.eyebrow{margin-bottom:6px;color:#6b7280;font-size:11px;font-weight:700}
.page-heading h1{margin:0 0 5px;font-size:24px;line-height:30px}
.page-heading p{margin:0;color:#6b7280}
.source-notice{display:flex;gap:10px;align-items:center;margin-bottom:16px;padding:11px 14px;border:1px solid #fde68a;border-radius:8px;background:#fffbeb;color:#92400e}
.source-notice span{font-size:12px}
.kpis{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px;margin-bottom:16px}
.kpi-card{min-height:100px;padding:16px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}
.kpi-label{display:block;color:#6b7280;font-size:12px}
.kpi-card strong{display:block;margin:9px 0 5px;font-size:22px;line-height:26px}
.kpi-card small{color:#6b7280;font-size:11px}
.danger-value,.overage{color:#b91c1c!important}
.muted{color:#6b7280}
.panel{min-width:0;padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}
.table-panel{padding:0;overflow:hidden}
.table-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;padding:18px 18px 0;margin-bottom:14px}
.table-heading h2{margin:0 0 4px;font-size:14px}
.table-heading span{color:#6b7280;font-size:11px}
.table-wrap{width:100%;overflow-x:auto}
.table-wrap :deep(.el-table){min-width:820px}
.table-wrap :deep(.el-table th.el-table__cell){background:#f9fafb;color:#6b7280;font-size:11px}
.table-wrap :deep(.el-table td.el-table__cell),.table-wrap :deep(.el-table th.el-table__cell){height:48px}
.el-empty{padding:20px}
.el-select{width:100%}
@media(max-width:1000px){.kpis{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:640px){.page-heading{flex-direction:column}.page-heading .el-button{width:100%}.kpis{grid-template-columns:1fr}.panel{padding:14px}.table-heading{padding:14px 14px 0;flex-direction:column;align-items:stretch}.table-heading .el-button{width:100%}}
</style>
