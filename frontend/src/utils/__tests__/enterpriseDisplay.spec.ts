import { describe, expect, it } from 'vitest'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'
import { enterpriseStatusLabel, subscriptionStatusLabel, employeeStatusLabel, keyStatusLabel, sourceStatusLabel, allocationStatusLabel, auditEventLabel, auditReasonLabel, auditActorLabel, windowTypeLabel } from '../enterpriseDisplay'

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

  it('审计事件、原因、操作者、窗口在中英文下都不裸露机器码（SHAN-392 返工）', () => {
    for (const messages of [zh, en]) {
      const t = (key: string) => translate(messages, key)
      for (const event of ['key.employee_create', 'key.employee_disable', 'key.employee_rotate']) {
        expect(auditEventLabel(event, t)).not.toBe(t('admin.enterprise.audit.event.unknown'))
        expect(auditEventLabel(event, t)).not.toBe(event)
      }
      for (const reason of ['rejected', 'invalid_request', 'invalid_token', 'not_found', 'weak_password', 'version_conflict']) {
        expect(auditReasonLabel(reason, t)).not.toBe(reason)
        expect(auditReasonLabel(reason, t)).not.toContain('undefined')
      }
      for (const window of ['week', 'day', 'month']) {
        expect(windowTypeLabel(window, t)).not.toBe(window)
      }
      expect(windowTypeLabel('quarter', t)).toBe(t('admin.enterprise.enums.unknown'))
      for (const actor of ['enterprise_employee:12', 'enterprise_admin:3', 'enterprise_public', 'enterprise_actor']) {
        const label = auditActorLabel(actor, t)
        expect(label).not.toContain('enterprise_')
        expect(label).not.toMatch(/\d/)
      }
    }
  })

  it('中文口径：原因、窗口、操作者与自由文本', () => {
    const t = (key: string) => translate(zh, key)
    expect(auditReasonLabel('rejected', t)).toBe('被拒绝')
    expect(auditReasonLabel('管理员手填原因', t)).toBe('管理员手填原因')
    expect(windowTypeLabel('week', t)).toBe('按周')
    expect(auditActorLabel('enterprise_employee:12', t)).toBe('企业员工')
    expect(auditActorLabel('enterprise_admin:3', t)).toBe('企业管理员')
    expect(auditActorLabel('system', t)).toBe('system')
  })
})
