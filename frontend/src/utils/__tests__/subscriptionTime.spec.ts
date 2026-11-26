import { describe, expect, it } from 'vitest'
import { subscriptionDaysHoursRemaining, subscriptionDaysRemaining } from '../subscriptionTime'

describe('subscriptionTime', () => {
  const now = new Date('2026-11-27T10:30:00Z')

  it('reports subscription expiry in rounded-up whole days', () => {
    expect(subscriptionDaysRemaining('2026-11-28T10:30:00Z', now)).toBe(1)
    expect(subscriptionDaysRemaining('2026-11-28T10:30:01Z', now)).toBe(2)
    expect(subscriptionDaysRemaining('2026-11-27T10:29:59Z', now)).toBe(0)
  })

  it('uses days and hours only for a rolling weekly reset', () => {
    expect(subscriptionDaysHoursRemaining('2026-12-03T20:30:00Z', now)).toEqual({ days: 6, hours: 10 })
    expect(subscriptionDaysHoursRemaining('2026-11-27T11:29:59Z', now)).toEqual({ days: 0, hours: 0 })
  })

  it('does not manufacture a countdown for an expired or invalid source timestamp', () => {
    expect(subscriptionDaysHoursRemaining('2026-11-27T10:30:00Z', now)).toBeNull()
    expect(subscriptionDaysHoursRemaining('invalid', now)).toBeNull()
    expect(subscriptionDaysRemaining(undefined, now)).toBeNull()
  })
})
