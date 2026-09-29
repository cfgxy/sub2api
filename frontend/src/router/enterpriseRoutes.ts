import type { RouteRecordRaw } from 'vue-router'

export const enterpriseRoutes: RouteRecordRaw[] = [
  { path: '/enterprise', redirect: '/enterprise/login' },
  { path: '/enterprise/login', name: 'EnterpriseLogin', component: () => import('@/views/enterprise/EnterpriseLoginView.vue'), meta: { requiresEnterpriseAuth: false, title: '企业登录' } },
  { path: '/enterprise/forgot-password', name: 'EnterpriseForgotPassword', component: () => import('@/views/enterprise/EnterpriseForgotPasswordView.vue'), meta: { requiresEnterpriseAuth: false, title: '找回企业密码' } },
  { path: '/enterprise/reset-password', name: 'EnterpriseResetPassword', component: () => import('@/views/enterprise/EnterpriseResetPasswordView.vue'), meta: { requiresEnterpriseAuth: false, title: '重置企业密码' } },
  { path: '/enterprise/session-states', name: 'EnterpriseSessionStates', component: () => import('@/views/enterprise/EnterpriseSessionStatesView.vue'), meta: { requiresEnterpriseAuth: false, title: '认证与访问状态' } },
  { path: '/enterprise/change-password', name: 'EnterpriseChangePassword', component: () => import('@/views/enterprise/EnterpriseChangePasswordView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'employee', title: '修改初始密码' } },
  {
    path: '/enterprise',
    component: () => import('@/components/enterprise/EnterpriseLayout.vue'),
    meta: { requiresEnterpriseAuth: true },
    children: [
      { path: 'home', name: 'EnterpriseEmployeeHome', component: () => import('@/views/enterprise/EnterpriseEmployeeHomeView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'employee', title: '个人概览', titleKey: 'enterprise.shell.routeTitle.home' } },
      { path: 'usage', name: 'EnterpriseEmployeeUsage', component: () => import('@/views/enterprise/EnterpriseEmployeeUsageView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'employee', title: '个人用量', titleKey: 'enterprise.shell.routeTitle.usage' } },
      { path: 'keys', name: 'EnterpriseKeys', component: () => import('@/views/enterprise/EnterpriseKeysView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'employee', title: 'API Key', titleKey: 'enterprise.shell.routeTitle.keys' } },
      { path: 'settings', name: 'EnterpriseEmployeeSettings', component: () => import('@/views/enterprise/EnterpriseEmployeeSettingsView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'employee', title: '个人设置', titleKey: 'enterprise.shell.routeTitle.settings' } },
      { path: 'sessions', name: 'EnterpriseSessions', component: () => import('@/views/enterprise/EnterpriseSessionsView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'employee', title: '会话管理', titleKey: 'enterprise.shell.routeTitle.sessions' } },
      { path: 'admin/employees', name: 'EnterpriseEmployees', component: () => import('@/views/enterprise/admin/EnterpriseEmployeesView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '员工管理', titleKey: 'enterprise.shell.routeTitle.employees' } },
      { path: 'admin/employees/:id', name: 'EnterpriseEmployeeDetail', component: () => import('@/views/enterprise/admin/EnterpriseEmployeeDetailView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '员工详情', titleKey: 'enterprise.shell.routeTitle.employeeDetail' } },
      { path: 'admin/departments', name: 'EnterpriseDepartments', component: () => import('@/views/enterprise/admin/EnterpriseDepartmentsView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '部门管理', titleKey: 'enterprise.shell.routeTitle.departments' } },
      { path: 'admin/workbench', name: 'EnterpriseWorkbench', component: () => import('@/views/enterprise/admin/EnterpriseWorkbenchView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '用量工作台', titleKey: 'enterprise.shell.routeTitle.workbench' } },
      { path: 'admin/usage', name: 'EnterpriseAdminUsage', component: () => import('@/views/enterprise/admin/EnterpriseAdminUsageView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '企业用量', titleKey: 'enterprise.shell.routeTitle.adminUsage' } },
      { path: 'admin/audit', name: 'EnterpriseAdminAudit', component: () => import('@/views/enterprise/admin/EnterpriseAdminAuditView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '管理审计', titleKey: 'enterprise.shell.routeTitle.adminAudit' } },
      { path: 'admin/allocation', name: 'EnterpriseAllocation', component: () => import('@/views/enterprise/admin/EnterpriseAllocationView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '额度分配', titleKey: 'enterprise.shell.routeTitle.allocation' } },
      { path: 'admin/keys', name: 'EnterpriseAdminKeys', component: () => import('@/views/enterprise/admin/EnterpriseAdminKeysView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '员工 Key', titleKey: 'enterprise.shell.routeTitle.adminKeys' } },
      { path: 'admin/brand', name: 'EnterpriseBrand', component: () => import('@/views/enterprise/admin/EnterpriseBrandView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '企业品牌', titleKey: 'enterprise.shell.routeTitle.brand' } },
      { path: 'admin/sessions', name: 'EnterpriseAdminSessions', component: () => import('@/views/enterprise/EnterpriseSessionsView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '我的会话', titleKey: 'enterprise.shell.routeTitle.adminSessions' } },
    ],
  },
  { path: '/enterprise/:pathMatch(.*)*', redirect: { name: 'EnterpriseSessionStates', query: { state: 'not-found' } }, meta: { requiresEnterpriseAuth: false, title: '页面不存在' } },
]
