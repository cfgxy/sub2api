<template><section><div class="heading"><div><h1>企业详情</h1><p>查看企业入口与生命周期状态。</p></div><el-button @click="router.back()">返回</el-button></div><el-skeleton v-if="loading" :rows="5" animated/><el-descriptions v-else-if="item" :column="1" border><el-descriptions-item label="企业名称">{{item.name}}</el-descriptions-item><el-descriptions-item label="入口域名">{{item.portal_host}}</el-descriptions-item><el-descriptions-item label="专用上游用户 ID">{{item.dedicated_upstream_user_id}}</el-descriptions-item><el-descriptions-item label="状态">{{item.status}}</el-descriptions-item><el-descriptions-item label="创建时间">{{item.created_at}}</el-descriptions-item></el-descriptions></section></template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'

const route = useRoute()
const router = useRouter()
const item = ref<PlatformEnterprise>()
const loading = ref(false)

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
<style scoped>.heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:20px}.heading h1{margin:0 0 6px;font-size:26px}.heading p{color:#64748b}</style>
