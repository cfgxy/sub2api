<template>
  <section><div class="heading"><div><h1>企业租户</h1><p>创建和管理企业入口、专用上游用户及租户状态。</p></div><el-button type="primary" :icon="Plus" @click="dialog=true">开通企业</el-button></div><div class="filters"><el-input v-model="search" clearable placeholder="搜索名称或入口域名" @keyup.enter="load"/><el-select v-model="status" clearable placeholder="全部状态" @change="load"><el-option label="启用" value="active"/><el-option label="停用" value="disabled"/></el-select><el-button :icon="Search" @click="load">查询</el-button></div><el-table v-loading="loading" :data="items"><el-table-column prop="name" label="企业" min-width="180"/><el-table-column prop="portal_host" label="入口域名" min-width="220"/><el-table-column prop="dedicated_upstream_user_id" label="专用用户 ID" width="130"/><el-table-column prop="status" label="状态" width="100"/><el-table-column label="操作" width="180"><template #default="{row}"><el-button text @click="router.push(`/admin/enterprises/${row.id}`)">查看</el-button><el-button v-if="row.status==='active'" text type="danger" @click="disable(row)">停用</el-button></template></el-table-column></el-table><el-empty v-if="!loading&&!items.length" description="暂无企业租户"/>
    <el-dialog v-model="dialog" title="开通企业" width="min(92vw, 520px)"><el-form :model="form" label-position="top"><el-form-item label="企业名称"><el-input v-model="form.name"/></el-form-item><el-form-item label="企业入口域名"><el-input v-model="form.portal_host" placeholder="enterprise.example.com"/></el-form-item><el-form-item label="专用上游用户 ID"><el-input-number v-model="form.dedicated_upstream_user_id" :min="1"/></el-form-item><el-form-item label="开通原因"><el-input v-model="form.reason" maxlength="500"/></el-form-item></el-form><template #footer><el-button @click="dialog=false">取消</el-button><el-button type="primary" :loading="saving" @click="create">开通</el-button></template></el-dialog>
  </section>
</template>
<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Plus, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'

const router = useRouter()
const items = ref<PlatformEnterprise[]>([])
const loading = ref(false)
const saving = ref(false)
const search = ref('')
const status = ref('')
const dialog = ref(false)
const form = reactive({ name: '', portal_host: '', dedicated_upstream_user_id: 0, reason: '' })

async function load() {
  loading.value = true
  try {
    items.value = await enterprisePlatformAPI.list({ search: search.value, status: status.value })
  } catch {
    ElMessage.error('企业列表暂时不可用')
  } finally {
    loading.value = false
  }
}

async function create() {
  if (!form.name || !form.portal_host || !form.dedicated_upstream_user_id || !form.reason) {
    ElMessage.warning('请完整填写开通信息')
    return
  }
  saving.value = true
  try {
    await enterprisePlatformAPI.create(form)
    dialog.value = false
    Object.assign(form, { name: '', portal_host: '', dedicated_upstream_user_id: 0, reason: '' })
    ElMessage.success('企业已开通')
    await load()
  } catch {
    ElMessage.error('企业开通失败，请核对唯一性约束')
  } finally {
    saving.value = false
  }
}

async function disable(row: PlatformEnterprise) {
  try {
    await ElMessageBox.confirm(`确认停用 ${row.name}？停用会拒绝新登录并保留历史记录。`, '停用企业', { type: 'warning' })
    await enterprisePlatformAPI.disable(row.id, '平台运营确认停用')
    ElMessage.success('企业已停用')
    await load()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error('企业停用失败')
  }
}

onMounted(load)
</script>
<style scoped>.heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}.heading h1{margin:0 0 6px;font-size:26px}.heading p{color:#64748b}.filters{display:flex;gap:12px;margin-bottom:18px}.filters .el-input{max-width:340px}.filters .el-select{width:160px}@media(max-width:640px){.heading,.filters{flex-direction:column}.filters .el-input,.filters .el-select{width:100%;max-width:none}}</style>
