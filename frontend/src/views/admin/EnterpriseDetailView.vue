<template><section><div class="heading"><div><h1>企业详情</h1><p>查看企业入口、订阅和停用影响范围。</p></div><el-button @click="router.back()">返回</el-button></div><el-skeleton v-if="loading" :rows="5" animated/><template v-else-if="item"><el-descriptions :column="1" border><el-descriptions-item label="企业名称">{{item.name}}</el-descriptions-item><el-descriptions-item label="入口域名">{{item.portal_host}}</el-descriptions-item><el-descriptions-item label="管理员邮箱">{{item.admin_email || '未配置'}}</el-descriptions-item><el-descriptions-item label="专用上游用户 ID">{{item.dedicated_upstream_user_id}}</el-descriptions-item><el-descriptions-item label="状态">{{item.status}}</el-descriptions-item><el-descriptions-item label="创建时间">{{item.created_at}}</el-descriptions-item></el-descriptions><div class="impact"><h2>停用影响范围</h2><el-descriptions :column="2" border><el-descriptions-item label="员工总数">{{item.employee_count}}</el-descriptions-item><el-descriptions-item label="在职员工">{{item.active_employee_count}}</el-descriptions-item><el-descriptions-item label="活动会话">{{item.active_session_count}}</el-descriptions-item><el-descriptions-item label="活动 Key">{{item.active_key_count}}</el-descriptions-item><el-descriptions-item label="关联订阅" :span="2">{{subscriptionLabel}}</el-descriptions-item></el-descriptions></div></template></section></template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'

const route = useRoute()
const router = useRouter()
const item = ref<PlatformEnterprise>()
const loading = ref(false)
const subscriptionLabel = computed(() => {
  if (!item.value?.subscription) return '无活动或待生效订阅'
  const subscription = item.value.subscription
  return `${subscription.plan}（${subscription.status}，weekly 上限 ${subscription.weekly_limit || '未配置'}，有效期至 ${subscription.expires_at}）`
})

onMounted(async () => {
  loading.value = true
  try {
    item.value = await enterprisePlatformAPI.get(Number(route.params.id))
  } catch {
    ElMessage.error('企业详情暂时不可用')
  } finally {
    loading.value = false
  }
})
</script>
<style scoped>.heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}.heading h1{margin:0 0 6px;font-size:26px}.heading p{color:#64748b}.impact{margin-top:20px}.impact h2{margin:0 0 12px;font-size:18px}</style>
