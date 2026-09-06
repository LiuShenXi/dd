import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get, post } }))

import { carpoolAPI } from '@/api/carpool'

describe('carpool user API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('uses self-scoped endpoints without target identifiers', async () => {
    get.mockResolvedValue({ data: { server_now: '2026-09-06T00:00:00Z' } })
    await carpoolAPI.getDetails()
    expect(get).toHaveBeenCalledWith('/user/carpool/details')

    get.mockResolvedValue({ data: { eligible: false } })
    await carpoolAPI.getBoosts()
    expect(get).toHaveBeenCalledWith('/user/carpool/boosts')
  })

  it('claims with an empty body and the stable request key supplied by the store', async () => {
    post.mockResolvedValue({ data: { eligible: true, remaining: 1 } })
    await carpoolAPI.claimBoost('stable-key')
    expect(post).toHaveBeenCalledWith('/user/carpool/boosts', {}, {
      headers: { 'Idempotency-Key': 'stable-key' },
    })
  })
})
