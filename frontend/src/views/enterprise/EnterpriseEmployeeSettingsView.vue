<template>
  <section class="workspace">
    <div class="page-heading">
      <div>
        <h1>个人设置</h1>
        <p>管理登录密码与当前账号的活跃会话。</p>
      </div>
    </div>

    <div class="panel">
      <h2>账号信息</h2>
      <el-descriptions :column="1" border>
        <el-descriptions-item label="邮箱">{{ profile?.email || '加载中' }}</el-descriptions-item>
        <el-descriptions-item label="状态">{{ profile ? (profile.status === 'active' ? '在职' : profile.status) : '加载中' }}</el-descriptions-item>
      </el-descriptions>
    </div>

    <div class="panel">
      <h2>修改密码</h2>
      <p class="hint">修改后，当前会话仍可继续使用；其他设备可单独退出。</p>
      <el-form :model="form" label-position="top" @submit.prevent="submitPasswordChange">
        <el-form-item label="当前密码">
          <el-input v-model="form.current_password" type="password" show-password autocomplete="current-password" data-testid="current-password" />
        </el-form-item>
        <div class="field-row">
          <el-form-item label="新密码">
            <el-input v-model="form.new_password" type="password" show-password autocomplete="new-password" data-testid="new-password" />
          </el-form-item>
          <el-form-item label="确认新密码">
            <el-input v-model="form.confirm_password" type="password" show-password autocomplete="new-password" data-testid="confirm-password" />
          </el-form-item>
        </div>
        <p v-if="passwordValidationMessage" class="field-error" data-testid="password-validation-error">{{ passwordValidationMessage }}</p>
        <div class="actions">
          <el-button type="primary" :loading="saving" :disabled="!isPasswordFormFillable" data-testid="submit-password" @click="submitPasswordChange">
            更新密码
          </el-button>
          <span v-if="isPasswordPolicySatisfied" class="success" data-testid="password-policy-ok">✓ 密码策略已满足</span>
        </div>
      </el-form>
    </div>

    <div class="panel">
      <h2>登录会话</h2>
      <p class="hint">识别活跃设备，并在不再使用时结束会话。</p>
      <div v-if="sessionsLoading" data-testid="sessions-loading">加载中…</div>
      <div v-else-if="sessionsError" data-testid="sessions-error" class="field-error">
        会话列表暂时不可用，<el-button link type="primary" @click="loadSessions">重试</el-button>
      </div>
      <template v-else>
        <div v-if="sessions.length === 0" data-testid="sessions-empty" class="hint">暂无活跃会话记录。</div>
        <div v-for="session in sessions" :key="session.id" class="session-row" data-testid="session-row">
          <div class="device">
            <div class="device-icon">◫</div>
            <div>
              <b>{{ session.current ? '当前浏览器' : describeUserAgent(session.user_agent) }}</b>
              <p>{{ session.current ? '当前会话' : maskIpAddress(session.ip_address) }} · 最近活动：{{ formatRelativeTime(session.last_seen_at) }}</p>
            </div>
          </div>
          <span v-if="session.current" class="tag" data-testid="session-current-tag">当前设备</span>
          <el-button
            v-else
            size="small"
            :loading="revokingSessionId === session.id"
            data-testid="revoke-session"
            @click="confirmRevokeSession(session.id)"
          >退出该设备</el-button>
        </div>
        <div class="notice">
          <div>
            <b>退出当前会话不会影响其他设备</b>
            <p>如怀疑账号被他人使用，请选择「退出全部设备」并重新登录。</p>
          </div>
        </div>
      </template>
    </div>

    <div class="panel danger-panel">
      <h2>会话退出</h2>
      <p class="hint">这些操作会终止登录状态，不会删除账号或企业数据。</p>
      <div class="danger-row">
        <div>
          <b>退出当前会话</b>
          <p>仅退出正在使用的当前浏览器会话。</p>
        </div>
        <el-button data-testid="logout-current" :loading="loggingOutCurrent" @click="confirmLogoutCurrent">退出当前会话</el-button>
      </div>
      <div class="danger-row">
        <div>
          <b>退出全部设备</b>
          <p>终止本账号在所有设备上的登录会话，需重新登录。</p>
        </div>
        <el-button data-testid="logout-all" :loading="loggingOutAll" @click="confirmLogoutAll">退出全部设备</el-button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseEmployee, EnterpriseSession } from '@/types/enterprise'
import { useRouter } from 'vue-router'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'
import { describeUserAgent, maskIpAddress } from '@/utils/maskSessionDevice'

const profile = ref<EnterpriseEmployee>()
const saving = ref(false)
const form = reactive({ current_password: '', new_password: '', confirm_password: '' })

const sessions = ref<EnterpriseSession[]>([])
const sessionsLoading = ref(true)
const sessionsError = ref(false)
const revokingSessionId = ref<string | null>(null)
const loggingOutCurrent = ref(false)
const loggingOutAll = ref(false)

const router = useRouter()
const auth = useEnterpriseAuthStore()

const isPasswordFormFillable = computed(() =>
  form.current_password.length > 0 && form.new_password.length > 0 && form.confirm_password.length > 0,
)

const isPasswordPolicySatisfied = computed(() => passwordValidationMessage.value === '' && isPasswordFormFillable.value)

const passwordValidationMessage = computed(() => {
  if (!isPasswordFormFillable.value) return ''
  if (form.new_password.length < 12) return '新密码至少需要 12 个字符'
  if (!/[a-zA-Z]/.test(form.new_password) || !/[0-9]/.test(form.new_password)) return '新密码需同时包含字母和数字'
  if (form.new_password !== form.confirm_password) return '两次输入的新密码不一致'
  return ''
})

function formatRelativeTime(iso: string): string {
  const target = new Date(iso).getTime()
  if (Number.isNaN(target)) return '未知时间'
  const diffMs = Date.now() - target
  if (diffMs < 60_000) return '刚刚'
  const minutes = Math.floor(diffMs / 60_000)
  if (minutes < 60) return `${minutes} 分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时前`
  const days = Math.floor(hours / 24)
  return `${days} 天前`
}

async function loadProfile() {
  try {
    profile.value = await enterpriseAPI.getEmployeeProfile()
  } catch {
    ElMessage.error('个人资料暂时不可用')
  }
}

async function loadSessions() {
  sessionsLoading.value = true
  sessionsError.value = false
  try {
    sessions.value = await enterpriseAPI.listSessions()
  } catch {
    sessionsError.value = true
  } finally {
    sessionsLoading.value = false
  }
}

async function submitPasswordChange() {
  if (!isPasswordFormFillable.value || passwordValidationMessage.value) return
  saving.value = true
  try {
    await enterpriseAPI.changePassword(form.current_password, form.new_password)
    form.current_password = ''
    form.new_password = ''
    form.confirm_password = ''
    auth.clear()
    ElMessage.success('密码已更新，请重新登录')
    await router.replace('/enterprise/login')
  } catch {
    ElMessage.error('密码更新失败，请确认当前密码是否正确')
  } finally {
    saving.value = false
  }
}

async function confirmRevokeSession(id: string) {
  try {
    await ElMessageBox.confirm('退出该设备后需要在该设备上重新登录，是否继续？', '退出该设备', { type: 'warning' })
  } catch {
    return
  }
  revokingSessionId.value = id
  try {
    await enterpriseAPI.revokeSession(id)
    ElMessage.success('已退出该设备')
    await loadSessions()
  } catch {
    ElMessage.error('退出该设备失败，请稍后重试')
  } finally {
    revokingSessionId.value = null
  }
}

async function confirmLogoutCurrent() {
  try {
    await ElMessageBox.confirm('退出当前会话后需要重新登录，是否继续？', '退出当前会话', { type: 'warning' })
  } catch {
    return
  }
  loggingOutCurrent.value = true
  try {
    await auth.logout()
    await router.replace('/enterprise/login')
  } finally {
    loggingOutCurrent.value = false
  }
}

async function confirmLogoutAll() {
  try {
    await ElMessageBox.confirm('退出全部设备后所有已登录设备均需重新登录，是否继续？', '退出全部设备', { type: 'warning' })
  } catch {
    return
  }
  loggingOutAll.value = true
  try {
    await enterpriseAPI.revokeAllSessions()
    auth.clear()
    ElMessage.success('已退出全部设备')
    await router.replace('/enterprise/login')
  } catch {
    ElMessage.error('退出全部设备失败，请稍后重试')
  } finally {
    loggingOutAll.value = false
  }
}

onMounted(() => {
  loadProfile()
  loadSessions()
})
</script>

<style scoped>
.workspace { max-width: 760px; }
.page-heading { margin-bottom: 20px; }
.page-heading h1 { margin: 0 0 6px; font-size: 26px; }
.page-heading p { color: #64748b; }
.panel { margin-bottom: 16px; padding: 18px; border: 1px solid #e5e7eb; border-radius: 8px; background: #fff; }
.panel h2 { margin: 0 0 6px; font-size: 15px; }
.hint { margin: 0 0 14px; color: #64748b; font-size: 12px; }
.field-row { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.field-error { margin: -8px 0 14px; color: #b42318; font-size: 12px; }
.actions { display: flex; align-items: center; gap: 12px; }
.success { color: #15803d; font-size: 12px; font-weight: 700; }
.session-row { display: flex; align-items: center; justify-content: space-between; padding: 14px 0; border-bottom: 1px solid #f1f2f4; }
.session-row:last-of-type { border-bottom: none; }
.device { display: flex; align-items: center; gap: 11px; }
.device-icon { display: grid; place-items: center; width: 34px; height: 34px; border-radius: 6px; background: #f2f4f7; color: #475467; font-weight: 800; }
.device b { font-size: 13px; }
.device p { margin: 4px 0 0; color: #64748b; font-size: 11px; }
.tag { display: inline-flex; align-items: center; padding: 4px 10px; border-radius: 12px; background: #ecfdf3; color: #15803d; font-size: 11px; font-weight: 700; }
.notice { margin-top: 14px; padding: 11px 12px; border: 1px solid #fde68a; border-radius: 6px; background: #fffbeb; }
.notice b { font-size: 12px; }
.notice p { margin: 3px 0 0; color: #64748b; font-size: 11px; }
.danger-panel .danger-row { display: flex; align-items: center; justify-content: space-between; padding: 14px 0; border-top: 1px solid #f1f2f4; }
.danger-panel .danger-row:first-of-type { border-top: none; padding-top: 4px; }
.danger-row b { font-size: 13px; }
.danger-row p { margin: 4px 0 0; color: #64748b; font-size: 11px; }
</style>
