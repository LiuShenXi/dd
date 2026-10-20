import { describe, expect, it } from 'vitest'
import { formatQuotaUSD, normalizeQuotaProjection, quotaAtoms, quotaProgress, quotaStatusLabel, validQuotaInput } from '../claudeQuota'
import { quotaSummary } from '@/__tests__/subscriptionQuotaFixtures'
describe('Claude decimal strings and discriminated projections', () => {
  it('preserves atomic units and the NUMERIC maximum without a float roundtrip', () => {
    expect(formatQuotaUSD('999999999999.99999999')).toBe('$999,999,999,999.99999999')
    expect(formatQuotaUSD('0.00000001')).toBe('$0.00000001')
    expect(formatQuotaUSD('0.00000000')).toBe('$0.00')
    expect(quotaProgress('1.00000000', '0.80000000', '0.70000000')).toBe(100)
  })
  it.each(['', '01', '-1', '1e2', '1.000000001', '1000000000000', 'NaN', ' 1', '.5'])('rejects invalid financial input %s instead of inventing zero', value => {
    expect(validQuotaInput(value)).toBe(false); expect(quotaAtoms(value)).toBeNull(); expect(formatQuotaUSD(value)).toBe('—')
  })
  it('normalizes only legacy missing mode to windows and preserves unavailable term', () => {
    expect(normalizeQuotaProjection({ id: 1 })).toEqual({ id: 1, quota_mode: 'windows', quota_summary: null })
    const unavailable = normalizeQuotaProjection({ quota_mode: 'term' as const })
    expect(unavailable).toEqual({ quota_mode: 'term', quota_summary: null })
    const projected = normalizeQuotaProjection({ quota_mode: 'term' as const, quota_summary: quotaSummary() })
    if (projected.quota_mode === 'term') expect(projected.quota_summary?.current_term?.remaining_usd).toBe('85.00000000')
    expect(quotaStatusLabel('future_protocol_state', key => key)).toBe('future_protocol_state')
  })
})
