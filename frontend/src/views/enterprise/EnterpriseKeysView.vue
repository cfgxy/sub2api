<template>
  <section class="workspace">
    <div class="page-heading">
      <div>
        <h1>API Key</h1>
        <p>查看本人密钥状态、额度和当前计费窗口。</p>
      </div>
      <el-button v-if="!key" type="primary" :icon="Plus" :loading="mutating" data-testid="create-key" data-tour="enterprise-key-create" @click="createKey">创建 API Key</el-button>
      <div v-else class="actions">
        <el-button :icon="Monitor" data-testid="developer-tools" @click="openDeveloperTools">开发工具</el-button>
        <el-button :icon="Refresh" :loading="mutating" data-testid="rotate-key" @click="rotateKey">轮换</el-button>
        <el-button type="danger" plain :icon="CircleClose" :loading="mutating" data-testid="disable-key" @click="disableKey">停用</el-button>
      </div>
      <el-button link type="primary" data-testid="open-guide" data-tour="enterprise-key-guide" @click="openGuide">接入指南</el-button>
    </div>

    <div v-loading="loading">
      <div v-if="!loading && loadError" data-testid="keys-load-error" class="field-error">
        API Key 信息暂时不可用，请稍后重试。
        <el-button link type="primary" data-testid="keys-load-retry" @click="load">重新加载</el-button>
      </div>
      <el-empty v-else-if="!loading && !key" description="还没有 API 密钥" data-testid="keys-empty">
        <el-button type="primary" :icon="Plus" data-testid="empty-create-key" @click="createKey">创建 API Key</el-button>
        <el-button link type="primary" data-testid="empty-open-guide" @click="openGuide">查看接入指南</el-button>
      </el-empty>
      <template v-if="key">
        <div class="key-line">
          <div data-testid="current-key"><span class="label">当前 Key</span><code>{{ key.masked_key }}</code></div>
          <el-button v-if="key.key" :icon="CopyDocument" aria-label="复制当前 API Key" title="复制当前 API Key" data-testid="copy-current-key" @click="copyCurrentKey" />
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

    <el-dialog v-model="secretVisible" title="API Key 已创建" width="min(560px, 92vw)" :close-on-click-modal="false" :teleported="false" @close="clearSecret">
      <div data-tour="enterprise-key-secret">
        <el-alert type="success" :closable="false" title="已创建 API Key，可在本页随时复制。" />
        <div class="secret-row">
          <el-input class="secret-input" :model-value="secret" readonly data-testid="plaintext-key" />
          <el-button :icon="CopyDocument" aria-label="复制 API Key" title="复制 API Key" data-testid="copy-secret" @click="copySecret" />
        </div>
        <p class="secret-warning">密钥在页面上以掩码显示，复制按钮会复制完整值。</p>
      </div>
      <template #footer>
        <div class="dialog-actions">
          <el-button link type="primary" data-testid="dialog-open-guide" @click="openGuide">去接入指南</el-button>
          <el-button type="primary" data-testid="close-secret" @click="clearSecret">关闭</el-button>
        </div>
      </template>
    </el-dialog>

    <el-dialog v-model="toolsVisible" title="开发工具" width="min(560px, 92vw)" :teleported="false" @close="resetTools">
      <div class="tool-tabs" role="tablist" aria-label="开发工具">
        <button type="button" role="tab" :aria-selected="activeTool === 'codex'" data-testid="codex-tab" @click="activeTool = 'codex'">Codex++</button>
        <button v-if="showCcsTool" type="button" role="tab" :aria-selected="activeTool === 'ccs'" data-testid="ccs-tab" @click="activeTool = 'ccs'">CCSwitch</button>
      </div>
      <div class="tool-step">
        <span>01</span>
        <div><h3>安装 {{ activeTool === 'codex' ? 'Codex++' : 'CCSwitch' }}</h3>
          <a :href="activeTool === 'codex' ? 'https://github.com/cfgxy/CodexPlusPlus' : 'https://github.com/farion1231/cc-switch'" target="_blank" rel="noopener noreferrer">下载工具</a>
        </div>
      </div>
      <div class="tool-step">
        <span>02</span>
        <div class="tool-import"><h3>导入当前密钥</h3>
          <p v-if="toolsLoading">正在加载可用模型与接入地址…</p>
          <p v-else-if="toolsError">{{ toolsError }} <el-button link type="primary" @click="loadTools">重试</el-button></p>
          <template v-else>
            <el-select v-model="selectedToolModel" data-testid="tool-models" aria-label="选择模型" placeholder="选择可用模型" class="tool-model-select">
              <el-option v-for="model in toolModels" :key="model.id" :label="model.id" :value="model.id" />
            </el-select>
            <p v-if="!toolModels.length">当前无可导入的 OpenAI 或 Anthropic 模型，请联系管理员检查订阅与账号。</p>
            <el-button v-if="activeTool === 'codex'" type="primary" :disabled="!canImport" data-testid="codex-import" @click="importTool('codex')">导入到 Codex++</el-button>
            <el-button v-else type="primary" :disabled="!canImport" data-testid="ccs-import" @click="importTool('ccs')">导入到 CCSwitch</el-button>
          </template>
        </div>
      </div>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { onBeforeRouteLeave, useRouter } from 'vue-router'
import { driver, type Driver } from 'driver.js'
import 'driver.js/dist/driver.css'
import { CircleClose, CopyDocument, Monitor, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getPublicSettings } from '@/api/auth'
import { enterpriseAPI, fetchEnterpriseGuideModels, isEnterpriseKeyMutationStateConflict, type EnterpriseGuideModel } from '@/api/enterprise'
import type { EnterpriseEmployeeKey } from '@/types/enterprise'
import { buildEnterpriseCcsImportUri, buildEnterpriseCodexImportUri, enterpriseImportBaseUrls, type EnterpriseImportAddress, type EnterpriseImportPlatform } from './enterpriseImport'

const router = useRouter()
const key = ref<EnterpriseEmployeeKey | null>(null)
const loading = ref(false)
const loadError = ref(false)
const mutating = ref(false)
const secret = ref('')
const secretVisible = ref(false)
const toolsVisible = ref(false)
const toolsLoading = ref(false)
const toolsError = ref('')
const toolModels = ref<EnterpriseGuideModel[]>([])
const selectedToolModel = ref('')
const toolAddress = ref<EnterpriseImportAddress | null>(null)
const showCcsTool = ref(true)
const activeTool = ref<'codex' | 'ccs'>('codex')
let guideTour: Driver | null = null
let toolRequest = 0

const selectedModel = computed(() => toolModels.value.find((model) => model.id === selectedToolModel.value))
const canImport = computed(() => !!toolAddress.value && !!selectedModel.value && !!key.value?.key && key.value.status === 'active')

function guideTourStorageKey(): string {
  try {
    const principal = JSON.parse(localStorage.getItem('enterprise_principal') || '{}') as {
      enterprise_id?: number
      principal_id?: number
    }
    if (principal.enterprise_id && principal.principal_id) {
      return `enterprise-key-guide-seen:${principal.enterprise_id}:${principal.principal_id}`
    }
  } catch {
    // 首访标记不是认证状态，损坏时仍显示一次引导。
  }
  return 'enterprise-key-guide-seen'
}

const formatUsage = (value: number) => `$${value.toFixed(2)}`
const formatLimit = (value: number) => value > 0 ? formatUsage(value) : '不限'
const percentage = (usage: number, limit: number) => limit > 0 ? Math.min(100, Math.round((usage / limit) * 100)) : 0
const statusLabel = (status: string) => ({ active: '有效', disabled: '已停用', quota_exhausted: '额度用尽', expired: '已过期' }[status] || status)

async function load() {
  loading.value = true
  loadError.value = false
  try {
    key.value = await enterpriseAPI.getCurrentKey()
  } catch {
    loadError.value = true
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
  if (key.value) key.value.key = plaintext
  secret.value = plaintext
  secretVisible.value = true
}

function clearSecret() {
  secret.value = ''
  secretVisible.value = false
}

async function copySecret() {
  if (!secret.value) return
  try {
    await navigator.clipboard.writeText(secret.value)
    ElMessage.success('已复制')
    if (guideTour?.getActiveIndex() === 1) {
      guideTour.moveNext()
    }
  } catch {
    ElMessage.warning('复制失败，请手动复制')
  }
}

async function copyCurrentKey() {
  if (!key.value?.key) return
  try {
    await navigator.clipboard.writeText(key.value.key)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败，请重试')
  }
}

async function loadTools() {
  const request = ++toolRequest
  toolsLoading.value = true
  toolsError.value = ''
  toolAddress.value = null
  toolModels.value = []
  try {
    const [settings, models] = await Promise.all([getPublicSettings(), fetchEnterpriseGuideModels()])
    if (request !== toolRequest) return
    toolAddress.value = enterpriseImportBaseUrls(settings.api_base_url?.trim() || window.location.origin)
    showCcsTool.value = !settings.hide_ccs_import_button
    toolModels.value = models.filter((model) => model.platform === 'openai' || model.platform === 'anthropic')
    selectedToolModel.value = toolModels.value[0]?.id || ''
    if (!showCcsTool.value) activeTool.value = 'codex'
  } catch {
    if (request === toolRequest) toolsError.value = '接入配置暂时无法获取，请重试。'
  } finally {
    if (request === toolRequest) toolsLoading.value = false
  }
}

function openDeveloperTools() {
  toolsVisible.value = true
  activeTool.value = 'codex'
  void loadTools()
}

function resetTools() {
  ++toolRequest
  toolsVisible.value = false
  toolAddress.value = null
  toolModels.value = []
  selectedToolModel.value = ''
}

function importTool(tool: 'codex' | 'ccs') {
  const model = selectedModel.value
  const address = toolAddress.value
  const current = key.value
  if (!current?.key || current.status !== 'active' || !address || !model ||
      (model.platform !== 'openai' && model.platform !== 'anthropic')) return
  const input = { address, key: current.key, platform: model.platform as EnterpriseImportPlatform, model: model.id, name: `Sub2API enterprise / Key #${current.id}` }
  try {
    const uri = tool === 'codex' ? buildEnterpriseCodexImportUri(input) : buildEnterpriseCcsImportUri(input)
    window.open(uri, '_self')
    ElMessage.info(`已请求打开 ${tool === 'codex' ? 'Codex++' : 'CCSwitch'}，请在应用中确认导入。`)
  } catch {
    ElMessage.warning('无法打开导入，请检查接入配置及工具安装状态。')
  }
}

function openGuide() {
  void router.push({ name: 'EnterpriseKeyGuide' }).catch(() => {
    clearSecret()
  })
}

function finishGuideTour() {
  localStorage.setItem(guideTourStorageKey(), 'true')
  guideTour?.destroy()
  guideTour = null
}

async function startGuideTour() {
  if (localStorage.getItem(guideTourStorageKey()) === 'true') return
  await nextTick()
  const createButton = document.querySelector('[data-tour="enterprise-key-create"]')
  const guideButton = document.querySelector('[data-tour="enterprise-key-guide"]')
  if (!guideButton) return

  const steps = createButton
    ? [
        { element: '[data-tour="enterprise-key-create"]', popover: { title: '创建密钥', description: '先创建一个用于调用 API 的密钥。' } },
        { element: '[data-tour="enterprise-key-secret"]', popover: { title: '复制密钥', description: '先复制密钥，再到接入指南发起第一次请求。' } },
        { element: '[data-tour="enterprise-key-guide"]', popover: { title: '打开接入指南', description: '在指南中查看地址、模型和调用示例。' } },
      ]
    : [{ element: '[data-tour="enterprise-key-guide"]', popover: { title: '打开接入指南', description: '在指南中查看地址、模型和调用示例。' } }]

  guideTour = driver({
    steps,
    showProgress: true,
    allowClose: true,
    nextBtnText: '下一步',
    prevBtnText: '上一步',
    doneBtnText: '完成',
    onNextClick: () => {
      const activeIndex = guideTour?.getActiveIndex() ?? 0
      if (createButton && activeIndex === 0 && !secretVisible.value) {
        ElMessage.info('请先点击创建 API Key')
        return
      }
      if (createButton && activeIndex === 1 && secretVisible.value) {
        ElMessage.info('请先复制 API Key，再继续')
        return
      }
      if (activeIndex >= steps.length - 1) finishGuideTour()
      else guideTour?.moveNext()
    },
    onCloseClick: finishGuideTour,
  })
  guideTour.drive()
}

async function createKey() {
  if (mutating.value) return
  mutating.value = true
  try {
    const result = await enterpriseAPI.createKey()
    key.value = result.key
    showSecret(result.plaintext, result.replayed)
    if (result.replayed) {
      await load()
      ElMessage.info('请求已处理，可在本页复制完整密钥')
    }
    else {
      ElMessage.success('API Key 已创建')
      if (guideTour?.getActiveIndex() === 0) {
        await nextTick()
        guideTour.moveNext()
      }
    }
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
    if (result.replayed) {
      await load()
      ElMessage.info('请求已处理，可在本页复制完整密钥')
    }
    else ElMessage.success('API Key 已轮换')
  } catch (error) {
    await handleMutationFailure(error, '轮换 API Key 失败，请重试')
  } finally { mutating.value = false }
}

onBeforeRouteLeave(() => {
  clearSecret()
  resetTools()
  guideTour?.destroy()
  guideTour = null
})
onUnmounted(() => {
  clearSecret()
  resetTools()
  guideTour?.destroy()
  guideTour = null
})
onMounted(async () => {
  await load()
  void startGuideTour()
})
</script>

<style scoped>
.workspace{min-width:0}.field-error{display:flex;align-items:center;gap:8px;padding:14px 16px;border:1px solid #f5c2c7;border-radius:6px;background:#fdf2f2;color:#b42318;font-size:14px}.page-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:24px}.page-heading h1{margin:0 0 6px;font-size:26px}.page-heading p,.label,article span,article small{margin:0;color:#64748b}.actions{display:flex;gap:10px}.key-line{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:20px 0;border-top:1px solid #dce5e2;border-bottom:1px solid #dce5e2}.key-line>div{display:grid;gap:8px}.key-line code{font-size:16px}.metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:20px;padding-top:24px}.metrics article{display:grid;gap:10px;min-width:0;padding:18px;border:1px solid #dce5e2;border-radius:6px;background:#fff}.metrics strong{font-size:20px;overflow-wrap:anywhere}.metrics small{min-height:34px}.secret-row{display:flex;align-items:center;gap:8px}.secret-input{margin-top:18px;min-width:0;flex:1}.secret-input :deep(input){font-family:monospace}.secret-warning{margin:10px 0 0;color:#64748b;font-size:13px}.dialog-actions{display:flex;align-items:center;justify-content:space-between;gap:12px}@media(max-width:900px){.metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:640px){.page-heading{flex-direction:column}.actions,.page-heading>.el-button{width:100%}.actions .el-button{flex:1}.metrics{grid-template-columns:1fr}.dialog-actions{flex-wrap:wrap}.dialog-actions .el-button{flex:1}}
.tool-tabs{display:flex;gap:24px;border-bottom:1px solid #dce5e2}.tool-tabs button{padding:10px 2px;border:0;border-bottom:2px solid transparent;background:none;color:#64748b;cursor:pointer}.tool-tabs button[aria-selected="true"]{border-color:#187b61;color:#146b55;font-weight:600}.tool-step{display:grid;grid-template-columns:28px minmax(0,1fr);gap:12px;padding:20px 0;border-bottom:1px solid #e5ecea}.tool-step:last-child{border-bottom:0}.tool-step>span{font-family:monospace;color:#83928f}.tool-step h3{margin:0 0 10px;font-size:14px;font-weight:600}.tool-step p{color:#64748b;font-size:13px}.tool-step a{color:#146b55}.tool-import{display:grid;justify-items:start;gap:8px;min-width:0}.tool-model-select{width:min(100%,320px)}
</style>
