<template>
  <section class="guide-page">
    <header class="guide-header">
      <div>
        <button class="back-link" type="button" data-testid="guide-back" @click="backToKeys">
          <ArrowLeft class="icon" aria-hidden="true" />
          API Key
        </button>
        <h1>API 接入指南</h1>
        <p>使用企业 API Key 配置地址、模型并发起第一次请求。</p>
      </div>
    </header>

    <ol class="guide-steps" aria-label="接入步骤">
      <li class="guide-step is-complete"><span>1</span>创建密钥</li>
      <li class="guide-step is-current"><span>2</span>配置地址与模型</li>
      <li class="guide-step"><span>3</span>发起请求</li>
    </ol>

    <section class="guide-section" data-testid="base-url-section">
      <div class="section-heading">
        <div>
          <p class="eyebrow">步骤 2</p>
          <h2>配置 Base URL</h2>
          <p>示例和请求地址始终使用当前部署配置。</p>
        </div>
        <button
          v-if="baseState === 'ready'"
          class="icon-button"
          type="button"
          aria-label="复制 Base URL"
          title="复制 Base URL"
          data-testid="copy-base-url"
          @click="copyValue(baseUrl, 'Base URL 已复制')"
        >
          <CopyDocument aria-hidden="true" />
        </button>
      </div>

      <div v-if="baseState === 'loading'" class="loading-line" data-testid="base-url-loading">正在读取接入地址…</div>
      <div v-else-if="baseState === 'error'" class="state-message is-error" data-testid="base-url-error">
        <span>接入地址暂时无法获取，请稍后重试。</span>
        <button class="text-button" type="button" data-testid="retry-base-url" @click="loadGuide">重试</button>
      </div>
      <div v-else class="value-line" data-testid="base-url-value">
        <code>{{ baseUrl }}</code>
      </div>
    </section>

    <section class="guide-section" data-testid="models-section">
      <div class="section-heading">
        <div>
          <p class="eyebrow">步骤 2</p>
          <h2>选择可用模型</h2>
          <p>按当前企业会话、订阅权限与可调度账号显示支持示例调用的模型。</p>
        </div>
        <button
          v-if="modelState === 'error'"
          class="icon-button"
          type="button"
          aria-label="重新加载模型"
          title="重新加载模型"
          data-testid="retry-models-icon"
          @click="loadModels"
        >
          <Refresh aria-hidden="true" />
        </button>
      </div>

      <div v-if="modelState === 'loading'" class="model-list" data-testid="models-loading" aria-label="正在加载模型">
        <span v-for="index in 3" :key="index" class="model-skeleton" aria-hidden="true"></span>
      </div>
      <div v-else-if="modelState === 'error'" class="state-message is-error" data-testid="models-error">
        <span>{{ modelErrorMessage }}</span>
        <button class="text-button" type="button" data-testid="retry-models" @click="loadModels">重试</button>
      </div>
      <div v-else-if="modelState === 'empty'" class="state-message" data-testid="models-empty">
        <span>当前暂无可调用的示例模型。</span>
        <span class="muted">请确认密钥状态与订阅，或联系企业管理员检查账号配置。</span>
      </div>
      <div v-else class="model-list" data-testid="models-list" role="radiogroup" aria-label="选择示例模型">
        <div
          v-for="model in models"
          :key="model.id"
          class="model-item"
          :class="{ 'is-selected': selectedModel === model.id }"
          role="radio"
          tabindex="0"
          :aria-checked="selectedModel === model.id"
          @click="selectedModel = model.id"
          @keydown.enter="selectedModel = model.id"
          @keydown.space.prevent="selectedModel = model.id"
        >
          <span class="model-copy">
            <code>{{ model.id }}</code>
            <small v-if="model.description || model.display_name || model.owned_by">{{ model.description || model.display_name || model.owned_by }}</small>
          </span>
          <span class="model-actions">
            <span v-if="selectedModel === model.id" class="selected-label">当前示例</span>
            <button
              class="copy-model"
              type="button"
              :aria-label="`复制模型 ID ${model.id}`"
              title="复制模型 ID"
              @click.stop="copyValue(model.id, '模型 ID 已复制')"
            >
              <CopyDocument aria-hidden="true" />
            </button>
          </span>
        </div>
      </div>
    </section>

    <section class="guide-section examples-section" data-testid="examples-section">
      <div class="section-heading">
        <div>
          <p class="eyebrow">步骤 3</p>
          <h2>发起请求</h2>
          <p>将示例中的占位符替换为你刚刚复制的 API Key 和模型 ID。</p>
        </div>
      </div>

      <div class="example-tabs" role="tablist" aria-label="调用示例">
        <button
          v-for="tab in exampleTabs"
          :key="tab.id"
          class="example-tab"
          :class="{ 'is-active': activeExample === tab.id }"
          type="button"
          role="tab"
          :aria-selected="activeExample === tab.id"
          @click="activeExample = tab.id"
        >
          {{ tab.label }}
        </button>
      </div>
      <div class="code-panel">
        <div class="code-toolbar">
          <span>{{ activeExampleLabel }}</span>
          <button class="code-copy" type="button" data-testid="copy-example" :disabled="baseState !== 'ready' || modelState !== 'ready'" @click="copyValue(activeExampleCode, '示例已复制')">
            <CopyDocument aria-hidden="true" />
            复制示例
          </button>
        </div>
        <pre><code>{{ activeExampleCode }}</code></pre>
      </div>
    </section>

    <section class="guide-section errors-section" data-testid="errors-section">
      <div class="section-heading">
        <div>
          <h2>常见错误</h2>
          <p>先确认密钥状态、订阅额度和请求频率，再重试。</p>
        </div>
      </div>
      <div class="error-table" role="table" aria-label="常见错误码">
        <div v-for="item in errorItems" :key="item.code" class="error-row" role="row">
          <code>{{ item.code }}</code>
          <span>{{ item.meaning }}</span>
          <small>{{ item.action }}</small>
        </div>
      </div>
    </section>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowLeft, CopyDocument, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { getPublicSettings } from '@/api/auth'
import {
  fetchEnterpriseGuideModels,
  type EnterpriseGuideModel,
} from '@/api/enterprise'
import { enterpriseImportBaseUrls } from './enterpriseImport'

type ExampleId = 'curl' | 'python' | 'javascript'
type LoadState = 'loading' | 'ready' | 'empty' | 'error'

const router = useRouter()
const baseState = ref<Exclude<LoadState, 'empty'>>('loading')
const baseUrl = ref('')
const models = ref<EnterpriseGuideModel[]>([])
const selectedModel = ref('')
const modelState = ref<LoadState>('loading')
const modelErrorStatus = ref(0)
const activeExample = ref<ExampleId>('curl')
let modelController: AbortController | null = null
let modelRequestId = 0
let baseRequestId = 0

const exampleTabs: Array<{ id: ExampleId; label: string }> = [
  { id: 'curl', label: 'curl' },
  { id: 'python', label: 'Python' },
  { id: 'javascript', label: 'JavaScript' },
]

const errorItems = [
  { code: '401', meaning: '密钥无效或已停用', action: '回到 API Key 页面确认状态，无法找回完整密钥时请轮换。' },
  { code: '403', meaning: '权限或计费条件不满足', action: '确认企业订阅、员工额度与模型权限，必要时联系企业管理员。' },
  { code: '429', meaning: '频率或窗口用量已达上限', action: '按响应中的 Retry-After 或窗口重置时间等待后再试。' },
]

const activeExampleLabel = computed(() => exampleTabs.find((tab) => tab.id === activeExample.value)?.label || 'curl')
const modelErrorMessage = computed(() => modelErrorStatus.value === 401 || modelErrorStatus.value === 403
  ? '企业会话无权读取模型，请重新登录并确认员工权限。'
  : '模型清单暂时无法加载，请稍后重试。')
const selectedModelId = computed(() => selectedModel.value || models.value[0]?.id || '{{MODEL_ID}}')
const exampleBaseUrl = computed(() => baseUrl.value || '{{API_BASE_URL}}')
const shellQuote = (value: string) => `'${value.replace(/'/g, "'\\''")}'`
const activeExampleCode = computed(() => {
  const endpoint = `${exampleBaseUrl.value}/chat/completions`
  const model = JSON.stringify(selectedModelId.value)
  if (activeExample.value === 'python') {
    return `import json\nfrom urllib.request import Request, urlopen\n\nrequest = Request(\n    ${JSON.stringify(endpoint)},\n    data=json.dumps({\n        'model': ${model},\n        'messages': [{'role': 'user', 'content': 'Hello'}],\n    }).encode('utf-8'),\n    headers={\n        'Content-Type': 'application/json',\n        'Authorization': 'Bearer {{API_KEY}}',\n    },\n    method='POST',\n)\nwith urlopen(request, timeout=30) as response:\n    print(json.load(response))`
  }
  if (activeExample.value === 'javascript') {
    return `const response = await fetch(${JSON.stringify(endpoint)}, {\n  method: 'POST',\n  headers: {\n    'Content-Type': 'application/json',\n    Authorization: 'Bearer {{API_KEY}}',\n  },\n  body: JSON.stringify({\n    model: ${model},\n    messages: [{ role: 'user', content: 'Hello' }],\n  }),\n});\nif (!response.ok) throw new Error(\`HTTP \${response.status}\`);\nconsole.log(await response.json());`
  }
  const payload = `{"model":${model},"messages":[{"role":"user","content":"Hello"}]}`
  return `curl ${shellQuote(endpoint)} \\\n  -H 'Content-Type: application/json' \\\n  -H 'Authorization: Bearer {{API_KEY}}' \\\n  -d ${shellQuote(payload)}`
})

function readStatus(error: unknown): number {
  if (typeof error === 'object' && error !== null && 'status' in error && typeof error.status === 'number') return error.status
  if (typeof error === 'object' && error !== null && 'response' in error && typeof error.response === 'object' && error.response !== null &&
    'status' in error.response && typeof error.response.status === 'number') return error.response.status
  return 0
}

async function loadModels() {
  modelController?.abort()
  modelController = new AbortController()
  const requestId = ++modelRequestId
  modelState.value = 'loading'
  modelErrorStatus.value = 0
  models.value = []
  selectedModel.value = ''

  try {
    const response = await fetchEnterpriseGuideModels(modelController.signal)
    if (requestId !== modelRequestId) return
    models.value = response
    modelState.value = response.length ? 'ready' : 'empty'
    selectedModel.value = response[0]?.id || ''
  } catch (error) {
    if (requestId !== modelRequestId || (typeof error === 'object' && error !== null && 'name' in error && error.name === 'AbortError')) return
    modelErrorStatus.value = readStatus(error)
    modelState.value = 'error'
  }
}

async function loadGuide() {
  const requestId = ++baseRequestId
  baseState.value = 'loading'
  baseUrl.value = ''
  try {
    const settings = await getPublicSettings()
    if (requestId !== baseRequestId) return
    baseUrl.value = enterpriseImportBaseUrls(settings.api_base_url).openai
    baseState.value = 'ready'
    await loadModels()
  } catch {
    if (requestId !== baseRequestId) return
    baseState.value = 'error'
    modelController?.abort()
    modelRequestId += 1
    modelState.value = 'error'
    modelErrorStatus.value = 0
  }
}

async function copyValue(value: string, successMessage: string) {
  if (!value) return
  try {
    await navigator.clipboard.writeText(value)
    ElMessage.success(successMessage)
  } catch {
    ElMessage.warning('复制失败，请手动复制')
  }
}

function backToKeys() {
  void router.push({ name: 'EnterpriseKeys' })
}

onMounted(loadGuide)
onUnmounted(() => {
  modelController?.abort()
})
</script>

<style scoped>
.guide-page{max-width:1120px;margin:0 auto;color:#0f172a}.guide-header{display:flex;justify-content:space-between;gap:24px;margin-bottom:24px}.back-link{display:inline-flex;align-items:center;gap:6px;margin-bottom:12px;padding:0;color:#0f766e;font-size:14px;font-weight:600;background:transparent;border:0;cursor:pointer}.back-link:hover{color:#115e59}.icon{width:16px;height:16px}.guide-header h1{margin:0;font-size:26px;line-height:1.25}.guide-header p,.section-heading p{margin:8px 0 0;color:#64748b;font-size:14px;line-height:1.5}.guide-steps{display:flex;gap:0;margin:0 0 24px;padding:0;list-style:none;color:#64748b;font-size:14px}.guide-step{display:flex;align-items:center;gap:8px;flex:1;min-width:0}.guide-step:not(:last-child)::after{content:'';height:1px;flex:1;margin:0 16px;background:#cbd5e1}.guide-step span{display:inline-flex;align-items:center;justify-content:center;width:24px;height:24px;border:1px solid #cbd5e1;border-radius:50%;font-size:12px}.guide-step.is-complete,.guide-step.is-current{color:#0f766e;font-weight:600}.guide-step.is-complete span,.guide-step.is-current span{border-color:#0f766e;color:#0f766e}.guide-section{padding:24px 0;border-top:1px solid #dce5e2}.section-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:16px}.section-heading h2{margin:0;font-size:18px;line-height:1.4}.eyebrow{margin:0 0 4px!important;color:#0f766e!important;font-size:12px!important;font-weight:700;letter-spacing:0}.value-line{display:flex;align-items:center;min-height:48px;padding:12px 14px;border:1px solid #cbd5e1;border-radius:6px;background:#f8fafc;overflow-x:auto}.value-line code{color:#0f172a;font:500 14px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:nowrap}.icon-button{display:inline-flex;align-items:center;justify-content:center;width:36px;height:36px;border:1px solid #cbd5e1;border-radius:6px;background:#fff;color:#0f766e;cursor:pointer}.icon-button:hover{background:#f0fdfa}.icon-button :deep(svg),.copy-model :deep(svg),.code-copy :deep(svg){width:16px;height:16px}.loading-line,.state-message{display:flex;align-items:center;justify-content:space-between;gap:12px;min-height:48px;padding:12px 14px;border:1px solid #cbd5e1;border-radius:6px;color:#64748b;font-size:14px}.state-message.is-error{border-color:#fecaca;background:#fff7f7;color:#b42318}.state-message .muted{color:#64748b}.text-button{padding:0;border:0;background:transparent;color:#0f766e;font-weight:600;cursor:pointer}.text-button:hover{text-decoration:underline}.model-list{display:grid;gap:8px}.model-item{display:flex;align-items:center;justify-content:space-between;gap:16px;width:100%;padding:12px 14px;border:1px solid #cbd5e1;border-radius:6px;background:#fff;text-align:left;cursor:pointer}.model-item:hover,.model-item.is-selected{border-color:#14b8a6;background:#f0fdfa}.model-copy{display:grid;gap:4px;min-width:0}.model-copy code{overflow-wrap:anywhere;color:#0f172a;font:600 14px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace}.model-copy small{color:#64748b;font-size:12px}.model-actions{display:flex;align-items:center;gap:12px;flex-shrink:0}.selected-label{color:#0f766e;font-size:12px}.copy-model{display:inline-flex;padding:6px;color:#64748b;cursor:pointer}.copy-model:hover{color:#0f766e}.model-skeleton{height:52px;border-radius:6px;background:linear-gradient(90deg,#f1f5f9 25%,#e2e8f0 37%,#f1f5f9 63%);background-size:400% 100%;animation:loading 1.4s ease infinite}@keyframes loading{0%{background-position:100% 0}100%{background-position:-100% 0}}.example-tabs{display:flex;gap:4px;overflow-x:auto;margin-bottom:12px;border-bottom:1px solid #cbd5e1}.example-tab{padding:8px 12px;border:0;border-bottom:2px solid transparent;background:transparent;color:#64748b;font-size:14px;white-space:nowrap;cursor:pointer}.example-tab.is-active{border-bottom-color:#0f766e;color:#0f766e;font-weight:600}.code-panel{overflow:hidden;border-radius:6px;background:#0f172a;color:#e2e8f0}.code-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:10px 14px;border-bottom:1px solid #334155;color:#cbd5e1;font-size:12px}.code-copy{display:inline-flex;align-items:center;gap:6px;padding:6px 9px;border:0;border-radius:4px;background:#334155;color:#f8fafc;font-size:12px;cursor:pointer}.code-copy:hover{background:#475569}.code-panel pre{margin:0;max-height:360px;overflow:auto;padding:16px}.code-panel code{font:13px/1.65 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre}.error-table{display:grid;border-top:1px solid #e2e8f0}.error-row{display:grid;grid-template-columns:64px minmax(160px,1fr) minmax(240px,2fr);gap:16px;align-items:center;padding:14px 0;border-bottom:1px solid #e2e8f0;font-size:14px}.error-row code{color:#b42318;font-weight:700}.error-row small{color:#64748b;font-size:13px}@media(max-width:640px){.guide-page{width:100%}.guide-steps{display:grid;gap:10px}.guide-step:not(:last-child)::after{display:none}.section-heading{gap:8px}.model-item{align-items:flex-start}.model-actions{flex-direction:column;gap:4px}.error-row{grid-template-columns:48px 1fr;gap:8px}.error-row small{grid-column:2}.code-panel pre{padding:12px}.code-panel code{font-size:12px}}
:global(.dark) .guide-page{color:#f8fafc}:global(.dark) .guide-step:not(:last-child)::after{background:#475569}:global(.dark) .guide-step span{border-color:#475569}:global(.dark) .guide-header p,:global(.dark) .section-heading p,:global(.dark) .model-copy small,:global(.dark) .error-row small,:global(.dark) .loading-line,:global(.dark) .state-message .muted{color:#94a3b8}:global(.dark) .guide-section{border-color:#334155}:global(.dark) .value-line,:global(.dark) .model-item,:global(.dark) .icon-button{border-color:#475569;background:#1e293b;color:#f8fafc}:global(.dark) .model-item:hover,:global(.dark) .model-item.is-selected{border-color:#14b8a6;background:#134e4a}:global(.dark) .value-line code,:global(.dark) .model-copy code{color:#f8fafc}:global(.dark) .model-skeleton{background:linear-gradient(90deg,#1e293b 25%,#334155 37%,#1e293b 63%);background-size:400% 100%}:global(.dark) .error-table,:global(.dark) .error-row{border-color:#334155}:global(.dark) .example-tabs{border-color:#475569}
.code-copy:disabled{opacity:.45;cursor:not-allowed}
</style>
