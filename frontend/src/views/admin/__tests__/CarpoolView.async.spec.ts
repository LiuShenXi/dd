import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import CarpoolView from '@/views/admin/CarpoolView.vue'
import enAdminCarpool from '@/i18n/locales/en/admin/carpool'
import zhAdminCarpool from '@/i18n/locales/zh/admin/carpool'
import type { CarpoolAdminTerm, CarpoolLedgerEntry, CarpoolPaymentRecord, CarpoolPlan, CarpoolResetBatch, CarpoolResetObservation } from '@/types/carpool'

const api = vi.hoisted(() => ({
  listPlans: vi.fn(), listTerms: vi.fn(), listCycles: vi.fn(), listLedger: vi.fn(), listBillingExceptions: vi.fn(),
  listResetBatches: vi.fn(), listResetObservations: vi.fn(), listPayments: vi.fn(), addPayment: vi.fn(),
  renewTerm: vi.fn(), terminateTerm: vi.fn(), adjustCycle: vi.fn(), reconcileBillingException: vi.fn(),
  registerResetQualification: vi.fn(), scheduleResetBatch: vi.fn(), createPlanVersion: vi.fn(),
}))
const showSuccess = vi.hoisted(() => vi.fn())
const i18nTranslations = vi.hoisted(() => new Map<string, string>())

vi.mock('@/api/admin', () => ({ adminAPI: { carpool: api } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => i18nTranslations.get(key) ?? key }),
}))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

const plan: CarpoolPlan = {
  plan_id: 3, code: 'four-seat', name: 'Four-seat', version: 1, list_price_cny: '330.00', weekly_quota_usd: '550.00000000',
  cycle_5_quota_usd: null, duration_days: 28, cycle_days: 7, boost_ratio: '0.10000000', boost_amount_usd: '55.00000000',
  boost_count: 3, rounding_mode: 'half_up', enabled: true, is_latest: true,
}

function term(id: number, userId: number): CarpoolAdminTerm {
  return {
    id, user_id: userId, scope_id: 1, group_id: 8, plan_id: plan.plan_id, plan_snapshot: plan,
    starts_at: '2026-09-01T12:00:00+08:00', expires_at: '2026-09-29T12:00:00+08:00', status: 'active',
    boost_used: 0, boost_remaining: 3, history_complete: true, statistics_since: '2026-09-01T12:00:00+08:00',
    payment_net_cny: '0.00', current_cycle: null, created_at: '2026-09-01T12:00:00+08:00',
  }
}

const stubs = {
  AppLayout: { template: '<main><slot /></main>' },
  BaseDialog: { props: ['show'], emits: ['close'], template: '<section v-if="show" class="dialog"><slot /></section>' },
  EmptyState: true, LoadingSpinner: true, Pagination: true, Icon: true,
  Select: { name: 'Select', props: ['modelValue', 'options'], emits: ['update:modelValue'], template: '<span class="select-stub">{{ modelValue }}</span>' },
}

function button(wrapper: VueWrapper, text: string, index = 0) {
  const matches = wrapper.findAll('button').filter((item) => item.text().includes(text))
  if (!matches[index]) throw new Error(`button not found: ${text} at ${index}`)
  return matches[index]
}

describe('CarpoolView async identity guards', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    i18nTranslations.clear()
    api.listPlans.mockResolvedValue([plan])
    api.listTerms.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    api.listCycles.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    api.listLedger.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    api.listBillingExceptions.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    api.listResetBatches.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    api.listResetObservations.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    api.listPayments.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 50 })
    api.addPayment.mockResolvedValue({})
    api.renewTerm.mockResolvedValue({})
    api.terminateTerm.mockResolvedValue({})
  })

  it('keeps the newest term load when an older refresh resolves last', async () => {
    let resolveFirst!: (value: { items: CarpoolAdminTerm[]; total: number; page: number; page_size: number }) => void
    api.listTerms
      .mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce({ items: [term(2, 202)], total: 1, page: 1, page_size: 20 })
    const wrapper = mount(CarpoolView, { global: { stubs } })
    await flushPromises()

    await button(wrapper, 'carpool.refresh').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('#202')

    resolveFirst({ items: [term(1, 101)], total: 1, page: 1, page_size: 20 })
    await flushPromises()
    expect(wrapper.text()).toContain('#202')
    expect(wrapper.text()).not.toContain('#101')
  })

  it('does not let a delayed payment success close a newly selected term', async () => {
    const first = term(1, 101)
    const second = term(2, 202)
    let resolvePayment!: (value: unknown) => void
    const secondHistory = { id: 22, term_id: 2, amount_cny: '20.00', payment_kind: 'payment', paid_at: '2026-09-05T12:00:00+08:00', channel: 'second-history', external_order_no: null, recorded_by: 1, notes: null, recorded_at: '2026-09-05T12:00:00+08:00' } as CarpoolPaymentRecord
    api.listTerms.mockResolvedValue({ items: [first, second], total: 2, page: 1, page_size: 20 })
    api.listPayments.mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 50 }).mockResolvedValueOnce({ items: [secondHistory], total: 1, page: 1, page_size: 50 })
    api.addPayment.mockReturnValueOnce(new Promise((resolve) => { resolvePayment = resolve }))
    const wrapper = mount(CarpoolView, { global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.actions.payment', 0).trigger('click')
    await flushPromises()
    await wrapper.get('.dialog input[type="number"]').setValue('10')
    await wrapper.get('.dialog input[type="datetime-local"]').setValue('2026-09-05T12:00')
    await wrapper.get('.dialog form').trigger('submit')
    await button(wrapper, 'admin.carpool.actions.payment', 1).trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('second-history')

    resolvePayment({})
    await flushPromises()
    expect(wrapper.find('.dialog').exists()).toBe(true)
    expect(wrapper.text()).toContain('second-history')
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('closes and clears a successful payment before its refresh resolves', async () => {
    const first = term(1, 101)
    let resolveRefresh!: (value: { items: CarpoolAdminTerm[]; total: number; page: number; page_size: number }) => void
    api.listTerms.mockResolvedValueOnce({ items: [first], total: 1, page: 1, page_size: 20 }).mockReturnValueOnce(new Promise((resolve) => { resolveRefresh = resolve }))
    const wrapper = mount(CarpoolView, { global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.actions.payment').trigger('click')
    await flushPromises()
    await wrapper.get('.dialog input[type="number"]').setValue('10')
    await wrapper.get('.dialog input[type="datetime-local"]').setValue('2026-09-05T12:00')
    await wrapper.get('.dialog form').trigger('submit')
    await flushPromises()

    expect(api.addPayment).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.dialog').exists()).toBe(false)
    expect(showSuccess).toHaveBeenCalledTimes(1)
    resolveRefresh({ items: [first], total: 1, page: 1, page_size: 20 })
    await flushPromises()
  })

  it('does not let a stale cross-dialog success close the current action', async () => {
    const first = term(1, 101)
    let resolveRenew!: (value: unknown) => void
    api.listTerms.mockResolvedValue({ items: [first], total: 1, page: 1, page_size: 20 })
    api.renewTerm.mockReturnValueOnce(new Promise((resolve) => { resolveRenew = resolve }))
    const wrapper = mount(CarpoolView, { global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.actions.renew', 0).trigger('click')
    await wrapper.get('.dialog form').trigger('submit')
    await button(wrapper, 'common.cancel').trigger('click')
    await button(wrapper, 'admin.carpool.actions.terminate', 0).trigger('click')
    await flushPromises()

    resolveRenew({})
    await flushPromises()
    expect(wrapper.find('.dialog textarea').exists()).toBe(true)
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('does not let a stale cross-dialog error overwrite the current action', async () => {
    const first = term(1, 101)
    let rejectRenew!: (reason: unknown) => void
    api.listTerms.mockResolvedValue({ items: [first], total: 1, page: 1, page_size: 20 })
    api.renewTerm.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectRenew = reject }))
    const wrapper = mount(CarpoolView, { global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.actions.renew', 0).trigger('click')
    await wrapper.get('.dialog form').trigger('submit')
    await button(wrapper, 'common.cancel').trigger('click')
    await button(wrapper, 'admin.carpool.actions.terminate', 0).trigger('click')
    await flushPromises()

    rejectRenew(new Error('stale renewal failure'))
    await flushPromises()
    expect(wrapper.find('.dialog textarea').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('stale renewal failure')
  })

  it('offers only enabled plans and maps a superseded term to its latest tier version', async () => {
    const oldTerm = { ...term(1, 101), plan_id: 2, plan_snapshot: { ...plan, plan_id: 2, version: 1 } }
    const replacement = { ...plan, plan_id: 4, version: 2 }
    const disabled = { ...plan, plan_id: 5, code: 'disabled-tier', name: 'Disabled', enabled: false }
    api.listPlans.mockResolvedValue([replacement, disabled])
    api.listTerms.mockResolvedValue({ items: [oldTerm], total: 1, page: 1, page_size: 20 })
    const wrapper = mount(CarpoolView, { global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.actions.renew', 0).trigger('click')
    const select = wrapper.get('.dialog').findComponent({ name: 'Select' })
    expect(select.props('modelValue')).toBe(4)
    expect(select.props('options')).toEqual([{ value: 4, label: 'Four-seat' }])
    await wrapper.get('.dialog form').trigger('submit')
    await flushPromises()
    expect(api.renewTerm).toHaveBeenCalledWith(1, { plan_id: 4, notes: null, payment: null }, expect.any(String))
  })

  it('lets administrators change the snapshot boost count from three to two', async () => {
    api.createPlanVersion.mockResolvedValue({ ...plan, plan_id: 4, version: 2 })
    const wrapper = mount(CarpoolView, { global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.tabs.plans').trigger('click')
    await flushPromises()
    await button(wrapper, 'admin.carpool.actions.editPlan').trigger('click')
    const boostCount = wrapper.get('.dialog input[type="number"][min="2"]')
    expect(boostCount.attributes('disabled')).toBeUndefined()
    expect((boostCount.element as HTMLInputElement).value).toBe('3')
    await boostCount.setValue('2')
    await wrapper.get('.dialog form').trigger('submit')
    await flushPromises()

    expect(api.createPlanVersion).toHaveBeenCalledWith(3, {
      name: 'Four-seat',
      list_price_cny: '330.00',
      weekly_quota_usd: '550.00000000',
      boost_ratio: '0.10000000',
      boost_count: 2,
      enabled: true,
    }, expect.any(String))
  })

  it('localizes known reset values while preserving unknown values and administrator text', async () => {
    const reset = zhAdminCarpool.carpool.reset
    const batches: CarpoolResetBatch[] = [
      ['qualified', 'invalid_schedule'],
      ['scheduled', 'cooldown'],
      ['running', 'missed'],
      ['completed', 'execution_boundary_moved'],
      ['cancelled', 'Administrator entered this reason'],
      ['needs_review', null],
      ['future_status', 'future_delay_code'],
    ].map(([status, delayReason], index) => ({
      id: index + 1,
      scope_id: 1,
      status: String(status),
      schedule_revision: 1,
      delay_reason: delayReason,
    }))
    const observations: CarpoolResetObservation[] = ['unknown', 'healthy', 'incomplete', 'error', 'future_health'].map((healthStatus, index) => ({
      upstream_identity_hash: `identity-${index}`,
      baseline_complete: true,
      last_complete_at: null,
      health_status: healthStatus,
      known_credit_count: 1,
      unassigned_credit_count: 0,
    }))
    Object.entries(reset.batchStatus).forEach(([key, value]) => i18nTranslations.set(`admin.carpool.reset.batchStatus.${key}`, value))
    Object.entries(reset.healthStatus).forEach(([key, value]) => i18nTranslations.set(`admin.carpool.reset.healthStatus.${key}`, value))
    Object.entries(reset.delayReason).forEach(([key, value]) => i18nTranslations.set(`admin.carpool.reset.delayReason.${key}`, value))
    i18nTranslations.set('admin.carpool.reset.health', reset.health)
    api.listResetBatches.mockResolvedValue({ items: batches, total: batches.length, page: 1, page_size: 20 })
    api.listResetObservations.mockResolvedValue({ items: observations, total: observations.length, page: 1, page_size: 20 })
    const wrapper = mount(CarpoolView, { global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.tabs.resets').trigger('click')
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain(reset.health)
    Object.values(reset.batchStatus).forEach((label) => expect(text).toContain(label))
    Object.values(reset.healthStatus).forEach((label) => expect(text).toContain(label))
    Object.values(reset.delayReason).forEach((label) => expect(text).toContain(label))
    expect(text).toContain('future_status')
    expect(text).toContain('future_health')
    expect(text).toContain('Administrator entered this reason')
    expect(text).toContain('future_delay_code')
  })

  it.each([
    { locale: 'zh', messages: zhAdminCarpool },
    { locale: 'en', messages: enAdminCarpool },
  ])('localizes every ledger event and bucket in $locale while preserving unknown and audit values', async ({ messages }) => {
    const eventLabels = Object.entries(messages.carpool.ledgerEvent)
    const bucketLabels = Object.entries(messages.carpool.bucket)
    const entries: CarpoolLedgerEntry[] = eventLabels.map(([eventType], index) => ({
      id: index + 1,
      user_id: 101,
      term_id: 201,
      cycle_id: 301,
      event_type: eventType,
      bucket: bucketLabels[index % bucketLabels.length][0] as CarpoolLedgerEntry['bucket'],
      delta_usd: '1.00000000',
      event_key: `audit-reference-${index}`,
      api_key_id: null,
      request_id: null,
      reset_batch_id: null,
      boost_slot: null,
      actor_id: 1,
      reverses_ledger_id: null,
      reason: `Administrator reason ${index}`,
      effective_at: '2026-09-06T12:00:00+08:00',
      recorded_at: '2026-09-06T12:00:00+08:00',
    }))
    entries.push({
      ...entries[0],
      id: 99,
      event_type: 'future_event',
      bucket: 'future_bucket' as CarpoolLedgerEntry['bucket'],
      event_key: 'Administrator reference untouched',
      reason: 'Administrator reason untouched',
    })
    eventLabels.forEach(([key, value]) => i18nTranslations.set(`admin.carpool.ledgerEvent.${key}`, value))
    bucketLabels.forEach(([key, value]) => i18nTranslations.set(`admin.carpool.bucket.${key}`, value))
    api.listLedger.mockResolvedValue({ items: entries, total: entries.length, page: 1, page_size: 20 })
    const wrapper = mount(CarpoolView, { global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.tabs.ledger').trigger('click')
    await flushPromises()

    const rows = wrapper.get('tbody').findAll('tr')
    eventLabels.forEach(([, eventLabel], index) => {
      const cells = rows[index].findAll('td')
      expect(cells[2].text()).toBe(eventLabel)
      expect(cells[3].text()).toBe(bucketLabels[index % bucketLabels.length][1])
    })
    const unknownCells = rows[entries.length - 1].findAll('td')
    expect(unknownCells[2].text()).toBe('future_event')
    expect(unknownCells[3].text()).toBe('future_bucket')
    expect(unknownCells[5].text()).toBe('Administrator reference untouched')
    expect(unknownCells[7].text()).toBe('Administrator reason untouched')
  })
})
