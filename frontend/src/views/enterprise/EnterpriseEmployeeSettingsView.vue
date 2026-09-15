<template>
  <section class="workspace">
    <div class="page-heading"><div><h1>个人设置</h1><p>维护企业账号密码，不显示或保存任何凭据。</p></div></div>
    <div class="panel"><el-descriptions :column="1" border><el-descriptions-item label="邮箱">{{ profile?.email || '加载中' }}</el-descriptions-item><el-descriptions-item label="状态">{{ profile?.status === 'active' ? '在职' : profile?.status || '加载中' }}</el-descriptions-item></el-descriptions></div>
    <div class="panel"><h2>修改密码</h2><el-form :model="form" label-position="top" @submit.prevent="changePassword"><el-form-item label="当前密码"><el-input v-model="form.current_password" type="password" show-password autocomplete="current-password" /></el-form-item><el-form-item label="新密码"><el-input v-model="form.new_password" type="password" show-password autocomplete="new-password" /></el-form-item><el-button type="primary" :loading="saving" @click="changePassword">保存密码</el-button></el-form></div>
  </section>
</template>
<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseEmployee } from '@/types/enterprise'
import { useRouter } from 'vue-router'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'
const profile=ref<EnterpriseEmployee>(),saving=ref(false),form=reactive({current_password:'',new_password:''})
const router = useRouter()
const auth = useEnterpriseAuthStore()
async function load(){try{profile.value=await enterpriseAPI.getEmployeeProfile()}catch{ElMessage.error('个人资料暂时不可用')}}
async function changePassword(){if(form.new_password.length<12){ElMessage.warning('新密码至少需要 12 个字符');return}saving.value=true;try{await enterpriseAPI.changePassword(form.current_password,form.new_password);form.current_password='';form.new_password='';auth.clear();ElMessage.success('密码已更新，请重新登录');await router.replace('/enterprise/login')}catch{ElMessage.error('密码更新失败')}finally{saving.value=false}}
onMounted(load)
</script>
<style scoped>.workspace{max-width:700px}.page-heading{margin-bottom:20px}.page-heading h1{margin:0 0 6px;font-size:26px}.page-heading p{color:#64748b}.panel{margin-bottom:16px;padding:18px;border:1px solid #e5e7eb;border-radius:8px;background:#fff}.panel h2{margin:0 0 18px;font-size:15px}</style>
