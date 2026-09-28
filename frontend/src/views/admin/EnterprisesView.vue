<template>
  <PlatformEnterpriseShell><section>
    <div class="heading"><div><h1>{{ t('admin.enterprise.list.title') }}</h1><p>{{ t('admin.enterprise.list.description') }}</p></div><el-button type="primary" :icon="Plus" @click="router.push('/admin/enterprises/new')">{{ t('admin.enterprise.list.create') }}</el-button></div>
    <div class="filters"><el-input v-model="search" clearable :placeholder="t('admin.enterprise.list.search')" @keyup.enter="load"/><el-select v-model="status" clearable :placeholder="t('admin.enterprise.list.allStatuses')" @change="load"><el-option :label="enterpriseStatusLabel('active', t)" value="active"/><el-option :label="enterpriseStatusLabel('disabled', t)" value="disabled"/></el-select><el-button :icon="Search" @click="load">{{ t('admin.enterprise.list.query') }}</el-button></div>
    <div class="toolbar"><span class="count" data-testid="enterprises-count">{{ loading ? t('admin.enterprise.list.loading') : t('admin.enterprise.list.count', { count: items.length }) }}</span><el-button text :icon="RefreshRight" @click="load">{{ t('admin.enterprise.list.refresh') }}</el-button></div>
    <el-alert v-if="!loading&&sourceUnavailable" :title="t('admin.enterprise.list.unavailableTitle')" :description="t('admin.enterprise.list.unavailableDescription')" type="error" show-icon class="source-alert" data-testid="enterprises-source-unavailable"/>
    <el-table v-else v-loading="loading" :data="items">
      <el-table-column prop="name" :label="t('admin.enterprise.common.name')" min-width="180"/>
      <el-table-column prop="portal_host" :label="t('admin.enterprise.common.portalHost')" min-width="220"/>
      <el-table-column prop="dedicated_upstream_user_id" :label="t('admin.enterprise.common.upstreamUserId')" width="130"/>
      <el-table-column prop="admin_email" :label="t('admin.enterprise.common.adminAccount')" width="180"/>
      <el-table-column :label="t('admin.enterprise.common.createdAt')" width="180"><template #default="{row}">{{ formatDate(row.created_at) }}</template></el-table-column>
      <el-table-column :label="t('admin.enterprise.list.summary')" min-width="200"><template #default="{row}"><span v-if="row.subscriptions?.length">{{ row.subscriptions.map(subscriptionSummary).join('；') }}</span><span v-else class="muted">{{ t('admin.enterprise.common.noSubscriptions') }}</span></template></el-table-column>
      <el-table-column :label="t('admin.enterprise.common.status')" width="100"><template #default="{row}">{{ enterpriseStatusLabel(row.status, t) }}</template></el-table-column>
      <el-table-column :label="t('admin.enterprise.list.operations')" width="250"><template #default="{row}"><el-button text @click="router.push(`/admin/enterprises/${row.id}`)">{{ t('admin.enterprise.common.view') }}</el-button><el-button v-if="row.status==='active'" text type="danger" @click="disable(row)">{{ t('admin.enterprise.common.disable') }}</el-button><el-button v-else text type="success" data-testid="enterprise-enable" @click="enable(row)">{{ t('admin.enterprise.common.enable') }}</el-button><el-button text data-testid="enterprise-update-host" @click="updateHost(row)">{{ t('admin.enterprise.list.changeHost') }}</el-button></template></el-table-column>
    </el-table><el-empty v-if="!loading&&!sourceUnavailable&&!items.length" :description="t('admin.enterprise.list.empty')"/>
  </section></PlatformEnterpriseShell>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Plus, RefreshRight, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'
import PlatformEnterpriseShell from '@/components/admin/PlatformEnterpriseShell.vue'
import { formatDate } from '@/utils/format'
import { enterpriseStatusLabel, subscriptionStatusLabel } from '@/utils/enterpriseDisplay'

const router = useRouter()
const { t } = useI18n({ useScope: 'global' })
const items = ref<PlatformEnterprise[]>([])
const loading = ref(false)
const search = ref('')
const status = ref('')
const sourceUnavailable = ref(false)

function subscriptionSummary(subscription: PlatformEnterprise['subscriptions'][number]) {
  return t('admin.enterprise.list.summaryValue', { plan: subscription.plan, status: subscriptionStatusLabel(subscription.status, t) })
}

async function load() {
  loading.value = true
  sourceUnavailable.value = false
  try {
    items.value = await enterprisePlatformAPI.list({ search: search.value, status: status.value })
  } catch {
    items.value = []
    sourceUnavailable.value = true
    ElMessage.error(t('admin.enterprise.list.unavailableMessage'))
  } finally {
    loading.value = false
  }
}

async function disable(row: PlatformEnterprise) {
  try {
    const impact = await enterprisePlatformAPI.get(row.id)
    const subscriptionLabel = impact.subscriptions.length
      ? impact.subscriptions.map((subscription) => t('admin.enterprise.list.disableSubscription', { plan: subscription.plan, status: subscriptionStatusLabel(subscription.status, t), limit: subscription.weekly_limit || t('admin.enterprise.common.notConfigured') })).join('；')
      : t('admin.enterprise.common.noActiveSubscriptions')
    await ElMessageBox.confirm(
      t('admin.enterprise.list.disableConfirm', { name: row.name, subscriptions: subscriptionLabel, employees: impact.employee_count, activeEmployees: impact.active_employee_count, sessions: impact.active_session_count, keys: impact.active_key_count }),
      t('admin.enterprise.list.disableTitle'),
      { type: 'warning' },
    )
    await enterprisePlatformAPI.disable(row.id, t('admin.enterprise.list.disableReason'))
    ElMessage.success(t('admin.enterprise.list.disableSuccess'))
    await load()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error(t('admin.enterprise.list.disableFailed'))
  }
}

async function enable(row: PlatformEnterprise) {
  try {
    await ElMessageBox.confirm(
      t('admin.enterprise.list.enableConfirm', { name: row.name }),
      t('admin.enterprise.list.enableTitle'),
      { type: 'warning' },
    )
    await enterprisePlatformAPI.enable(row.id, t('admin.enterprise.list.enableReason'))
    ElMessage.success(t('admin.enterprise.list.enableSuccess'))
    await load()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error(t('admin.enterprise.list.enableFailed'))
  }
}

async function updateHost(row: PlatformEnterprise) {
  try {
    const { value } = await ElMessageBox.prompt(
      t('admin.enterprise.list.hostConfirm', { name: row.name, host: row.portal_host }),
      t('admin.enterprise.list.hostTitle'),
      {
        inputValue: row.portal_host,
        inputPattern: /^(?=.{1,255}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/,
        inputErrorMessage: t('admin.enterprise.list.hostInvalid'),
      },
    )
    await enterprisePlatformAPI.updateHost(row.id, value.trim().toLowerCase(), t('admin.enterprise.list.hostReason'))
    ElMessage.success(t('admin.enterprise.list.hostSuccess'))
    await load()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error(t('admin.enterprise.list.hostFailed'))
  }
}

onMounted(load)
</script>
<style scoped>.heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}.heading h1{margin:0 0 6px;font-size:26px}.heading p{color:#64748b}.filters{display:flex;gap:12px;margin-bottom:12px}.filters .el-input{max-width:340px}.filters .el-select{width:160px}.toolbar{display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;color:#64748b;font-size:13px}.muted{color:#94a3b8}.source-alert{margin-bottom:12px}@media(max-width:640px){.heading,.filters{flex-direction:column}.filters .el-input,.filters .el-select{width:100%;max-width:none}}</style>
