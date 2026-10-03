<template>
  <AppLayout>
    <div class="model-pricing-page">
      <section class="model-pricing-notice" aria-labelledby="model-pricing-title">
        <div>
          <p class="model-pricing-eyebrow">Apinoria</p>
          <h1 id="model-pricing-title">模型与价格</h1>
          <p>个人主站模型价格参考页</p>
        </div>
        <span class="model-pricing-badge">静态参考价</span>
      </section>

      <section class="apinoria-pricing" v-html="content" aria-label="模型价格参考表"></section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import '@/styles/model-pricing.css'
import { renderModelPricingMarkdown } from './modelPricingContent'

const content = computed(() => renderModelPricingMarkdown())
</script>

<style scoped>
.model-pricing-page {
  margin: 0 auto;
  max-width: 1200px;
}

.model-pricing-notice {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1rem;
  padding: 1rem 1.25rem;
  border: 1px solid rgb(14 116 144 / 0.25);
  border-radius: 0.5rem;
  background: rgb(236 254 255 / 0.8);
  color: rgb(15 23 42);
}

.model-pricing-eyebrow {
  margin: 0 0 0.15rem;
  color: rgb(8 145 178);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.model-pricing-notice h1 {
  margin: 0;
  font-size: 1.35rem;
  font-weight: 700;
}

.model-pricing-notice p:last-child {
  margin: 0.2rem 0 0;
  color: rgb(71 85 105);
  font-size: 0.8125rem;
}

.model-pricing-badge {
  flex: 0 0 auto;
  border: 1px solid rgb(8 145 178 / 0.35);
  border-radius: 999px;
  padding: 0.35rem 0.65rem;
  color: rgb(14 116 144);
  font-size: 0.75rem;
  font-weight: 700;
  white-space: nowrap;
}

:global(.dark) .model-pricing-notice {
  border-color: rgb(34 211 238 / 0.25);
  background: rgb(8 47 73 / 0.45);
  color: rgb(241 245 249);
}

:global(.dark) .model-pricing-notice p:last-child {
  color: rgb(148 163 184);
}

:global(.dark) .model-pricing-badge {
  border-color: rgb(34 211 238 / 0.35);
  color: rgb(103 232 249);
}

@media (max-width: 640px) {
  .model-pricing-notice {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
