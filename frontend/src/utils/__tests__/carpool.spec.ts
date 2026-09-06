import { describe, expect, it } from 'vitest'
import { countdownParts, cycleGridTemplate, cycleProgress, formatCarpoolAmount, formatCarpoolTargetAmount, termEventPosition } from '@/utils/carpool'
import type { CarpoolUserCycle } from '@/types/carpool'

const cycle: CarpoolUserCycle = {
  cycle_no: 1,
  starts_at: '2026-09-01T00:00:00Z',
  ends_at: '2026-09-08T00:00:00Z',
  status: 'active',
}

describe('carpool timeline helpers', () => {
  it('derives new and legacy visual proportions from snapshot cycle dates', () => {
    const weekly = Array.from({ length: 4 }, (_, index) => ({
      ...cycle,
      cycle_no: index + 1,
      starts_at: new Date(Date.parse(cycle.starts_at) + index * 7 * 86_400_000).toISOString(),
      ends_at: new Date(Date.parse(cycle.starts_at) + (index + 1) * 7 * 86_400_000).toISOString(),
    }))
    expect(cycleGridTemplate(weekly)).toBe('7fr 7fr 7fr 7fr')
    expect(cycleGridTemplate([...weekly, { ...cycle, cycle_no: 5, starts_at: weekly[3].ends_at, ends_at: new Date(Date.parse(weekly[3].ends_at) + 2 * 86_400_000).toISOString() }])).toBe('7fr 7fr 7fr 7fr 2fr')
  })

  it('clamps cycle progress at both time boundaries', () => {
    expect(cycleProgress(cycle, Date.parse('2026-08-31T23:59:59Z'))).toBe(0)
    expect(cycleProgress(cycle, Date.parse('2026-09-04T12:00:00Z'))).toBe(0.5)
    expect(cycleProgress(cycle, Date.parse('2026-09-08T00:00:00Z'))).toBe(1)
  })

  it('stops countdowns at zero instead of rendering negative time', () => {
    expect(countdownParts('2026-09-06T22:00:00+08:00', Date.parse('2026-09-06T14:00:01Z'))).toEqual({
      days: 0,
      hours: 0,
      minutes: 0,
      seconds: 0,
    })
  })

  it('formats backend decimal strings without using them for accounting', () => {
    expect(formatCarpoolAmount('157.00000000')).toBe('157.00')
    expect(formatCarpoolAmount('1234.56780000')).toBe('1,234.57')
    expect(formatCarpoolTargetAmount('700.00000000')).toBe('700')
  })

  it('positions reset markers by actual successful time and rejects out-of-term values', () => {
    expect(termEventPosition('2026-09-08T00:00:00Z', '2026-09-01T00:00:00Z', '2026-09-29T00:00:00Z')).toBe(25)
    expect(termEventPosition('2026-09-30T00:00:00Z', '2026-09-01T00:00:00Z', '2026-09-29T00:00:00Z')).toBeNull()
  })
})
