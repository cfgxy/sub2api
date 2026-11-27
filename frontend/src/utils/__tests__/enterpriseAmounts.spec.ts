import { describe, expect, it } from 'vitest'
import { formatEnterpriseAmount, formatEnterpriseCurrency } from '../enterpriseAmounts'

describe('enterprise summary amounts', () => {
  it('rounds summaries and quotas to two decimal places', () => {
    expect(formatEnterpriseAmount('1234.56789')).toBe('1,234.57')
    expect(formatEnterpriseAmount('12.50000000')).toBe('12.50')
    expect(formatEnterpriseCurrency('9.999')).toBe('$10.00')
  })

  it('falls back safely for missing or invalid values', () => {
    expect(formatEnterpriseAmount(undefined)).toBe('0.00')
    expect(formatEnterpriseAmount('not-a-number')).toBe('0.00')
  })
})
