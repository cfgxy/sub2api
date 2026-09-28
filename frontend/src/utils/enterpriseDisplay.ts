type Translate = (key: string) => string

const enumKeys = {
  enterprise: { active: 'active', disabled: 'disabled' },
  subscription: { active: 'active', expired: 'expired', suspended: 'suspended' },
  employee: { active: 'active', disabled: 'disabled', terminated: 'terminated' },
  key: { active: 'active', disabled: 'disabled', quota_exhausted: 'quotaExhausted', expired: 'expired' },
  source: { available: 'available', unavailable: 'unavailable' },
  allocation: { normal: 'normal', overage: 'overage' },
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
