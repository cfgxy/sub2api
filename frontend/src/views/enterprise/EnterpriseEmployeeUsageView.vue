<template>
  <section class="workspace">
    <div class="page-heading"><div><h1>个人用量</h1><p>只展示当前员工的 weekly 归因结果。</p></div><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></div>
    <el-alert v-if="usage?.source_status === 'unavailable'" type="warning" :closable="false" title="用量来源暂不可用" description="当前没有可核验的订阅窗口，未显示伪造的实时余额。" />
    <el-empty v-else-if="!loading && usage?.requests === 0" description="当前 weekly 窗口暂无调用记录" />
    <div v-if="usage" class="panel"><el-descriptions :column="2" border><el-descriptions-item label="窗口">weekly</el-descriptions-item><el-descriptions-item label="窗口锚点">{{ usage.window_anchor || '未获取' }}</el-descriptions-item><el-descriptions-item label="allocation">{{ usage.allocation }}</el-descriptions-item><el-descriptions-item label="actual cost">{{ usage.actual_cost }}</el-descriptions-item><el-descriptions-item label="remaining">{{ usage.remaining }}</el-descriptions-item><el-descriptions-item label="overage">{{ usage.overage }}</el-descriptions-item><el-descriptions-item label="请求数">{{ usage.requests }}</el-descriptions-item></el-descriptions></div>
    <div class="panel"><h2>调用明细</h2><el-table v-loading="loading" :data="records" stripe><el-table-column prop="request_at" label="请求时间"/><el-table-column prop="api_key_masked" label="Key"/><el-table-column prop="actual_cost" label="actual cost"/></el-table><el-empty v-if="!loading&&!records.length" description="当前 weekly 窗口暂无调用明细" /></div>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseEmployeeUsageRecord, EnterpriseEmployeeUsageSummary } from '@/types/enterprise'
const loading = ref(false), usage = ref<EnterpriseEmployeeUsageSummary>(), records = ref<EnterpriseEmployeeUsageRecord[]>([])
async function load(){loading.value=true;try{[usage.value,records.value]=await Promise.all([enterpriseAPI.getEmployeeUsage(),enterpriseAPI.listEmployeeUsage()])}catch{ElMessage.error('个人用量暂时不可用')}finally{loading.value=false}}
onMounted(load)
</script>
<style scoped>.workspace{max-width:1100px}.page-heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}.page-heading h1{margin:0 0 6px;font-size:26px}.page-heading p{color:#64748b}.panel{margin-top:20px;padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}@media(max-width:640px){.page-heading{flex-direction:column}}</style>
