<template><PlatformEnterpriseShell><section><div class="heading"><div><h1>企业详情</h1><p>查看企业入口、订阅和停用影响范围。</p></div><div class="actions"><el-button @click="router.back()">返回</el-button><el-button v-if="item?.status==='active'" type="danger" @click="disable">停用</el-button></div></div><el-skeleton v-if="loading" :rows="5" animated/><el-alert v-else-if="sourceUnavailable" title="企业详情来源不可用" description="当前未取得平台实时数据，停用操作已禁用，请刷新后重试。" type="error" show-icon/><template v-else-if="item"><el-descriptions :column="1" border><el-descriptions-item label="企业名称">{{item.name}}</el-descriptions-item><el-descriptions-item label="入口域名">{{item.portal_host}}</el-descriptions-item><el-descriptions-item label="管理员邮箱">{{item.admin_email || '未配置'}}</el-descriptions-item><el-descriptions-item label="专用上游用户 ID">{{item.dedicated_upstream_user_id}}</el-descriptions-item><el-descriptions-item label="状态">{{item.status}}</el-descriptions-item><el-descriptions-item label="创建时间">{{item.created_at}}</el-descriptions-item></el-descriptions><div class="impact"><h2>停用影响范围</h2><el-descriptions :column="2" border><el-descriptions-item label="员工总数">{{item.employee_count}}</el-descriptions-item><el-descriptions-item label="在职员工">{{item.active_employee_count}}</el-descriptions-item><el-descriptions-item label="活动会话">{{item.active_session_count}}</el-descriptions-item><el-descriptions-item label="活动 Key">{{item.active_key_count}}</el-descriptions-item><el-descriptions-item label="关联订阅" :span="2"><div v-if="item.subscriptions.length" class="subscriptions"><div v-for="subscription in item.subscriptions" :key="subscription.id">{{subscriptionLabel(subscription)}}</div></div><span v-else>无活动或待生效订阅</span></el-descriptions-item></el-descriptions></div></template></section></PlatformEnterpriseShell></template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'
import PlatformEnterpriseShell from '@/components/admin/PlatformEnterpriseShell.vue'

const route = useRoute()
const router = useRouter()
const item = ref<PlatformEnterprise>()
const loading = ref(false)
const sourceUnavailable = ref(false)
function subscriptionLabel(subscription: PlatformEnterprise['subscriptions'][number]) {
  return `${subscription.plan}（${subscription.status}，weekly 上限 ${subscription.weekly_limit || '未配置'}，有效期至 ${subscription.expires_at}）`
}

async function load() {
	loading.value = true
	item.value = undefined
	sourceUnavailable.value = false
	try {
		item.value = await enterprisePlatformAPI.get(Number(route.params.id))
	} catch {
		sourceUnavailable.value = true
		ElMessage.error('企业详情暂时不可用')
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
		ElMessage.error('企业详情暂时不可用')
		return
	}
	item.value = current
	if (current.status !== 'active') return
	const subscriptionLabelText = current.subscriptions.length
		? current.subscriptions.map(subscriptionLabel).join('；')
		: '无活动或待生效订阅'
	try {
		await ElMessageBox.confirm(
			`确认停用 ${current.name}？关联订阅：${subscriptionLabelText}；员工 ${current.employee_count} 人（在职 ${current.active_employee_count} 人）；活动会话 ${current.active_session_count} 个；活动 Key ${current.active_key_count} 个。停用会拒绝新登录、撤销活动会话并禁用活动 Key，保留历史记录。`,
			'停用企业',
			{ type: 'warning' },
		)
		await enterprisePlatformAPI.disable(current.id, '平台运营确认停用')
		ElMessage.success('企业已停用')
		await load()
	} catch (error) {
		if (error !== 'cancel') ElMessage.error('企业停用失败')
	}
}

onMounted(load)
</script>
<style scoped>.heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}.heading h1{margin:0 0 6px;font-size:26px}.heading p{color:#64748b}.actions{display:flex;gap:12px}.impact{margin-top:20px}.impact h2{margin:0 0 12px;font-size:18px}.subscriptions{display:grid;gap:6px}@media(max-width:640px){.heading{align-items:flex-start;flex-direction:column}.actions{width:100%}}</style>
