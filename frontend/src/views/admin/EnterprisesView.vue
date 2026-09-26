<template>
  <PlatformEnterpriseShell><section><div class="heading"><div><h1>企业租户</h1><p>创建和管理企业入口、专用上游用户及租户状态。</p></div><el-button type="primary" :icon="Plus" @click="router.push('/admin/enterprises/new')">开通企业</el-button></div><div class="filters"><el-input v-model="search" clearable placeholder="搜索名称或入口域名" @keyup.enter="load"/><el-select v-model="status" clearable placeholder="全部状态" @change="load"><el-option label="启用" value="active"/><el-option label="停用" value="disabled"/></el-select><el-button :icon="Search" @click="load">查询</el-button></div><div class="toolbar"><span class="count" data-testid="enterprises-count">{{loading?'加载中…':`共 ${items.length} 家企业`}}</span><el-button text :icon="RefreshRight" @click="load">刷新</el-button></div><el-alert v-if="!loading&&sourceUnavailable" title="企业列表来源不可用" description="当前未取得平台实时数据，以下不展示旧数据；请刷新后重试。" type="error" show-icon class="source-alert" data-testid="enterprises-source-unavailable"/><el-table v-else v-loading="loading" :data="items"><el-table-column prop="name" label="企业" min-width="180"/><el-table-column prop="portal_host" label="入口域名" min-width="220"/><el-table-column prop="dedicated_upstream_user_id" label="专用用户 ID" width="130"/><el-table-column prop="admin_email" label="主账号" width="180"/><el-table-column label="创建时间" width="180"><template #default="{row}">{{row.created_at}}</template></el-table-column><el-table-column label="订阅摘要" min-width="200"><template #default="{row}"><span v-if="row.subscriptions?.length">{{row.subscriptions.map(subscriptionSummary).join('；')}}</span><span v-else class="muted">无订阅</span></template></el-table-column><el-table-column prop="status" label="状态" width="100"/><el-table-column label="操作" width="250"><template #default="{row}"><el-button text @click="router.push(`/admin/enterprises/${row.id}`)">查看</el-button><el-button v-if="row.status==='active'" text type="danger" @click="disable(row)">停用</el-button><el-button v-else text type="success" data-testid="enterprise-enable" @click="enable(row)">启用</el-button><el-button text data-testid="enterprise-update-host" @click="updateHost(row)">改域名</el-button></template></el-table-column></el-table><el-empty v-if="!loading&&!sourceUnavailable&&!items.length" description="暂无企业租户"/>
  </section></PlatformEnterpriseShell>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Plus, RefreshRight, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'
import PlatformEnterpriseShell from '@/components/admin/PlatformEnterpriseShell.vue'

const router = useRouter()
const items = ref<PlatformEnterprise[]>([])
const loading = ref(false)
const search = ref('')
const status = ref('')
const sourceUnavailable = ref(false)

function subscriptionSummary(subscription: PlatformEnterprise['subscriptions'][number]) {
  return `${subscription.plan}（${subscription.status}）`
}

async function load() {
  loading.value = true
  sourceUnavailable.value = false
  try {
    items.value = await enterprisePlatformAPI.list({ search: search.value, status: status.value })
  } catch {
    items.value = []
    sourceUnavailable.value = true
    ElMessage.error('企业列表暂时不可用，当前未展示旧数据')
  } finally {
    loading.value = false
  }
}

async function disable(row: PlatformEnterprise) {
  try {
    const impact = await enterprisePlatformAPI.get(row.id)
    const subscriptionLabel = impact.subscriptions.length
      ? impact.subscriptions.map((subscription) => `${subscription.plan}（${subscription.status}，weekly 上限 ${subscription.weekly_limit || '未配置'}）`).join('；')
      : '无活动或待生效订阅'
    await ElMessageBox.confirm(
      `确认停用 ${row.name}？关联订阅：${subscriptionLabel}；员工 ${impact.employee_count} 人（在职 ${impact.active_employee_count} 人）；活动会话 ${impact.active_session_count} 个；活动 Key ${impact.active_key_count} 个。停用会拒绝新登录、撤销活动会话并禁用活动 Key，保留历史记录。`,
      '停用企业',
      { type: 'warning' },
    )
    await enterprisePlatformAPI.disable(row.id, '平台运营确认停用')
    ElMessage.success('企业已停用')
    await load()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error('企业停用失败')
  }
}

async function enable(row: PlatformEnterprise) {
  try {
    await ElMessageBox.confirm(
      `确认启用 ${row.name}？启用后恢复门户登录；停用期间被撤销的会话、已禁用的 Key 分配不会自动恢复，需管理员按需重新分配。`,
      '启用企业',
      { type: 'warning' },
    )
    await enterprisePlatformAPI.enable(row.id, '平台运营确认启用')
    ElMessage.success('企业已启用')
    await load()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error('企业启用失败')
  }
}

async function updateHost(row: PlatformEnterprise) {
  try {
    const { value } = await ElMessageBox.prompt(
      `修改 ${row.name} 的入口域名（当前 ${row.portal_host}）。修改后旧域名上的企业访问与 token 会因 Host 绑定失效，请以新域名访问门户。`,
      '修改入口域名',
      {
        inputValue: row.portal_host,
        inputPattern: /^(?=.{1,255}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/,
        inputErrorMessage: '域名格式不正确（仅允许小写字母、数字、点与连字符）',
      },
    )
    await enterprisePlatformAPI.updateHost(row.id, value.trim().toLowerCase(), '平台运营修改入口域名')
    ElMessage.success('入口域名已修改')
    await load()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error('入口域名修改失败')
  }
}

onMounted(load)
</script>
<style scoped>.heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}.heading h1{margin:0 0 6px;font-size:26px}.heading p{color:#64748b}.filters{display:flex;gap:12px;margin-bottom:12px}.filters .el-input{max-width:340px}.filters .el-select{width:160px}.toolbar{display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;color:#64748b;font-size:13px}.muted{color:#94a3b8}.source-alert{margin-bottom:12px}@media(max-width:640px){.heading,.filters{flex-direction:column}.filters .el-input,.filters .el-select{width:100%;max-width:none}}</style>
