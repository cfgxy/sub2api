<template>
  <div class="enterprise-layout">
    <aside class="enterprise-sidebar">
      <div class="brand"><Icon name="shield" size="lg" /><strong>企业工作台</strong></div>
      <nav><RouterLink v-for="item in navigation" :key="item.path" :to="item.path"><Icon :name="item.icon" size="sm" />{{ item.label }}</RouterLink></nav>
      <div class="identity"><span>{{ auth.principal?.email }}</span><small>{{ auth.isAdmin ? '企业管理员' : '企业员工' }}</small></div>
    </aside>
    <div class="enterprise-main">
      <header>
        <el-dropdown class="mobile-nav" trigger="click" @command="router.push">
          <el-button :icon="Menu">导航</el-button>
          <template #dropdown><el-dropdown-menu><el-dropdown-item v-for="item in navigation" :key="item.path" :command="item.path">{{ item.label }}</el-dropdown-item></el-dropdown-menu></template>
        </el-dropdown>
        <div class="header-title">{{ route.meta.title }}</div>
        <el-button text :icon="SwitchButton" @click="handleLogout">退出</el-button>
      </header>
      <main><RouterView /></main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { Menu, SwitchButton } from '@element-plus/icons-vue'
import Icon from '@/components/icons/Icon.vue'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'

const auth = useEnterpriseAuthStore()
const router = useRouter()
const route = useRoute()
const navigation = computed(() => auth.isAdmin ? [
  { path: '/enterprise/admin/employees', label: '员工', icon: 'users' as const },
  { path: '/enterprise/admin/departments', label: '部门', icon: 'grid' as const },
  { path: '/enterprise/admin/brand', label: '品牌', icon: 'sparkles' as const },
  { path: '/enterprise/admin/sessions', label: '我的会话', icon: 'shield' as const },
] : [{ path: '/enterprise/sessions', label: '我的会话', icon: 'shield' as const }])
async function handleLogout() { await auth.logout(); await router.replace('/enterprise/login') }
</script>

<style scoped>
.enterprise-layout { display: flex; min-height: 100vh; background: #f4f7f6; color: #172121; }
.enterprise-sidebar { position: fixed; inset: 0 auto 0 0; display: flex; width: 240px; flex-direction: column; padding: 24px 16px; background: #173f3d; color: white; }
.brand { display: flex; align-items: center; gap: 12px; padding: 8px 10px 28px; }
nav { display: grid; gap: 6px; }
nav a { display: flex; align-items: center; gap: 10px; padding: 11px 12px; border-radius: 6px; color: #d1fae5; text-decoration: none; }
nav a.router-link-active { background: #f0fdfa; color: #115e59; font-weight: 700; }
.identity { display: grid; gap: 3px; margin-top: auto; padding: 16px 10px 0; border-top: 1px solid rgba(255,255,255,.2); overflow-wrap: anywhere; }
.identity small { color: #a7f3d0; }
.enterprise-main { min-width: 0; flex: 1; margin-left: 240px; }
header { position: sticky; top: 0; z-index: 5; display: flex; height: 64px; align-items: center; gap: 16px; padding: 0 28px; border-bottom: 1px solid #dce5e2; background: rgba(255,255,255,.96); }
.header-title { flex: 1; font-weight: 700; }
main { padding: 28px; }
.mobile-nav { display: none; }
@media (max-width: 760px) { .enterprise-sidebar { display: none; } .enterprise-main { margin-left: 0; } .mobile-nav { display: inline-flex; } header { padding: 0 16px; } main { padding: 18px 14px; } }
</style>
