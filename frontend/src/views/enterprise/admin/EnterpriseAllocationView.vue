<template><section class="workspace"><div class="page-heading"><div><h1>额度分配</h1><p>企业额度只按 weekly 窗口配置，allocation 是管理与计量口径。</p></div></div><div class="panel"><el-form :model="form" label-position="top"><el-form-item label="员工"><el-select v-model="form.employee_id" filterable placeholder="选择员工"><el-option v-for="employee in employees" :key="employee.id" :label="employee.email" :value="employee.id"/></el-select></el-form-item><el-form-item label="上游订阅 ID"><el-input-number v-model="form.subscription_id" :min="1"/></el-form-item><el-form-item label="weekly 窗口锚点"><el-input v-model="form.window_anchor" placeholder="YYYY-MM-DDTHH:mm:ssZ"/></el-form-item><el-form-item label="credit"><el-input v-model="form.credit"/></el-form-item><el-form-item label="reason"><el-input v-model="form.reason" maxlength="500"/></el-form-item><el-button type="primary" :loading="saving" @click="save">保存 allocation</el-button></el-form></div><div v-if="summary" class="panel"><el-descriptions :column="2" border><el-descriptions-item label="allocation">{{ summary.configured_credit }}</el-descriptions-item><el-descriptions-item label="actual cost">{{ summary.usage_credit }}</el-descriptions-item><el-descriptions-item label="remaining">{{ summary.remaining_credit }}</el-descriptions-item><el-descriptions-item label="overage">{{ summary.overage_credit }}</el-descriptions-item></el-descriptions></div></section></template>
<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseAllocationSummary, EnterpriseEmployee } from '@/types/enterprise'
import { useRoute } from 'vue-router'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'

const auth = useEnterpriseAuthStore()
const route = useRoute()
const employees = ref<EnterpriseEmployee[]>([])
const saving = ref(false)
const summary = ref<EnterpriseAllocationSummary>()
const form = reactive({ employee_id: Number(route.query.employee_id) || 0, subscription_id: 0, window_anchor: '', credit: '0', reason: '' })

async function load() {
  try {
    employees.value = await enterpriseAPI.listEmployees()
  } catch {
    ElMessage.error('员工目录暂时不可用')
  }
}

async function save() {
  if (!form.employee_id || !form.subscription_id || !form.window_anchor || !form.reason) {
    ElMessage.warning('请完整填写 weekly allocation 信息')
    return
  }
  saving.value = true
  try {
    const current = await enterpriseAPI.getAllocationSummary(form.subscription_id, form.employee_id, {
      enterprise_id: auth.principal?.enterprise_id || 0,
      window_type: 'week',
      window_anchor: form.window_anchor,
    })
    await enterpriseAPI.setAllocation(form.subscription_id, form.employee_id, {
      enterprise_id: auth.principal?.enterprise_id || 0,
      window_type: 'week',
      window_anchor: form.window_anchor,
      credit: form.credit,
      expected_version: current.allocation_version,
      reason: form.reason,
    })
    summary.value = await enterpriseAPI.getAllocationSummary(form.subscription_id, form.employee_id, {
      enterprise_id: auth.principal?.enterprise_id || 0,
      window_type: 'week',
      window_anchor: form.window_anchor,
    })
    ElMessage.success('allocation 已保存')
  } catch {
    ElMessage.error('allocation 保存失败，请核对企业和订阅来源')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
<style scoped>.workspace{max-width:760px}.page-heading{margin-bottom:20px}.page-heading h1{margin:0 0 6px;font-size:26px}.page-heading p{color:#64748b}.panel{margin-bottom:16px;padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.el-select{width:100%}</style>
