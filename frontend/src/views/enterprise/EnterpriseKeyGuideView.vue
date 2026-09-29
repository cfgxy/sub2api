<template>
  <section class="space-y-6">
    <header class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('enterprise.guide.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.guide.description') }}</p>
      </div>
      <RouterLink to="/enterprise/keys" class="btn btn-secondary btn-md self-start" data-testid="guide-back">
        <Icon name="arrowLeft" size="sm" />
        {{ t('enterprise.guide.back') }}
      </RouterLink>
    </header>

    <div
      v-if="!loading && loadError"
      class="card card-body flex flex-col gap-3 border-red-200 text-sm text-red-600 dark:border-red-900/50 dark:text-red-400 sm:flex-row sm:items-center sm:justify-between"
      role="alert"
      data-testid="guide-load-error"
    >
      <span>{{ t('enterprise.guide.loadError') }}</span>
      <button class="btn btn-secondary btn-sm self-start" data-testid="guide-load-retry" @click="load">
        {{ t('enterprise.common.retry') }}
      </button>
    </div>

    <div v-else-if="loading" class="flex justify-center py-16" data-testid="guide-loading">
      <LoadingSpinner size="lg" />
    </div>

    <EmptyState
      v-else-if="!key"
      :title="t('enterprise.guide.noKeyTitle')"
      :description="t('enterprise.guide.noKeyDescription')"
      data-testid="guide-no-key"
    >
      <template #action>
        <RouterLink to="/enterprise/keys" class="btn btn-primary btn-md">{{ t('enterprise.guide.noKeyAction') }}</RouterLink>
      </template>
    </EmptyState>

    <template v-else>
      <div class="card card-body space-y-3" data-testid="guide-key">
        <div class="flex flex-wrap items-center justify-between gap-4">
          <div class="min-w-0">
            <span class="block text-xs text-gray-500 dark:text-dark-400">{{ t('enterprise.guide.key.title') }}</span>
            <code class="mt-1 block break-all text-base text-gray-900 dark:text-white">{{ key.masked_key }}</code>
          </div>
          <div class="flex items-center gap-3">
            <StatusBadge :status="key.status" :label="keyStatusLabel(key.status, t)" />
            <button v-if="fullKey" class="btn btn-secondary btn-sm" data-testid="guide-copy-key" @click="copyKey">
              <Icon name="copy" size="sm" />
              {{ t('enterprise.guide.key.copy') }}
            </button>
          </div>
        </div>
        <p v-if="!fullKey" class="text-sm text-yellow-700 dark:text-yellow-500" data-testid="guide-key-unavailable">
          {{ t('enterprise.guide.key.unavailable') }}
        </p>
      </div>

      <div class="card card-body space-y-4" data-testid="guide-base-url">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.guide.baseUrl.title') }}</h2>
        <p v-if="!address" class="text-sm text-red-600 dark:text-red-400" role="alert" data-testid="guide-base-url-error">
          {{ t('enterprise.guide.baseUrl.loadError') }}
        </p>
        <template v-else>
          <div v-for="row in addressRows" :key="row.id" class="flex flex-wrap items-center justify-between gap-3">
            <div class="min-w-0">
              <span class="block text-xs text-gray-500 dark:text-dark-400">{{ row.label }}</span>
              <code class="mt-1 block break-all text-sm text-gray-900 dark:text-white" :data-testid="`guide-base-url-${row.id}`">{{ row.value }}</code>
            </div>
            <button class="btn btn-secondary btn-sm" :data-testid="`guide-copy-base-url-${row.id}`" @click="copyText(row.value, t('enterprise.guide.baseUrl.copied'))">
              <Icon name="copy" size="sm" />
              {{ t('enterprise.guide.baseUrl.copy') }}
            </button>
          </div>
        </template>
      </div>

      <div class="card card-body space-y-4" data-testid="guide-models">
        <div>
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.guide.models.title') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.guide.models.description') }}</p>
        </div>
        <div v-if="modelsLoading" class="flex justify-center py-6" data-testid="guide-models-loading">
          <LoadingSpinner size="md" />
        </div>
        <div
          v-else-if="modelsError"
          class="flex flex-col gap-3 text-sm text-red-600 dark:text-red-400 sm:flex-row sm:items-center sm:justify-between"
          role="alert"
          data-testid="guide-models-error"
        >
          <span>{{ t('enterprise.guide.models.loadError') }}</span>
          <button class="btn btn-secondary btn-sm self-start" data-testid="guide-models-retry" @click="loadModels">
            {{ t('enterprise.common.retry') }}
          </button>
        </div>
        <EmptyState
          v-else-if="models.length === 0"
          :title="t('enterprise.guide.models.empty')"
          :description="t('enterprise.guide.models.emptyHint')"
          data-testid="guide-models-empty"
        />
        <ul v-else class="grid gap-2 sm:grid-cols-2" role="radiogroup" data-testid="guide-model-list">
          <li v-for="model in models" :key="`${model.platform}:${model.id}`">
            <label
              class="flex cursor-pointer items-center justify-between gap-3 rounded-lg border px-3 py-2 text-sm transition-colors"
              :class="isSelected(model)
                ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/20 dark:text-primary-300'
                : 'border-gray-200 text-gray-700 hover:border-gray-300 dark:border-dark-700 dark:text-dark-200 dark:hover:border-dark-600'"
            >
              <span class="flex min-w-0 items-center gap-2">
                <input
                  type="radio"
                  class="h-4 w-4 flex-shrink-0 accent-primary-600"
                  name="guide-model"
                  :checked="isSelected(model)"
                  data-testid="guide-model-option"
                  @change="selected = model"
                />
                <code class="break-all">{{ model.id }}</code>
              </span>
              <span class="flex-shrink-0 text-xs text-gray-500 dark:text-dark-400">{{ platformLabel(model.platform, t) }}</span>
            </label>
          </li>
        </ul>
      </div>

      <div class="card card-body space-y-4" data-testid="guide-example">
        <div>
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.guide.example.title') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.guide.example.description', { placeholder: ENTERPRISE_KEY_PLACEHOLDER }) }}</p>
        </div>
        <p v-if="!selected || !address" class="text-sm text-gray-500 dark:text-dark-400" data-testid="guide-example-empty">
          {{ t('enterprise.guide.example.pickModel') }}
        </p>
        <template v-else>
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="flex gap-2" role="tablist">
              <button
                v-for="kind in exampleKinds"
                :key="kind"
                type="button"
                role="tab"
                class="btn btn-sm"
                :class="exampleKind === kind ? 'btn-primary' : 'btn-secondary'"
                :aria-selected="exampleKind === kind"
                :data-testid="`guide-example-tab-${kind}`"
                @click="exampleKind = kind"
              >
                {{ t(`enterprise.guide.example.${kind}`) }}
              </button>
            </div>
            <button class="btn btn-secondary btn-sm" data-testid="guide-copy-example" @click="copyText(exampleText, t('enterprise.guide.example.copied'))">
              <Icon name="copy" size="sm" />
              {{ t('enterprise.guide.example.copy') }}
            </button>
          </div>
          <pre class="overflow-x-auto rounded-lg bg-gray-50 p-4 text-xs text-gray-800 dark:bg-dark-800 dark:text-dark-100" data-testid="guide-example-code"><code>{{ exampleText }}</code></pre>
        </template>
      </div>

      <div class="card card-body space-y-4" data-testid="guide-import">
        <div>
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.guide.importTools.title') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.guide.importTools.description') }}</p>
        </div>
        <div class="flex flex-wrap gap-3">
          <button class="btn btn-primary btn-md" :disabled="!codexReady" data-testid="guide-import-codex" @click="importCodex">
            {{ t('enterprise.guide.importTools.codex') }}
          </button>
          <button class="btn btn-primary btn-md" :disabled="!ccsReady" data-testid="guide-import-ccs" @click="importCcs">
            {{ t('enterprise.guide.importTools.ccs') }}
          </button>
        </div>
        <p v-if="importHint" class="text-sm text-gray-500 dark:text-dark-400" data-testid="guide-import-hint">{{ importHint }}</p>
      </div>

      <div class="card card-body space-y-3" data-testid="guide-errors">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('enterprise.guide.errors.title') }}</h2>
        <dl class="space-y-3">
          <div v-for="item in errorItems" :key="item" class="text-sm">
            <dt class="font-medium text-gray-900 dark:text-white">
              {{ t(`enterprise.guide.errors.${item}.code`) }} · {{ t(`enterprise.guide.errors.${item}.meaning`) }}
            </dt>
            <dd class="mt-0.5 text-gray-500 dark:text-dark-400">{{ t(`enterprise.guide.errors.${item}.action`) }}</dd>
          </div>
        </dl>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import { getPublicSettings } from '@/api/auth'
import { enterpriseAPI, fetchEnterpriseGuideModels, type EnterpriseGuideModel } from '@/api/enterprise'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores/app'
import { keyStatusLabel, platformLabel } from '@/utils/enterpriseDisplay'
import {
  ENTERPRISE_KEY_PLACEHOLDER,
  buildEnterpriseCcsImportUri,
  buildEnterpriseCodexImportUri,
  buildEnterpriseGuideExample,
  canImportEnterpriseCcs,
  enterpriseImportBaseUrls,
  resolveEnterpriseCodexCandidate,
  type EnterpriseGuideExampleKind,
  type EnterpriseImportAddress,
} from '@/utils/enterpriseImport'
import type { EnterpriseEmployeeKey } from '@/types/enterprise'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const exampleKinds: EnterpriseGuideExampleKind[] = ['curl', 'python', 'javascript']
const errorItems = ['unauthorized', 'forbidden', 'rateLimited'] as const

const key = ref<EnterpriseEmployeeKey | null>(null)
const address = ref<EnterpriseImportAddress | null>(null)
const loading = ref(true)
const loadError = ref(false)
const models = ref<EnterpriseGuideModel[]>([])
const modelsLoading = ref(false)
const modelsError = ref(false)
const selected = ref<EnterpriseGuideModel | null>(null)
const exampleKind = ref<EnterpriseGuideExampleKind>('curl')

const MASKED_KEY = /\*|•|…|\.\.\./
const fullKey = computed(() => {
  const value = key.value?.key?.trim() ?? ''
  return value && !MASKED_KEY.test(value) ? value : ''
})

const addressRows = computed(() => address.value
  ? [
      { id: 'openai', label: t('enterprise.guide.baseUrl.openai'), value: address.value.openai },
      { id: 'anthropic', label: t('enterprise.guide.baseUrl.anthropic'), value: address.value.root },
    ]
  : [])

const exampleText = computed(() =>
  selected.value && address.value ? buildEnterpriseGuideExample(exampleKind.value, address.value, selected.value) : '')

const keyActive = computed(() => key.value?.status === 'active')
const codexCandidate = computed(() =>
  address.value ? resolveEnterpriseCodexCandidate(address.value, selected.value) : null)
const codexReady = computed(() => !!codexCandidate.value && !!fullKey.value && keyActive.value)
const ccsReady = computed(() => canImportEnterpriseCcs(selected.value) && !!address.value && !!fullKey.value && keyActive.value)

const importHint = computed(() => {
  if (!selected.value) return t('enterprise.guide.importTools.pickModel')
  if (!keyActive.value) return t('enterprise.guide.importTools.inactive')
  if (!fullKey.value) return t('enterprise.guide.importTools.needFullKey')
  if (selected.value.platform !== 'openai') {
    return canImportEnterpriseCcs(selected.value)
      ? t('enterprise.guide.importTools.codexOnlyOpenai')
      : t('enterprise.guide.importTools.ccsUnsupported')
  }
  return ''
})

const isSelected = (model: EnterpriseGuideModel) =>
  selected.value?.id === model.id && selected.value?.platform === model.platform

async function copyText(text: string, message: string) {
  await copyToClipboard(text, message)
}

async function copyKey() {
  if (fullKey.value) await copyToClipboard(fullKey.value, t('enterprise.guide.key.copied'))
}

function openImport(build: () => string) {
  try {
    window.open(build(), '_self')
  } catch {
    appStore.showError(t('enterprise.guide.importTools.failed'), 5000)
  }
}

function importCodex() {
  const candidate = codexCandidate.value
  if (!candidate || !codexReady.value) return
  openImport(() => buildEnterpriseCodexImportUri(candidate, fullKey.value))
}

function importCcs() {
  const model = selected.value
  if (!model || !address.value || !ccsReady.value) return
  const current = address.value
  openImport(() => buildEnterpriseCcsImportUri(current, model, fullKey.value))
}

async function loadAddress() {
  try {
    const settings = await getPublicSettings()
    address.value = enterpriseImportBaseUrls(settings.api_base_url?.trim() || window.location.origin)
  } catch {
    address.value = null
  }
}

async function loadModels() {
  modelsLoading.value = true
  modelsError.value = false
  try {
    models.value = await fetchEnterpriseGuideModels()
    selected.value = models.value[0] ?? null
  } catch {
    models.value = []
    selected.value = null
    modelsError.value = true
  } finally {
    modelsLoading.value = false
  }
}

async function load() {
  loading.value = true
  loadError.value = false
  try {
    key.value = await enterpriseAPI.getCurrentKey()
  } catch {
    key.value = null
    loadError.value = true
    loading.value = false
    return
  }
  loading.value = false
  if (!key.value) return
  await Promise.all([loadAddress(), loadModels()])
}

onMounted(load)
</script>
