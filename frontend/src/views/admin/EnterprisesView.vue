<template>
  <PlatformEnterpriseShell>
    <TablePageLayout>
      <template #actions>
        <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('admin.enterprise.list.title') }}</h1>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.list.description') }}</p>
          </div>
          <button class="btn btn-primary btn-md" data-testid="enterprise-create-entry" @click="router.push('/admin/enterprises/new')">
            <Icon name="plus" size="sm" />
            {{ t('admin.enterprise.list.create') }}
          </button>
        </div>
      </template>

      <template #filters>
        <div class="space-y-3">
          <div class="flex flex-wrap items-center gap-3">
            <div class="min-w-[220px] flex-1 sm:max-w-sm">
              <SearchInput v-model="search" :placeholder="t('admin.enterprise.list.search')" data-testid="enterprises-search" @search="load" />
            </div>
            <div class="w-full sm:w-40">
              <Select
                :model-value="status"
                :options="statusOptions"
                :placeholder="t('admin.enterprise.list.allStatuses')"
                clearable
                data-testid="enterprises-status"
                @update:model-value="onStatusChange"
              />
            </div>
            <button class="btn btn-secondary btn-md" data-testid="enterprises-query" @click="load">
              <Icon name="search" size="sm" />
              {{ t('admin.enterprise.list.query') }}
            </button>
          </div>
          <div class="flex items-center justify-between text-sm text-gray-500 dark:text-dark-400">
            <span data-testid="enterprises-count">
              {{ loading ? t('admin.enterprise.list.loading') : t('admin.enterprise.list.count', { count: items.length }) }}
            </span>
            <button class="btn btn-ghost btn-sm" :disabled="loading" data-testid="enterprises-refresh" @click="load">
              <Icon name="refresh" size="sm" :class="loading && 'animate-spin'" />
              {{ t('admin.enterprise.list.refresh') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <div
          v-if="!loading && sourceUnavailable"
          class="p-6 text-sm text-red-600 dark:text-red-400"
          role="alert"
          data-testid="enterprises-source-unavailable"
        >
          <strong class="block text-base font-semibold">{{ t('admin.enterprise.list.unavailableTitle') }}</strong>
          <p class="mt-1">{{ t('admin.enterprise.list.unavailableDescription') }}</p>
          <button class="btn btn-secondary btn-sm mt-4" data-testid="enterprises-source-retry" @click="load">
            {{ t('admin.enterprise.list.refresh') }}
          </button>
        </div>
        <DataTable v-else :columns="columns" :data="items" :loading="loading" row-key="id">
          <template #cell-created_at="{ row }">{{ formatDate(row.created_at) }}</template>
          <template #cell-summary="{ row }">
            <span v-if="row.subscriptions?.length">{{ row.subscriptions.map(subscriptionSummary).join('；') }}</span>
            <span v-else class="text-gray-400 dark:text-dark-500" data-testid="enterprise-no-subscriptions">
              {{ t('admin.enterprise.common.noSubscriptions') }}
            </span>
          </template>
          <template #cell-status="{ row }">
            <StatusBadge :status="row.status" :label="enterpriseStatusLabel(row.status, t)" />
          </template>
          <template #cell-actions="{ row }">
            <div class="flex flex-wrap items-center gap-1">
              <button class="btn btn-ghost btn-sm" @click="router.push(`/admin/enterprises/${row.id}`)">
                {{ t('admin.enterprise.common.view') }}
              </button>
              <button
                v-if="row.status === 'active'"
                class="btn btn-ghost btn-sm text-red-600 dark:text-red-400"
                data-testid="enterprise-disable"
                @click="askDisable(row)"
              >
                {{ t('admin.enterprise.common.disable') }}
              </button>
              <button
                v-else
                class="btn btn-ghost btn-sm text-green-600 dark:text-green-400"
                data-testid="enterprise-enable"
                @click="askEnable(row)"
              >
                {{ t('admin.enterprise.common.enable') }}
              </button>
              <button class="btn btn-ghost btn-sm" data-testid="enterprise-update-host" @click="openHostDialog(row)">
                {{ t('admin.enterprise.list.changeHost') }}
              </button>
            </div>
          </template>
          <template #empty>
            <EmptyState :title="t('admin.enterprise.list.empty')" data-testid="enterprises-empty" />
          </template>
        </DataTable>
      </template>
    </TablePageLayout>

    <ConfirmDialog
      :show="!!pendingAction"
      :title="confirmTitle"
      :message="confirmMessage"
      :danger="pendingAction?.kind === 'disable'"
      @confirm="runPendingAction"
      @cancel="pendingAction = null"
    />

    <BaseDialog :show="!!hostDialogRow" :title="t('admin.enterprise.list.hostTitle')" width="normal" @close="closeHostDialog">
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-gray-400">
          {{ hostDialogRow ? t('admin.enterprise.list.hostConfirm', { name: hostDialogRow.name, host: hostDialogRow.portal_host }) : '' }}
        </p>
        <div data-testid="enterprise-host-input">
          <Input
            v-model="hostValue"
            :label="t('admin.enterprise.list.hostLabel')"
            placeholder="enterprise.example.com"
            :error="hostError"
          />
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary btn-md" data-testid="enterprise-host-cancel" @click="closeHostDialog">
            {{ t('admin.enterprise.common.cancel') }}
          </button>
          <button class="btn btn-primary btn-md" :disabled="hostSaving" data-testid="enterprise-host-submit" @click="submitHost">
            {{ t('admin.enterprise.list.hostTitle') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </PlatformEnterpriseShell>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Input from '@/components/common/Input.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import type { Column } from '@/components/common/types'
import Icon from '@/components/icons/Icon.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'
import PlatformEnterpriseShell from '@/components/admin/PlatformEnterpriseShell.vue'
import { useAppStore } from '@/stores/app'
import { formatDate } from '@/utils/format'
import { enterpriseStatusLabel, subscriptionStatusLabel } from '@/utils/enterpriseDisplay'

// 入口域名格式：小写字母、数字、点与连字符，单段不超过 63 字符、整体不超过 255 字符
const HOST_PATTERN = /^(?=.{1,255}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/

type PendingAction = { kind: 'disable' | 'enable'; row: PlatformEnterprise } | null

const router = useRouter()
const { t } = useI18n({ useScope: 'global' })
const appStore = useAppStore()
const items = ref<PlatformEnterprise[]>([])
const loading = ref(false)
const search = ref('')
const status = ref('')
const sourceUnavailable = ref(false)
const pendingAction = ref<PendingAction>(null)
const confirmTitle = ref('')
const confirmMessage = ref('')
const hostDialogRow = ref<PlatformEnterprise | null>(null)
const hostValue = ref('')
const hostError = ref('')
const hostSaving = ref(false)

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.enterprise.common.name') },
  { key: 'portal_host', label: t('admin.enterprise.common.portalHost') },
  { key: 'dedicated_upstream_user_id', label: t('admin.enterprise.common.upstreamUserId') },
  { key: 'admin_email', label: t('admin.enterprise.common.adminAccount') },
  { key: 'created_at', label: t('admin.enterprise.common.createdAt') },
  { key: 'summary', label: t('admin.enterprise.list.summary') },
  { key: 'status', label: t('admin.enterprise.common.status') },
  { key: 'actions', label: t('admin.enterprise.list.operations') },
])

const statusOptions = computed(() => [
  { value: 'active', label: enterpriseStatusLabel('active', t) },
  { value: 'disabled', label: enterpriseStatusLabel('disabled', t) },
])

function subscriptionSummary(subscription: PlatformEnterprise['subscriptions'][number]) {
  return t('admin.enterprise.list.summaryValue', { plan: subscription.plan, status: subscriptionStatusLabel(subscription.status, t) })
}

function onStatusChange(value: string | number | boolean | null) {
  status.value = value === null || value === undefined ? '' : String(value)
  load()
}

async function load() {
  loading.value = true
  sourceUnavailable.value = false
  try {
    items.value = await enterprisePlatformAPI.list({ search: search.value, status: status.value })
  } catch {
    items.value = []
    sourceUnavailable.value = true
    appStore.showError(t('admin.enterprise.list.unavailableMessage'))
  } finally {
    loading.value = false
  }
}

async function askDisable(row: PlatformEnterprise) {
  let impact: PlatformEnterprise
  try {
    impact = await enterprisePlatformAPI.get(row.id)
  } catch {
    appStore.showError(t('admin.enterprise.list.disableFailed'))
    return
  }
  const subscriptionLabel = impact.subscriptions?.length
    ? impact.subscriptions.map((subscription) => t('admin.enterprise.list.disableSubscription', {
        plan: subscription.plan,
        status: subscriptionStatusLabel(subscription.status, t),
        limit: subscription.weekly_limit || t('admin.enterprise.common.notConfigured'),
      })).join('；')
    : t('admin.enterprise.common.noActiveSubscriptions')
  confirmTitle.value = t('admin.enterprise.list.disableTitle')
  confirmMessage.value = t('admin.enterprise.list.disableConfirm', {
    name: row.name,
    subscriptions: subscriptionLabel,
    employees: impact.employee_count,
    activeEmployees: impact.active_employee_count,
    sessions: impact.active_session_count,
    keys: impact.active_key_count,
  })
  pendingAction.value = { kind: 'disable', row }
}

function askEnable(row: PlatformEnterprise) {
  confirmTitle.value = t('admin.enterprise.list.enableTitle')
  confirmMessage.value = t('admin.enterprise.list.enableConfirm', { name: row.name })
  pendingAction.value = { kind: 'enable', row }
}

async function runPendingAction() {
  const action = pendingAction.value
  pendingAction.value = null
  if (!action) return
  if (action.kind === 'disable') await disable(action.row)
  else await enable(action.row)
}

async function disable(row: PlatformEnterprise) {
  try {
    await enterprisePlatformAPI.disable(row.id, t('admin.enterprise.list.disableReason'))
    appStore.showSuccess(t('admin.enterprise.list.disableSuccess'))
    await load()
  } catch {
    appStore.showError(t('admin.enterprise.list.disableFailed'))
  }
}

async function enable(row: PlatformEnterprise) {
  try {
    await enterprisePlatformAPI.enable(row.id, t('admin.enterprise.list.enableReason'))
    appStore.showSuccess(t('admin.enterprise.list.enableSuccess'))
    await load()
  } catch {
    appStore.showError(t('admin.enterprise.list.enableFailed'))
  }
}

function openHostDialog(row: PlatformEnterprise) {
  hostDialogRow.value = row
  hostValue.value = row.portal_host
  hostError.value = ''
}

function closeHostDialog() {
  hostDialogRow.value = null
  hostValue.value = ''
  hostError.value = ''
}

async function submitHost() {
  const row = hostDialogRow.value
  if (!row) return
  const next = hostValue.value.trim().toLowerCase()
  if (!HOST_PATTERN.test(next)) {
    hostError.value = t('admin.enterprise.list.hostInvalid')
    return
  }
  hostSaving.value = true
  try {
    await enterprisePlatformAPI.updateHost(row.id, next, t('admin.enterprise.list.hostReason'))
    closeHostDialog()
    appStore.showSuccess(t('admin.enterprise.list.hostSuccess'))
    await load()
  } catch {
    appStore.showError(t('admin.enterprise.list.hostFailed'))
  } finally {
    hostSaving.value = false
  }
}

onMounted(load)
</script>
