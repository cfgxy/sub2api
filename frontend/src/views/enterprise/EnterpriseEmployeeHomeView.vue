<template>
  <section class="workspace">
    <div class="page-heading"><div><h1>个人概览</h1><p>查看本人 weekly allocation、实际用量和访问状态。</p></div><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></div>
    <el-alert v-if="usage?.source_status === 'unavailable'" type="warning" :closable="false" title="当前订阅来源不可用，页面未使用缓存值填充。" />
    <div class="metrics" v-if="usage">
      <article><span>allocation</span><strong>{{ usage.allocation }}</strong><small>当前 weekly 窗口</small></article>
      <article><span>actual cost</span><strong>{{ usage.actual_cost }}</strong><small>{{ usage.requests }} 次请求</small></article>
      <article><span>remaining</span><strong>{{ usage.remaining }}</strong><small>不会显示为负数</small></article>
      <article :class="{ danger: usage.overage !== '0' && usage.overage !== '0.00000000' }"><span>overage</span><strong>{{ usage.overage }}</strong><small>个人超用不改变企业总池判断</small></article>
    </div>
    <div class="panel"><h2>访问状态</h2><p>{{ home?.key ? `当前 Key 状态：${statusLabel(home.key.status)}` : '尚未创建当前 API Key' }}</p><RouterLink to="/enterprise/keys">管理 API Key</RouterLink></div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { RouterLink } from 'vue-router'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseEmployeeHome, EnterpriseEmployeeUsageSummary } from '@/types/enterprise'

const loading = ref(false)
const home = ref<EnterpriseEmployeeHome>()
const usage = ref<EnterpriseEmployeeUsageSummary>()
const statusLabel = (status: string) => ({ active: '有效', disabled: '已停用', quota_exhausted: '额度用尽', expired: '已过期' }[status] || status)
async function load() { loading.value = true; try { home.value = await enterpriseAPI.getEmployeeHome(); usage.value = home.value.usage } catch { ElMessage.error('个人概览暂时不可用') } finally { loading.value = false } }
onMounted(load)
</script>

<style scoped>
.workspace{max-width:1100px}.page-heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}.page-heading h1{margin:0 0 6px;font-size:26px}.page-heading p,.metrics small,.panel p{color:#64748b}.metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px;margin:20px 0}.metrics article,.panel{padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.metrics span,.metrics small{display:block;font-size:12px}.metrics strong{display:block;margin:10px 0 6px;font-size:24px}.danger strong{color:#b91c1c}.panel h2{margin:0;font-size:15px}.panel a{color:#2563eb;text-decoration:none}@media(max-width:760px){.metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:480px){.page-heading{flex-direction:column}.metrics{grid-template-columns:1fr}}
</style>
