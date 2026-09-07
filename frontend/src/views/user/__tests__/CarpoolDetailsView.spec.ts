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
  quota: { available_usd: '838.00000000' },
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

  it('renders a future rolling term as one continuous membership rail', () => {
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
    expect(wrapper.get('[data-testid="carpool-time-rail"]').attributes('style')).toContain('1fr')
    expect(wrapper.findAll('[data-testid="carpool-time-rail"] > div')).toHaveLength(1)
    expect(wrapper.get('[data-testid="carpool-next-natural-refill"]').text()).toContain('Next natural refill')
    expect(wrapper.get('[data-testid="carpool-next-natural-refill"]').text()).toContain('09/14/2026')
    expect(wrapper.get('[data-testid="carpool-membership-expiry"]').text()).toContain('Fixed membership expiry')
    expect(wrapper.find('[data-testid="carpool-cycle-labels"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="carpool-time-rail"]').text().toLowerCase()).not.toContain('quota')
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
    wrapper.unmount()
  })

  it('preserves a legacy five-cycle snapshot shape from its actual dates', () => {
    const legacyCycles = Array.from({ length: 4 }, (_, index) => ({
      cycle_no: index + 1,
      starts_at: new Date(Date.parse('2026-09-07T12:00:00+08:00') + index * 7 * 86_400_000).toISOString(),
      ends_at: new Date(Date.parse('2026-09-07T12:00:00+08:00') + (index + 1) * 7 * 86_400_000).toISOString(),
      status: 'scheduled' as const,
    }))
    const store = useCarpoolStore()
    store.details = {
      ...pendingDetails,
      term: {
        ...pendingDetails.term!,
        reset_mode: 'fixed',
        next_natural_reset_at: undefined,
        expires_at: '2026-10-07T12:00:00+08:00',
        reset_count_basis: 'current_term',
        cycles: [
          ...legacyCycles,
          { cycle_no: 5, starts_at: legacyCycles[3].ends_at, ends_at: '2026-10-07T12:00:00+08:00', status: 'scheduled' },
        ],
      },
    }
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(store.details)
    const wrapper = mountDetailsView()

    expect(wrapper.get('[data-testid="carpool-time-rail"]').attributes('style')).toContain('7fr 7fr 7fr 7fr 2fr')
    expect(wrapper.get('[data-testid="carpool-cycle-labels"]').text()).toContain('5')
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

  it('keeps nearby target and Shanghai-time pairs together above the rail', () => {
    const store = useCarpoolStore()
    store.details = activeDetails
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(activeDetails)
    const wrapper = mountDetailsView()

    const targets = wrapper.get('[data-testid="carpool-reset-targets"]')
    const times = wrapper.get('[data-testid="carpool-reset-times"]')
    const rail = wrapper.get('[data-testid="carpool-time-rail"]')
    expect(targets.text()).toContain('Filled to $700')
    expect(wrapper.findAll('[data-testid="carpool-reset-marker"]')).toHaveLength(2)
    expect(wrapper.findAll('[data-testid="carpool-reset-marker"]')[0].attributes('style')).toContain('left:')
    expect(times.text()).toContain('09/02/2026')
    expect(times.text()).toContain('09/04/2026')
    expect(times.text()).toContain('22:00:20')
    expect(times.element.compareDocumentPosition(rail.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(wrapper.findAll('.carpool-timeline__annotation').map((annotation) => annotation.attributes('data-annotation-lane'))).toEqual(['0', '0'])
    expect(wrapper.findAll('.carpool-timeline__leaders path')).toHaveLength(2)
    expect(wrapper.findAll('[data-testid="carpool-reset-marker"]').every((marker) => marker.text() === '')).toBe(true)
    wrapper.unmount()
  })

  it('clamps edge annotations and wraps only when a compact row is full', () => {
    const store = useCarpoolStore()
    store.details = {
      ...activeDetails,
      term: {
        ...activeDetails.term!,
        reset_count: 3,
        reset_events: [
          { cycle_no: 1, occurred_at: activeDetails.term!.starts_at, target_quota_usd: '700.00000000' },
          { cycle_no: 1, occurred_at: '2026-09-01T12:01:00+08:00', target_quota_usd: '1234567890.12300000' },
          { cycle_no: 4, occurred_at: '2026-09-29T11:59:59+08:00', target_quota_usd: '700.00000000' },
        ],
      },
    }
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(store.details)
    const wrapper = mountDetailsView()
    const annotations = wrapper.findAll('.carpool-timeline__annotation')

    expect(annotations).toHaveLength(3)
    expect(annotations[0].attributes('style')).toContain('left: 0px')
    expect(annotations[1].attributes('data-annotation-lane')).toBe('0')
    expect(annotations[1].attributes('style')).toContain('height: 64px')
    expect(annotations[2].attributes('style')).toContain('left: 192px')
    expect(annotations[2].attributes('data-annotation-lane')).toBe('1')
    expect(wrapper.get('[data-testid="carpool-reset-targets"]').text()).toContain('Filled to $1,234,567,890.12')
    wrapper.unmount()
  })

  it('keeps a sparse-left and dense-right desktop row inside the measured rail', async () => {
    let resizeCallback: ResizeObserverCallback | undefined
    class ResizeObserverMock {
      constructor(callback: ResizeObserverCallback) {
        resizeCallback = callback
      }

      observe() {}
      disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
    const store = useCarpoolStore()
    store.details = {
      ...activeDetails,
      term: {
        ...activeDetails.term!,
        reset_count: 5,
        reset_events: [
          { cycle_no: 1, occurred_at: '2026-09-01T12:00:00+08:00', target_quota_usd: '700.00000000' },
          { cycle_no: 3, occurred_at: '2026-09-21T12:00:00+08:00', target_quota_usd: '700.00000000' },
          { cycle_no: 4, occurred_at: '2026-09-23T12:00:00+08:00', target_quota_usd: '700.00000000' },
          { cycle_no: 4, occurred_at: '2026-09-25T12:00:00+08:00', target_quota_usd: '700.00000000' },
          { cycle_no: 4, occurred_at: '2026-09-27T12:00:00+08:00', target_quota_usd: '700.00000000' },
        ],
      },
    }
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(store.details)
    const wrapper = mountDetailsView()
    await wrapper.vm.$nextTick()
    expect(resizeCallback).toBeDefined()

    resizeCallback!([{ contentRect: { width: 1120 } } as ResizeObserverEntry], {} as ResizeObserver)
    await wrapper.vm.$nextTick()
    const annotations = wrapper.findAll('.carpool-timeline__annotation')
    const boxes = annotations.map((annotation) => {
      const style = annotation.attributes('style')
      return {
        left: Number(style.match(/left: (-?[\d.]+)px/)?.[1]),
        width: Number(style.match(/width: ([\d.]+)px/)?.[1]),
      }
    })

    expect(annotations).toHaveLength(5)
    expect(annotations.every((annotation) => annotation.attributes('data-annotation-lane') === '0')).toBe(true)
    expect(boxes[0].left).toBeGreaterThanOrEqual(0)
    expect(boxes.at(-1)!.left + boxes.at(-1)!.width).toBeLessThanOrEqual(1120)
    boxes.slice(1).forEach((box, index) => {
      expect(box.left).toBeGreaterThanOrEqual(boxes[index].left + boxes[index].width + 10)
    })
    wrapper.unmount()
  })

  it('uses the same paired above-rail annotation contract in Chinese', () => {
    const store = useCarpoolStore()
    store.details = activeDetails
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(activeDetails)
    const wrapper = mountDetailsView('zh')
    const annotations = wrapper.get('[data-testid="carpool-reset-targets"]')

    expect(annotations.text()).toContain('已加满至 $700')
    expect(annotations.text()).toContain('2026/09/02')
    expect(annotations.text()).toContain('22:00:20')
    wrapper.unmount()
  })

  it('bounds dense history while keeping every exact node and an accessible full disclosure', async () => {
    const resetEvents = Array.from({ length: 14 }, (_, index) => ({
      cycle_no: Math.min(4, Math.floor((index * 2 + 1) / 7) + 1),
      occurred_at: new Date(Date.parse(activeDetails.term!.starts_at) + (index * 2 + 1) * 86_400_000).toISOString(),
      target_quota_usd: '700.00000000',
    }))
    const store = useCarpoolStore()
    store.details = {
      ...activeDetails,
      term: { ...activeDetails.term!, reset_count: resetEvents.length, reset_events: resetEvents },
    }
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(store.details)
    const wrapper = mountDetailsView()

    expect(wrapper.findAll('[data-testid="carpool-reset-marker"]')).toHaveLength(14)
    expect(wrapper.findAll('.carpool-timeline__annotation')).toHaveLength(2)
    expect(wrapper.get('[data-testid="carpool-reset-targets"]').attributes('style')).toContain('height: 90px')
    expect(wrapper.get('[data-testid="carpool-reset-targets"]').text()).toContain('09/26/2026')
    expect(wrapper.get('[data-testid="carpool-reset-targets"]').text()).toContain('09/28/2026')
    expect(wrapper.get('[data-testid="carpool-reset-targets"]').text()).not.toContain('09/02/2026')
    const toggle = wrapper.get('[data-testid="carpool-reset-history-toggle"]')
    expect(toggle.element.tagName).toBe('BUTTON')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(toggle.text()).toContain('Resets this term · 14')
    expect(wrapper.get('[data-testid="carpool-reset-history"]').isVisible()).toBe(false)

    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('[data-testid="carpool-reset-history"]').attributes('style') ?? '').not.toContain('display: none')
    expect(wrapper.findAll('[data-testid="carpool-reset-history-item"]')).toHaveLength(14)
    expect(wrapper.get('[data-testid="carpool-reset-history"]').text()).toContain('09/02/2026')
    expect(wrapper.get('[data-testid="carpool-reset-history"]').text()).toContain('09/28/2026')
    wrapper.unmount()
  })

  it('keeps arbitrary dense reset history within a 390px rail', async () => {
    let resizeCallback: ResizeObserverCallback | undefined
    class ResizeObserverMock {
      constructor(callback: ResizeObserverCallback) { resizeCallback = callback }
      observe() {}
      disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
    const resetEvents = Array.from({ length: 8 }, (_, index) => ({
      cycle_no: index + 1,
      occurred_at: new Date(Date.parse(activeDetails.term!.starts_at) + (index + 1) * 2 * 86_400_000).toISOString(),
      target_quota_usd: `${700 + index}.00000000`,
    }))
    const store = useCarpoolStore()
    store.details = { ...activeDetails, term: { ...activeDetails.term!, reset_count: resetEvents.length, reset_events: resetEvents } }
    vi.spyOn(store, 'fetchDetails').mockResolvedValue(store.details)
    const wrapper = mountDetailsView()
    await wrapper.vm.$nextTick()

    resizeCallback!([{ contentRect: { width: 390 } } as ResizeObserverEntry], {} as ResizeObserver)
    await wrapper.vm.$nextTick()

    expect(wrapper.findAll('[data-testid="carpool-reset-marker"]')).toHaveLength(8)
    expect(wrapper.findAll('[data-testid="carpool-reset-history-item"]')).toHaveLength(8)
    wrapper.findAll('.carpool-timeline__annotation').forEach((annotation) => {
      const style = annotation.attributes('style')
      const left = Number(style.match(/left: (-?[\d.]+)px/)?.[1])
      const width = Number(style.match(/width: ([\d.]+)px/)?.[1])
      expect(left).toBeGreaterThanOrEqual(0)
      expect(left + width).toBeLessThanOrEqual(390)
    })
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

    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()

    expect(api.getDetails).toHaveBeenCalledTimes(2)
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
