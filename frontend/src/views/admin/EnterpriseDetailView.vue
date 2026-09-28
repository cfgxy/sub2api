<template><PlatformEnterpriseShell><section>
  <div class="heading"><div><span class="eyebrow">{{ t('admin.enterprise.detail.breadcrumb') }}</span><h1>{{ item?.name || t('admin.enterprise.detail.title') }}</h1><p>{{ t('admin.enterprise.detail.description') }}</p></div><div class="actions"><el-button @click="router.back()">{{ t('admin.enterprise.common.back') }}</el-button><el-button v-if="item?.status==='active'" type="danger" @click="disable">{{ t('admin.enterprise.common.disable') }}</el-button></div></div>
  <el-skeleton v-if="loading" :rows="5" animated/>
  <el-alert v-else-if="sourceUnavailable" :title="t('admin.enterprise.detail.unavailableTitle')" :description="t('admin.enterprise.detail.unavailableDescription')" type="error" show-icon/>
  <template v-else-if="item"><el-tabs v-model="activeTab">
    <el-tab-pane :label="t('admin.enterprise.detail.overview')" name="overview"><el-descriptions :column="2" border>
      <el-descriptions-item :label="t('admin.enterprise.common.name')">{{ item.name }}</el-descriptions-item>
      <el-descriptions-item :label="t('admin.enterprise.common.status')">{{ enterpriseStatusLabel(item.status, t) }}</el-descriptions-item>
      <el-descriptions-item :label="t('admin.enterprise.common.portalHost')">{{ item.portal_host }}</el-descriptions-item>
      <el-descriptions-item :label="t('admin.enterprise.common.createdAt')">{{ formatDate(item.created_at) }}</el-descriptions-item>
    </el-descriptions><div class="impact"><h2>{{ t('admin.enterprise.detail.impact') }}</h2><el-descriptions :column="2" border>
      <el-descriptions-item :label="t('admin.enterprise.common.employeeCount')">{{ item.employee_count }}</el-descriptions-item>
      <el-descriptions-item :label="t('admin.enterprise.common.activeEmployees')">{{ item.active_employee_count }}</el-descriptions-item>
      <el-descriptions-item :label="t('admin.enterprise.common.activeSessions')">{{ item.active_session_count }}</el-descriptions-item>
      <el-descriptions-item :label="t('admin.enterprise.common.activeKeys')">{{ item.active_key_count }}</el-descriptions-item>
      <el-descriptions-item :label="t('admin.enterprise.common.subscriptions')" :span="2"><div v-if="item.subscriptions.length" class="subscriptions"><div v-for="subscription in item.subscriptions" :key="subscription.id">{{ subscriptionLabel(subscription) }}</div></div><span v-else>{{ t('admin.enterprise.common.noActiveSubscriptions') }}</span></el-descriptions-item>
    </el-descriptions></div></el-tab-pane>
    <el-tab-pane :label="t('admin.enterprise.detail.access')" name="access"><el-descriptions :column="1" border>
      <el-descriptions-item :label="t('admin.enterprise.detail.adminEmail')">{{ item.admin_email || t('admin.enterprise.common.notConfigured') }}</el-descriptions-item>
      <el-descriptions-item :label="t('admin.enterprise.common.upstreamUserId')">{{ item.dedicated_upstream_user_id }}</el-descriptions-item>
      <el-descriptions-item :label="t('admin.enterprise.common.activeSessions')">{{ item.active_session_count }}</el-descriptions-item>
      <el-descriptions-item :label="t('admin.enterprise.common.activeKeys')">{{ item.active_key_count }}</el-descriptions-item>
    </el-descriptions></el-tab-pane>
  </el-tabs></template>
</section></PlatformEnterpriseShell></template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'
import PlatformEnterpriseShell from '@/components/admin/PlatformEnterpriseShell.vue'
import { formatDate } from '@/utils/format'
import { enterpriseStatusLabel, subscriptionStatusLabel } from '@/utils/enterpriseDisplay'

const route = useRoute()
const router = useRouter()
const { t } = useI18n({ useScope: 'global' })
const item = ref<PlatformEnterprise>()
const loading = ref(false)
const sourceUnavailable = ref(false)
const activeTab = ref('overview')
function subscriptionLabel(subscription: PlatformEnterprise['subscriptions'][number]) {
  return t('admin.enterprise.detail.subscriptionValue', { plan: subscription.plan, status: subscriptionStatusLabel(subscription.status, t), limit: subscription.weekly_limit || t('admin.enterprise.common.notConfigured'), expiresAt: formatDate(subscription.expires_at) })
}

async function load() {
	loading.value = true
	item.value = undefined
	sourceUnavailable.value = false
	try {
		item.value = await enterprisePlatformAPI.get(Number(route.params.id))
	} catch {
		sourceUnavailable.value = true
			ElMessage.error(t('admin.enterprise.detail.unavailableMessage'))
	} finally {
		loading.value = false
	}
}

async function disable() {
	let current: PlatformEnterprise
	try {
		current = await enterprisePlatformAPI.get(Number(route.params.id))
	} catch {
		item.value = undefined
		sourceUnavailable.value = true
			ElMessage.error(t('admin.enterprise.detail.unavailableMessage'))
		return
	}
	item.value = current
	if (current.status !== 'active') return
	const subscriptionLabelText = current.subscriptions.length
		? current.subscriptions.map(subscriptionLabel).join('；')
			: t('admin.enterprise.common.noActiveSubscriptions')
	try {
		await ElMessageBox.confirm(
				t('admin.enterprise.list.disableConfirm', { name: current.name, subscriptions: subscriptionLabelText, employees: current.employee_count, activeEmployees: current.active_employee_count, sessions: current.active_session_count, keys: current.active_key_count }),
				t('admin.enterprise.list.disableTitle'),
			{ type: 'warning' },
		)
			await enterprisePlatformAPI.disable(current.id, t('admin.enterprise.list.disableReason'))
			ElMessage.success(t('admin.enterprise.list.disableSuccess'))
		await load()
	} catch (error) {
			if (error !== 'cancel') ElMessage.error(t('admin.enterprise.list.disableFailed'))
	}
}

onMounted(load)
</script>
<style scoped>.heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}.heading .eyebrow{display:block;margin-bottom:4px;color:#94a3b8;font-size:12px}.heading h1{margin:0 0 6px;font-size:26px}.heading p{color:#64748b}.actions{display:flex;gap:12px}.impact{margin-top:20px}.impact h2{margin:0 0 12px;font-size:18px}.subscriptions{display:grid;gap:6px}@media(max-width:640px){.heading{align-items:flex-start;flex-direction:column}.actions{width:100%}}</style>
