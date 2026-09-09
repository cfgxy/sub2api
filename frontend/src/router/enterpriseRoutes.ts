import type { RouteRecordRaw } from 'vue-router'

export const enterpriseRoutes: RouteRecordRaw[] = [
  { path: '/enterprise', redirect: '/enterprise/login' },
  { path: '/enterprise/login', name: 'EnterpriseLogin', component: () => import('@/views/enterprise/EnterpriseLoginView.vue'), meta: { requiresEnterpriseAuth: false, title: '企业登录' } },
  { path: '/enterprise/forgot-password', name: 'EnterpriseForgotPassword', component: () => import('@/views/enterprise/EnterpriseForgotPasswordView.vue'), meta: { requiresEnterpriseAuth: false, title: '找回企业密码' } },
  { path: '/enterprise/reset-password', name: 'EnterpriseResetPassword', component: () => import('@/views/enterprise/EnterpriseResetPasswordView.vue'), meta: { requiresEnterpriseAuth: false, title: '重置企业密码' } },
  { path: '/enterprise/change-password', name: 'EnterpriseChangePassword', component: () => import('@/views/enterprise/EnterpriseChangePasswordView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'employee', title: '修改初始密码' } },
  {
    path: '/enterprise',
    component: () => import('@/components/enterprise/EnterpriseLayout.vue'),
    meta: { requiresEnterpriseAuth: true },
    children: [
      { path: 'sessions', name: 'EnterpriseSessions', component: () => import('@/views/enterprise/EnterpriseSessionsView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'employee', title: '会话管理' } },
      { path: 'admin/employees', name: 'EnterpriseEmployees', component: () => import('@/views/enterprise/admin/EnterpriseEmployeesView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '员工管理' } },
      { path: 'admin/departments', name: 'EnterpriseDepartments', component: () => import('@/views/enterprise/admin/EnterpriseDepartmentsView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '部门管理' } },
      { path: 'admin/brand', name: 'EnterpriseBrand', component: () => import('@/views/enterprise/admin/EnterpriseBrandView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '企业品牌' } },
      { path: 'admin/sessions', name: 'EnterpriseAdminSessions', component: () => import('@/views/enterprise/EnterpriseSessionsView.vue'), meta: { requiresEnterpriseAuth: true, enterpriseRole: 'admin', title: '我的会话' } },
    ],
  },
]
