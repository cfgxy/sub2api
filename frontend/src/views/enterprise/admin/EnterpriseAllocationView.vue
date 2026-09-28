<template>
  <TablePageLayout>
    <template #actions>
      <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div class="min-w-0">
          <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-dark-400">
            企业管理 / 订阅与额度
          </p>
          <h2 class="mt-1 text-xl font-bold text-gray-900 dark:text-white">订阅与{{ terms.allocation }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
            从{{ terms.pool }}向员工分配{{ terms.allocation }}；部门不参与分配，只作组织归属展示。
          </p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">
            <Icon name="refresh" size="md" class="mr-2" :class="{ 'animate-spin': loading }" />
            刷新数据
          </button>
          <button type="button" class="btn btn-primary" :disabled="!subscriptionId" @click="openDialog()">
            <Icon name="plus" size="md" class="mr-2" />
            调整员工{{ terms.allocation }}
          </button>
        </div>
      </div>
    </template>

    <template #filters>
      <div class="space-y-4">
        <div
          v-if="sourceState === 'unavailable'"
          role="status"
          class="card flex flex-col gap-1 border-l-4 border-l-amber-500 p-4"
        >
          <span class="text-sm font-semibold text-gray-900 dark:text-white">数据来源暂时不可用</span>
          <span class="text-xs text-gray-500 dark:text-dark-400">
            无法获取权威{{ terms.allocation }}或订阅信息，页面已清除本次查询前的数据，不以旧值伪装实时结果。
          </span>
          <div>
            <button type="button" class="btn btn-secondary btn-sm mt-2" :disabled="loading" @click="load">
              重试
            </button>
          </div>
        </div>

        <div
          v-else-if="!loading && !subscriptionId"
          role="status"
          class="card flex flex-col gap-1 border-l-4 border-l-amber-500 p-4"
        >
          <span class="text-sm font-semibold text-gray-900 dark:text-white">暂无生效订阅</span>
          <span class="text-xs text-gray-500 dark:text-dark-400">
            企业当前没有生效订阅，无法进行{{ terms.allocation }}分配。
          </span>
        </div>

        <template v-else>
          <section class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4" aria-label="企业总池分配概览">
            <div class="card p-4">
              <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ terms.pool }}</p>
              <p class="mt-2 truncate text-xl font-bold text-gray-900 dark:text-white">
                {{ result?.authoritative_limit ?? '未获取' }}
              </p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                本周期 · 来源{{ poolSourceText }}
              </p>
            </div>
            <div class="card p-4">
              <p class="text-xs font-medium text-gray-500 dark:text-dark-400">员工{{ terms.allocation }}合计</p>
              <p class="mt-2 truncate text-xl font-bold text-gray-900 dark:text-white">
                {{ result?.allocated_total || '0' }}
              </p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                {{ result?.items?.length || 0 }} 名员工已分配
              </p>
            </div>
            <div class="card p-4">
              <p class="text-xs font-medium text-gray-500 dark:text-dark-400">企业池未分配{{ terms.allocation }}</p>
              <p class="mt-2 truncate text-xl font-bold text-gray-900 dark:text-white">
                {{ result?.unallocated_total || '0' }}
              </p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">最小为 0，不显示负数</p>
            </div>
            <div class="card p-4">
              <p class="text-xs font-medium text-gray-500 dark:text-dark-400">独立{{ terms.overage }}</p>
              <p
                class="mt-2 truncate text-xl font-bold"
                :class="hasOverallocation ? 'text-red-600 dark:text-red-400' : 'text-gray-900 dark:text-white'"
              >
                {{ result?.overallocated_by || '0' }}
              </p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                超用独立记录，不冲减员工个人{{ terms.remaining }}
              </p>
            </div>
          </section>

          <div v-if="result?.warning" role="status" class="card flex flex-col gap-1 border-l-4 border-l-amber-500 p-4">
            <span class="text-sm font-semibold text-gray-900 dark:text-white">企业池已超分配</span>
            <span class="text-xs text-gray-500 dark:text-dark-400">
              {{ result.warning }}：当前仅作提示，不代表请求级硬阻断，员工调用仍以上游硬{{ terms.quota }}为准。
            </span>
          </div>
        </template>
      </div>
    </template>

    <template #table>
      <div v-if="sourceState === 'unavailable' || (!loading && !subscriptionId)" class="p-6">
        <EmptyState
          icon="inbox"
          :title="sourceState === 'unavailable' ? '暂不展示历史数据' : '暂无可分配的订阅'"
          description="恢复数据来源或激活订阅后，员工额度明细会在此展示。"
        />
      </div>
      <DataTable
        v-else
        :columns="columns"
        :data="result?.items || []"
        :loading="loading"
        row-key="employee_id"
        :actions-count="1"
      >
        <template #cell-department_id="{ value }">{{ departmentName(value) }}</template>
        <template #cell-overage_credit="{ value }">
          <span
            :class="isPositiveAmount(value) ? 'font-semibold text-red-600 dark:text-red-400' : 'text-gray-500 dark:text-dark-400'"
          >
            {{ value }}
          </span>
        </template>
        <template #cell-status="{ value }">
          <StatusBadge :status="value === 'overage' ? 'error' : 'active'" :label="allocationStatusText(value)" />
        </template>
        <template #cell-actions="{ row }">
          <button type="button" class="btn btn-ghost btn-sm" @click="openDialog(row)">调整</button>
        </template>
        <template #empty>
          <EmptyState
            icon="users"
            :title="`当前订阅暂无员工${terms.allocation}`"
            :description="`从${terms.pool}为员工分配${terms.allocation}后，明细会在此展示。`"
          />
        </template>
      </DataTable>
    </template>
  </TablePageLayout>

  <BaseDialog
    :show="dialogOpen"
    :title="`调整员工${terms.allocation}`"
    width="normal"
    @close="dialogOpen = false"
  >
    <div class="space-y-4">
      <div>
        <label class="input-label" for="allocation-employee">员工</label>
        <Select
          id="allocation-employee"
          v-model="form.employee_id"
          :options="employeeOptions"
          placeholder="选择员工"
          searchable
          :disabled="!!editingRow"
        />
      </div>
      <Input
        v-model="form.credit"
        :label="terms.allocation"
        placeholder="非负数值字符串"
        required
        hint="留空或负数无效；实际可用仍以上游硬额度为准"
      />
      <div>
        <label class="input-label" for="allocation-reason">调整原因（必填）</label>
        <textarea
          id="allocation-reason"
          v-model="form.reason"
          class="input min-h-[72px] w-full resize-y"
          maxlength="200"
          placeholder="不得包含密钥、Token、Cookie 或邮箱等敏感信息"
        ></textarea>
        <p class="input-hint">{{ form.reason.length }} / 200</p>
      </div>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="dialogOpen = false">取消</button>
      <button type="button" class="btn btn-primary" :disabled="saving" @click="save">保存</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Input from '@/components/common/Input.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { enterpriseAPI } from '@/api/enterprise'
import { allocationStatusLabel, sourceStatusLabel } from '@/utils/enterpriseDisplay'
import type { Column } from '@/components/common/types'
import type {
  EnterpriseAllocationListResult,
  EnterpriseAllocationListItem,
  EnterpriseDepartment,
  EnterpriseEmployee
} from '@/types/enterprise'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'

const auth = useEnterpriseAuthStore()
const appStore = useAppStore()
const { t } = useI18n()
const enterpriseId = computed(() => auth.principal?.enterprise_id || 0)

const terms = computed(() => ({
  allocation: t('admin.enterprise.terms.allocation'),
  remaining: t('admin.enterprise.terms.remaining'),
  overage: t('admin.enterprise.terms.overage'),
  quota: t('admin.enterprise.terms.quota'),
  used: t('admin.enterprise.terms.used'),
  pool: t('admin.enterprise.terms.pool')
}))

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

// 后端以 NUMERIC(20,8) 定长字符串返回（零值为 "0.00000000"），必须按数值判断而非字符串字面量比较
const isPositiveAmount = (value?: string | null) => Number(value ?? 0) > 0
const hasOverallocation = computed(() => isPositiveAmount(result.value?.overallocated_by))
const departmentName = (id?: number | null) =>
  departments.value.find((department) => department.id === id)?.name || '未分配部门'
const allocationStatusText = (value: string) => allocationStatusLabel(value, t)
const poolSourceText = computed(() =>
  sourceStatusLabel(result.value?.pool_source_status || 'unavailable', t)
)

const columns = computed<Column[]>(() => [
  { key: 'email', label: '员工' },
  { key: 'department_id', label: '部门归属' },
  { key: 'configured_credit', label: terms.value.allocation },
  { key: 'usage_credit', label: terms.value.used },
  { key: 'remaining_credit', label: terms.value.remaining },
  { key: 'overage_credit', label: terms.value.overage },
  { key: 'status', label: '状态' },
  { key: 'actions', label: '操作' }
])

const employeeOptions = computed(() =>
  employees.value.map((employee) => ({ value: employee.id, label: employee.email }))
)

async function loadDirectories() {
  try {
    ;[departments.value, employees.value] = await Promise.all([
      enterpriseAPI.listDepartments(),
      enterpriseAPI.listEmployees()
    ])
  } catch {
    // 组织目录仅用于展示部门名称，加载失败不阻断额度主流程
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
    appStore.showError(`${terms.value.allocation}数据暂时不可用`)
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
    appStore.showWarning(`请完整填写员工、${terms.value.allocation}与调整原因`)
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
    appStore.showSuccess(`${terms.value.allocation}已保存`)
    dialogOpen.value = false
    await load()
  } catch {
    appStore.showError(`${terms.value.allocation}保存失败，请核对数值、版本号或企业订阅来源`)
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await loadDirectories()
  await load()
})
</script>
