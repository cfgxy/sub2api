<template>
  <div class="space-y-6">
    <button type="button" class="btn btn-ghost btn-sm" @click="router.push({ name: 'EnterpriseEmployees' })">
      <Icon name="arrowLeft" size="sm" class="mr-1.5" />
      返回员工列表
    </button>

    <!-- 加载态 -->
    <div v-if="detailLoading" class="flex items-center justify-center py-16">
      <LoadingSpinner />
    </div>

    <!-- 错误态 -->
    <div v-else-if="detailError" class="card p-6">
      <EmptyState
        icon="exclamationTriangle"
        title="员工详情加载失败"
        :description="detailError"
        action-text="重试"
        @action="loadDetail"
      />
    </div>

    <template v-else-if="detail">
      <!-- 身份卡：企业邮箱是唯一身份标识，不从邮箱推断姓名 -->
      <div class="card flex flex-col gap-4 p-4 sm:p-6 lg:flex-row lg:items-center lg:justify-between">
        <div class="flex min-w-0 items-center gap-4">
          <div
            class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-full bg-primary-100 text-lg font-bold text-primary-600 dark:bg-primary-900/30 dark:text-primary-400"
            aria-hidden="true"
          >
            {{ avatarLetter }}
          </div>
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h2 class="truncate text-lg font-bold text-gray-900 dark:text-white">
                {{ detail.email }}
              </h2>
              <StatusBadge :status="detail.status" :label="employeeStatusText(detail.status)" />
            </div>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
              {{ maskDots }} · {{ detail.department_name || '未分配部门' }}
            </p>
          </div>
        </div>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="detail.status === 'terminated'"
            @click="openEdit"
          >
            <Icon name="edit" size="sm" class="mr-1.5" />
            编辑资料
          </button>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            @click="router.push({ path: '/enterprise/admin/allocation', query: { employee_id: String(detail.id) } })"
          >
            配置{{ terms.allocation }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm" @click="router.push('/enterprise/admin/keys')">
            查看员工 Key
          </button>
          <button
            type="button"
            class="btn btn-danger btn-sm"
            :disabled="detail.status === 'terminated'"
            @click="terminateConfirm = true"
          >
            标记离职
          </button>
        </div>
      </div>

      <div class="tabs w-full overflow-x-auto sm:w-auto sm:inline-flex">
        <button
          v-for="tab in tabs"
          :key="tab.name"
          type="button"
          class="tab whitespace-nowrap"
          :class="{ 'tab-active': activeTab === tab.name }"
          @click="switchTab(tab.name)"
        >
          {{ tab.label }}
        </button>
      </div>

      <!-- 基本信息 -->
      <div v-if="activeTab === 'profile'" class="card divide-y divide-gray-200 dark:divide-dark-700">
        <div v-for="row in profileRows" :key="row.label" class="flex gap-4 p-4">
          <span class="w-28 flex-shrink-0 text-sm text-gray-500 dark:text-dark-400">{{ row.label }}</span>
          <span class="min-w-0 break-all text-sm text-gray-900 dark:text-white">{{ row.value }}</span>
        </div>
      </div>

      <!-- 额度与用量 -->
      <div v-else-if="activeTab === 'usage'" class="space-y-4">
        <div v-if="usageLoading && !usageLoaded" class="flex items-center justify-center py-16">
          <LoadingSpinner />
        </div>
        <div v-else-if="usageError" class="card p-6">
          <EmptyState
            icon="exclamationTriangle"
            title="额度与用量加载失败"
            :description="usageError"
            action-text="重试"
            @action="loadUsage"
          />
        </div>
        <template v-else>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <div class="card p-4">
              <p class="text-xs font-medium text-gray-500 dark:text-dark-400">请求数</p>
              <p class="mt-2 text-xl font-bold text-gray-900 dark:text-white">
                {{ usageSummary?.total_requests ?? 0 }}
              </p>
            </div>
            <div class="card p-4">
              <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ terms.actualCost }}</p>
              <p class="mt-2 text-xl font-bold text-gray-900 dark:text-white">
                {{ usageSummary?.total_usage_credit ?? '0' }}
              </p>
            </div>
            <div class="card p-4">
              <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ terms.allocation }}</p>
              <p class="mt-2 text-xl font-bold text-gray-900 dark:text-white">
                {{ employeeConfiguredCredit }}
              </p>
            </div>
          </div>
          <div class="card">
            <DataTable :columns="usageColumns" :data="usage.items" :loading="usageLoading" row-key="attribution_id">
              <template #cell-request_at="{ value }">{{ formatDate(value) }}</template>
              <template #cell-classification="{ value }">
                {{ value === 'employee' ? '员工' : '受控外部' }}
              </template>
              <template #empty>
                <EmptyState icon="inbox" title="暂无用量记录" description="该员工在当前范围内尚未产生调用。" />
              </template>
            </DataTable>
          </div>
          <Pagination
            v-if="usage.total"
            :total="usage.total"
            :page="usagePage"
            :page-size="pageSize"
            @update:page="onUsagePage"
          />
        </template>
      </div>

      <!-- API Key -->
      <div v-else-if="activeTab === 'key'" class="card">
        <div v-if="detail.current_key" class="divide-y divide-gray-200 dark:divide-dark-700">
          <div v-for="row in keyRows" :key="row.label" class="flex gap-4 p-4">
            <span class="w-28 flex-shrink-0 text-sm text-gray-500 dark:text-dark-400">{{ row.label }}</span>
            <span class="min-w-0 break-all text-sm text-gray-900 dark:text-white" :class="row.mono ? 'font-mono tracking-wider' : ''">
              {{ row.value }}
            </span>
          </div>
        </div>
        <div v-else class="p-6">
          <EmptyState
            icon="key"
            title="该员工当前没有可用的 API Key"
            description="可在「员工 Key」页为其分配或代轮换 Key。"
          />
        </div>
      </div>

      <!-- 状态历史 -->
      <div v-else class="space-y-4">
        <div v-if="historyLoading && !historyLoaded" class="flex items-center justify-center py-16">
          <LoadingSpinner />
        </div>
        <div v-else-if="historyError" class="card p-6">
          <EmptyState
            icon="exclamationTriangle"
            title="状态历史加载失败"
            :description="historyError"
            action-text="重试"
            @action="loadHistory"
          />
        </div>
        <template v-else>
          <div class="card p-4 sm:p-6">
            <div v-if="!history.items.length">
              <EmptyState icon="clock" title="暂无状态变更记录" description="该员工尚未发生状态变更。" />
            </div>
            <ul v-else class="space-y-4 border-l border-gray-200 pl-5 dark:border-dark-700">
              <li v-for="event in history.items" :key="event.id" class="relative">
                <span
                  class="absolute -left-[23px] top-1.5 h-2 w-2 rounded-full bg-primary-500 ring-2 ring-white dark:ring-dark-900"
                  aria-hidden="true"
                ></span>
                <p class="text-sm font-semibold text-gray-900 dark:text-white">
                  <span :title="event.event_type">{{ auditEventLabel(event.event_type, t) }}</span>
                </p>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                  {{ formatDate(event.created_at) }} · {{ event.actor_ref }}
                </p>
              </li>
            </ul>
          </div>
          <Pagination
            v-if="history.total"
            :total="history.total"
            :page="historyPage"
            :page-size="pageSize"
            @update:page="onHistoryPage"
          />
        </template>
      </div>
    </template>

    <BaseDialog :show="editVisible" title="编辑员工" width="normal" @close="editVisible = false">
      <div class="space-y-4">
        <Input :model-value="detail?.email || ''" label="邮箱" disabled readonly />
        <div>
          <label class="input-label" for="detail-department">部门</label>
          <Select
            id="detail-department"
            v-model="editDepartment"
            :options="departmentOptions"
            placeholder="不分配部门"
            clearable
          />
        </div>
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="editVisible = false">取消</button>
        <button type="button" class="btn btn-primary" :disabled="saving" @click="saveEmployee">保存</button>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="terminateConfirm"
      title="员工离职"
      :message="`确认将 ${detail?.email || ''} 标记为离职？该操作会撤销其会话。`"
      danger
      confirm-text="确认离职"
      @confirm="terminate"
      @cancel="terminateConfirm = false"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import Input from '@/components/common/Input.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import {
  enterpriseAPI,
  isEnterpriseEmployeeDepartmentInvalid,
  isEnterpriseEmployeeVersionConflict
} from '@/api/enterprise'
import { auditEventLabel, employeeStatusLabel, keyStatusLabel } from '@/utils/enterpriseDisplay'
import type { Column } from '@/components/common/types'
import type {
  EnterpriseDepartment,
  EnterpriseEmployeeDetail,
  EnterprisePaginated,
  EnterpriseWorkbenchAuditEvent,
  EnterpriseWorkbenchSummary,
  EnterpriseWorkbenchUsageRow
} from '@/types/enterprise'

const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const { t } = useI18n()

const terms = computed(() => ({
  allocation: t('admin.enterprise.terms.allocation'),
  actualCost: t('admin.enterprise.terms.actualCost'),
  quota: t('admin.enterprise.terms.quota')
}))

const employeeId = computed(() => Number(route.params.id))
const detail = ref<EnterpriseEmployeeDetail>()
const detailLoading = ref(false)
const detailError = ref('')
const departments = ref<EnterpriseDepartment[]>([])
const editVisible = ref(false)
const editDepartment = ref<number | null>(null)
const saving = ref(false)
const terminateConfirm = ref(false)
const activeTab = ref('profile')
const pageSize = ref(20)

const usage = reactive<EnterprisePaginated<EnterpriseWorkbenchUsageRow>>({
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
  pages: 1
})
const usageSummary = ref<EnterpriseWorkbenchSummary>()
const usageLoading = ref(false)
const usageError = ref('')
const usageLoaded = ref(false)
const usagePage = ref(1)

const history = reactive<EnterprisePaginated<EnterpriseWorkbenchAuditEvent>>({
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
  pages: 1
})
const historyLoading = ref(false)
const historyError = ref('')
const historyLoaded = ref(false)
const historyPage = ref(1)

const maskDots = '••••••••••'
const tabs = [
  { name: 'profile', label: '基本信息' },
  { name: 'usage', label: '额度与用量' },
  { name: 'key', label: 'API Key' },
  { name: 'history', label: '状态历史' }
]

const employeeStatusText = (value: string) => employeeStatusLabel(value, t)
const keyStatusText = (value: string) => keyStatusLabel(value, t)
const employeeConfiguredCredit = computed(
  () =>
    usageSummary.value?.employee_summaries?.find((item) => item.employee_id === employeeId.value)
      ?.configured_credit ?? '0'
)
const avatarLetter = computed(() => detail.value?.email?.[0]?.toUpperCase() || '?')
const formatDate = (value?: string) =>
  value ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '-'

const departmentOptions = computed(() =>
  departments.value.map((department) => ({ value: department.id, label: department.name }))
)

const profileRows = computed(() => {
  if (!detail.value) return []
  const rows = [
    { label: '员工 ID', value: maskDots },
    { label: '企业邮箱', value: detail.value.email },
    { label: '部门归属', value: detail.value.department_name || '未分配' },
    { label: '状态', value: employeeStatusText(detail.value.status) },
    { label: '首次改密', value: detail.value.must_change_password ? '待完成' : '已完成' },
    { label: '加入时间', value: formatDate(detail.value.created_at) }
  ]
  if (detail.value.terminated_at) rows.push({ label: '离职时间', value: formatDate(detail.value.terminated_at) })
  return rows
})

const keyRows = computed(() => {
  const key = detail.value?.current_key
  if (!key) return []
  return [
    { label: 'Key', value: key.masked_key, mono: true },
    { label: '状态', value: keyStatusText(key.status), mono: false },
    { label: terms.value.quota, value: `${key.quota_used} / ${key.quota}`, mono: false },
    {
      label: '限流（5h / 1d / 7d）',
      value: `${key.usage_5h}/${key.rate_limit_5h} · ${key.usage_1d}/${key.rate_limit_1d} · ${key.usage_7d}/${key.rate_limit_7d}`,
      mono: false
    },
    { label: '创建时间', value: formatDate(key.created_at), mono: false }
  ]
})

const usageColumns = computed<Column[]>(() => [
  { key: 'request_at', label: '请求时间' },
  { key: 'api_key_masked', label: 'Key' },
  { key: 'window_type', label: '窗口' },
  { key: 'usage_credit', label: terms.value.actualCost },
  { key: 'classification', label: '归属' }
])

async function loadDetail() {
  detailLoading.value = true
  detailError.value = ''
  try {
    const [loadedDetail, loadedDepartments] = await Promise.all([
      enterpriseAPI.getEmployee(employeeId.value),
      enterpriseAPI.listDepartments()
    ])
    detail.value = loadedDetail
    departments.value = loadedDepartments
  } catch (error) {
    detailError.value = (error as { message?: string })?.message || '员工详情加载失败'
  } finally {
    detailLoading.value = false
  }
}

async function loadUsage() {
  usageLoading.value = true
  usageError.value = ''
  try {
    const [rows, summary] = await Promise.all([
      enterpriseAPI.listWorkbenchUsage({
        employee_id: employeeId.value,
        page: usagePage.value,
        page_size: pageSize.value
      }),
      enterpriseAPI.getWorkbenchSummary({ employee_id: employeeId.value })
    ])
    Object.assign(usage, rows)
    usageSummary.value = summary
    usageLoaded.value = true
  } catch (error) {
    usageError.value = (error as { message?: string })?.message || '额度与用量加载失败'
  } finally {
    usageLoading.value = false
  }
}

async function loadHistory() {
  historyLoading.value = true
  historyError.value = ''
  try {
    Object.assign(
      history,
      await enterpriseAPI.listWorkbenchAuditEvents({
        employee_id: employeeId.value,
        page: historyPage.value,
        page_size: pageSize.value
      })
    )
    historyLoaded.value = true
  } catch (error) {
    historyError.value = (error as { message?: string })?.message || '状态历史加载失败'
  } finally {
    historyLoading.value = false
  }
}

function onUsagePage(page: number) {
  usagePage.value = page
  loadUsage()
}
function onHistoryPage(page: number) {
  historyPage.value = page
  loadHistory()
}

function switchTab(name: string) {
  activeTab.value = name
  if (name === 'usage' && !usageLoaded.value) loadUsage()
  if (name === 'history' && !historyLoaded.value) loadHistory()
}

function openEdit() {
  editDepartment.value = detail.value?.department_id ?? null
  editVisible.value = true
}

async function handleEmployeeVersionConflict(error: unknown) {
  if (!isEnterpriseEmployeeVersionConflict(error)) throw error
  appStore.showWarning('该员工已被其他管理员修改，已刷新为最新数据，请重新操作。')
  await loadDetail()
}

async function saveEmployee() {
  if (!detail.value) return
  saving.value = true
  try {
    await enterpriseAPI.updateEmployee(detail.value.id, {
      status: detail.value.status === 'disabled' ? 'disabled' : 'active',
      department_id: editDepartment.value,
      version: detail.value.version
    })
    editVisible.value = false
    appStore.showSuccess('员工信息已更新')
    await loadDetail()
    usageLoaded.value = false
    historyLoaded.value = false
    if (activeTab.value === 'usage') await loadUsage()
    if (activeTab.value === 'history') await loadHistory()
  } catch (error) {
    if (isEnterpriseEmployeeVersionConflict(error)) {
      await handleEmployeeVersionConflict(error)
      editDepartment.value = detail.value?.department_id ?? null
    } else if (isEnterpriseEmployeeDepartmentInvalid(error)) {
      appStore.showError('所选部门无效，请重新选择部门后重试')
    } else {
      editVisible.value = false
      appStore.showError((error as { message?: string })?.message || '员工信息更新失败')
    }
  } finally {
    saving.value = false
  }
}

async function terminate() {
  terminateConfirm.value = false
  if (!detail.value) return
  await enterpriseAPI.terminateEmployee(detail.value.id)
  appStore.showSuccess('员工已离职')
  await loadDetail()
}

onMounted(loadDetail)
</script>
