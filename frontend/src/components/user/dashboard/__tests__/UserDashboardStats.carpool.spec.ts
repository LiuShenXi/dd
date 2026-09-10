import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import { useCarpoolStore } from '@/stores/carpool'
import type { CarpoolDetails } from '@/types/carpool'

const api = vi.hoisted(() => ({ getBoosts: vi.fn(), claimBoost: vi.fn(), getDetails: vi.fn() }))
vi.mock('@/api/carpool', () => ({ carpoolAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const unavailable = {
  eligible: false,
  remaining: 0,
  total: 3,
  amount_usd: '55.00000000',
  help_text: 'server text is not rendered directly',
  unavailable_reason: 'term_expired',
}

const activeDetails: CarpoolDetails = {
  server_now: '2026-09-06T00:00:00Z',
  timezone: 'Asia/Shanghai',
  quota: { available_usd: '838.00000000' },
  usage: { total_used_usd: '0.00000000', history_complete: true, statistics_since: null },
  term: {
    status: 'active', starts_at: '2026-09-01T00:00:00Z', expires_at: '2026-09-29T00:00:00Z', reset_count: 0,
    reset_count_basis: 'current_term', current_cycle_no: 1, reset_events: [],
    cycles: [{ cycle_no: 1, starts_at: '2026-09-01T00:00:00Z', ends_at: '2026-09-08T00:00:00Z', status: 'active' }],
  },
  reset_window: { status: 'none', scheduled_at: null, schedule_revision: 0, eligible_for_me: false, ineligible_reason: null },
}

function mountStats() {
  return mount(UserDashboardStats, {
    props: { stats: { by_platform: [] } as never, balance: 100, isSimple: false, platformQuotas: [] },
    global: { stubs: { Icon: true, HelpTooltip: true } },
  })
}

describe('UserDashboardStats carpool boost', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    setActivePinia(createPinia())
    vi.clearAllMocks()
    api.getBoosts.mockResolvedValue(unavailable)
    api.claimBoost.mockResolvedValue(unavailable)
    api.getDetails.mockResolvedValue(activeDetails)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('localizes a known backend reason code instead of displaying the raw code', async () => {
    const wrapper = mountStats()
    await flushPromises()
    expect(wrapper.text()).toContain('carpool.boost.reasons.termExpired')
    expect(wrapper.text()).not.toContain('term_expired')
    wrapper.unmount()
  })

  it('offers a retry after boost status loading fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    api.getBoosts.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(unavailable)
    const wrapper = mountStats()
    await flushPromises()

    const retry = wrapper.findAll('button').find((item) => item.text().includes('carpool.boost.retry'))
    expect(retry).toBeDefined()
    await retry!.trigger('click')
    await flushPromises()
    expect(api.getBoosts).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('wraps the boost action before the copy becomes unreadably narrow', async () => {
    const wrapper = mountStats()
    await flushPromises()

    expect(wrapper.get('[data-testid="carpool-boost-layout"]').classes()).toContain('flex-wrap')
    expect(wrapper.get('[data-testid="carpool-boost-copy"]').classes()).toEqual(expect.arrayContaining(['min-w-[7rem]', 'flex-1']))
    expect(wrapper.get('[data-testid="carpool-boost-action"]').classes()).toEqual(expect.arrayContaining(['ml-auto', 'shrink-0']))
    wrapper.unmount()
  })

  it('shows authoritative carpool quota without adding ordinary balance', async () => {
    const store = useCarpoolStore()
    store.details = activeDetails
    const wrapper = mountStats()
    await flushPromises()

    expect(wrapper.get('[data-testid="dashboard-funding-label"]').text()).toBe('carpool.availableQuota')
    expect(wrapper.get('[data-testid="dashboard-funding-amount"]').text()).toBe('$838.00')
    expect(wrapper.text()).not.toContain('$938.00')
    wrapper.unmount()
  })

  it('keeps ordinary balance behavior for a confirmed non-subscriber', async () => {
    const store = useCarpoolStore()
    store.details = { ...activeDetails, billing_mode: 'standard', quota: null, term: null }
    const wrapper = mountStats()
    await flushPromises()

    expect(wrapper.get('[data-testid="dashboard-funding-label"]').text()).toBe('dashboard.balance')
    expect(wrapper.get('[data-testid="dashboard-funding-amount"]').text()).toBe('$100.00')
    wrapper.unmount()
  })

  it.each(['expired', 'terminated', 'pending'] as const)('shows zero for a %s subscription without exposing the retained balance', async (status) => {
    const store = useCarpoolStore()
    store.details = { ...activeDetails, billing_mode: 'carpool', quota: null, term: { ...activeDetails.term!, status } }
    const wrapper = mountStats()
    await flushPromises()
    expect(wrapper.get('[data-testid="dashboard-funding-amount"]').text()).toBe('$0.00')
    expect(wrapper.get('[data-testid="dashboard-funding-label"]').text()).toBe('carpool.availableQuota')
    expect(wrapper.text()).not.toContain('$100.00')
    wrapper.unmount()
  })

  it('keeps an unknown or failed first billing lookup from flashing the retained balance', async () => {
    const store = useCarpoolStore()
    const wrapper = mountStats()
    await flushPromises()
    expect(wrapper.get('[data-testid="dashboard-funding-amount"]').text()).toBe('--')
    store.detailsError = true
    await flushPromises()
    expect(wrapper.get('[data-testid="dashboard-funding-amount"]').text()).toBe('--')
    expect(wrapper.text()).not.toContain('$100.00')
    wrapper.unmount()
  })

  it('labels an established carpool quota as stale after refresh failure', async () => {
    const store = useCarpoolStore()
    store.details = activeDetails
    store.detailsError = true
    const wrapper = mountStats()
    await flushPromises()

    expect(wrapper.get('[data-testid="dashboard-funding-amount"]').text()).toBe('$838.00')
    expect(wrapper.text()).toContain('carpool.quotaStale')
    wrapper.unmount()
  })

  it('does not let an older quota read overwrite the post-boost refresh', async () => {
    const eligible = { ...unavailable, eligible: true, remaining: 2 }
    api.getBoosts.mockResolvedValue(eligible)
    api.claimBoost.mockResolvedValue({ ...eligible, remaining: 1 })
    let resolveOlder!: (value: CarpoolDetails) => void
    let resolvePostBoost!: (value: CarpoolDetails) => void
    api.getDetails
      .mockReturnValueOnce(new Promise((resolve) => { resolveOlder = resolve }))
      .mockReturnValueOnce(new Promise((resolve) => { resolvePostBoost = resolve }))
    const store = useCarpoolStore()
    store.details = { ...activeDetails, quota: { available_usd: '700.00000000' } }
    const olderRequest = store.fetchDetails()
    const wrapper = mountStats()
    await flushPromises()

    await wrapper.get('[data-testid="carpool-boost-action"]').trigger('click')
    await flushPromises()
    expect(api.getDetails).toHaveBeenCalledTimes(2)

    resolvePostBoost(activeDetails)
    await flushPromises()
    expect(store.availableQuotaUsd).toBe('838.00000000')

    resolveOlder({ ...activeDetails, quota: { available_usd: '700.00000000' } })
    await olderRequest
    expect(store.availableQuotaUsd).toBe('838.00000000')
    wrapper.unmount()
  })
})
