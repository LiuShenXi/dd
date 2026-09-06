import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const { claimBoost, getDetails } = vi.hoisted(() => ({ claimBoost: vi.fn(), getDetails: vi.fn() }))
vi.mock('@/api/carpool', () => ({
  carpoolAPI: {
    getDetails,
    getBoosts: vi.fn(),
    claimBoost,
  },
}))

import { useCarpoolStore } from '@/stores/carpool'
import type { CarpoolDetails } from '@/types/carpool'

function details(available: string): CarpoolDetails {
  return {
    server_now: '2026-09-06T00:00:00Z',
    timezone: 'Asia/Shanghai',
    quota: { available_usd: available },
    usage: { total_used_usd: '0.00000000', history_complete: true, statistics_since: null },
    term: {
      status: 'active', starts_at: '2026-09-01T00:00:00Z', expires_at: '2026-09-29T00:00:00Z', reset_count: 0,
      reset_count_basis: 'current_term', current_cycle_no: 1, reset_events: [],
      cycles: [{ cycle_no: 1, starts_at: '2026-09-01T00:00:00Z', ends_at: '2026-09-08T00:00:00Z', status: 'active' }],
    },
    reset_window: { status: 'none', scheduled_at: null, schedule_revision: 0, eligible_for_me: false, ineligible_reason: null },
  }
}

describe('carpool store idempotency', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    claimBoost.mockReset()
    getDetails.mockReset()
  })

  it('retains a claim key after an ambiguous failure and clears it after success', async () => {
    claimBoost.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({
      eligible: true,
      remaining: 1,
      total: 3,
      amount_usd: '55.00000000',
      help_text: 'help',
      unavailable_reason: null,
    })
    const store = useCarpoolStore()

    await expect(store.claimBoost()).rejects.toThrow('network')
    await store.claimBoost()

    expect(claimBoost).toHaveBeenCalledTimes(2)
    expect(claimBoost.mock.calls[0][0]).toBe(claimBoost.mock.calls[1][0])
  })

  it('does not let a completed request from the previous identity refill the next user cache', async () => {
    let resolveFirst: ((value: CarpoolDetails) => void) | undefined
    getDetails.mockReturnValue(new Promise((resolve) => { resolveFirst = resolve }))
    const store = useCarpoolStore()
    store.setIdentity(101)
    const pending = store.fetchDetails()

    store.setIdentity(202)
    resolveFirst?.(details('10.00000000'))
    await pending

    expect(store.details).toBeNull()
    expect(store.detailsLoading).toBe(false)
  })

  it('keeps the newest same-identity quota when responses arrive out of order', async () => {
    let resolveFirst!: (value: CarpoolDetails) => void
    let resolveSecond!: (value: CarpoolDetails) => void
    getDetails
      .mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve }))
      .mockReturnValueOnce(new Promise((resolve) => { resolveSecond = resolve }))
    const store = useCarpoolStore()
    store.setIdentity(101)

    const first = store.fetchDetails()
    const second = store.fetchDetails()
    resolveSecond(details('838.00000000'))
    await second
    expect(store.isCurrentDetails(await second)).toBe(true)
    resolveFirst(details('700.00000000'))
    const staleResult = await first

    expect(store.availableQuotaUsd).toBe('838.00000000')
    expect(store.isCurrentDetails(staleResult)).toBe(false)
    expect(store.detailsLoading).toBe(false)
  })

  it('retains established quota and marks it stale when the latest refresh fails', async () => {
    getDetails.mockResolvedValueOnce(details('838.00000000')).mockRejectedValueOnce(new Error('network'))
    const store = useCarpoolStore()
    store.setIdentity(101)

    await store.fetchDetails()
    await expect(store.fetchDetails()).rejects.toThrow('network')

    expect(store.availableQuotaUsd).toBe('838.00000000')
    expect(store.detailsError).toBe(true)
  })

  it('identifies only the newest same-identity failure as current', async () => {
    let rejectFirst!: (error: Error) => void
    let rejectSecond!: (error: Error) => void
    getDetails
      .mockReturnValueOnce(new Promise((_resolve, reject) => { rejectFirst = reject }))
      .mockReturnValueOnce(new Promise((_resolve, reject) => { rejectSecond = reject }))
    const store = useCarpoolStore()
    store.setIdentity(101)
    const first = store.fetchDetails()
    const second = store.fetchDetails()
    const secondError = new Error('newer failure')
    rejectSecond(secondError)
    await expect(second).rejects.toBe(secondError)
    expect(store.isCurrentDetailsError(secondError)).toBe(true)

    const firstError = new Error('older failure')
    rejectFirst(firstError)
    await expect(first).rejects.toBe(firstError)
    expect(store.isCurrentDetailsError(firstError)).toBe(false)
    expect(store.isCurrentDetailsError(secondError)).toBe(true)
  })

  it('does not classify a prior account failure as current after identity changes', async () => {
    let rejectRequest!: (error: Error) => void
    getDetails.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectRequest = reject }))
    const store = useCarpoolStore()
    store.setIdentity(101)
    const request = store.fetchDetails()

    store.setIdentity(202)
    const error = new Error('prior account failure')
    rejectRequest(error)
    await expect(request).rejects.toBe(error)

    expect(store.isCurrentDetailsError(error)).toBe(false)
    expect(store.detailsError).toBe(false)
  })

  it('polls visible-page quota and stops on reset', async () => {
    vi.useFakeTimers()
    getDetails.mockResolvedValue(details('838.00000000'))
    const store = useCarpoolStore()
    store.setIdentity(101)
    store.startDetailsPolling()

    await vi.advanceTimersByTimeAsync(45_000)
    expect(getDetails).toHaveBeenCalledTimes(1)
    store.reset()
    await vi.advanceTimersByTimeAsync(45_000)
    expect(getDetails).toHaveBeenCalledTimes(1)
    vi.useRealTimers()
  })
})
