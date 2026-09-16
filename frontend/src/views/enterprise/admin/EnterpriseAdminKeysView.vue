<template><section class="workspace"><div class="page-heading"><div><h1>员工 Key</h1><p>管理员只能查看掩码、归属和状态，不能读取明文。</p></div><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></div><el-table v-loading="loading" :data="keys" stripe><el-table-column prop="employee_email" label="员工" min-width="220"/><el-table-column prop="api_key_id" label="Key ID" width="100"/><el-table-column prop="generation" label="generation" width="110"/><el-table-column prop="status" label="状态" width="100"/><el-table-column prop="updated_at" label="更新时间" width="190"/><el-table-column label="操作" width="120" fixed="right"><template #default="{row}"><el-button v-if="row.status==='active'" text type="danger" @click="revoke(row.api_key_id)">撤销</el-button></template></el-table-column></el-table><el-empty v-if="!loading&&!keys.length" description="暂无员工 Key"/></section></template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseKeySummary } from '@/types/enterprise'

const keys = ref<EnterpriseKeySummary[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    keys.value = await enterpriseAPI.listAdminKeys()
  } catch {
    ElMessage.error('员工 Key 暂时不可用')
  } finally {
    loading.value = false
  }
}

async function revoke(id: number) {
  try {
    await ElMessageBox.confirm('撤销后该 Key 将立即无法调用，且不会恢复。', '撤销员工 Key', { type: 'warning' })
    await enterpriseAPI.revokeAdminKey(id, globalThis.crypto?.randomUUID?.() || `${Date.now()}-${id}`)
    ElMessage.success('Key 已撤销')
    await load()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error('Key 撤销失败')
  }
}

onMounted(load)
</script>
<style scoped>.workspace{max-width:1100px}.page-heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}.page-heading h1{margin:0 0 6px;font-size:26px}.page-heading p{color:#64748b}.el-table{min-width:760px}@media(max-width:640px){.page-heading{flex-direction:column}}</style>
