<template>
  <main class="enterprise-auth">
    <header class="topbar"><div class="wordmark"><span class="mark">S</span><div><strong>{{ brand.enterprise_name }}</strong><small>企业 API 访问与用量管理</small></div></div><span class="topbar-note">安全、统一的企业访问入口</span></header>
    <div class="auth-background" :style="backgroundStyle" aria-hidden="true" />
    <section class="auth-content">
      <div class="brand-copy"><div class="eyebrow">企业入口</div><h1>{{ brand.title }}</h1><p>{{ brand.body }}</p><span>{{ brand.slogan }}</span></div>
      <section class="auth-card"><slot /></section>
      <footer>仅限授权企业成员访问 · 不会在页面显示密码、Token 或完整 API Key</footer>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive } from 'vue'
import { enterpriseAPI } from '@/api/enterprise'
import type { EnterpriseBrand } from '@/types/enterprise'

const defaultBrand: EnterpriseBrand = {
  enterprise_name: 'Sub2API',
  title: '企业工作台',
  body: '使用企业管理员或员工账号安全访问组织资源。',
  slogan: '安全、统一的企业访问入口',
  background_url: '/logo.svg',
  background_content_type: 'image/svg+xml',
  background_sha256: '',
  background_size_bytes: 0,
}
const brand = reactive<EnterpriseBrand>({ ...defaultBrand })

function applyBrand(response: EnterpriseBrand) {
  brand.enterprise_name = response.enterprise_name?.trim() || defaultBrand.enterprise_name
  brand.title = response.title?.trim() || defaultBrand.title
  brand.body = response.body?.trim() || defaultBrand.body
  brand.slogan = response.slogan?.trim() || defaultBrand.slogan
  brand.background_url = response.background_url === '/api/v1/enterprise/brand/background' ? response.background_url : defaultBrand.background_url
  brand.background_content_type = response.background_content_type || defaultBrand.background_content_type
  brand.background_sha256 = /^[a-f\d]{64}$/i.test(response.background_sha256 || '') ? response.background_sha256 : defaultBrand.background_sha256
  brand.background_size_bytes = response.background_size_bytes > 0 ? response.background_size_bytes : defaultBrand.background_size_bytes
}

const backgroundStyle = computed(() => {
  const url = brand.background_url === '/api/v1/enterprise/brand/background' ? brand.background_url : defaultBrand.background_url
  const version = url === '/api/v1/enterprise/brand/background' && /^[a-f\d]{64}$/i.test(brand.background_sha256)
    ? `?v=${brand.background_sha256}`
    : ''
  return { backgroundImage: `url("${url}${version}")` }
})

onMounted(async () => {
  try { applyBrand(await enterpriseAPI.getBrand()) } catch { /* 品牌来源不可用时保留受控默认值 */ }
})
</script>

<style scoped>
.enterprise-auth{position:relative;min-height:100vh;background:#f0f2f5;color:#172033;isolation:isolate}.auth-background{position:absolute;inset:64px 0 0;z-index:-1;background-position:center;background-size:cover;opacity:.16}.topbar{position:relative;z-index:1;display:flex;align-items:center;justify-content:space-between;height:64px;padding:0 max(20px,calc((100vw - 1328px)/2));background:#fff;border-bottom:1px solid #dfe3ea}.wordmark{display:flex;align-items:center;gap:10px;font-size:17px}.wordmark strong,.wordmark small{display:block}.wordmark strong{font-weight:750}.wordmark small{margin-top:2px;color:#667085;font-size:11px}.mark{display:grid;width:28px;height:28px;place-items:center;border-radius:7px;background:#2563eb;color:#fff;font-size:14px;font-weight:800}.topbar-note{color:#667085;font-size:12px}.auth-content{position:relative;z-index:1;display:grid;grid-template-columns:minmax(0,420px) minmax(360px,460px);gap:72px;align-items:center;width:min(960px,calc(100% - 40px));min-height:calc(100vh - 64px);margin:0 auto;padding:48px 0}.brand-copy .eyebrow{margin-bottom:10px;color:#2563eb;font-size:12px;font-weight:700;letter-spacing:.08em}.brand-copy h1{margin:0 0 14px;font-size:36px;line-height:1.2}.brand-copy p{margin:0 0 18px;color:#526078;font-size:15px;line-height:1.8}.brand-copy span{color:#667085;font-size:13px}.auth-card{padding:32px;background:#fff;border:1px solid #dfe3ea;border-radius:8px;box-shadow:0 12px 28px rgba(25,35,55,.06)}.auth-card :deep(.auth-panel h2){margin:0 0 8px;font-size:24px;line-height:1.3}.auth-card :deep(.subtitle){margin:0 0 24px;color:#667085;line-height:1.6}.auth-card :deep(.el-button){min-height:40px}.auth-card :deep(.auth-links){display:flex;justify-content:flex-end;gap:16px;margin-top:18px;font-size:13px}.auth-card :deep(a){color:#2563eb;text-decoration:none}.auth-content footer{grid-column:1/-1;align-self:end;color:#98a2b3;text-align:center;font-size:11px}@media(max-width:760px){.topbar{padding:0 20px}.topbar-note{display:none}.auth-content{display:flex;flex-direction:column;align-items:stretch;gap:24px;width:calc(100% - 32px);min-height:calc(100vh - 64px);padding:30px 0 22px}.brand-copy h1{font-size:28px}.brand-copy p{margin-bottom:8px;font-size:14px}.auth-card{padding:24px}.auth-content footer{margin-top:auto}}
</style>
