import { describe, expect, it } from 'vitest'
import type { HomeProducts } from '@/api/homeProducts'
import { buildCarpoolProducts, carpoolProductRules } from '../carpoolProducts'

const t = (key: string, params?: Record<string, string | number>) =>
  params && Object.keys(params).length ? `${key}:${JSON.stringify(params)}` : key

function payload(overrides: Partial<HomeProducts> = {}): HomeProducts {
  return {
    gpt_carpool: {
      enabled: true,
      status: 'available',
      rules: [],
      tiers: [
        { code: 'two_seat', name: 'Two-seat', seats: 2, price_cny: '655.00', weekly_quota_usd: '1000.00000000', duration_days: 28, cycle_days: 7, boost_ratio: '0.10000000', boost_count: 2 },
        { code: 'four_seat', name: 'Four-seat', seats: 4, price_cny: '330.00', weekly_quota_usd: '500.00000000', duration_days: 28, cycle_days: 7, boost_ratio: '0.10000000', boost_count: 2 }
      ]
    },
    claude_weekly: {
      enabled: true,
      status: 'pilot',
      rules: [],
      plan_id: 3,
      plan_name: '周卡',
      price_cny: 100,
      original_price: null,
      quota_usd: '150.00000000',
      rate_multiplier: 0.2,
      duration_days: 7
    },
    contact: '',
    ...overrides
  }
}

describe('buildCarpoolProducts', () => {
  it('uses real plan prices and quotas', () => {
    const [gpt, weekly, monthly] = buildCarpoolProducts(payload())
    expect(gpt.priceCny).toBe(330)
    expect(gpt.tiers?.map((tier) => [tier.seats, tier.priceCny, tier.weeklyQuotaUsd])).toEqual([
      [4, 330, 500],
      [2, 655, 1000]
    ])
    expect(gpt.highlight).toEqual({ quota: 1000 })
    expect(gpt.ruleParams).toEqual({ days: 28, cycle: 7, boosts: 2, percent: 10 })
    expect(weekly.priceCny).toBe(100)
    expect(weekly.highlight).toEqual({ quota: 150, rate: 0.2 })
    expect(monthly.id).toBe('claude-monthly')
  })

  it('shows TBD when the weekly card has no linked plan', () => {
    const weekly = buildCarpoolProducts(
      payload({ claude_weekly: { ...payload().claude_weekly, price_cny: null, quota_usd: null, rate_multiplier: null } })
    )[1]
    expect(weekly.priceCny).toBeNull()
    expect(weekly.highlight).toBeUndefined()
    expect(carpoolProductRules(weekly, t)).toEqual([
      'home.products.claudeWeekly.rules.term',
      'home.products.claudeWeekly.rules.models',
      'home.products.claudeWeekly.rules.tokens'
    ])
  })

  it('hides disabled products and falls back to TBD cards without data', () => {
    const ids = buildCarpoolProducts(payload({ gpt_carpool: { ...payload().gpt_carpool, enabled: false } })).map((p) => p.id)
    expect(ids).toEqual(['claude-weekly', 'claude-monthly'])
    expect(buildCarpoolProducts(null).every((p) => p.priceCny === null)).toBe(true)
  })

  it('prefers admin-configured rules over defaults', () => {
    const gpt = buildCarpoolProducts(payload({ gpt_carpool: { ...payload().gpt_carpool, rules: ['自定义规则'] } }))[0]
    expect(carpoolProductRules(gpt, t)).toEqual(['自定义规则'])
  })
})
