<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <!-- Background Decoration -->
    <div class="pointer-events-none fixed inset-0 bg-mesh-gradient"></div>

    <!-- Sidebar -->
    <aside
      class="sidebar"
      :class="[
        sidebarCollapsed ? 'w-[72px]' : 'w-64',
        { '-translate-x-full lg:translate-x-0': !mobileOpen }
      ]"
    >
      <!-- Brand -->
      <div class="sidebar-header" :class="{ 'sidebar-header-collapsed': sidebarCollapsed }">
        <div class="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-xl bg-gradient-to-br from-primary-500 to-primary-600 text-sm font-bold text-white shadow-glow">
          S
        </div>
        <div class="sidebar-brand" :class="{ 'sidebar-brand-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">
          <span class="sidebar-brand-title text-lg font-bold text-gray-900 dark:text-white">Sub2API</span>
          <span class="sidebar-brand-title text-[10px] font-medium text-gray-400 dark:text-dark-500">企业控制台</span>
        </div>
      </div>

      <!-- Navigation -->
      <nav class="sidebar-nav scrollbar-hide">
        <div class="sidebar-section">
          <div class="sidebar-section-title" :class="{ 'sidebar-section-title-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">
            <span class="sidebar-section-title-text" :class="{ 'sidebar-section-title-text-collapsed': sidebarCollapsed }">
              {{ auth.isAdmin ? '企业管理' : '个人功能' }}
            </span>
          </div>

          <RouterLink
            v-for="item in navigation"
            :key="item.path"
            :to="item.path"
            class="sidebar-link mb-1"
            :class="{ 'sidebar-link-active': isActive(item.path), 'sidebar-link-collapsed': sidebarCollapsed }"
            :title="sidebarCollapsed ? item.label : undefined"
            @click="handleMenuItemClick"
          >
            <Icon :name="item.icon" size="md" class="flex-shrink-0" />
            <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{ item.label }}</span>
          </RouterLink>
        </div>
      </nav>

      <!-- Bottom Section -->
      <div class="mt-auto border-t border-gray-100 p-3 dark:border-dark-800">
        <!-- Identity -->
        <div class="mb-2 flex items-center gap-2.5 px-2.5 py-1.5" :class="{ 'sidebar-link-collapsed': sidebarCollapsed }">
          <div class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-xl bg-gradient-to-br from-primary-500 to-primary-600 text-sm font-medium text-white shadow-sm">
            {{ userInitial }}
          </div>
          <div class="sidebar-label min-w-0" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">
            <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ auth.principal?.email }}</div>
            <div class="text-xs text-gray-500 dark:text-dark-400">{{ auth.isAdmin ? '企业管理员' : '企业员工' }}</div>
          </div>
        </div>

        <!-- Theme Toggle -->
        <button
          data-testid="theme-toggle"
          @click="toggleTheme"
          class="sidebar-link mb-2 w-full"
          :class="{ 'sidebar-link-collapsed': sidebarCollapsed }"
          :title="sidebarCollapsed ? (isDark ? '切换为亮色' : '切换为暗色') : undefined"
        >
          <component :is="isDark ? SunIcon : MoonIcon" class="h-5 w-5 flex-shrink-0" :class="isDark ? 'text-amber-500' : ''" />
          <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{ isDark ? '亮色模式' : '暗色模式' }}</span>
        </button>

        <!-- Collapse Button -->
        <button
          data-testid="sidebar-collapse"
          @click="toggleSidebar"
          class="sidebar-link w-full"
          :class="{ 'sidebar-link-collapsed': sidebarCollapsed }"
          :title="sidebarCollapsed ? '展开侧边栏' : '收起侧边栏'"
        >
          <component :is="sidebarCollapsed ? ChevronDoubleRightIcon : ChevronDoubleLeftIcon" class="h-5 w-5 flex-shrink-0" />
          <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">收起侧边栏</span>
        </button>
      </div>
    </aside>

    <!-- Mobile Overlay -->
    <transition name="fade">
      <div
        v-if="mobileOpen"
        data-testid="mobile-overlay"
        class="fixed inset-0 z-30 bg-black/50 lg:hidden"
        @click="closeMobile"
      ></div>
    </transition>

    <!-- Main Content Area -->
    <div
      data-testid="main-area"
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64']"
    >
      <!-- Header -->
      <header class="glass sticky top-0 z-30 border-b border-gray-200/50 dark:border-dark-700/50">
        <div class="flex h-16 items-center justify-between gap-2 px-2 sm:px-4 md:px-6">
          <!-- Left: Mobile Menu Toggle + Page Title -->
          <div class="flex shrink-0 items-center gap-2 sm:gap-4">
            <button
              data-testid="mobile-menu-toggle"
              @click="toggleMobileSidebar"
              class="btn-ghost btn-icon lg:hidden"
              aria-label="切换菜单"
            >
              <Icon name="menu" size="md" />
            </button>

            <div class="hidden lg:block">
              <h1 class="text-lg font-semibold text-gray-900 dark:text-white">{{ pageTitle }}</h1>
            </div>
          </div>

          <!-- Right: User Dropdown -->
          <div class="flex min-w-0 items-center gap-1 sm:gap-3">
            <div class="relative" ref="dropdownRef">
              <button
                data-testid="user-dropdown-toggle"
                @click="toggleDropdown"
                class="flex items-center gap-2 rounded-xl p-1.5 transition-colors hover:bg-gray-100 dark:hover:bg-dark-800"
                aria-label="用户菜单"
              >
                <div class="flex h-8 w-8 items-center justify-center rounded-xl bg-gradient-to-br from-primary-500 to-primary-600 text-sm font-medium text-white shadow-sm">
                  {{ userInitial }}
                </div>
                <div class="hidden text-left md:block">
                  <div class="text-sm font-medium text-gray-900 dark:text-white">{{ auth.principal?.email }}</div>
                  <div class="text-xs text-gray-500 dark:text-dark-400">{{ auth.isAdmin ? '企业管理员' : '企业员工' }}</div>
                </div>
                <Icon name="chevronDown" size="sm" class="hidden text-gray-400 md:block" />
              </button>

              <!-- Dropdown Menu -->
              <transition name="dropdown">
                <div v-if="dropdownOpen" class="dropdown right-0 mt-2 w-56">
                  <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                    <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ auth.principal?.email }}</div>
                    <div class="text-xs text-gray-500 dark:text-dark-400">{{ auth.isAdmin ? '企业管理员' : '企业员工' }}</div>
                  </div>

                  <div class="py-1">
                    <button
                      data-testid="logout"
                      @click="handleLogout"
                      class="dropdown-item w-full text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                    >
                      <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15M12 9l-3 3m0 0l3 3m-3-3h12.75" />
                      </svg>
                      退出登录
                    </button>
                  </div>
                </div>
              </transition>
            </div>
          </div>
        </div>
      </header>

      <!-- Main Content -->
      <main class="p-4 md:p-6 lg:p-8">
        <RouterView />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'

const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const auth = useEnterpriseAuthStore()

const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const mobileOpen = computed(() => appStore.mobileOpen)

// 导航 IA 与 L0 版本保持一致：管理员 9 项 / 员工 5 项
const navigation = computed(() => auth.isAdmin ? [
  { path: '/enterprise/admin/workbench', label: '工作台', icon: 'chartBar' as const },
  { path: '/enterprise/admin/usage', label: '企业用量', icon: 'search' as const },
  { path: '/enterprise/admin/audit', label: '管理审计', icon: 'clipboard' as const },
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

const pageTitle = computed(() => (route.meta.title as string) || '')

const userInitial = computed(() => (auth.principal?.email || '?').charAt(0).toUpperCase())

function isActive(path: string): boolean {
  return route.path === path || route.path.startsWith(path + '/')
}

function toggleSidebar() {
  appStore.toggleSidebar()
}

function toggleMobileSidebar() {
  appStore.toggleMobileSidebar()
}

function closeMobile() {
  appStore.setMobileOpen(false)
}

function handleMenuItemClick() {
  if (mobileOpen.value) {
    setTimeout(() => {
      appStore.setMobileOpen(false)
    }, 150)
  }
}

// ---- 主题切换（与 AppSidebar 同一套约定：localStorage theme + html.dark）----
const isDark = ref(document.documentElement.classList.contains('dark'))

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

// 用户可能直接落在企业端而不经过标准端侧栏，这里补一次主题初始化
const savedTheme = localStorage.getItem('theme')
if (
  savedTheme === 'dark' ||
  (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
) {
  isDark.value = true
  document.documentElement.classList.add('dark')
}

// ---- 用户下拉（与 AppHeader 同一套交互：点击外部关闭）----
const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)

function toggleDropdown() {
  dropdownOpen.value = !dropdownOpen.value
}

function closeDropdown() {
  dropdownOpen.value = false
}

function handleClickOutside(event: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) {
    closeDropdown()
  }
}

async function handleLogout() {
  closeDropdown()
  await auth.logout()
  await router.replace('/enterprise/login')
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})

// ---- 主题/折叠按钮内联 SVG（与 AppSidebar 同源）----
const SunIcon = {
  render: () =>
    h(
      'svg',
      { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor', 'stroke-width': '1.5' },
      [
        h('path', {
          'stroke-linecap': 'round',
          'stroke-linejoin': 'round',
          d: 'M12 3v2.25m6.364.386l-1.591 1.591M21 12h-2.25m-.386 6.364l-1.591-1.591M12 18.75V21m-4.773-4.227l-1.591 1.591M5.25 12H3m4.227-4.773L5.636 5.636M15.75 12a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0z'
        })
      ]
    )
}

const MoonIcon = {
  render: () =>
    h(
      'svg',
      { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor', 'stroke-width': '1.5' },
      [
        h('path', {
          'stroke-linecap': 'round',
          'stroke-linejoin': 'round',
          d: 'M21.752 15.002A9.718 9.718 0 0118 15.75c-5.385 0-9.75-4.365-9.75-9.75 0-1.33.266-2.597.748-3.752A9.753 9.753 0 003 11.25C3 16.635 7.365 21 12.75 21a9.753 9.753 0 009.002-5.998z'
        })
      ]
    )
}

const ChevronDoubleLeftIcon = {
  render: () =>
    h(
      'svg',
      { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor', 'stroke-width': '1.5' },
      [
        h('path', {
          'stroke-linecap': 'round',
          'stroke-linejoin': 'round',
          d: 'm18.75 4.5-7.5 7.5 7.5 7.5m-6-15L5.25 12l7.5 7.5'
        })
      ]
    )
}

const ChevronDoubleRightIcon = {
  render: () =>
    h(
      'svg',
      { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor', 'stroke-width': '1.5' },
      [
        h('path', {
          'stroke-linecap': 'round',
          'stroke-linejoin': 'round',
          d: 'm5.25 4.5 7.5 7.5-7.5 7.5m6-15 7.5 7.5-7.5 7.5'
        })
      ]
    )
}
</script>

<style scoped>
/* 折叠态标签动画，与 AppSidebar 同一套参数 */
.sidebar-header-collapsed {
  gap: 0;
  padding-left: 1.125rem;
  padding-right: 1.125rem;
}

.sidebar-brand {
  min-width: 0;
  flex: 1 1 auto;
  white-space: nowrap;
  transition:
    max-width 0.22s ease,
    opacity 0.14s ease,
    transform 0.14s ease;
  max-width: 12rem;
}

.sidebar-brand-collapsed {
  max-width: 0;
  overflow: hidden;
  opacity: 0;
  transform: translateX(-4px);
  pointer-events: none;
}

.sidebar-brand-title {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sidebar-link-collapsed {
  gap: 0;
  padding-left: 0.875rem;
  padding-right: 0.875rem;
}

.sidebar-section-title {
  position: relative;
  display: flex;
  align-items: center;
  min-height: 1.25rem;
  overflow: hidden;
  white-space: nowrap;
}

.sidebar-section-title-text {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition:
    opacity 0.16s ease,
    transform 0.16s ease;
}

.sidebar-section-title::after {
  content: '';
  position: absolute;
  left: 0.75rem;
  right: 0.75rem;
  top: 50%;
  height: 1px;
  background: rgb(229 231 235);
  opacity: 0;
  transform: translateY(-50%);
  transition: opacity 0.18s ease;
}

.dark .sidebar-section-title::after {
  background: rgb(55 65 81);
}

.sidebar-section-title-text-collapsed {
  opacity: 0;
  transform: translateX(-4px);
}

.sidebar-section-title-collapsed::after {
  opacity: 1;
  transition-delay: 0.08s;
}

.sidebar-label {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition:
    max-width 0.2s ease,
    opacity 0.12s ease,
    transform 0.12s ease;
  max-width: 12rem;
}

.sidebar-label-collapsed {
  max-width: 0;
  opacity: 0;
  transform: translateX(-4px);
  pointer-events: none;
}
</style>
