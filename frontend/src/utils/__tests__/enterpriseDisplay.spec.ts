import { describe, expect, it } from 'vitest'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'
import { enterpriseStatusLabel, subscriptionStatusLabel, employeeStatusLabel, keyStatusLabel, sourceStatusLabel, allocationStatusLabel } from '../enterpriseDisplay'

function translate(messages: Record<string, any>, key: string): string {
  return key.split('.').reduce((value, part) => value[part], messages) as string
}

describe('企业公共枚举展示映射', () => {
  it.each([
    [enterpriseStatusLabel, ['active', 'disabled']],
    [subscriptionStatusLabel, ['active', 'expired', 'suspended']],
    [employeeStatusLabel, ['active', 'disabled', 'terminated']],
    [keyStatusLabel, ['active', 'disabled', 'quota_exhausted', 'expired']],
    [sourceStatusLabel, ['available', 'unavailable']],
    [allocationStatusLabel, ['normal', 'overage']],
  ])('在两种语言下映射全部已知值，未知值不裸露', (format, statuses) => {
    for (const messages of [zh, en]) {
      const t = (key: string) => translate(messages, key)
      for (const status of statuses) {
        expect(format(status, t)).not.toBe(status)
        expect(format(status, t)).not.toContain('undefined')
      }
      expect(format('unexpected_api_status', t)).toBe(t('admin.enterprise.enums.unknown'))
    }
  })

  it('企业状态和额度术语遵守 L0 口径', () => {
    const t = (key: string) => translate(zh, key)
    expect(enterpriseStatusLabel('active', t)).toBe('启用')
    expect(enterpriseStatusLabel('disabled', t)).toBe('停用')
    expect(t('admin.enterprise.terms.allocation')).toBe('额度')
    expect(t('admin.enterprise.terms.remaining')).toBe('剩余额度')
    expect(t('admin.enterprise.terms.overage')).toBe('超用量')
  })
})
