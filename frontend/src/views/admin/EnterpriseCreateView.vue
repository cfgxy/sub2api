<template>
  <div class="platform-page">
    <aside class="side">
      <div class="logo"><span class="logo-mark">S</span><div><strong>Sub2API</strong><small>{{ t('admin.enterprise.create.platform') }}</small></div></div>
      <div class="nav-label">{{ t('admin.enterprise.create.platform') }}</div>
      <RouterLink class="nav active" to="/admin/enterprises">{{ t('admin.enterprise.common.management') }}</RouterLink>
      <RouterLink class="nav" to="/admin/audit-logs">{{ t('admin.enterprise.create.audit') }}</RouterLink>
      <div class="side-help">{{ t('admin.enterprise.create.help') }}</div>
    </aside>
    <div class="content">
      <header class="topbar"><div class="crumb">{{ t('admin.enterprise.create.breadcrumb') }}</div><div class="health"><i></i>{{ t('admin.enterprise.create.healthy') }}</div></header>
      <main>
        <RouterLink class="back" to="/admin/enterprises">‹ {{ t('admin.enterprise.create.backToList') }}</RouterLink>
        <div class="titlebar"><div><h1>{{ t('admin.enterprise.create.title') }}</h1><p>{{ t('admin.enterprise.create.description') }}</p></div></div>
        <el-alert v-if="success && created" type="success" :title="t('admin.enterprise.create.successTitle')" :closable="false" show-icon class="result" data-testid="enterprise-create-success">
          <template #default>
            <div class="result-grid">
              <div><span>{{ t('admin.enterprise.common.name') }}</span><strong>{{ created.name }}</strong></div>
              <div><span>{{ t('admin.enterprise.common.portalHost') }}</span><strong>{{ created.portal_host }}</strong></div>
              <div><span>{{ t('admin.enterprise.create.dedicatedAccount') }}</span><strong>{{ created.admin_email || t('admin.enterprise.create.configured') }}</strong></div>
              <div><span>{{ t('admin.enterprise.common.upstreamUserId') }}</span><strong>{{ created.dedicated_upstream_user_id }}</strong></div>
            </div>
            <p class="result-note">{{ t('admin.enterprise.create.successNote') }}</p>
            <el-button text type="primary" @click="router.push(`/admin/enterprises/${created.id}`)">{{ t('admin.enterprise.create.viewDetail') }} ›</el-button>
          </template>
        </el-alert>
        <div class="layout">
          <el-form ref="formRef" class="form-panel" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
            <section class="section"><div class="section-head"><span>1</span><div><h2>{{ t('admin.enterprise.create.basicTitle') }}</h2><p>{{ t('admin.enterprise.create.basicDescription') }}</p></div></div>
              <el-form-item :label="t('admin.enterprise.common.name')" prop="name"><el-input v-model="form.name" maxlength="255" :placeholder="t('admin.enterprise.create.namePlaceholder')" /></el-form-item>
              <el-form-item :label="t('admin.enterprise.create.portalLabel')" prop="portal_host"><el-input v-model="form.portal_host" placeholder="enterprise.example.com" /></el-form-item>
            </section>
            <section class="section"><div class="section-head"><span>2</span><div><h2>{{ t('admin.enterprise.create.accountTitle') }}</h2><p>{{ t('admin.enterprise.create.accountDescription') }}</p></div></div>
              <el-form-item :label="t('admin.enterprise.create.dedicatedAccount')" prop="dedicated_upstream_user_id">
                <el-select v-model="form.dedicated_upstream_user_id" filterable remote clearable :remote-method="searchUsers" :loading="searching" :disabled="!enterprisesLoaded" :placeholder="t('admin.enterprise.create.searchAccount')" style="width:100%" @change="selectUser">
                  <el-option v-for="option in userOptions" :key="option.user.id" :value="option.user.id" :label="optionLabel(option)" :disabled="!!option.reason" />
                </el-select>
              </el-form-item>
              <el-alert v-if="sourceError" type="error" :title="sourceError" :closable="false" show-icon class="source-error" />
              <p v-else-if="selectedReason" class="source-error">{{ selectedReason }}</p>
              <el-alert type="info" :closable="false" :title="t('admin.enterprise.create.sourceTitle')" :description="t('admin.enterprise.create.sourceDescription')" />
            </section>
            <section class="section"><div class="section-head"><span>3</span><div><h2>{{ t('admin.enterprise.create.reasonTitle') }}</h2><p>{{ t('admin.enterprise.create.reasonDescription') }}</p></div></div>
              <el-form-item :label="t('admin.enterprise.create.reasonLabel')" prop="reason"><el-input v-model="form.reason" type="textarea" maxlength="500" show-word-limit :rows="3" :placeholder="t('admin.enterprise.create.reasonPlaceholder')" /></el-form-item>
            </section>
            <div class="actions"><el-button @click="router.push('/admin/enterprises')">{{ t('admin.enterprise.common.cancel') }}</el-button><el-button type="primary" native-type="submit" :loading="saving">{{ t('admin.enterprise.create.title') }}</el-button></div>
          </el-form>
          <aside class="check-panel"><h3>{{ t('admin.enterprise.create.checksTitle') }}</h3><ul><li>{{ t('admin.enterprise.create.checksName') }}</li><li>{{ t('admin.enterprise.create.checksActive') }}</li><li>{{ t('admin.enterprise.create.checksSubscription') }}</li><li>{{ t('admin.enterprise.create.checksPrivacy') }}</li></ul><div class="note">{{ t('admin.enterprise.create.note') }}</div></aside>
        </div>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'
import { list as listUsers } from '@/api/admin/users'
import type { AdminUser } from '@/types'

const router = useRouter()
const { t, locale } = useI18n({ useScope: 'global' })
const formRef = ref<FormInstance>()
const saving = ref(false)
const success = ref(false)
const created = ref<PlatformEnterprise>()
const searching = ref(false)
const enterprisesLoaded = ref(false)
const sourceError = ref('')
const existingEnterprises = ref<PlatformEnterprise[]>([])
const candidates = ref<AdminUser[]>([])
const selectedUser = ref<AdminUser>()
let searchSequence = 0
const form = reactive<{ name: string; portal_host: string; dedicated_upstream_user_id: number | ''; reason: string }>({ name: '', portal_host: '', dedicated_upstream_user_id: '', reason: '' })
const userOptions = computed(() => candidates.value.filter((user) => Number.isInteger(user.id) && user.id > 0).map((user) => ({ user, reason: ineligibleReason(user) })))
const selectedReason = computed(() => selectedUser.value ? ineligibleReason(selectedUser.value) : '')

function optionLabel(option: { user: AdminUser; reason: string }) {
  const account = `${option.user.username} · ${option.user.email}`
  if (!option.reason) return account
  return locale.value === 'zh' ? `${account}（${option.reason}）` : `${account} (${option.reason})`
}

onMounted(async () => {
  try {
    existingEnterprises.value = await enterprisePlatformAPI.list()
    enterprisesLoaded.value = true
  } catch {
    sourceError.value = t('admin.enterprise.create.listFailed')
  }
})

function ineligibleReason(user: AdminUser): string {
  if (!enterprisesLoaded.value) return t('admin.enterprise.create.loadingAccount')
  if (user.status !== 'active') return t('admin.enterprise.create.disabledAccount')
  if (existingEnterprises.value.some((item) => item.dedicated_upstream_user_id === user.id)) return t('admin.enterprise.create.linkedAccount')
  const now = Date.now()
  if (!user.subscriptions?.some((item) => item.status === 'active'
    && Date.parse(item.starts_at) <= now && Date.parse(item.expires_at || '') > now
    && item.group?.status === 'active' && Number(item.group.weekly_limit_usd) > 0)) {
    return t('admin.enterprise.create.noWeeklySubscription')
  }
  return ''
}

async function searchUsers(query: string) {
  const sequence = ++searchSequence
  candidates.value = []
  if (!query.trim() || !enterprisesLoaded.value) return
  sourceError.value = ''
  searching.value = true
  try {
    const response = await listUsers(1, 20, { search: query.trim(), include_subscriptions: true })
    if (sequence === searchSequence) candidates.value = response.items
  } catch {
    if (sequence === searchSequence) sourceError.value = t('admin.enterprise.create.searchFailed')
  } finally {
    if (sequence === searchSequence) searching.value = false
  }
}

function selectUser(id: number | '') {
  selectedUser.value = userOptions.value.find((option) => option.user.id === id)?.user
}

const rules = computed<FormRules>(() => ({
  name: [{ required: true, message: t('admin.enterprise.create.nameRequired'), trigger: 'blur' }],
  portal_host: [{ required: true, message: t('admin.enterprise.create.hostRequired'), trigger: 'blur' }, { pattern: /^[a-z0-9.-]+$/, message: t('admin.enterprise.create.hostInvalid'), trigger: 'blur' }],
  dedicated_upstream_user_id: [{ type: 'number', min: 1, message: t('admin.enterprise.create.accountRequired'), trigger: 'change' }],
  reason: [{ required: true, message: t('admin.enterprise.create.reasonRequired'), trigger: 'blur' }],
}))

async function submit() {
  if (!await formRef.value?.validate().catch(() => false)) return
  if (!enterprisesLoaded.value || !selectedUser.value || form.dedicated_upstream_user_id !== selectedUser.value.id || selectedReason.value) {
    ElMessage.error(selectedReason.value || sourceError.value || t('admin.enterprise.create.accountRequired'))
    return
  }
  saving.value = true
  success.value = false
  try {
    created.value = await enterprisePlatformAPI.create({ ...form, dedicated_upstream_user_id: selectedUser.value.id })
    success.value = true
    ElMessage.success(t('admin.enterprise.create.createSuccess'))
    window.scrollTo({ top: 0, behavior: 'smooth' })
  } catch (error) {
    const message = (error as { message?: string }).message
    ElMessage.error(message || t('admin.enterprise.create.createFailed'))
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.platform-page{min-height:100vh;background:#f0f2f5;color:#111827}.side{position:fixed;inset:0 auto 0 0;width:232px;padding:20px 12px;background:#fff;border-right:1px solid #e5e7eb}.logo{display:flex;align-items:center;gap:10px;padding:0 10px;margin-bottom:26px}.logo-mark{display:grid;width:30px;height:30px;place-items:center;border-radius:7px;background:#2563eb;color:#fff;font-weight:800}.logo strong,.logo small{display:block}.logo small{margin-top:2px;color:#6b7280;font-size:10px}.nav-label{margin:16px 10px 7px;color:#9ca3af;font-size:11px;font-weight:700}.nav{display:block;padding:11px;border-radius:6px;color:#4b5563;text-decoration:none}.nav.active{background:#eff6ff;color:#2563eb;font-weight:700}.side-help{position:absolute;right:22px;bottom:20px;left:22px;padding-top:14px;border-top:1px solid #e5e7eb;color:#6b7280;font-size:12px}.content{margin-left:232px}.topbar{position:sticky;top:0;z-index:2;display:flex;align-items:center;height:64px;padding:0 28px;background:#fff;border-bottom:1px solid #e5e7eb}.crumb{font-weight:650}.crumb span{margin-left:8px;color:#9ca3af;font-weight:400}.health{display:flex;align-items:center;gap:7px;margin-left:auto;color:#4b5563;font-size:12px}.health i{width:7px;height:7px;border-radius:50%;background:#15803d}main{max-width:1120px;margin:0 auto;padding:28px}.back{display:inline-block;margin-bottom:12px;color:#64748b;font-size:13px;text-decoration:none}.titlebar{margin-bottom:18px}.titlebar h1{margin:0 0 6px;font-size:24px}.titlebar p{margin:0;color:#526078}.result{margin-bottom:16px}.result-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:6px 20px;margin-bottom:8px}.result-grid span{display:block;color:#6b7280;font-size:12px}.result-grid strong{font-weight:600;overflow-wrap:anywhere}.result-note{margin:4px 0 8px;color:#6b7280;font-size:12px}.layout{display:grid;grid-template-columns:minmax(0,1fr) 300px;gap:16px;align-items:start}.form-panel,.check-panel{background:#fff;border:1px solid #e5e7eb;border-radius:8px}.section{padding:20px 22px;border-bottom:1px solid #e5e7eb}.section-head{display:flex;gap:11px;margin-bottom:16px}.section-head>span{display:grid;width:24px;height:24px;place-items:center;border-radius:50%;background:#eff6ff;color:#2563eb;font-weight:700}.section-head h2{margin:2px 0 4px;font-size:15px}.section-head p{margin:0;color:#6b7280;font-size:12px}.actions{display:flex;justify-content:flex-end;gap:10px;padding:16px 22px}.check-panel{padding:20px}.check-panel h3{margin:0 0 14px;font-size:14px}.check-panel ul{display:grid;gap:12px;margin:0;padding:0 0 0 18px;color:#526078;font-size:12px;line-height:18px}.note{margin-top:18px;padding:12px;background:#fffbeb;border:1px solid #fde68a;border-radius:6px;color:#92400e;font-size:12px;line-height:18px}@media(max-width:760px){.side{display:none}.content{margin-left:0}.topbar{padding:0 16px}.crumb span,.health{display:none}main{padding:18px 14px}.layout{grid-template-columns:1fr}.check-panel{order:-1}.actions{padding:16px}.actions .el-button{flex:1}}
.source-error{margin:0 0 12px;color:#b42318;font-size:12px}
</style>
