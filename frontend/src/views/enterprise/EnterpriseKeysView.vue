<template>
  <section class="workspace">
    <div class="page-heading">
      <div>
        <h1>API Key</h1>
        <p>查看本人密钥状态、额度和当前计费窗口。</p>
      </div>
      <el-button v-if="!key" type="primary" :icon="Plus" :loading="mutating" data-testid="create-key" @click="createKey">创建 Key</el-button>
      <div v-else class="actions">
        <el-button :icon="Refresh" :loading="mutating" data-testid="rotate-key" @click="rotateKey">轮换</el-button>
        <el-button type="danger" plain :icon="CircleClose" :loading="mutating" data-testid="disable-key" @click="disableKey">停用</el-button>
      </div>
    </div>

    <div v-loading="loading">
      <el-empty v-if="!loading && !key" description="尚未创建 API Key" />
      <template v-if="key">
        <div class="key-line">
          <div><span class="label">当前 Key</span><code>{{ key.masked_key }}</code></div>
          <el-tag :type="key.status === 'active' ? 'success' : 'info'">{{ statusLabel(key.status) }}</el-tag>
        </div>

        <div class="metrics">
          <article>
            <span>总额度</span>
            <strong>{{ formatLimit(key.quota) }}</strong>
            <small>已使用 {{ formatUsage(key.quota_used) }}</small>
            <el-progress :percentage="percentage(key.quota_used, key.quota)" :show-text="false" />
          </article>
        </div>
      </template>
    </div>

    <el-dialog v-model="secretVisible" title="请立即保存 API Key" width="min(560px, 92vw)" :close-on-click-modal="false" :teleported="false" @close="clearSecret">
      <el-alert type="warning" :closable="false" title="该明文仅显示一次，关闭后无法再次查看。" />
      <el-input class="secret-input" :model-value="secret" readonly data-testid="plaintext-key" />
      <template #footer><el-button type="primary" data-testid="close-secret" @click="clearSecret">我已保存</el-button></template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { CircleClose, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { enterpriseAPI, isEnterpriseKeyMutationStateConflict } from '@/api/enterprise'
import type { EnterpriseEmployeeKey } from '@/types/enterprise'

const key = ref<EnterpriseEmployeeKey | null>(null)
const loading = ref(false)
const mutating = ref(false)
const secret = ref('')
const secretVisible = ref(false)

const formatUsage = (value: number) => `$${value.toFixed(2)}`
const formatLimit = (value: number) => value > 0 ? formatUsage(value) : '不限'
const percentage = (usage: number, limit: number) => limit > 0 ? Math.min(100, Math.round((usage / limit) * 100)) : 0
const statusLabel = (status: string) => ({ active: '有效', disabled: '已停用', quota_exhausted: '额度用尽', expired: '已过期' }[status] || status)

async function load() {
  loading.value = true
  try {
    key.value = await enterpriseAPI.getCurrentKey()
  } catch {
    ElMessage.error('加载 API Key 失败，请稍后重试')
  } finally {
    loading.value = false
  }
}

async function handleMutationFailure(error: unknown, message: string) {
  if (isEnterpriseKeyMutationStateConflict(error)) {
    await load()
    ElMessage.warning('API Key 状态已更新，请确认后重试')
    return
  }
  ElMessage.error(message)
}

function showSecret(plaintext?: string, replayed = false) {
  clearSecret()
  if (replayed || !plaintext) return
  secret.value = plaintext
  secretVisible.value = true
}

function clearSecret() {
  secret.value = ''
  secretVisible.value = false
}

async function createKey() {
  if (mutating.value) return
  mutating.value = true
  try {
    const result = await enterpriseAPI.createKey()
    key.value = result.key
    showSecret(result.plaintext, result.replayed)
    if (result.replayed) ElMessage.warning('请求已处理，原明文无法再次显示')
    else ElMessage.success('API Key 已创建')
  } catch (error) {
    await handleMutationFailure(error, '创建 API Key 失败，请重试')
  } finally { mutating.value = false }
}

async function disableKey() {
  if (!key.value || mutating.value) return
  mutating.value = true
  try {
    await ElMessageBox.confirm('停用后该 Key 将立即无法调用，且不能恢复。', '停用 API Key', { type: 'warning' })
  } catch {
    mutating.value = false
    return
  }
  try {
    await enterpriseAPI.disableKey(key.value.id)
    key.value = null
    clearSecret()
    ElMessage.success('API Key 已停用')
  } catch (error) {
    await handleMutationFailure(error, '停用 API Key 失败，请重试')
  } finally { mutating.value = false }
}

async function rotateKey() {
  if (!key.value || mutating.value) return
  mutating.value = true
  try {
    await ElMessageBox.confirm('旧 Key 将立即停用，新 Key 会继承现有额度和窗口用量。', '轮换 API Key', { type: 'warning' })
  } catch {
    mutating.value = false
    return
  }
  try {
    const result = await enterpriseAPI.rotateKey(key.value.id)
    key.value = result.key
    showSecret(result.plaintext, result.replayed)
    if (result.replayed) ElMessage.warning('请求已处理，原明文无法再次显示')
    else ElMessage.success('API Key 已轮换')
  } catch (error) {
    await handleMutationFailure(error, '轮换 API Key 失败，请重试')
  } finally { mutating.value = false }
}

onBeforeRouteLeave(() => { clearSecret() })
onUnmounted(clearSecret)
onMounted(load)
</script>

<style scoped>
.workspace{min-width:0}.page-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:24px}.page-heading h1{margin:0 0 6px;font-size:26px}.page-heading p,.label,article span,article small{margin:0;color:#64748b}.actions{display:flex;gap:10px}.key-line{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:20px 0;border-top:1px solid #dce5e2;border-bottom:1px solid #dce5e2}.key-line>div{display:grid;gap:8px}.key-line code{font-size:16px}.metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:20px;padding-top:24px}.metrics article{display:grid;gap:10px;min-width:0;padding:18px;border:1px solid #dce5e2;border-radius:6px;background:#fff}.metrics strong{font-size:20px;overflow-wrap:anywhere}.metrics small{min-height:34px}.secret-input{margin-top:18px}.secret-input :deep(input){font-family:monospace}@media(max-width:900px){.metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:640px){.page-heading{flex-direction:column}.actions,.page-heading>.el-button{width:100%}.actions .el-button{flex:1}.metrics{grid-template-columns:1fr}}
</style>
