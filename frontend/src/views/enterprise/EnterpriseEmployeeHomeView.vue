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
        <div class="pool-grid">
          <div class="ring" :style="{ background: `conic-gradient(#008f86 0 ${poolUsedPercentage}%, #e4e7ec ${poolUsedPercentage}%)` }"><span>{{ poolUsedPercentage }}%</span></div>
          <div class="pool-text">
            <strong>{{ pool.pool_used }} / {{ pool.pool_limit }}</strong>
            <p class="pool-note" :class="{ danger: pool.pool_exhausted }">{{ pool.pool_exhausted ? '企业总池已耗尽：与个人 overage 分开处理，请联系企业管理员' : `企业总池可用，本周期已使用 ${poolUsedPercentage}%，由企业总池统一承载` }}</p>
            <small :class="{ danger: pool.pool_exhausted }">企业池剩余 {{ pool.pool_remaining }}</small>
          </div>
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

    <div class="split-panels">
      <div class="panel table-panel">
        <div class="panel-heading"><h2>最近调用</h2><RouterLink to="/enterprise/usage" class="panel-link">查看全部用量</RouterLink></div>
        <div v-if="recentCallsState === 'unavailable'" class="empty-chart">调用明细来源暂时不可用</div>
        <div v-else-if="!recentCalls.length" class="empty-chart">暂无调用记录</div>
        <table v-else class="calls-table">
          <thead><tr><th>时间</th><th>Key</th><th>消耗额度</th></tr></thead>
          <tbody>
            <tr v-for="record in recentCalls" :key="`${record.request_at}-${record.api_key_masked}-${record.generation}`">
              <td>{{ formatDate(record.request_at) }}</td>
              <td>{{ record.api_key_masked }}</td>
              <td>{{ record.actual_cost }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="panel">
        <div class="panel-heading"><h2>我的用量提醒</h2><span class="chip" :class="reminderTone">{{ reminderLabel }}</span></div>
        <template v-if="usage">
          <b class="reminder-headline">个人 allocation 使用 {{ usagePercentage }}%</b>
          <p class="reminder-desc">尚余 {{ usage.remaining }} 次调用。{{ usage.overage !== '0' ? `当前个人超用 ${usage.overage}` : '当前无个人超用记录' }}</p>
          <div class="progress"><i :style="{ width: `${usagePercentage}%` }" /></div>
          <div class="scale"><span>0</span><span>{{ usage.allocation }} allocation</span></div>
        </template>
        <p v-else class="pool-note">个人用量数据暂不可用</p>
      </div>
    </div>

    <div class="panel"><h2>访问状态</h2><p>{{ home?.key ? `当前 Key 状态：${statusLabel(home.key.status)}` : '尚未创建当前 API Key' }}</p><RouterLink to="/enterprise/keys">管理 API Key</RouterLink></div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { RouterLink } from 'vue-router'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseEmployeeHome, EnterpriseEmployeeUsageSummary, EnterpriseEnterprisePoolStatus, EnterpriseEmployeeUsageTrendPoint, EnterpriseEmployeeUsageRecord } from '@/types/enterprise'

type SourceState = 'loading' | 'ready' | 'unavailable'

const loading = ref(false)
const home = ref<EnterpriseEmployeeHome>()
const usage = ref<EnterpriseEmployeeUsageSummary>()
const pool = ref<EnterpriseEnterprisePoolStatus>()
const recentTrend = ref<EnterpriseEmployeeUsageTrendPoint[]>([])
const recentCalls = ref<EnterpriseEmployeeUsageRecord[]>([])
const recentCallsState = ref<SourceState>('loading')
const statusLabel = (status: string) => ({ active: '有效', disabled: '已停用', quota_exhausted: '额度用尽', expired: '已过期' }[status] || status)
const formatDate = (value: string | Date) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
const trendLabel = (value: string) => new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric' }).format(new Date(value))
const trendHeight = (value: number) => Math.max(8, Math.round((value / Math.max(...recentTrend.value.map((point) => point.requests), 1)) * 100))

const poolUsedPercentage = computed(() => {
  if (!pool.value) return 0
  const limit = Number(pool.value.pool_limit)
  const used = Number(pool.value.pool_used)
  if (!limit) return 0
  return Math.min(100, Math.round((used / limit) * 100))
})
const usagePercentage = computed(() => {
  if (!usage.value) return 0
  const allocation = Number(usage.value.allocation)
  const cost = Number(usage.value.actual_cost)
  if (!allocation) return 0
  return Math.min(100, Math.round((cost / allocation) * 100))
})
const reminderTone = computed(() => (usagePercentage.value >= 90 ? 'danger' : usagePercentage.value >= 70 ? 'warning' : 'normal'))
const reminderLabel = computed(() => (usagePercentage.value >= 90 ? '临近额度上限' : usagePercentage.value >= 70 ? '使用偏高' : '使用正常'))

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
async function loadRecentCalls() {
  recentCallsState.value = 'loading'
  try {
    const result = await enterpriseAPI.listEmployeeUsage({ page: 1, page_size: 5 })
    recentCalls.value = result.items
    recentCallsState.value = 'ready'
  } catch {
    recentCalls.value = []
    recentCallsState.value = 'unavailable'
  }
}
onMounted(() => { load(); loadRecentCalls() })
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
.pool-grid{display:flex;align-items:center;gap:24px}
.ring{position:relative;flex:none;display:grid;width:96px;height:96px;place-items:center;border-radius:50%}
.ring:before{content:"";position:absolute;inset:8px;background:#fff;border-radius:50%}
.ring span{position:relative;font-size:16px;font-weight:700}
.pool-text{flex:1;min-width:0}
.pool-text strong{display:block;margin-bottom:6px;font-size:18px}
.pool-text small{color:#6b7280;font-size:12px}
.trend-chart{display:flex;align-items:flex-end;gap:16px;height:160px;padding:16px 12px 26px;border-left:1px solid #d1d5db;border-bottom:1px solid #d1d5db;background:repeating-linear-gradient(to bottom,transparent 0,transparent 32px,#f0f1f3 33px)}
.trend-column{display:flex;flex:1;min-width:18px;height:100%;flex-direction:column;align-items:center;justify-content:flex-end;gap:8px}
.trend-bar{width:min(36px,100%);min-height:8%;border-radius:4px 4px 0 0;background:#2563eb}
.trend-column small{color:#6b7280;font-size:10px;white-space:nowrap}
.empty-chart{display:grid;place-items:center;height:120px;color:#6b7280}
.split-panels{display:grid;grid-template-columns:1.3fr 1fr;gap:16px;margin-top:16px}
.split-panels .panel{margin-top:0}
.panel-link{color:#2563eb;font-size:12px;text-decoration:none;font-weight:600;white-space:nowrap}
.calls-table{width:100%;border-collapse:collapse;font-size:13px}
.calls-table th{padding:8px 6px;color:#6b7280;font-size:11px;text-align:left;border-bottom:1px solid #e5e7eb}
.calls-table td{padding:8px 6px;border-bottom:1px solid #f1f5f9}
.chip{padding:2px 10px;border-radius:999px;font-size:11px;font-weight:600}
.chip.normal{background:#ecfdf5;color:#059669}
.chip.warning{background:#fffbeb;color:#b45309}
.chip.danger{background:#fef2f2;color:#b91c1c}
.reminder-headline{display:block;margin-bottom:6px;font-size:15px}
.reminder-desc{margin:0 0 12px;color:#6b7280;font-size:12px}
.progress{height:8px;border-radius:999px;background:#e4e7ec;overflow:hidden}
.progress i{display:block;height:100%;background:#2563eb;border-radius:999px}
.scale{display:flex;justify-content:space-between;margin-top:6px;color:#6b7280;font-size:11px}
@media(max-width:760px){.metrics{grid-template-columns:repeat(2,minmax(0,1fr))}.split-panels{grid-template-columns:1fr}}
@media(max-width:480px){.page-heading{flex-direction:column}.metrics{grid-template-columns:1fr}}
</style>
