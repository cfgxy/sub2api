<template>
  <div class="platform-shell">
    <aside class="sidebar">
      <div class="brand"><span class="mark">S</span><div><strong>Sub2API</strong><small>平台管理</small></div></div>
      <div class="label">平台管理</div>
      <nav><RouterLink to="/admin/enterprises">企业管理</RouterLink><RouterLink to="/admin/audit-logs">平台审计</RouterLink></nav>
      <div class="help">帮助与支持</div>
    </aside>
    <div class="main"><header><div class="crumb">平台管理 <span>/ 企业管理</span></div><div class="health"><i></i>平台服务正常</div></header><main><slot /></main></div>
  </div>
</template>

<script setup lang="ts">
import { RouterLink } from 'vue-router'
</script>

<style scoped>
.platform-shell{min-height:100vh;background:#f0f2f5;color:#172033}.sidebar{position:fixed;inset:0 auto 0 0;width:232px;padding:20px 12px;background:#fff;border-right:1px solid #dfe3ea}.brand{display:flex;align-items:center;gap:10px;padding:0 10px;margin-bottom:26px}.brand strong,.brand small{display:block}.brand small{margin-top:2px;color:#667085;font-size:10px}.mark{display:grid;width:30px;height:30px;place-items:center;border-radius:7px;background:#2563eb;color:#fff;font-weight:800}.label{margin:16px 10px 7px;color:#98a2b3;font-size:11px;font-weight:700}.sidebar nav{display:grid;gap:2px}.sidebar nav a{padding:11px;border-radius:6px;color:#475467;font-size:13px;text-decoration:none}.sidebar nav a.router-link-active{background:#eff6ff;color:#2563eb;font-weight:700}.help{position:absolute;right:22px;bottom:20px;left:22px;padding-top:14px;border-top:1px solid #dfe3ea;color:#667085;font-size:12px}.main{margin-left:232px}.main>header{position:sticky;top:0;z-index:3;display:flex;align-items:center;height:64px;padding:0 28px;background:#fff;border-bottom:1px solid #dfe3ea}.crumb{font-size:14px;font-weight:650}.crumb span{margin-left:8px;color:#98a2b3;font-weight:400}.health{display:flex;align-items:center;gap:7px;margin-left:auto;color:#667085;font-size:12px}.health i{width:7px;height:7px;border-radius:50%;background:#15803d}.main>main{max-width:1180px;margin:0 auto;padding:28px}@media(max-width:760px){.sidebar{display:none}.main{margin-left:0}.main>header{padding:0 16px}.main>main{padding:18px 14px}.health{display:none}}
</style>
