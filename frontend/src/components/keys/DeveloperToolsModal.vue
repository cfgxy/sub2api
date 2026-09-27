<template>
  <BaseDialog :show="show" :title="t('keys.developerTools.title')" width="wide" @close="emit('close')">
    <div class="space-y-5">
      <div class="border-b border-gray-200 dark:border-dark-700" role="tablist" :aria-label="t('keys.developerTools.title')">
        <div class="flex gap-5">
          <button
            type="button"
            role="tab"
            data-testid="codex-tab"
            :aria-selected="activeTab === 'codex'"
            :class="['border-b-2 px-1 py-2.5 text-sm font-medium transition-colors',
              activeTab === 'codex' ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400']"
            @click="activeTab = 'codex'"
          >Codex++</button>
          <button
            v-if="canImportCcs"
            type="button"
            role="tab"
            data-testid="ccs-tab"
            :aria-selected="activeTab === 'ccs'"
            :class="['border-b-2 px-1 py-2.5 text-sm font-medium transition-colors',
              activeTab === 'ccs' ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400']"
            @click="activeTab = 'ccs'"
          >CCSwitch</button>
        </div>
      </div>

      <div class="space-y-5">
        <section class="flex gap-4 border-b border-gray-200 pb-5 dark:border-dark-700">
          <span class="w-6 flex-none font-mono text-sm text-gray-400">01</span>
          <div class="min-w-0 flex-1 space-y-3">
            <h3 class="text-sm font-medium text-gray-900 dark:text-white">{{ t('keys.developerTools.download') }}</h3>
            <a
              :href="activeTab === 'codex' ? 'https://github.com/cfgxy/CodexPlusPlus' : 'https://github.com/farion1231/cc-switch'"
              target="_blank"
              rel="noopener noreferrer"
              class="btn btn-secondary inline-flex items-center gap-2 text-sm"
            >
              <Icon name="download" size="sm" />
              {{ activeTab === 'codex' ? 'Codex++' : 'CCSwitch' }}
            </a>
          </div>
        </section>

        <section class="flex gap-4">
          <span class="w-6 flex-none font-mono text-sm text-gray-400">02</span>
          <div class="min-w-0 flex-1 space-y-3">
            <h3 class="text-sm font-medium text-gray-900 dark:text-white">{{ t('keys.developerTools.import') }}</h3>
            <template v-if="activeTab === 'codex'">
              <button type="button" data-testid="codex-import" class="btn btn-primary" disabled>
                <Icon name="upload" size="sm" class="mr-2" />
                {{ t('keys.developerTools.importToCodex') }}
              </button>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t(codexCandidate ? 'keys.developerTools.pending' : 'keys.developerTools.codexUnavailable') }}
              </p>
            </template>
            <button v-else type="button" data-testid="ccs-import" class="btn btn-primary" @click="emit('importCcs')">
              <Icon name="upload" size="sm" class="mr-2" />
              {{ t('keys.importToCcSwitch') }}
            </button>
          </div>
        </section>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { CodexPlusPlusCandidate } from '@/utils/codexPlusPlusImport'

const props = defineProps<{
  show: boolean
  canImportCcs: boolean
  codexCandidate: CodexPlusPlusCandidate | null
}>()
const emit = defineEmits<{ close: []; importCcs: [] }>()
const { t } = useI18n()
const activeTab = ref<'codex' | 'ccs'>('codex')

watch(() => props.show, (show) => {
  if (show) activeTab.value = 'codex'
})
</script>
