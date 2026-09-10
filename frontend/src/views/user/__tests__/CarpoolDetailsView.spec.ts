import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import CarpoolDetailsView from '@/views/user/CarpoolDetailsView.vue'
import { useCarpoolStore } from '@/stores/carpool'
import en from '@/i18n/locales/en/carpool'
import zh from '@/i18n/locales/zh/carpool'
import type { CarpoolBoostStatus, CarpoolDetails } from '@/types/carpool'

const api = vi.hoisted(() => ({ getDetails: vi.fn(), getBoosts: vi.fn(), claimBoost: vi.fn() }))
vi.mock('@/api/carpool', () => ({ carpoolAPI: api }))

interface MessageContext {
  named: (key: string) => unknown
}

function runtimeMessages(value: unknown): unknown {
  if (typeof value === 'string') {
    return (context: MessageContext) => value.replace(/\{(\w+)\}/g, (_match, key: string) => String(context.named(key)))
  }
  if (typeof value === 'object' && value !== null) {
    return Object.fromEntries(Object.entries(value).map(([key, nested]) => [key, runtimeMessages(nested)]))
  }
  return value
}

const pendingDetails: CarpoolDetails = {
  server_now: '2026-09-06T12:00:00+08:00',
  timezone: 'Asia/Shanghai',
  quota: null,
  usage: { total_used_usd: '0.00000000', history_complete: false, statistics_since: null },
  term: {
    status: 'pending',
    starts_at: '2026-09-07T12:00:00+08:00',
    expires_at: '2026-10-05T12:00:00+08:00',
    reset_mode: 'rolling',
    next_natural_reset_at: '2026-09-14T12:00:00+08:00',
    reset_count: 0,
    reset_count_basis: 'current_term',
    current_cycle_no: null,
    cycles: [{ cycle_no: 1, starts_at: '2026-09-07T12:00:00+08:00', ends_at: '2026-09-14T12:00:00+08:00', status: 'scheduled' }],
    reset_events: [],
  },
  reset_window: { status: 'none', scheduled_at: null, schedule_revision: 0, eligible_for_me: false, ineligible_reason: null },
}

const activeDetails: CarpoolDetails = {
  ...pendingDetails,
  quota: { available_usd: '838.00000000', remaining_percent: '99.00000000' },
  usage: { total_used_usd: '12.00000000', history_complete: true, statistics_since: '2026-09-01T12:00:00+08:00' },
  term: {
    ...pendingDetails.term!,
    status: 'active',
    starts_at: '2026-09-01T12:00:00+08:00',
    expires_at: '2026-09-29T12:00:00+08:00',
    next_natural_reset_at: '2026-09-11T22:00:20+08:00',
    reset_count: 2,
    current_cycle_no: 1,
    cycles: [{ cycle_no: 1, starts_at: '2026-09-01T12:00:00+08:00', ends_at: '2026-09-11T22:00:20+08:00', status: 'active' }],
    reset_events: [
      { cycle_no: 1, occurred_at: '2026-09-02T22:00:20+08:00', target_quota_usd: '700.00000000' },
      { cycle_no: 1, occurred_at: '2026-09-04T22:00:20+08:00', target_quota_usd: '700.00000000' },
    ],
  },
}

const boostStatus: CarpoolBoostStatus = {
  eligible: true,
  remaining: 1,
  total: 3,
  amount_usd: '55.00000000',
  help_text: 'authoritative server text',
  unavailable_reason: null,
}

function mountDetailsView(language: 'en' | 'zh' = 'en') {
  const messages = language === 'zh' ? zh : en
  const i18n = createI18n({ legacy: false, locale: language, messages: { [language]: runtimeMessages(messages) as Record<string, unknown> } })
  return mount(CarpoolDetailsView, {
    global: {
      plugins: [i18n],
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        LoadingSpinner: true,
        Icon: true,
      },
    },
  })
}

describe('CarpoolDetailsView', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date', 'performance'] })
    vi.setSystemTime(new Date('2026-09-06T04:00:00Z'))
    setActivePinia(createPinia())
    vi.clearAllMocks()
    api.getDetails.mockResolvedValue(pendingDetails)
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('renders a future rolling term with unavailable quota instead of a false zero', () => {
    const store = useCarpoolStore()
    store.details = pendingDetails
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(pendingDetails)
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: runtimeMessages(en) as Record<string, unknown> } })
    const wrapper = mount(CarpoolDetailsView, {
      global: {
        plugins: [i18n],
        stubs: {
          AppLayout: { template: '<main><slot /></main>' },
          LoadingSpinner: true,
          Icon: true,
        },
      },
    })

    expect(wrapper.text()).toContain('Not started yet')
    expect(wrapper.text()).toContain('Times shown in Asia/Shanghai')
    expect(wrapper.text()).toContain('Earlier usage history is unavailable')
    expect(wrapper.get('[data-testid="carpool-cycle-quota"]').text()).toContain('Quota unavailable')
    expect(wrapper.find('[data-testid="carpool-quota-progress"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="carpool-next-natural-refill"]').text()).toContain('Next natural refill')
    expect(wrapper.get('[data-testid="carpool-next-natural-refill"]').text()).toContain('09/14/2026')
    expect(wrapper.get('[data-testid="carpool-membership-expiry"]').text()).toContain('Fixed membership expiry')
    expect(wrapper.find('[data-testid="carpool-available-quota"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows server-authoritative available quota separately for an active term', () => {
    const store = useCarpoolStore()
    store.details = activeDetails
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(activeDetails)
    const wrapper = mountDetailsView()

    const quota = wrapper.get('[data-testid="carpool-available-quota"]')
    expect(quota.text()).toContain('Carpool available quota')
    expect(quota.text()).toContain('$838.00')
    expect(quota.text()).toContain('Separate from ordinary account balance')
    expect(wrapper.get('[data-testid="carpool-cycle-quota"] .text-3xl').text()).toBe('99%')
    expect(wrapper.get('[data-testid="carpool-cycle-quota"] .text-sm.font-medium.text-gray-500').text()).toBe('remaining')
    expect(wrapper.get('[data-testid="carpool-quota-progress"]').attributes('aria-valuenow')).toBe('99')
    expect(wrapper.get('[data-testid="carpool-quota-progress-fill"]').attributes('style')).toContain('width: 99%')
    expect(wrapper.get('[data-testid="carpool-current-cycle"]').text()).toContain('Current cycle 1')
    wrapper.unmount()
  })

  it('keeps an explicit rolling null as no refill in the final period', () => {
    const store = useCarpoolStore()
    store.details = { ...activeDetails, term: { ...activeDetails.term!, next_natural_reset_at: null } }
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(store.details)
    const wrapper = mountDetailsView()

    expect(wrapper.get('[data-testid="carpool-next-natural-refill"]').text()).toContain('No further natural refill before expiry')
    expect(wrapper.get('[data-testid="carpool-next-natural-refill"]').text()).not.toContain('09/11/2026')
    expect(wrapper.get('[data-testid="carpool-membership-expiry"]').text()).toContain('09/29/2026')
    wrapper.unmount()
  })

  it.each([
    ['0.00000000', '0', 'width: 0%'],
    ['100.00000000', '100', 'width: 100%'],
    ['-12.50000000', '0', 'width: 0%'],
    ['125.50000000', '100', 'width: 100%'],
  ])('renders a server-owned %s remaining quota boundary', (percent, ariaValue, width) => {
    const store = useCarpoolStore()
    store.details = { ...activeDetails, quota: { ...activeDetails.quota!, remaining_percent: percent } }
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(store.details)
    const wrapper = mountDetailsView()

    expect(wrapper.get('[data-testid="carpool-quota-progress"]').attributes('aria-valuenow')).toBe(ariaValue)
    expect(wrapper.get('[data-testid="carpool-quota-progress-fill"]').attributes('style')).toContain(width)
    wrapper.unmount()
  })

  it.each([null, undefined, '', 'not-a-number'])('treats %s as unavailable quota', (remainingPercent) => {
    const store = useCarpoolStore()
    store.details = {
      ...activeDetails,
      quota: { ...activeDetails.quota!, remaining_percent: remainingPercent },
    }
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(store.details)
    const wrapper = mountDetailsView()

    expect(wrapper.get('[data-testid="carpool-cycle-quota"]').text()).toContain('Quota unavailable')
    expect(wrapper.find('[data-testid="carpool-quota-progress"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each(['pending', 'expired', 'terminated'] as const)('does not render stale quota for a %s term', (status) => {
    const store = useCarpoolStore()
    store.details = {
      ...activeDetails,
      term: { ...activeDetails.term!, status },
      quota: { ...activeDetails.quota!, remaining_percent: '99.00000000' },
    }
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(store.details)
    const wrapper = mountDetailsView()

    expect(wrapper.get('[data-testid="carpool-cycle-quota"]').text()).toContain('Quota unavailable')
    expect(wrapper.find('[data-testid="carpool-quota-progress"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('removes reset annotations while preserving the current cycle and accounting history', () => {
    const store = useCarpoolStore()
    store.details = activeDetails
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(activeDetails)
    const wrapper = mountDetailsView('zh')

    expect(wrapper.text()).not.toContain('已加满')
    expect(wrapper.find('[data-testid="carpool-reset-targets"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="carpool-reset-marker"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="carpool-current-cycle"]').text()).toContain('当前第 1 周期')
    expect(wrapper.text()).toContain('账期记录')
    expect(wrapper.text()).toContain('第 1 周期')
    wrapper.unmount()
  })

  it('localizes reset ineligibility codes instead of exposing backend codes', () => {
    const store = useCarpoolStore()
    store.details = {
      ...pendingDetails,
      reset_window: {
        status: 'scheduled',
        scheduled_at: '2026-09-08T22:00:00+08:00',
        schedule_revision: 1,
        eligible_for_me: false,
        ineligible_reason: 'term_expired',
      },
    }
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(store.details)
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: runtimeMessages(en) as Record<string, unknown> } })
    const wrapper = mount(CarpoolDetailsView, {
      global: { plugins: [i18n], stubs: { AppLayout: { template: '<main><slot /></main>' }, LoadingSpinner: true, Icon: true } },
    })

    expect(wrapper.text()).toContain('Your carpool term expired before this reset.')
    expect(wrapper.text()).not.toContain('term_expired')
    wrapper.unmount()
  })

  it('refetches at a server-calibrated reset boundary without optimistic accounting changes', async () => {
    const scheduledDetails: CarpoolDetails = {
      ...activeDetails,
      reset_window: { status: 'scheduled', scheduled_at: '2026-09-06T12:00:02+08:00', schedule_revision: 3, eligible_for_me: true, ineligible_reason: null },
    }
    const authoritativeDetails: CarpoolDetails = {
      ...scheduledDetails,
      server_now: '2026-09-06T12:00:03+08:00',
      usage: { ...scheduledDetails.usage, total_used_usd: '13.00000000' },
      term: { ...scheduledDetails.term!, reset_count: 3 },
      reset_window: { status: 'completed', scheduled_at: scheduledDetails.reset_window.scheduled_at, schedule_revision: 3, eligible_for_me: true, ineligible_reason: null },
      quota: { available_usd: '700.00000000' },
    }
    let resolveBoundary!: (details: CarpoolDetails) => void
    api.getDetails
      .mockResolvedValueOnce(scheduledDetails)
      .mockReturnValueOnce(new Promise((resolve) => { resolveBoundary = resolve }))
    const store = useCarpoolStore()
    store.boosts = { ...boostStatus }
    const wrapper = mountDetailsView()
    await flushPromises()

    expect(api.getDetails).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(api.getDetails).toHaveBeenCalledTimes(2)
    expect(store.details?.term?.reset_count).toBe(2)
    expect(store.details?.usage.total_used_usd).toBe('12.00000000')
    expect(store.boosts).toEqual(boostStatus)

    resolveBoundary(authoritativeDetails)
    await flushPromises()
    expect(store.details?.term?.reset_count).toBe(3)
    expect(store.details?.usage.total_used_usd).toBe('13.00000000')
    expect(store.boosts).toEqual(boostStatus)
    wrapper.unmount()
  })

  it('refetches authoritative details when the current cycle expires', async () => {
    const expiringDetails: CarpoolDetails = {
      ...activeDetails,
      term: {
        ...activeDetails.term!,
        cycles: [{ cycle_no: 1, starts_at: '2026-09-01T12:00:00+08:00', ends_at: '2026-09-06T12:00:02+08:00', status: 'active' }],
      },
    }
    let resolveBoundary!: (details: CarpoolDetails) => void
    api.getDetails
      .mockResolvedValueOnce(expiringDetails)
      .mockReturnValueOnce(new Promise((resolve) => { resolveBoundary = resolve }))
    const wrapper = mountDetailsView()
    await flushPromises()

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(api.getDetails).toHaveBeenCalledTimes(2)
    resolveBoundary(expiringDetails)
    await flushPromises()
    wrapper.unmount()
  })

  it('refetches at the rolling natural-refill boundary', async () => {
    const expiringDetails: CarpoolDetails = {
      ...activeDetails,
      term: { ...activeDetails.term!, next_natural_reset_at: '2026-09-06T12:00:02+08:00' },
    }
    const refreshedDetails: CarpoolDetails = {
      ...expiringDetails,
      server_now: '2026-09-06T12:00:03+08:00',
      term: { ...expiringDetails.term!, next_natural_reset_at: '2026-09-13T12:00:02+08:00' },
    }
    api.getDetails.mockResolvedValueOnce(expiringDetails).mockResolvedValueOnce(refreshedDetails)
    const wrapper = mountDetailsView()
    await flushPromises()

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(api.getDetails).toHaveBeenCalledTimes(2)
    expect(useCarpoolStore().details?.term?.next_natural_reset_at).toBe('2026-09-13T12:00:02+08:00')
    wrapper.unmount()
  })

  it('refetches when a pending rolling membership reaches its start', async () => {
    const startingDetails: CarpoolDetails = {
      ...pendingDetails,
      term: {
        ...pendingDetails.term!,
        starts_at: '2026-09-06T12:00:02+08:00',
        expires_at: '2026-10-04T12:00:02+08:00',
        next_natural_reset_at: '2026-09-13T12:00:02+08:00',
        cycles: [{ cycle_no: 1, starts_at: '2026-09-06T12:00:02+08:00', ends_at: '2026-09-13T12:00:02+08:00', status: 'scheduled' }],
      },
    }
    const activeAtStart: CarpoolDetails = {
      ...startingDetails,
      server_now: '2026-09-06T12:00:03+08:00',
      quota: { available_usd: '550.00000000' },
      term: { ...startingDetails.term!, status: 'active', current_cycle_no: 1, cycles: [{ ...startingDetails.term!.cycles[0], status: 'active' }] },
    }
    api.getDetails.mockResolvedValueOnce(startingDetails).mockResolvedValueOnce(activeAtStart)
    const wrapper = mountDetailsView()
    await flushPromises()

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(api.getDetails).toHaveBeenCalledTimes(2)
    expect(useCarpoolStore().details?.term?.status).toBe('active')
    expect(useCarpoolStore().availableQuotaUsd).toBe('550.00000000')
    wrapper.unmount()
  })

  it('refetches authoritative timing and quota when the page becomes visible', async () => {
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    api.getDetails.mockResolvedValue(activeDetails)
    const wrapper = mountDetailsView()
    await flushPromises()
    vi.clearAllMocks()

    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()

    expect(api.getDetails).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('shows no reset countdown when there is no qualification', async () => {
    api.getDetails.mockResolvedValueOnce(activeDetails)
    const wrapper = mountDetailsView()
    await flushPromises()

    expect(wrapper.text()).toContain('No reset window')
    expect(wrapper.find('.font-mono.text-lg').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(api.getDetails).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('keeps a reset outside the current term explicitly ineligible', async () => {
    const outsideTerm: CarpoolDetails = {
      ...activeDetails,
      reset_window: {
        status: 'scheduled',
        scheduled_at: '2026-10-02T22:00:00+08:00',
        schedule_revision: 4,
        eligible_for_me: false,
        ineligible_reason: 'term_expired',
      },
    }
    api.getDetails.mockResolvedValueOnce(outsideTerm)
    const wrapper = mountDetailsView()
    await flushPromises()

    expect(wrapper.text()).toContain('Your carpool term expired before this reset.')
    expect(wrapper.text()).not.toContain('term_expired')
    wrapper.unmount()
  })
})
