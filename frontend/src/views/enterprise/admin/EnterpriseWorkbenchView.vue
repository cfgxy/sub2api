<template>
  <section class="workspace">
    <div class="page-heading">
      <div><h1>用量工作台</h1><p>按企业组织、员工、历史 Key 和时间窗口核对周期用量与审计记录。</p></div>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </div>

    <el-form class="filters" inline @submit.prevent="load">
      <el-form-item label="部门"><el-select v-model="filters.department_id" clearable placeholder="全部部门" @change="load"><el-option v-for="item in departments" :key="item.id" :label="item.name" :value="item.id" /></el-select></el-form-item>
      <el-form-item label="员工"><el-select v-model="filters.employee_id" clearable filterable placeholder="全部员工" @change="load"><el-option v-for="item in employees" :key="item.id" :label="item.email" :value="item.id" /></el-select></el-form-item>
      <el-form-item label="历史 Key"><el-input-number v-model="filters.api_key_id" :min="1" :controls="false" placeholder="Key ID" @change="load" /></el-form-item>
      <el-form-item label="窗口"><el-select v-model="filters.window_type" clearable placeholder="全部窗口" @change="load"><el-option label="日" value="day" /><el-option label="周" value="week" /><el-option label="月" value="month" /></el-select></el-form-item>
      <el-form-item label="窗口锚点"><el-date-picker v-model="filters.window_anchor" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" clearable /></el-form-item>
      <el-form-item label="开始时间"><el-date-picker v-model="filters.start_at" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" clearable /></el-form-item>
      <el-form-item label="结束时间"><el-date-picker v-model="filters.end_at" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" clearable /></el-form-item>
      <el-form-item><el-button type="primary" :icon="Search" @click="load">查询</el-button></el-form-item>
    </el-form>

    <div class="metrics" v-loading="loading">
      <el-card shadow="never"><span>筛选用量</span><strong>{{ summary?.total_usage_credit || '0' }}</strong></el-card>
      <el-card shadow="never"><span>请求数</span><strong>{{ summary?.total_requests || 0 }}</strong></el-card>
      <el-card shadow="never"><span>涉及员工</span><strong>{{ summary?.employee_count || 0 }}</strong></el-card>
      <el-card shadow="never"><span>在职员工</span><strong>{{ summary?.active_employee_count || 0 }}</strong></el-card>
    </div>

    <el-tabs v-model="activeTab" @tab-change="loadTab">
      <el-tab-pane label="员工汇总" name="summary"><el-table :data="summary?.employee_summaries || []" stripe><el-table-column prop="email" label="员工" min-width="240" /><el-table-column prop="requests" label="请求数" width="120" /><el-table-column prop="configured_credit" label="配置积分" width="140" /><el-table-column prop="usage_credit" label="用量积分" width="140" /></el-table><el-empty v-if="!loading && !(summary?.employee_summaries?.length)" description="当前筛选条件暂无用量" /></el-tab-pane>
      <el-tab-pane label="用量明细" name="usage"><el-table v-loading="usageLoading" :data="usage.items" stripe><el-table-column prop="request_at" label="请求时间" width="190"><template #default="{ row }">{{ formatDate(row.request_at) }}</template></el-table-column><el-table-column prop="employee_email" label="员工" min-width="220" /><el-table-column prop="api_key_masked" label="Key" width="150" /><el-table-column prop="window_type" label="窗口" width="90" /><el-table-column prop="usage_credit" label="用量积分" width="140" /><el-table-column prop="classification" label="归属" width="130"><template #default="{ row }">{{ row.classification === 'employee' ? '员工' : '受控外部' }}</template></el-table-column></el-table><el-empty v-if="!usageLoading && !usage.items.length" description="当前筛选条件暂无明细" /><el-pagination v-if="usage.total" v-model:current-page="usagePage" v-model:page-size="pageSize" :total="usage.total" layout="total, sizes, prev, pager, next" @current-change="loadUsage" @size-change="loadUsage" /></el-tab-pane>
      <el-tab-pane label="审计记录" name="audit"><el-table v-loading="auditLoading" :data="audit.items" stripe><el-table-column prop="created_at" label="时间" width="190"><template #default="{ row }">{{ formatDate(row.created_at) }}</template></el-table-column><el-table-column prop="event_type" label="事件" min-width="220" /><el-table-column prop="entity_type" label="对象" width="130" /><el-table-column prop="actor_ref" label="操作者" width="180" /></el-table><el-empty v-if="!auditLoading && !audit.items.length" description="暂无审计记录" /><el-pagination v-if="audit.total" v-model:current-page="auditPage" v-model:page-size="pageSize" :total="audit.total" layout="total, sizes, prev, pager, next" @current-change="loadAudit" @size-change="loadAudit" /></el-tab-pane>
    </el-tabs>
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseEmployee, EnterprisePaginated, EnterpriseWorkbenchAuditEvent, EnterpriseWorkbenchSummary, EnterpriseWorkbenchUsageRow } from '@/types/enterprise'

const departments = ref<EnterpriseDepartment[]>([])
const employees = ref<EnterpriseEmployee[]>([])
const summary = ref<EnterpriseWorkbenchSummary>()
const usage = reactive<EnterprisePaginated<EnterpriseWorkbenchUsageRow>>({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
const audit = reactive<EnterprisePaginated<EnterpriseWorkbenchAuditEvent>>({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
const filters = reactive<{ department_id?: number; employee_id?: number; api_key_id?: number; window_type?: string; window_anchor?: string; start_at?: string; end_at?: string }>({})
const activeTab = ref('summary'), loading = ref(false), usageLoading = ref(false), auditLoading = ref(false), usagePage = ref(1), auditPage = ref(1), pageSize = ref(20)
const formatDate = (value: string) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
const params = (page?: number): Record<string, string | number> => Object.fromEntries(Object.entries({ ...filters, page, page_size: pageSize.value }).filter(([, value]) => value !== undefined && value !== '')) as Record<string, string | number>
async function loadSummary() { summary.value = await enterpriseAPI.getWorkbenchSummary(params()) }
async function loadUsage() { usageLoading.value = true; try { Object.assign(usage, await enterpriseAPI.listWorkbenchUsage(params(usagePage.value))) } finally { usageLoading.value = false } }
async function loadAudit() { auditLoading.value = true; try { Object.assign(audit, await enterpriseAPI.listWorkbenchAuditEvents(params(auditPage.value))) } finally { auditLoading.value = false } }
async function loadTab() { if (activeTab.value === 'usage') await loadUsage(); if (activeTab.value === 'audit') await loadAudit() }
async function load() { loading.value = true; try { await Promise.all([loadSummary(), loadUsage(), loadAudit(), loadDirectories()]) } catch { ElMessage.error('工作台数据加载失败') } finally { loading.value = false } }
async function loadDirectories() { [departments.value, employees.value] = await Promise.all([enterpriseAPI.listDepartments(), enterpriseAPI.listEmployees()]) }
onMounted(load)
</script>

<style scoped>
.workspace{min-width:0}.page-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:20px}.page-heading h1{margin:0 0 6px;font-size:26px}.page-heading p{margin:0;color:#64748b}.filters{padding:16px 18px;margin-bottom:18px;background:#fff;border:1px solid #dce5e2;border-radius:6px}.metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px;margin-bottom:20px}.metrics .el-card{border-radius:6px}.metrics span{display:block;color:#64748b;font-size:13px}.metrics strong{display:block;margin-top:8px;font-size:24px;color:#173f3d}.el-pagination{justify-content:flex-end;margin-top:18px}@media(max-width:900px){.metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:640px){.page-heading{flex-direction:column}.page-heading .el-button{width:100%}.filters{display:block}.filters .el-form-item{display:block;margin-right:0}.filters .el-select,.filters .el-date-editor{width:100%}.metrics{grid-template-columns:1fr 1fr}}
</style>
