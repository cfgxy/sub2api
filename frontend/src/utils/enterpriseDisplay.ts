type Translate = (key: string) => string

const enumKeys = {
  enterprise: { active: 'active', disabled: 'disabled' },
  subscription: { active: 'active', expired: 'expired', suspended: 'suspended' },
  employee: { active: 'active', disabled: 'disabled', terminated: 'terminated' },
  key: { active: 'active', disabled: 'disabled', quota_exhausted: 'quotaExhausted', expired: 'expired' },
  source: { available: 'available', unavailable: 'unavailable' },
  allocation: { normal: 'normal', overage: 'overage' },
  platform: { openai: 'openai', anthropic: 'anthropic', gemini: 'gemini', antigravity: 'antigravity' },
} as const

function label(group: keyof typeof enumKeys, value: string, t: Translate): string {
  const key = (enumKeys[group] as Record<string, string>)[value]
  return t(`admin.enterprise.enums.${key ? `${group}.${key}` : 'unknown'}`)
}

export const enterpriseStatusLabel = (value: string, t: Translate) => label('enterprise', value, t)
export const subscriptionStatusLabel = (value: string, t: Translate) => label('subscription', value, t)
export const employeeStatusLabel = (value: string, t: Translate) => label('employee', value, t)
export const keyStatusLabel = (value: string, t: Translate) => label('key', value, t)
export const sourceStatusLabel = (value: string, t: Translate) => label('source', value, t)
export const allocationStatusLabel = (value: string, t: Translate) => label('allocation', value, t)
export const platformLabel = (value: string, t: Translate) => label('platform', value, t)

const auditEvents: Record<string, string> = {
  'allocation.update': 'allocationUpdate',
  'allocation.credit_changed': 'allocationCreditChanged',
  'allocation.version_conflict': 'allocationVersionConflict',
  'employee.created': 'employeeCreated',
  'employee.updated': 'employeeUpdated',
  'employee.terminated': 'employeeTerminated',
  'employee.update_rejected': 'employeeUpdateRejected',
  'employee.password_changed': 'employeePasswordChanged',
  'employee.password_reset_by_admin': 'employeePasswordReset',
  'employee.create': 'employeeCreate',
  'employee.update': 'employeeUpdate',
  'employee.terminate': 'employeeTerminate',
  'department.created': 'departmentCreated',
  'department.deleted': 'departmentDeleted',
  'department.create': 'departmentCreate',
  'department.disable': 'departmentDisable',
  'key.revoke': 'keyRevoke',
  'key.rotate': 'keyRotate',
  'key.employee_create': 'keyEmployeeCreate',
  'key.employee_disable': 'keyEmployeeDisable',
  'key.employee_rotate': 'keyEmployeeRotate',
  'subscription.activated': 'subscriptionActivated'
}

const auditEntities: Record<string, string> = {
  employee: 'employee', department: 'department', api_key: 'apiKey',
  enterprise_allocation: 'allocation', enterprise_subscription: 'subscription',
  enterprise: 'enterprise'
}

export const auditEventLabel = (value: string, t: Translate) =>
  t(`admin.enterprise.audit.event.${auditEvents[value] || 'unknown'}`)
export const auditEntityLabel = (value: string, t: Translate) =>
  t(`admin.enterprise.audit.entity.${auditEntities[value] || 'unknown'}`)

const auditReasons: Record<string, string> = {
  rejected: 'rejected', invalid_request: 'invalidRequest', invalid_token: 'invalidToken',
  not_found: 'notFound', weak_password: 'weakPassword', version_conflict: 'versionConflict'
}

// 已知机器原因码映射为中文；管理员手填的自由文本原因原样返回。
export const auditReasonLabel = (value: string, t: Translate) =>
  auditReasons[value] ? t(`admin.enterprise.audit.reason.${auditReasons[value]}`) : value

// 审计操作者标识含内部主键，界面只展示角色语义。
export function auditActorLabel(value: string, t: Translate): string {
  const kind = /^enterprise_(employee|admin):\d+$/.exec(value)?.[1]
  if (kind) return t(`admin.enterprise.audit.actor.${kind}`)
  if (value === 'enterprise_public') return t('admin.enterprise.audit.actor.publicEntry')
  if (value.startsWith('enterprise_')) return t('admin.enterprise.audit.actor.system')
  return value
}

export const windowTypeLabel = (value: string, t: Translate) =>
  t(['week', 'day', 'month'].includes(value) ? `admin.enterprise.enums.window.${value}` : 'admin.enterprise.enums.unknown')
