<template>
  <div class="auth-layout">
    <div class="auth-content">
      <div v-if="settingsLoaded" class="auth-brand">
        <div class="auth-logo"><img :src="siteLogo || '/logo.svg'" alt="Logo" /></div>
        <h1>{{ siteName }}</h1>
        <p>{{ siteSubtitle }}</p>
      </div>
      <div class="auth-panel"><slot /></div>
      <div class="auth-footer"><slot name="footer" /><p>© {{ currentYear }} {{ siteName }}</p></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

const appStore = useAppStore()
const siteName = computed(() => appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Subscription to API Conversion Platform')
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)
const currentYear = computed(() => new Date().getFullYear())

onMounted(() => { appStore.fetchPublicSettings() })
</script>

<style scoped>
.auth-layout{display:flex;min-height:100vh;align-items:center;justify-content:center;padding:32px 16px;background:#f0f2f5;color:#172033}.auth-content{width:100%;max-width:440px}.auth-brand{margin-bottom:24px;text-align:center}.auth-logo{display:inline-flex;width:48px;height:48px;align-items:center;justify-content:center;overflow:hidden;border:1px solid #dbe5f5;border-radius:8px;background:#fff}.auth-logo img{width:100%;height:100%;object-fit:contain}.auth-brand h1{margin:12px 0 4px;color:#172033;font-size:24px;font-weight:700;line-height:1.3}.auth-brand p{margin:0;color:#667085;font-size:13px}.auth-panel{padding:32px;background:#fff;border:1px solid #dfe3ea;border-radius:8px;box-shadow:0 12px 28px rgba(25,35,55,.06)}.auth-footer{margin-top:18px;color:#667085;text-align:center;font-size:12px}.auth-footer p{margin:14px 0 0;color:#98a2b3;font-size:11px}@media(max-width:480px){.auth-layout{padding:20px 14px}.auth-panel{padding:24px}}
</style>
