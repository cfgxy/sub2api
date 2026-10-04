<template>
  <AppLayout>
    <div class="min-w-0">
      <p v-if="loading" role="status" class="p-6 text-gray-500">{{ t('common.loading') }}</p>
      <p v-else-if="loadFailed" role="alert" class="p-6 text-red-600">{{ t('modelPricing.loadFailed') }}</p>
      <!-- 正文来自受认证保护的接口，展示前清理 HTML。 -->
      <div v-else class="model-pricing-content" v-html="content"></div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import DOMPurify from 'dompurify'
import AppLayout from '@/components/layout/AppLayout.vue'
import { getModelPricing } from '@/api/modelPricing'
import '@/styles/model-pricing.css'

const { t } = useI18n()
const content = ref('')
const loading = ref(true)
const loadFailed = ref(false)
const controller = new AbortController()

onMounted(async () => {
  try {
    const html = await getModelPricing(controller.signal)
    if (!controller.signal.aborted) content.value = DOMPurify.sanitize(html)
  } catch {
    if (!controller.signal.aborted) loadFailed.value = true
  } finally {
    loading.value = false
  }
})

onBeforeUnmount(() => controller.abort())
</script>
