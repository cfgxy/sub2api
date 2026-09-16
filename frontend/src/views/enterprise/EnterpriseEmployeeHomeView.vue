<template>
  <section class="workspace">
    <div class="page-heading"><div><h1>个人概览</h1><p>查看本人 weekly allocation、企业总池状态和近期调用趋势。</p></div><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></div>
    <el-alert v-if="usage?.source_status === 'unavailable'" type="warning" :closable="false" title="当前订阅来源不可用，页面未使用缓存值填充。" />
    <div class="metrics" v-if="usage">
      <article><span>allocation</span><strong>{{ usage.allocation }}</strong><small>当前 weekly 窗口</small></article>
      <article><span>actual cost</span><strong>{{ usage.actual_cost }}</strong><small>{{ usage.requests }} 次请求</small></article>
      <article><span>remaining</span><strong>{{ usage.remaining }}</strong><small>不会显示为负数</small></article>
      <article :class="{ danger: usage.overage !== '0' && usage.overage !== '0.00000000' }"><span>overage</span><strong>{{ usage.overage }}</strong><small>个人超用不改变企业总池判断</small></article>
    </div>

    <div class="panel">
      <h2>企业总池</h2>
      <p v-if="pool?.source_status === 'unavailable'" class="pool-note">企业总池来源暂不可用，未显示伪造余额。</p>
      <template v-else-if="pool">
        <p class="pool-note" :class="{ danger: pool.pool_exhausted }">{{ pool.pool_exhausted ? '企业总池已耗尽：与个人 overage 分开处理，请联系企业管理员' : '企业总池可用' }}</p>
        <div class="pool-figures">
          <span>总池上限 {{ pool.pool_limit }}</span>
          <span>已用 {{ pool.pool_used }}</span>
          <span :class="{ danger: pool.pool_exhausted }">剩余 {{ pool.pool_remaining }}</span>
        </div>
      </template>
    </div>

    <div class="panel">
      <div class="panel-heading"><h2>近 7 天调用趋势</h2><span>真实每日聚合，无记录不补零</span></div>
      <div v-if="!loading && !recentTrend.length" class="empty-chart">近 7 天暂无调用记录</div>
      <div v-else class="trend-chart" aria-label="近期调用趋势图">
        <div v-for="point in recentTrend" :key="point.at" class="trend-column">
          <span class="trend-bar" :style="{ height: `${trendHeight(point.requests)}%` }" :title="`${formatDate(point.at)}：${point.requests} 次`" />
          <small>{{ trendLabel(point.at) }}</small>
        </div>
      </div>
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
import type { EnterpriseEmployeeHome, EnterpriseEmployeeUsageSummary, EnterpriseEnterprisePoolStatus, EnterpriseEmployeeUsageTrendPoint } from '@/types/enterprise'

const loading = ref(false)
const home = ref<EnterpriseEmployeeHome>()
const usage = ref<EnterpriseEmployeeUsageSummary>()
const pool = ref<EnterpriseEnterprisePoolStatus>()
const recentTrend = ref<EnterpriseEmployeeUsageTrendPoint[]>([])
const statusLabel = (status: string) => ({ active: '有效', disabled: '已停用', quota_exhausted: '额度用尽', expired: '已过期' }[status] || status)
const formatDate = (value: string | Date) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
const trendLabel = (value: string) => new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric' }).format(new Date(value))
const trendHeight = (value: number) => Math.max(8, Math.round((value / Math.max(...recentTrend.value.map((point) => point.requests), 1)) * 100))
async function load() {
  loading.value = true
  try {
    home.value = await enterpriseAPI.getEmployeeHome()
    usage.value = home.value.usage
    pool.value = home.value.enterprise_pool
    recentTrend.value = home.value.recent_trend
  } catch {
    ElMessage.error('个人概览暂时不可用')
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>

<style scoped>
.workspace{max-width:1100px}
.page-heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}
.page-heading h1{margin:0 0 6px;font-size:26px}
.page-heading p,.metrics small,.panel p{color:#64748b}
.metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px;margin:20px 0}
.metrics article,.panel{padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}
.panel{margin-top:16px}
.metrics span,.metrics small{display:block;font-size:12px}
.metrics strong{display:block;margin:10px 0 6px;font-size:24px}
.danger,.danger strong{color:#b91c1c!important}
.panel h2{margin:0 0 10px;font-size:15px}
.panel-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:14px}
.panel-heading h2{margin:0}
.panel-heading span{color:#6b7280;font-size:11px}
.panel a{color:#2563eb;text-decoration:none}
.pool-note{margin:0 0 10px;font-size:13px}
.pool-figures{display:flex;gap:18px;flex-wrap:wrap;font-size:13px;color:#374151}
.trend-chart{display:flex;align-items:flex-end;gap:16px;height:160px;padding:16px 12px 26px;border-left:1px solid #d1d5db;border-bottom:1px solid #d1d5db;background:repeating-linear-gradient(to bottom,transparent 0,transparent 32px,#f0f1f3 33px)}
.trend-column{display:flex;flex:1;min-width:18px;height:100%;flex-direction:column;align-items:center;justify-content:flex-end;gap:8px}
.trend-bar{width:min(36px,100%);min-height:8%;border-radius:4px 4px 0 0;background:#2563eb}
.trend-column small{color:#6b7280;font-size:10px;white-space:nowrap}
.empty-chart{display:grid;place-items:center;height:120px;color:#6b7280}
@media(max-width:760px){.metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:480px){.page-heading{flex-direction:column}.metrics{grid-template-columns:1fr}}
</style>
