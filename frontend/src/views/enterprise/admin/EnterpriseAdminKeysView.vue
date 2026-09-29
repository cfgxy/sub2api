<template>
  <TablePageLayout>
    <template #actions>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 class="text-xl font-bold text-gray-900 dark:text-white">员工 Key</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">查看员工 Key 的归属与状态，可撤销已启用的 Key。</p>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">
          <Icon name="refresh" size="md" class="mr-2" :class="{ 'animate-spin': loading }" />刷新
        </button>
      </div>
    </template>

    <template #table>
      <div v-if="loadError" class="p-6" data-testid="admin-keys-load-error">
        <EmptyState icon="exclamationTriangle" title="员工 Key 信息暂时不可用" action-text="重新加载" @action="load" />
      </div>
      <DataTable v-else :columns="columns" :data="keys" :loading="loading" row-key="api_key_id" :actions-count="1">
        <template #cell-employee_email="{ value }"><span class="break-all">{{ value }}</span></template>
        <template #cell-status="{ value }"><StatusBadge :status="value" :label="keyStatusLabel(value, t)" /></template>
        <template #cell-updated_at="{ value }">{{ formatDate(value) }}</template>
        <template #cell-actions="{ row }">
          <button
            v-if="row.status === 'active'" type="button" class="btn btn-ghost btn-sm text-red-600 dark:text-red-400"
            :disabled="revokingId !== null" data-testid="admin-key-revoke" @click="confirmRevoke(row.api_key_id)"
          >{{ revokingId === row.api_key_id ? '处理中…' : '撤销' }}</button>
        </template>
        <template #empty><EmptyState icon="key" title="暂无员工 Key" /></template>
      </DataTable>
    </template>
  </TablePageLayout>
  <ConfirmDialog
    :show="confirmId !== null" title="撤销员工 Key" message="撤销后该 Key 将立即无法调用，且不会恢复。" danger
    @confirm="revoke" @cancel="confirmId = null"
  />
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { enterpriseAPI } from '@/api/enterprise'
import { keyStatusLabel } from '@/utils/enterpriseDisplay'
import type { Column } from '@/components/common/types'
import type { EnterpriseKeySummary } from '@/types/enterprise'

const { t } = useI18n()
const appStore = useAppStore()
const columns: Column[] = [
  { key: 'employee_email', label: '员工邮箱' }, { key: 'api_key_id', label: 'Key ID' },
  { key: 'generation', label: '代次' }, { key: 'status', label: '状态' },
  { key: 'updated_at', label: '更新时间' }, { key: 'actions', label: '操作' }
]
const keys = ref<EnterpriseKeySummary[]>([])
const loading = ref(false)
const loadError = ref(false)
const revokingId = ref<number | null>(null)
const confirmId = ref<number | null>(null)
const formatDate = (value: string) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))

async function load() {
  loading.value = true
  loadError.value = false
  keys.value = []
  try {
    keys.value = await enterpriseAPI.listAdminKeys({ suppressUnavailableRedirect: true })
  } catch {
    loadError.value = true
    appStore.showError('员工 Key 暂时不可用')
  } finally {
    loading.value = false
  }
}

function confirmRevoke(id: number) {
  if (revokingId.value !== null) return
  confirmId.value = id
}

async function revoke() {
  const id = confirmId.value
  confirmId.value = null
  if (id === null || revokingId.value !== null) return
  revokingId.value = id
  try {
    await enterpriseAPI.revokeAdminKey(id, globalThis.crypto?.randomUUID?.() || `${Date.now()}-${id}`)
    appStore.showSuccess('Key 已撤销')
    await load()
  } catch {
    appStore.showError('Key 撤销失败')
  } finally {
    revokingId.value = null
  }
}

onMounted(load)
</script>
