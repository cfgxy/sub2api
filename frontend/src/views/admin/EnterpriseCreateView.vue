<template>
  <div class="platform-page">
    <aside class="side">
      <div class="logo"><span class="logo-mark">S</span><div><strong>Sub2API</strong><small>平台管理</small></div></div>
      <div class="nav-label">平台管理</div>
      <RouterLink class="nav active" to="/admin/enterprises">企业管理</RouterLink>
      <RouterLink class="nav" to="/admin/audit-logs">平台审计</RouterLink>
      <div class="side-help">帮助与支持</div>
    </aside>
    <div class="content">
      <header class="topbar"><div class="crumb">平台管理 <span>/ 企业管理 / 创建企业</span></div><div class="health"><i></i>平台服务正常</div></header>
      <main>
        <RouterLink class="back" to="/admin/enterprises">‹ 返回企业列表</RouterLink>
        <div class="titlebar"><div><h1>创建企业</h1><p>建立企业基础身份与独立访问入口，创建后关联唯一 Sub2API 主账号。</p></div></div>
        <el-alert v-if="success && created" type="success" title="企业已开通" :closable="false" show-icon class="result" data-testid="enterprise-create-success">
          <template #default>
            <div class="result-grid">
              <div><span>企业名称</span><strong>{{ created.name }}</strong></div>
              <div><span>入口域名</span><strong>{{ created.portal_host }}</strong></div>
              <div><span>专用主账号</span><strong>{{ created.admin_email || '已配置' }}</strong></div>
              <div><span>专用上游用户 ID</span><strong>{{ created.dedicated_upstream_user_id }}</strong></div>
            </div>
            <p class="result-note">账号密码、Token 等敏感凭据不在此展示；如需查看订阅与状态，请前往企业详情。</p>
            <el-button text type="primary" @click="router.push(`/admin/enterprises/${created.id}`)">查看企业详情 ›</el-button>
          </template>
        </el-alert>
        <div class="layout">
          <el-form ref="formRef" class="form-panel" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
            <section class="section"><div class="section-head"><span>1</span><div><h2>企业基础信息</h2><p>用于平台识别与企业列表展示，不包含敏感凭据。</p></div></div>
              <el-form-item label="企业名称" prop="name"><el-input v-model="form.name" maxlength="255" placeholder="例如：云川数据" /></el-form-item>
              <el-form-item label="企业入口域名" prop="portal_host"><el-input v-model="form.portal_host" placeholder="enterprise.example.com" /></el-form-item>
            </section>
            <section class="section"><div class="section-head"><span>2</span><div><h2>唯一主账号与订阅</h2><p>开通前会校验账号状态及当前可用周订阅。</p></div></div>
              <el-form-item label="专用上游用户 ID" prop="dedicated_upstream_user_id"><el-input-number v-model="form.dedicated_upstream_user_id" :min="1" controls-position="right" /></el-form-item>
              <el-alert type="info" :closable="false" title="平台只保存关联关系" description="主账号密码、Token 和 Key 不会显示在企业端或审计记录中；没有可用订阅时创建会被拒绝。" />
            </section>
            <section class="section"><div class="section-head"><span>3</span><div><h2>开通说明</h2><p>说明会进入平台审计，便于后续追溯。</p></div></div>
              <el-form-item label="开通原因" prop="reason"><el-input v-model="form.reason" type="textarea" maxlength="500" show-word-limit :rows="3" placeholder="填写本次开通的业务原因" /></el-form-item>
            </section>
            <div class="actions"><el-button @click="router.push('/admin/enterprises')">取消</el-button><el-button type="primary" native-type="submit" :loading="saving">创建企业</el-button></div>
          </el-form>
          <aside class="check-panel"><h3>创建检查</h3><ul><li>企业名称和入口域名将执行格式与唯一性校验。</li><li>专用上游用户必须处于启用状态。</li><li>专用上游用户必须存在当前可用周订阅。</li><li>成功结果只展示脱敏主账号信息。</li></ul><div class="note">企业创建成功后，企业管理员从独立入口登录；员工账号不创建为 Sub2API 用户。</div></aside>
        </div>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'

const router = useRouter()
const formRef = ref<FormInstance>()
const saving = ref(false)
const success = ref(false)
const created = ref<PlatformEnterprise>()
const form = reactive({ name: '', portal_host: '', dedicated_upstream_user_id: 0, reason: '' })
const rules: FormRules = {
  name: [{ required: true, message: '请输入企业名称', trigger: 'blur' }],
  portal_host: [{ required: true, message: '请输入企业入口域名', trigger: 'blur' }, { pattern: /^[a-z0-9.-]+$/, message: '请输入有效域名', trigger: 'blur' }],
  dedicated_upstream_user_id: [{ required: true, message: '请输入专用上游用户 ID', trigger: 'change' }],
  reason: [{ required: true, message: '请输入开通原因', trigger: 'blur' }],
}

async function submit() {
  if (!await formRef.value?.validate().catch(() => false)) return
  saving.value = true
  success.value = false
  try {
    created.value = await enterprisePlatformAPI.create(form)
    success.value = true
    ElMessage.success('企业已开通')
    window.scrollTo({ top: 0, behavior: 'smooth' })
  } catch (error) {
    const message = (error as { message?: string }).message
    ElMessage.error(message || '企业开通失败，请检查主账号和订阅状态')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.platform-page{min-height:100vh;background:#f0f2f5;color:#111827}.side{position:fixed;inset:0 auto 0 0;width:232px;padding:20px 12px;background:#fff;border-right:1px solid #e5e7eb}.logo{display:flex;align-items:center;gap:10px;padding:0 10px;margin-bottom:26px}.logo-mark{display:grid;width:30px;height:30px;place-items:center;border-radius:7px;background:#2563eb;color:#fff;font-weight:800}.logo strong,.logo small{display:block}.logo small{margin-top:2px;color:#6b7280;font-size:10px}.nav-label{margin:16px 10px 7px;color:#9ca3af;font-size:11px;font-weight:700}.nav{display:block;padding:11px;border-radius:6px;color:#4b5563;text-decoration:none}.nav.active{background:#eff6ff;color:#2563eb;font-weight:700}.side-help{position:absolute;right:22px;bottom:20px;left:22px;padding-top:14px;border-top:1px solid #e5e7eb;color:#6b7280;font-size:12px}.content{margin-left:232px}.topbar{position:sticky;top:0;z-index:2;display:flex;align-items:center;height:64px;padding:0 28px;background:#fff;border-bottom:1px solid #e5e7eb}.crumb{font-weight:650}.crumb span{margin-left:8px;color:#9ca3af;font-weight:400}.health{display:flex;align-items:center;gap:7px;margin-left:auto;color:#4b5563;font-size:12px}.health i{width:7px;height:7px;border-radius:50%;background:#15803d}main{max-width:1120px;margin:0 auto;padding:28px}.back{display:inline-block;margin-bottom:12px;color:#64748b;font-size:13px;text-decoration:none}.titlebar{margin-bottom:18px}.titlebar h1{margin:0 0 6px;font-size:24px}.titlebar p{margin:0;color:#526078}.result{margin-bottom:16px}.result-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:6px 20px;margin-bottom:8px}.result-grid span{display:block;color:#6b7280;font-size:12px}.result-grid strong{font-weight:600;overflow-wrap:anywhere}.result-note{margin:4px 0 8px;color:#6b7280;font-size:12px}.layout{display:grid;grid-template-columns:minmax(0,1fr) 300px;gap:16px;align-items:start}.form-panel,.check-panel{background:#fff;border:1px solid #e5e7eb;border-radius:8px}.section{padding:20px 22px;border-bottom:1px solid #e5e7eb}.section-head{display:flex;gap:11px;margin-bottom:16px}.section-head>span{display:grid;width:24px;height:24px;place-items:center;border-radius:50%;background:#eff6ff;color:#2563eb;font-weight:700}.section-head h2{margin:2px 0 4px;font-size:15px}.section-head p{margin:0;color:#6b7280;font-size:12px}.actions{display:flex;justify-content:flex-end;gap:10px;padding:16px 22px}.check-panel{padding:20px}.check-panel h3{margin:0 0 14px;font-size:14px}.check-panel ul{display:grid;gap:12px;margin:0;padding:0 0 0 18px;color:#526078;font-size:12px;line-height:18px}.note{margin-top:18px;padding:12px;background:#fffbeb;border:1px solid #fde68a;border-radius:6px;color:#92400e;font-size:12px;line-height:18px}@media(max-width:760px){.side{display:none}.content{margin-left:0}.topbar{padding:0 16px}.crumb span,.health{display:none}main{padding:18px 14px}.layout{grid-template-columns:1fr}.check-panel{order:-1}.actions{padding:16px}.actions .el-button{flex:1}}
</style>
