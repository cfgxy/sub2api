<template>
  <main class="enterprise-auth" :style="backgroundStyle">
    <div class="enterprise-auth__shade"></div>
    <section class="enterprise-auth__brand" aria-label="企业品牌">
      <span class="enterprise-auth__mark"><Icon name="shield" size="xl" /></span>
      <p class="enterprise-auth__eyebrow">{{ brand.enterprise_name }}</p>
      <h1>{{ brand.title }}</h1>
      <p class="enterprise-auth__slogan">{{ brand.slogan }}</p>
      <p class="enterprise-auth__body">{{ brand.body }}</p>
    </section>
    <section class="enterprise-auth__form"><slot /></section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive } from 'vue'
import { enterpriseAPI } from '@/api/enterprise'
import Icon from '@/components/icons/Icon.vue'
import type { EnterpriseBrand } from '@/types/enterprise'

const defaultBrand: EnterpriseBrand = {
  enterprise_name: 'Sub2API',
  title: '企业工作台',
  body: '使用企业管理员或员工账号安全访问组织资源。',
  slogan: '安全、统一的企业访问入口',
  background_url: '/logo.svg',
  background_content_type: 'image/svg+xml',
  background_sha256: 'ce1f2ac07efcfff80904a9582578b5db8fdd14a3118a5c7b58f408ed06df18e1',
  background_size_bytes: 2010,
}
const brand = reactive<EnterpriseBrand>({ ...defaultBrand })
const controlledBackgroundURLs = new Set(['/logo.svg', '/api/v1/enterprise/brand/background'])
const backgroundStyle = computed(() => {
  const url = controlledBackgroundURLs.has(brand.background_url) ? brand.background_url : defaultBrand.background_url
  const version = url === '/api/v1/enterprise/brand/background' && /^[a-f\d]{64}$/i.test(brand.background_sha256)
    ? `?v=${brand.background_sha256}`
    : ''
  return { backgroundImage: `url("${url}${version}")` }
})

function applyBrand(response: EnterpriseBrand) {
  brand.enterprise_name = response.enterprise_name?.trim() || defaultBrand.enterprise_name
  brand.title = response.title?.trim() || defaultBrand.title
  brand.body = response.body?.trim() || defaultBrand.body
  brand.slogan = response.slogan?.trim() || defaultBrand.slogan
  brand.background_url = controlledBackgroundURLs.has(response.background_url) ? response.background_url : defaultBrand.background_url
  brand.background_content_type = response.background_content_type || defaultBrand.background_content_type
  brand.background_sha256 = response.background_sha256 || defaultBrand.background_sha256
  brand.background_size_bytes = response.background_size_bytes > 0 ? response.background_size_bytes : defaultBrand.background_size_bytes
}

onMounted(async () => {
  try { applyBrand(await enterpriseAPI.getBrand()) } catch { /* Keep per-field platform defaults when branding is unavailable. */ }
})
</script>

<style scoped>
.enterprise-auth { position: relative; display: grid; min-height: 100vh; grid-template-columns: minmax(0, 1.35fr) minmax(360px, .65fr); background-color: #143c3b; background-position: center; background-size: cover; color: white; }
.enterprise-auth__shade { position: absolute; inset: 0; background: rgba(9, 31, 31, .74); }
.enterprise-auth__brand, .enterprise-auth__form { position: relative; z-index: 1; }
.enterprise-auth__brand { align-self: end; max-width: 720px; padding: clamp(40px, 7vw, 96px); }
.enterprise-auth__mark { display: inline-flex; padding: 12px; border: 1px solid rgba(255,255,255,.35); border-radius: 8px; }
.enterprise-auth__eyebrow { margin: 28px 0 10px; color: #a7f3d0; font-size: 14px; font-weight: 700; }
h1 { margin: 0; max-width: 14ch; font-size: clamp(40px, 6vw, 72px); line-height: 1.05; letter-spacing: 0; overflow-wrap: anywhere; }
.enterprise-auth__slogan { margin: 20px 0 0; font-size: 22px; font-weight: 600; }
.enterprise-auth__body { max-width: 54ch; margin: 12px 0 0; color: #d1fae5; line-height: 1.7; }
.enterprise-auth__form { display: flex; align-items: center; background: rgba(255,255,255,.97); color: #172121; padding: clamp(24px, 5vw, 72px); }
.enterprise-auth__form :deep(.auth-panel) { width: 100%; max-width: 440px; margin: auto; }
.enterprise-auth__form :deep(.auth-panel h2) { margin: 0 0 8px; font-size: 28px; letter-spacing: 0; }
.enterprise-auth__form :deep(.auth-panel .subtitle) { margin: 0 0 28px; color: #64748b; }
.enterprise-auth__form :deep(.el-button) { width: 100%; }
.enterprise-auth__form :deep(.auth-links) { display: flex; justify-content: space-between; gap: 16px; margin-top: 20px; font-size: 14px; }
@media (max-width: 800px) { .enterprise-auth { grid-template-columns: 1fr; grid-template-rows: minmax(260px, 42vh) auto; } .enterprise-auth__brand { align-self: end; padding: 28px 24px; } h1 { font-size: 38px; } .enterprise-auth__eyebrow { margin-top: 18px; } .enterprise-auth__slogan { font-size: 18px; } .enterprise-auth__body { display: none; } .enterprise-auth__form { align-items: flex-start; min-height: 58vh; padding: 32px 24px; } }
</style>
