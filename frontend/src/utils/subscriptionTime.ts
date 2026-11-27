export interface DaysHours {
  days: number
  hours: number
}

function remainingMilliseconds(targetDate: string | Date | null | undefined, now: Date): number | null {
  if (!targetDate) return null
  const target = new Date(targetDate)
  const milliseconds = target.getTime() - now.getTime()
  return Number.isFinite(milliseconds) ? milliseconds : null
}

/**
 * Returns the number of calendar-sized 24-hour blocks remaining, rounded up
 * so a subscription with any positive time remaining still reports one day.
 */
export function subscriptionDaysRemaining(targetDate: string | Date | null | undefined, now = new Date()): number | null {
  const milliseconds = remainingMilliseconds(targetDate, now)
  if (milliseconds === null) return null
  if (milliseconds <= 0) return 0
  return Math.ceil(milliseconds / (24 * 60 * 60 * 1000))
}

/**
 * Formats only the stable days/hours units required by enterprise's rolling
 * weekly quota. Minutes are deliberately omitted so the UI never implies a
 * natural-day or 23:59 reset boundary.
 */
export function subscriptionDaysHoursRemaining(targetDate: string | Date | null | undefined, now = new Date()): DaysHours | null {
  const milliseconds = remainingMilliseconds(targetDate, now)
  if (milliseconds === null || milliseconds <= 0) return null
  const totalHours = Math.floor(milliseconds / (60 * 60 * 1000))
  return { days: Math.floor(totalHours / 24), hours: totalHours % 24 }
}
