<template>
  <div class="enterprise-layout">
    <aside class="enterprise-sidebar">
      <div class="brand"><span class="brand-mark">S</span><div><strong>Sub2API</strong><small>企业控制台 v1.0</small></div></div>
      <div class="nav-label">企业管理</div>
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
  { path: '/enterprise/admin/workbench', label: '工作台', icon: 'chartBar' as const },
  { path: '/enterprise/admin/allocation', label: '额度分配', icon: 'grid' as const },
  { path: '/enterprise/admin/employees', label: '员工', icon: 'users' as const },
  { path: '/enterprise/admin/keys', label: '员工 Key', icon: 'key' as const },
  { path: '/enterprise/admin/departments', label: '部门', icon: 'grid' as const },
  { path: '/enterprise/admin/brand', label: '品牌', icon: 'sparkles' as const },
  { path: '/enterprise/admin/sessions', label: '我的会话', icon: 'shield' as const },
] : [
  { path: '/enterprise/home', label: '个人概览', icon: 'chartBar' as const },
  { path: '/enterprise/keys', label: 'API Key', icon: 'key' as const },
  { path: '/enterprise/usage', label: '个人用量', icon: 'grid' as const },
  { path: '/enterprise/settings', label: '个人设置', icon: 'shield' as const },
  { path: '/enterprise/sessions', label: '我的会话', icon: 'shield' as const },
])
async function handleLogout() { await auth.logout(); await router.replace('/enterprise/login') }
</script>

<style scoped>
 .enterprise-layout { display: flex; min-height: 100vh; background: #f0f2f5; color: #111827; }
 .enterprise-sidebar { position: fixed; inset: 0 auto 0 0; display: flex; width: 232px; flex-direction: column; padding: 18px 12px; background: #fff; border-right: 1px solid #e5e7eb; }
 .brand { display: flex; align-items: center; gap: 10px; padding: 0 10px; margin-bottom: 24px; }
 .brand strong { display: block; font-size: 17px; }
 .brand small { display: block; margin-top: 2px; color: #6b7280; font-size: 10px; }
 .brand-mark { display: grid; width: 32px; height: 32px; place-items: center; border-radius: 7px; background: #2563eb; color: #fff; font-weight: 800; }
 .nav-label { padding: 0 10px; margin: 0 0 7px; color: #9ca3af; font-size: 10px; font-weight: 700; }
 nav { display: grid; gap: 2px; }
 nav a { display: flex; align-items: center; gap: 10px; min-height: 40px; padding: 0 11px; border-radius: 6px; color: #4b5563; text-decoration: none; }
 nav a.router-link-active { background: #eff6ff; color: #2563eb; font-weight: 700; }
 .identity { display: grid; gap: 3px; margin-top: auto; padding: 14px 10px 0; border-top: 1px solid #e5e7eb; overflow-wrap: anywhere; color: #4b5563; }
 .identity small { color: #6b7280; }
 .enterprise-main { min-width: 0; flex: 1; margin-left: 232px; }
 header { position: sticky; top: 0; z-index: 5; display: flex; height: 64px; align-items: center; gap: 16px; padding: 0 28px; border-bottom: 1px solid #e5e7eb; background: rgba(255,255,255,.96); }
.header-title { flex: 1; font-weight: 700; }
 main { padding: 24px 28px 30px; }
.mobile-nav { display: none; }
@media (max-width: 760px) { .enterprise-sidebar { display: none; } .enterprise-main { margin-left: 0; } .mobile-nav { display: inline-flex; } header { padding: 0 16px; } main { padding: 18px 14px; } }
</style>
