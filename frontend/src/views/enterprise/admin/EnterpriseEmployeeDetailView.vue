<template><section class="workspace"><div class="page-heading"><div><h1>员工详情</h1><p>查看员工状态与组织归属，额度和访问治理从此处进入。</p></div><el-button @click="router.back()">返回</el-button></div><el-skeleton v-if="loading" :rows="5" animated/><div v-else-if="employee" class="panel"><el-descriptions :column="1" border><el-descriptions-item label="邮箱">{{ employee.email }}</el-descriptions-item><el-descriptions-item label="状态">{{ employee.status }}</el-descriptions-item><el-descriptions-item label="部门">{{ employee.department_id || '未分配' }}</el-descriptions-item><el-descriptions-item label="首次改密">{{ employee.must_change_password ? '待完成' : '已完成' }}</el-descriptions-item></el-descriptions><div class="actions"><el-button type="primary" @click="router.push({path:'/enterprise/admin/allocation',query:{employee_id:String(employee.id)}})">配置 allocation</el-button><el-button @click="router.push('/enterprise/admin/keys')">查看员工 Key</el-button></div></div></section></template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseEmployee } from '@/types/enterprise'

const route = useRoute()
const router = useRouter()
const employee = ref<EnterpriseEmployee>()
const loading = ref(false)

onMounted(async () => {
  loading.value = true
  try {
    employee.value = await enterpriseAPI.getEmployee(Number(route.params.id))
  } catch {
    ElMessage.error('员工详情暂时不可用')
  } finally {
    loading.value = false
  }
})
</script>
<style scoped>.workspace{max-width:900px}.page-heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}.page-heading h1{margin:0 0 6px;font-size:26px}.page-heading p{color:#64748b}.panel{padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.actions{display:flex;gap:10px;margin-top:18px}@media(max-width:640px){.page-heading{flex-direction:column}.actions{flex-direction:column}}</style>
