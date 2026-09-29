<template>
  <TablePageLayout>
    <template #actions>
      <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div class="min-w-0">
          <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-dark-400">
            企业管理 / 组织
          </p>
          <h2 class="mt-1 text-xl font-bold text-gray-900 dark:text-white">员工</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
            管理员工状态、所属部门和初始登录凭据；企业邮箱是员工的唯一身份标识。
          </p>
        </div>
        <button type="button" class="btn btn-primary w-full sm:w-auto" @click="openCreate">
          <Icon name="plus" size="md" class="mr-2" />
          创建员工
        </button>
      </div>
    </template>

    <template #filters>
      <div class="card p-4 sm:p-6">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div class="sm:col-span-2">
            <label class="input-label" for="employee-search">搜索员工</label>
            <SearchInput id="employee-search" v-model="query" placeholder="搜索员工邮箱" />
          </div>
          <div>
            <label class="input-label" for="employee-status">状态</label>
            <Select
              id="employee-status"
              v-model="statusFilter"
              :options="statusOptions"
              placeholder="全部状态"
              clearable
            />
          </div>
          <div class="flex items-end">
            <button type="button" class="btn btn-secondary w-full" :disabled="loading" @click="load">
              <Icon name="refresh" size="md" class="mr-2" :class="{ 'animate-spin': loading }" />
              刷新
            </button>
          </div>
        </div>
      </div>
    </template>

    <template #table>
      <div v-if="loadError" class="p-6">
        <EmptyState
          icon="exclamationTriangle"
          title="员工列表加载失败"
          :description="loadError"
          action-text="重试"
          @action="load"
        />
      </div>
      <DataTable
        v-else
        :columns="columns"
        :data="filtered"
        :loading="loading"
        row-key="id"
        :actions-count="6"
        expandable-actions
      >
        <template #cell-id="{ row }">
          <span class="font-mono tracking-widest text-gray-500 dark:text-dark-400">
            {{ maskEmployeeId(row.id) }}
          </span>
        </template>
        <template #cell-department_id="{ value }">
          {{ departmentName(value) }}
        </template>
        <template #cell-status="{ value }">
          <StatusBadge :status="value" :label="employeeStatusText(value)" />
        </template>
        <template #cell-must_change_password="{ value }">
          <span :class="value ? 'text-yellow-600 dark:text-yellow-400' : 'text-gray-500 dark:text-dark-400'">
            {{ value ? '待完成' : '已完成' }}
          </span>
        </template>
        <template #cell-actions="{ row }">
          <div class="flex flex-wrap items-center gap-1">
            <button type="button" class="btn btn-ghost btn-sm" @click="goDetail(row)">详情</button>
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              :disabled="row.status === 'terminated'"
              @click="openEdit(row)"
            >
              编辑
            </button>
            <button
              v-if="row.status === 'active'"
              type="button"
              class="btn btn-ghost btn-sm"
              @click="askStatus(row, 'disabled')"
            >
              停用
            </button>
            <button
              v-else-if="row.status === 'disabled'"
              type="button"
              class="btn btn-ghost btn-sm"
              @click="askStatus(row, 'active')"
            >
              恢复
            </button>
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              :disabled="row.status === 'terminated'"
              @click="askResetPassword(row)"
            >
              重置密码
            </button>
            <button
              type="button"
              class="btn btn-ghost btn-sm text-red-600 dark:text-red-400"
              :disabled="row.status === 'terminated'"
              @click="askTerminate(row)"
            >
              离职
            </button>
          </div>
        </template>
        <template #empty>
          <EmptyState icon="users" title="暂无匹配员工" description="调整搜索或状态筛选，或创建新的员工账号。" />
        </template>
      </DataTable>
    </template>
  </TablePageLayout>

  <!-- 创建员工 -->
  <BaseDialog :show="createVisible" title="创建员工" width="normal" @close="createVisible = false">
    <div class="space-y-4">
      <p class="rounded-lg bg-yellow-50 p-3 text-sm text-yellow-700 dark:bg-yellow-900/20 dark:text-yellow-400">
        初始密码仅在创建成功后的交付卡片中显示一次，请立即复制并通过安全渠道交付员工。
      </p>
      <Input
        id="create-email"
        v-model="createForm.email"
        label="邮箱"
        type="email"
        required
        autocomplete="off"
        :error="createErrors.email"
        placeholder="employee@example.com"
      />
      <div>
        <Input
          id="create-password"
          v-model="createForm.initial_password"
          label="初始密码"
          type="password"
          required
          autocomplete="new-password"
          :error="createErrors.initial_password"
        />
        <button
          type="button"
          class="btn btn-secondary btn-sm mt-2"
          data-testid="generate-initial-password"
          @click="createForm.initial_password = generateInitialPassword()"
        >
          随机生成
        </button>
      </div>
      <div>
        <label class="input-label" for="create-department">部门</label>
        <Select
          id="create-department"
          v-model="createForm.department_id"
          :options="departmentOptions"
          placeholder="不分配部门"
          clearable
        />
      </div>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="createVisible = false">取消</button>
      <button type="button" class="btn btn-primary" :disabled="saving" @click="createEmployee">创建</button>
    </template>
  </BaseDialog>

  <!-- 编辑员工 -->
  <BaseDialog :show="editVisible" title="编辑员工" width="normal" @close="editVisible = false">
    <div class="space-y-4">
      <Input :model-value="editing?.email || ''" label="邮箱" disabled readonly />
      <div>
        <label class="input-label" for="edit-department">部门</label>
        <Select
          id="edit-department"
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

  <!-- 初始密码交付 -->
  <BaseDialog :show="deliveryVisible" title="初始密码交付" width="normal" @close="closeDelivery">
    <div class="space-y-4" data-testid="initial-password-delivery">
      <p class="rounded-lg bg-yellow-50 p-3 text-sm text-yellow-700 dark:bg-yellow-900/20 dark:text-yellow-400">
        系统当前不会自动发送邮件，请通过安全渠道将初始密码交付员工；首次登录后须立即修改。
      </p>
      <dl v-if="delivery" class="divide-y divide-gray-200 rounded-lg border border-gray-200 dark:divide-dark-700 dark:border-dark-700">
        <div class="flex gap-4 p-3">
          <dt class="w-24 flex-shrink-0 text-sm text-gray-500 dark:text-dark-400">邮箱</dt>
          <dd class="min-w-0 break-all text-sm text-gray-900 dark:text-white" data-testid="delivery-email">
            {{ delivery.email }}
          </dd>
        </div>
        <div class="flex gap-4 p-3">
          <dt class="w-24 flex-shrink-0 text-sm text-gray-500 dark:text-dark-400">初始密码</dt>
          <dd
            class="min-w-0 break-all font-mono tracking-wider text-sm text-gray-900 dark:text-white"
            data-testid="delivery-password"
          >
            {{ delivery.password }}
          </dd>
        </div>
        <div class="flex gap-4 p-3">
          <dt class="w-24 flex-shrink-0 text-sm text-gray-500 dark:text-dark-400">入口域名</dt>
          <dd class="min-w-0 break-all text-sm text-gray-900 dark:text-white" data-testid="delivery-origin">
            {{ delivery.origin }}
          </dd>
        </div>
      </dl>
      <div v-if="manualCopyText">
        <p role="alert" class="mb-2 text-sm text-red-600 dark:text-red-400">
          自动复制失败，请在下方文本框中手动复制交付信息。
        </p>
        <textarea
          ref="manualCopyInput"
          class="input min-h-[100px] w-full resize-y font-mono text-sm"
          data-testid="manual-copy-text"
          :value="manualCopyText"
          readonly
          aria-label="交付信息（手动复制）"
        ></textarea>
      </div>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="copyDelivery">复制交付信息</button>
      <button type="button" class="btn btn-primary" @click="closeDelivery">我已保存，关闭</button>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="confirmState.show"
    :title="confirmState.title"
    :message="confirmState.message"
    :danger="confirmState.danger"
    confirm-text="确认"
    @confirm="runConfirm"
    @cancel="confirmState.show = false"
  />
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import Input from '@/components/common/Input.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import {
  enterpriseAPI,
  isEnterpriseEmployeeDepartmentInvalid,
  isEnterpriseEmployeeVersionConflict
} from '@/api/enterprise'
import { employeeStatusLabel } from '@/utils/enterpriseDisplay'
import { validatePassword } from '@/features/enterprise/validation'
import type { Column } from '@/components/common/types'
import type { EnterpriseDepartment, EnterpriseEmployee } from '@/types/enterprise'

const router = useRouter()
const appStore = useAppStore()
const { t } = useI18n()

const employees = ref<EnterpriseEmployee[]>([])
const departments = ref<EnterpriseDepartment[]>([])
const loading = ref(false)
const loadError = ref('')
const saving = ref(false)
const query = ref('')
const statusFilter = ref('')
const createVisible = ref(false)
const editVisible = ref(false)
const editing = ref<EnterpriseEmployee>()
const editDepartment = ref<number | null>(null)

const employeeStatusText = (value: string) => employeeStatusLabel(value, t)
// 员工 ID 是内部主键，列表中不暴露原值
const maskEmployeeId = (_id: number) => '••••••••••'

const columns = computed<Column[]>(() => [
  { key: 'email', label: '邮箱' },
  { key: 'id', label: '员工 ID' },
  { key: 'department_id', label: '部门' },
  { key: 'status', label: '状态' },
  { key: 'must_change_password', label: '首次改密' },
  { key: 'actions', label: '操作' }
])

const statusOptions = [
  { value: 'active', label: employeeStatusLabel('active', t) },
  { value: 'disabled', label: employeeStatusLabel('disabled', t) },
  { value: 'terminated', label: employeeStatusLabel('terminated', t) }
]
const departmentOptions = computed(() =>
  departments.value.map((department) => ({ value: department.id, label: department.name }))
)

const createForm = reactive({ email: '', initial_password: '', department_id: null as number | null })
const createErrors = reactive({ email: '', initial_password: '' })

const INITIAL_PASSWORD_CHARSET = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789'
function generateInitialPassword(): string {
  const bytes = new Uint32Array(12)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => INITIAL_PASSWORD_CHARSET[b % INITIAL_PASSWORD_CHARSET.length]).join('')
}

const deliveryVisible = ref(false)
const delivery = ref<{ email: string; password: string; origin: string } | null>(null)
const manualCopyText = ref('')
const manualCopyInput = ref<HTMLTextAreaElement | null>(null)

function showDelivery(email: string, password: string) {
  manualCopyText.value = ''
  delivery.value = { email, password, origin: window.location.origin }
  deliveryVisible.value = true
}
function closeDelivery() {
  deliveryVisible.value = false
  delivery.value = null
  manualCopyText.value = ''
}

async function copyDelivery() {
  if (!delivery.value) return
  const text = `邮箱：${delivery.value.email}\n初始密码：${delivery.value.password}\n入口域名：${delivery.value.origin}`
  try {
    if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(text)
    manualCopyText.value = ''
    appStore.showSuccess('交付信息已复制')
  } catch {
    manualCopyText.value = text
    appStore.showError('无法自动复制，请使用下方文本框手动复制')
    await nextTick()
    manualCopyInput.value?.focus()
    manualCopyInput.value?.select()
  }
}

const filtered = computed(() =>
  employees.value.filter(
    (employee) =>
      (!query.value || employee.email.toLowerCase().includes(query.value.toLowerCase())) &&
      (!statusFilter.value || employee.status === statusFilter.value)
  )
)
const departmentName = (id?: number | null) =>
  departments.value.find((department) => department.id === id)?.name || '未分配'

function goDetail(row: EnterpriseEmployee) {
  router.push({ name: 'EnterpriseEmployeeDetail', params: { id: row.id } })
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [loadedEmployees, loadedDepartments] = await Promise.all([
      enterpriseAPI.listEmployees({ suppressUnavailableRedirect: true }),
      enterpriseAPI.listDepartments({ suppressUnavailableRedirect: true })
    ])
    employees.value = loadedEmployees
    departments.value = loadedDepartments
  } catch (error) {
    loadError.value = (error as { message?: string })?.message || '员工列表加载失败'
  } finally {
    loading.value = false
  }
}

function openCreate() {
  resetCreate()
  createVisible.value = true
}
function resetCreate() {
  createForm.email = ''
  createForm.initial_password = ''
  createForm.department_id = null
  createErrors.email = ''
  createErrors.initial_password = ''
}
function validateCreate(): boolean {
  createErrors.email = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(createForm.email) ? '' : '请输入有效邮箱'
  createErrors.initial_password = validatePassword(createForm.initial_password) || ''
  return !createErrors.email && !createErrors.initial_password
}

async function createEmployee() {
  if (!validateCreate()) return
  saving.value = true
  try {
    const created = await enterpriseAPI.createEmployee({ ...createForm })
    const password = createForm.initial_password
    createVisible.value = false
    showDelivery(created.email, password)
    resetCreate()
    appStore.showSuccess('员工已创建')
    await load()
  } catch (error) {
    if ((error as { status?: number })?.status === 409) {
      createErrors.email = '该邮箱已被使用，请更换邮箱'
      appStore.showError('该邮箱已被使用，请更换邮箱')
    } else {
      appStore.showError((error as { message?: string })?.message || '员工创建失败')
    }
  } finally {
    saving.value = false
  }
}

function openEdit(row: EnterpriseEmployee) {
  editing.value = row
  editDepartment.value = row.department_id ?? null
  editVisible.value = true
}

async function handleEmployeeVersionConflict(error: unknown) {
  if (!isEnterpriseEmployeeVersionConflict(error)) throw error
  appStore.showWarning('该员工已被其他管理员修改，已刷新为最新数据，请重新操作。')
  await load()
}

async function saveEmployee() {
  if (!editing.value) return
  const target = editing.value
  saving.value = true
  try {
    await enterpriseAPI.updateEmployee(target.id, {
      status: target.status === 'disabled' ? 'disabled' : 'active',
      department_id: editDepartment.value,
      version: target.version
    })
    editVisible.value = false
    appStore.showSuccess('员工信息已更新')
    await load()
  } catch (error) {
    if (isEnterpriseEmployeeVersionConflict(error)) {
      await handleEmployeeVersionConflict(error)
      const latest = employees.value.find((employee) => employee.id === target.id)
      if (latest) {
        editing.value = latest
        editDepartment.value = latest.department_id ?? null
      }
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

const confirmState = reactive({
  show: false,
  title: '',
  message: '',
  danger: false,
  action: null as null | (() => Promise<void>)
})

function ask(title: string, message: string, danger: boolean, action: () => Promise<void>) {
  confirmState.title = title
  confirmState.message = message
  confirmState.danger = danger
  confirmState.action = action
  confirmState.show = true
}

async function runConfirm() {
  const action = confirmState.action
  confirmState.show = false
  confirmState.action = null
  if (action) await action()
}

function askStatus(row: EnterpriseEmployee, status: 'active' | 'disabled') {
  ask(
    status === 'active' ? '恢复员工' : '停用员工',
    status === 'active'
      ? `确认恢复 ${row.email}？恢复后该员工可重新登录企业门户。`
      : `确认停用 ${row.email}？停用会拒绝其新登录并撤销活动会话。`,
    status === 'disabled',
    () => setStatus(row, status)
  )
}

async function setStatus(row: EnterpriseEmployee, status: 'active' | 'disabled') {
  try {
    await enterpriseAPI.updateEmployee(row.id, {
      status,
      department_id: row.department_id ?? null,
      version: row.version
    })
    appStore.showSuccess(status === 'active' ? '员工已恢复' : '员工已停用')
    await load()
  } catch (error) {
    await handleEmployeeVersionConflict(error)
  }
}

function askResetPassword(row: EnterpriseEmployee) {
  ask(
    '重置员工密码',
    `确认为 ${row.email} 重置密码？将生成新的初始密码并撤销其全部会话，员工下次登录须使用新初始密码。`,
    true,
    () => resetPassword(row)
  )
}

async function resetPassword(row: EnterpriseEmployee) {
  const result = await enterpriseAPI.resetEmployeePassword(row.id)
  showDelivery(row.email, result.initial_password)
}

function askTerminate(row: EnterpriseEmployee) {
  ask('员工离职', `确认将 ${row.email} 标记为离职？该操作会撤销其会话。`, true, () => terminate(row))
}

async function terminate(row: EnterpriseEmployee) {
  await enterpriseAPI.terminateEmployee(row.id)
  appStore.showSuccess('员工已离职')
  await load()
}

onMounted(load)
</script>
