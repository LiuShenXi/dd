import { describe, expect, it } from 'vitest'
import { adminNextNaturalResetAt, countdownParts, cycleGridTemplate, cycleProgress, formatCarpoolAmount, formatCarpoolTargetAmount, nextNaturalResetAt, previewNextNaturalResetAt, termEventPosition, timeRangeProgress } from '@/utils/carpool'
import type { CarpoolAdminTerm, CarpoolPlan, CarpoolPreview, CarpoolUserCycle, CarpoolUserTerm } from '@/types/carpool'

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

  it('uses a continuous membership range independently of accounting periods', () => {
    expect(timeRangeProgress('2026-09-01T00:00:00Z', '2026-09-29T00:00:00Z', Date.parse('2026-09-15T00:00:00Z'))).toBe(0.5)
  })

  it('does not infer a rolling refill from cycle expiry when the server returns null', () => {
    const term: CarpoolUserTerm = {
      status: 'active', starts_at: '2026-09-01T00:00:00Z', expires_at: '2026-09-29T00:00:00Z',
      reset_mode: 'rolling', next_natural_reset_at: null, reset_count: 0, reset_count_basis: 'current_term',
      current_cycle_no: 1, cycles: [cycle], reset_events: [],
    }
    expect(nextNaturalResetAt(term)).toBeNull()
  })

  it('falls back to the fixed current-cycle boundary for legacy responses only', () => {
    const term: CarpoolUserTerm = {
      status: 'active', starts_at: '2026-09-01T00:00:00Z', expires_at: '2026-09-29T00:00:00Z',
      reset_count: 0, reset_count_basis: 'current_term', current_cycle_no: 1, cycles: [cycle], reset_events: [],
    }
    expect(nextNaturalResetAt(term)).toBe(cycle.ends_at)
    expect(nextNaturalResetAt({ ...term, expires_at: cycle.ends_at })).toBeNull()
    expect(nextNaturalResetAt({ ...term, status: 'expired' })).toBeNull()
    expect(nextNaturalResetAt({ ...term, status: 'terminated' })).toBeNull()
  })

  it('uses legacy admin period state without overriding explicit rolling null', () => {
    const plan = { reset_mode: 'rolling' } as CarpoolPlan
    const term = {
      status: 'active', expires_at: '2026-09-29T00:00:00Z', next_natural_reset_at: null,
      plan_snapshot: plan, current_cycle: { ends_at: cycle.ends_at },
    } as CarpoolAdminTerm
    expect(adminNextNaturalResetAt(term)).toBeNull()
    expect(adminNextNaturalResetAt({ ...term, next_natural_reset_at: undefined, plan_snapshot: { ...plan, reset_mode: 'fixed' } })).toBe(cycle.ends_at)
    expect(adminNextNaturalResetAt({
      ...term,
      status: 'pending',
      next_natural_reset_at: undefined,
      plan_snapshot: { ...plan, reset_mode: 'fixed' },
      current_cycle: null,
      cycles: [{ state: 'scheduled', ends_at: cycle.ends_at } as unknown as NonNullable<CarpoolAdminTerm['cycles']>[number]],
    })).toBe(cycle.ends_at)
    expect(adminNextNaturalResetAt({ ...term, status: 'expired', next_natural_reset_at: undefined, plan_snapshot: { ...plan, reset_mode: 'fixed' } })).toBeNull()
  })

  it('uses initial actions for legacy preview fallback and skips missed periods', () => {
    const preview = {
      expires_at: '2026-09-29T00:00:00Z', plan: {},
      cycles: [
        { ...cycle, base_quota_usd: '550', initial_action: 'missed' },
        { ...cycle, cycle_no: 2, ends_at: '2026-09-15T00:00:00Z', base_quota_usd: '550', initial_action: 'grant' },
      ],
    } as CarpoolPreview
    expect(previewNextNaturalResetAt(preview)).toBe('2026-09-15T00:00:00Z')
    expect(previewNextNaturalResetAt({ ...preview, plan: { reset_mode: 'rolling' } as CarpoolPlan, next_natural_reset_at: null })).toBeNull()
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
