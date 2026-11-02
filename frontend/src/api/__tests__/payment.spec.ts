import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import { quotaOrderSnapshot } from '@/__tests__/subscriptionQuotaFixtures'
import { paymentAPI } from '@/api/payment'

describe('payment api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('keeps legacy public out_trade_no verification for upgrade compatibility', async () => {
    await paymentAPI.verifyOrderPublic('legacy-order-no')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/verify', {
      out_trade_no: 'legacy-order-no',
    })
  })

  it('keeps signed public resume-token resolve endpoint', async () => {
    await paymentAPI.resolveOrderPublicByResumeToken('resume-token-123')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/resolve', {
      resume_token: 'resume-token-123',
    })
  })
})

describe('Claude payment API snapshot boundary', () => {
  it('preserves the server snapshot and exact allowance across create and detail responses', async () => {
    const snapshot = quotaOrderSnapshot({ quota_usd: '999999999999.00000001' })
    post.mockResolvedValueOnce({ data: { order_id: 42, subscription_quota_snapshot: snapshot } })
    get.mockResolvedValueOnce({ data: { id: 42, subscription_quota_snapshot: snapshot } })
    const created = await paymentAPI.createOrder({ amount: 9.99, payment_type: 'alipay', order_type: 'subscription', plan_id: 7 })
    const detail = await paymentAPI.getOrder(42)
    expect(created.data.subscription_quota_snapshot?.quota_usd).toBe('999999999999.00000001')
    expect(detail.data.subscription_quota_snapshot).toEqual(snapshot)
    expect(post).toHaveBeenLastCalledWith('/payment/orders', { amount: 9.99, payment_type: 'alipay', order_type: 'subscription', plan_id: 7 })
    expect(get).toHaveBeenLastCalledWith('/payment/orders/42')
  })
})
