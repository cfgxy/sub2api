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
          <span class="sidebar-brand-title text-[10px] font-medium text-gray-400 dark:text-dark-500">平台管理</span>
        </div>
      </div>

      <!-- Navigation -->
      <nav class="sidebar-nav scrollbar-hide">
        <div class="sidebar-section">
          <div class="sidebar-section-title" :class="{ 'sidebar-section-title-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">
            <span class="sidebar-section-title-text" :class="{ 'sidebar-section-title-text-collapsed': sidebarCollapsed }">平台管理</span>
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
              <h1 class="text-lg font-semibold text-gray-900 dark:text-white">平台管理 / {{ pageTitle }}</h1>
            </div>
          </div>

          <!-- Right: Health Indicator -->
          <div class="hidden items-center gap-2 text-xs text-gray-500 dark:text-dark-400 sm:flex">
            <span class="h-2 w-2 rounded-full bg-green-600"></span>
            平台服务正常
          </div>
        </div>
      </header>

      <!-- Main Content -->
      <main class="p-4 md:p-6 lg:p-8">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, h, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'

const route = useRoute()
const appStore = useAppStore()

const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const mobileOpen = computed(() => appStore.mobileOpen)

const pageTitle = computed(() => (route.meta.title as string) || '企业管理')

const navigation = [
  { path: '/admin/enterprises', label: '企业管理', icon: 'grid' as const },
  { path: '/admin/audit-logs', label: '平台审计', icon: 'clipboard' as const },
]

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

// 平台管理员可能直接落在平台页，这里补一次主题初始化
const savedTheme = localStorage.getItem('theme')
if (
  savedTheme === 'dark' ||
  (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
) {
  isDark.value = true
  document.documentElement.classList.add('dark')
}

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
