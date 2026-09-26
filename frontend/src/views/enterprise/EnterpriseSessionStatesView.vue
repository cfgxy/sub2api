<template>
  <EnterpriseAuthShell auth-layout>
    <div class="space-y-6">
      <header class="text-center">
        <div class="mx-auto mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300">
          <Icon :name="activeState.icon" size="md" />
        </div>
        <p class="mb-1 text-xs font-semibold text-primary-600 dark:text-primary-400">认证与访问状态</p>
        <h2 class="text-2xl font-bold text-gray-900 dark:text-gray-100">{{ activeState.title }}</h2>
      </header>

      <div class="rounded-md border p-4" :class="toneStyles[activeState.tone].panel">
        <div class="flex items-start gap-3">
          <Icon :name="activeState.icon" size="lg" class="shrink-0" :class="toneStyles[activeState.tone].icon" />
          <div>
            <code class="text-xs font-semibold" :class="toneStyles[activeState.tone].code">{{ activeState.code }}</code>
            <p class="mt-2 text-sm leading-6" :class="toneStyles[activeState.tone].text">{{ activeState.description }}</p>
          </div>
        </div>
      </div>

      <RouterLink :to="activeState.actionTo" class="btn btn-primary flex w-full items-center justify-center gap-2">
        <Icon name="arrowRight" size="md" />
        {{ activeState.actionLabel }}
      </RouterLink>

      <details class="rounded-md border border-gray-200 dark:border-dark-700">
        <summary class="cursor-pointer px-3 py-2 text-sm font-medium text-gray-700 hover:text-primary-600 dark:text-dark-200 dark:hover:text-primary-300">查看其他状态</summary>
        <div class="space-y-2 border-t border-gray-200 p-3 dark:border-dark-700">
          <RouterLink
            v-for="state in states.filter((item) => item.key !== activeState.key)"
            :key="state.key"
            :to="`/enterprise/session-states?state=${state.key}`"
            class="flex items-center justify-between rounded-md px-2 py-2 text-sm text-gray-600 hover:bg-gray-50 hover:text-primary-600 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-primary-300"
          >
            <span class="flex items-center gap-2"><Icon :name="state.icon" size="sm" />{{ state.title }}</span>
            <Icon name="chevronRight" size="sm" />
          </RouterLink>
        </div>
      </details>

      <p class="text-center text-xs leading-5 text-gray-400 dark:text-dark-500">所有认证反馈均不披露账号是否存在于其他企业或其所属企业信息。</p>
    </div>
  </EnterpriseAuthShell>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import EnterpriseAuthShell from '@/components/enterprise/EnterpriseAuthShell.vue'

type StateTone = 'session' | 'warning' | 'danger' | 'access'
type StateKey = 'session-expired' | 'employee-disabled' | 'enterprise-disabled' | 'source-unavailable' | 'forbidden' | 'not-found' | 'cross-enterprise' | 'reset-link-invalid'
type IconName = 'refresh' | 'server' | 'ban' | 'questionCircle' | 'xCircle' | 'link' | 'shield'
interface StateItem { key: StateKey; title: string; code: string; description: string; tone: StateTone; icon: IconName; actionLabel: string; actionTo: string }

const route = useRoute()
const states: StateItem[] = [
  { key: 'session-expired', title: '登录会话已过期', code: 'SESSION_EXPIRED', description: '为保护您的账户，需要重新验证身份后才能继续操作。', tone: 'session', icon: 'refresh', actionLabel: '重新登录', actionTo: '/enterprise/login' },
  { key: 'employee-disabled', title: '员工账户已停用', code: 'MEMBER_DISABLED', description: '此账户当前无法访问企业工作区，请联系企业管理员确认账户状态。', tone: 'warning', icon: 'ban', actionLabel: '返回企业登录', actionTo: '/enterprise/login' },
  { key: 'enterprise-disabled', title: '企业工作区不可用', code: 'ORGANIZATION_DISABLED', description: '该企业工作区暂时无法提供服务，请联系企业管理员获取后续安排。', tone: 'danger', icon: 'server', actionLabel: '返回企业登录', actionTo: '/enterprise/login' },
  { key: 'source-unavailable', title: '服务来源暂时不可用', code: 'SOURCE_UNAVAILABLE', description: '暂时无法取得平台实时状态，请稍后重试；页面不会把旧数据标记为实时结果。', tone: 'warning', icon: 'server', actionLabel: '返回登录', actionTo: '/enterprise/login' },
  { key: 'forbidden', title: '无权访问此页面', code: '403 FORBIDDEN', description: '您的当前角色没有访问此资源的权限，可返回已授权的控制台页面。', tone: 'access', icon: 'shield', actionLabel: '返回控制台', actionTo: '/enterprise' },
  { key: 'not-found', title: '页面不存在', code: '404 NOT_FOUND', description: '您访问的页面可能已移动、删除，或地址不完整。', tone: 'access', icon: 'questionCircle', actionLabel: '返回控制台', actionTo: '/enterprise' },
  { key: 'cross-enterprise', title: '不能跨企业访问', code: 'CROSS_ORGANIZATION_DENIED', description: '当前链接不属于您的企业工作区，请从正确的企业入口重新登录。', tone: 'danger', icon: 'xCircle', actionLabel: '前往企业入口', actionTo: '/enterprise/login' },
  { key: 'reset-link-invalid', title: '重置链接不可用', code: 'RESET_LINK_EXPIRED_OR_USED', description: '该重置链接已过期、已使用或无法验证，请重新申请密码重置指引。', tone: 'warning', icon: 'link', actionLabel: '重新申请', actionTo: '/enterprise/forgot-password' },
]

const toneStyles: Record<StateTone, { panel: string; icon: string; code: string; text: string }> = {
  session: { panel: 'border-primary-200 bg-primary-50 dark:border-primary-800/50 dark:bg-primary-900/20', icon: 'text-primary-600 dark:text-primary-300', code: 'text-primary-700 dark:text-primary-300', text: 'text-primary-700 dark:text-primary-200' },
  warning: { panel: 'border-amber-200 bg-amber-50 dark:border-amber-800/50 dark:bg-amber-900/20', icon: 'text-amber-600 dark:text-amber-300', code: 'text-amber-700 dark:text-amber-300', text: 'text-amber-700 dark:text-amber-200' },
  danger: { panel: 'border-red-200 bg-red-50 dark:border-red-800/50 dark:bg-red-900/20', icon: 'text-red-600 dark:text-red-300', code: 'text-red-700 dark:text-red-300', text: 'text-red-700 dark:text-red-200' },
  access: { panel: 'border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-700/60', icon: 'text-gray-600 dark:text-dark-300', code: 'text-gray-700 dark:text-dark-200', text: 'text-gray-600 dark:text-dark-300' },
}

const activeState = computed(() => states.find((state) => state.key === route.query.state) || states[0])
</script>
