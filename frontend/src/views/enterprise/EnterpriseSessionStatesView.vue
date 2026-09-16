<template>
  <main class="state-page">
    <header class="topbar"><div class="wordmark"><span class="mark">S</span>Sub2API</div><span>企业 API 访问与用量管理</span></header>
    <section class="page-content">
      <div class="eyebrow">认证与访问状态</div>
      <h1>选择下一步操作</h1>
      <p class="intro">适用于企业子域名入口中的登录会话、账户状态和访问控制反馈。</p>
      <div class="state-grid">
        <article v-for="state in states" :key="state.key" class="state-card" :class="[state.tone, { active: state.key === activeState.key }]">
          <div class="state-head"><span class="state-icon">{{ state.icon }}</span><div><h2>{{ state.title }}</h2><code>{{ state.code }}</code></div></div>
          <p>{{ state.description }}</p>
          <RouterLink class="state-action" :to="state.actionTo">{{ state.actionLabel }}</RouterLink>
        </article>
      </div>
      <div class="hint">所有认证反馈均不披露账号是否存在于其他企业或其所属企业信息。</div>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

type StateTone = 'session' | 'warning' | 'danger' | 'access'
type StateKey = 'session-expired' | 'employee-disabled' | 'enterprise-disabled' | 'source-unavailable' | 'forbidden' | 'not-found' | 'cross-enterprise' | 'reset-link-invalid'
interface StateItem { key: StateKey; title: string; code: string; description: string; tone: StateTone; icon: string; actionLabel: string; actionTo: string }

const route = useRoute()
const states: StateItem[] = [
  { key: 'session-expired', title: '登录会话已过期', code: 'SESSION_EXPIRED', description: '为保护您的账户，需要重新验证身份后才能继续操作。', tone: 'session', icon: '↻', actionLabel: '重新登录', actionTo: '/enterprise/login' },
  { key: 'employee-disabled', title: '员工账户已停用', code: 'MEMBER_DISABLED', description: '此账户当前无法访问企业工作区，请联系企业管理员确认账户状态。', tone: 'warning', icon: '!', actionLabel: '返回企业登录', actionTo: '/enterprise/login' },
  { key: 'enterprise-disabled', title: '企业工作区不可用', code: 'ORGANIZATION_DISABLED', description: '该企业工作区暂时无法提供服务，请联系企业管理员获取后续安排。', tone: 'danger', icon: '!', actionLabel: '返回企业登录', actionTo: '/enterprise/login' },
  { key: 'source-unavailable', title: '服务来源暂时不可用', code: 'SOURCE_UNAVAILABLE', description: '暂时无法取得平台实时状态，请稍后重试；页面不会把旧数据标记为实时结果。', tone: 'warning', icon: '⌁', actionLabel: '返回登录', actionTo: '/enterprise/login' },
  { key: 'forbidden', title: '无权访问此页面', code: '403 FORBIDDEN', description: '您的当前角色没有访问此资源的权限，可返回已授权的控制台页面。', tone: 'access', icon: '×', actionLabel: '返回控制台', actionTo: '/enterprise' },
  { key: 'not-found', title: '页面不存在', code: '404 NOT_FOUND', description: '您访问的页面可能已移动、删除，或地址不完整。', tone: 'access', icon: '?', actionLabel: '返回控制台', actionTo: '/enterprise' },
  { key: 'cross-enterprise', title: '不能跨企业访问', code: 'CROSS_ORGANIZATION_DENIED', description: '当前链接不属于您的企业工作区，请从正确的企业入口重新登录。', tone: 'danger', icon: '×', actionLabel: '前往企业入口', actionTo: '/enterprise/login' },
  { key: 'reset-link-invalid', title: '重置链接不可用', code: 'RESET_LINK_EXPIRED_OR_USED', description: '该重置链接已过期、已使用或无法验证，请重新申请密码重置指引。', tone: 'warning', icon: '⌁', actionLabel: '重新申请', actionTo: '/enterprise/forgot-password' },
]
const activeState = computed(() => states.find((state) => state.key === route.query.state) || states[0])
</script>

<style scoped>
.state-page{min-height:100vh;background:#f0f2f5;color:#172033}.topbar{height:64px;display:flex;align-items:center;justify-content:space-between;padding:0 max(20px,calc((100vw - 1328px)/2));background:#fff;border-bottom:1px solid #dfe3ea}.wordmark{display:flex;align-items:center;gap:10px;font-size:17px;font-weight:750}.mark{display:grid;width:28px;height:28px;place-items:center;border-radius:7px;background:#2563eb;color:#fff;font-size:14px;font-weight:800}.topbar>span{color:#667085;font-size:12px}.page-content{width:min(1032px,calc(100% - 32px));margin:0 auto;padding:30px 0 26px}.eyebrow{margin-bottom:7px;color:#2563eb;font-size:12px;font-weight:700;letter-spacing:.08em}.page-content h1{margin:0 0 5px;font-size:25px;line-height:31px}.intro{margin:0 0 17px;color:#526078;font-size:13px;line-height:19px}.selected{margin-bottom:12px}.state-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.state-card{position:relative;min-height:164px;padding:16px 24px;overflow:hidden;background:#fff;border:1px solid #dfe3ea;border-radius:8px;box-shadow:0 8px 22px rgba(25,35,55,.045)}.state-card.active{border-color:var(--tone);box-shadow:0 0 0 1px var(--tone),0 8px 22px rgba(25,35,55,.045)}.state-card:before{position:absolute;inset:16px auto 16px 0;width:3px;border-radius:0 2px 2px 0;background:var(--tone);content:''}.state-head{display:flex;align-items:flex-start;gap:10px;margin-left:8px}.state-icon{display:grid;width:28px;height:28px;flex:0 0 auto;place-items:center;border-radius:7px;background:var(--tint);color:var(--tone);font-size:14px;font-weight:800}.state-head h2{margin:3px 0 0;font-size:15px;line-height:20px}.state-head code{display:block;margin-top:4px;color:#667085;font-size:11px}.state-card p{max-width:400px;margin:11px 0;color:#526078;font-size:13px;line-height:19px}.state-action{display:inline-flex;align-items:center;height:30px;padding:0 12px;border:1px solid var(--tone);border-radius:5px;color:var(--tone);font-size:12px;font-weight:700;text-decoration:none}.session{--tone:#2563eb;--tint:#eaf1ff}.warning{--tone:#a15c00;--tint:#fff6e5}.danger{--tone:#b42318;--tint:#fff0ef}.access{--tone:#475467;--tint:#f2f4f7}.hint{margin-top:14px;padding-top:12px;border-top:1px solid #dfe3ea;color:#667085;font-size:12px;line-height:19px}@media(max-width:760px){.topbar{padding:0 20px}.topbar>span{display:none}.page-content{padding-top:24px}.state-grid{grid-template-columns:1fr}.state-card{min-height:0}}
</style>
