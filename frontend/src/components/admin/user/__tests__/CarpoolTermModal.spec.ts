import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import CarpoolTermModal from '@/components/admin/user/CarpoolTermModal.vue'
import enAdminCarpool from '@/i18n/locales/en/admin/carpool'
import zhAdminCarpool from '@/i18n/locales/zh/admin/carpool'
import type { AdminUser } from '@/types'
import type { CarpoolAdminTerm, CarpoolPlan } from '@/types/carpool'

const api = vi.hoisted(() => ({
  listPlans: vi.fn(),
  listTerms: vi.fn(),
  previewTerm: vi.fn(),
  openTerm: vi.fn(),
  renewTerm: vi.fn(),
  getAll: vi.fn(),
}))
const i18nTranslations = vi.hoisted(() => new Map<string, string>())

vi.mock('@/api/admin', () => ({
  adminAPI: {
    carpool: {
      listPlans: api.listPlans,
      listTerms: api.listTerms,
      previewTerm: api.previewTerm,
      openTerm: api.openTerm,
      renewTerm: api.renewTerm,
    },
    groups: { getAll: api.getAll },
  },
}))

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => i18nTranslations.get(key) ?? key }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

const plan: CarpoolPlan = {
  plan_id: 3,
  code: 'four-seat',
  name: 'Four-seat',
  version: 2,
  list_price_cny: '330.00',
  weekly_quota_usd: '550.00000000',
  cycle_5_quota_usd: null,
  duration_days: 28,
  cycle_days: 7,
  reset_mode: 'rolling',
  boost_ratio: '0.10000000',
  boost_amount_usd: '55.00000000',
  boost_count: 3,
  rounding_mode: 'half_up',
  enabled: true,
  is_latest: true,
}

const activeTerm: CarpoolAdminTerm = {
  id: 9,
  user_id: 42,
  scope_id: 1,
  group_id: 8,
  plan_id: 3,
  plan_snapshot: plan,
  starts_at: '2026-09-01T12:00:00+08:00',
  expires_at: '2026-09-29T12:00:00+08:00',
  next_natural_reset_at: '2026-09-08T12:00:00+08:00',
  status: 'active',
  boost_used: 0,
  boost_remaining: 3,
  history_complete: true,
  statistics_since: '2026-09-01T12:00:00+08:00',
  payment_net_cny: '330.00',
  current_cycle: null,
  created_at: '2026-09-01T12:00:00+08:00',
}

const largerPlan: CarpoolPlan = { ...plan, plan_id: 4, code: 'larger', name: 'Larger', list_price_cny: '700.00', weekly_quota_usd: '1100.00000000' }

const user = { id: 42, email: 'synthetic@example.invalid' } as AdminUser

const stubs = {
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
  LoadingSpinner: true,
  Select: { name: 'Select', props: ['modelValue', 'options'], emits: ['update:modelValue'], template: '<span class="select-stub">{{ modelValue }}</span>' },
  Icon: true,
}

function button(wrapper: VueWrapper, text: string) {
  const match = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!match) throw new Error(`button not found: ${text}`)
  return match
}

function inputFor(wrapper: VueWrapper, labelText: string) {
  const label = wrapper.findAll('label').find((item) => item.text().includes(labelText))
  if (!label) throw new Error(`label not found: ${labelText}`)
  return label.get('input')
}

describe('CarpoolTermModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    i18nTranslations.clear()
    api.listPlans.mockResolvedValue([plan])
    api.getAll.mockResolvedValue([{ id: 8, name: 'Synthetic carpool', subscription_type: 'carpool', platform: 'openai', status: 'active' }])
    api.listTerms.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    api.openTerm.mockResolvedValue(activeTerm)
    api.renewTerm.mockResolvedValue(activeTerm)
  })

  it('submits takeover opening balances as final balances and retains its key across retry', async () => {
    api.openTerm.mockRejectedValueOnce(new Error('ambiguous network failure')).mockResolvedValueOnce(activeTerm)
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.modes.takeover').trigger('click')
    await inputFor(wrapper, 'admin.carpool.takeover.baseBalance').setValue('10.00000000')
    await inputFor(wrapper, 'admin.carpool.takeover.boostBalance').setValue('2.00000000')
    await inputFor(wrapper, 'admin.carpool.takeover.manualBalance').setValue('3.00000000')
    expect(wrapper.text()).not.toContain('admin.carpool.takeover.transfer')
    await inputFor(wrapper, 'admin.carpool.takeover.boostUsed').setValue('1')
    await inputFor(wrapper, 'admin.carpool.takeover.historyComplete').setValue(true)
    await inputFor(wrapper, 'admin.carpool.takeover.historicalUsed').setValue('4.00000000')
    await inputFor(wrapper, 'admin.carpool.takeover.statisticsSince').setValue('2026-09-02T10:30')
    await inputFor(wrapper, 'admin.carpool.takeover.nextNaturalRefill').setValue('2026-09-08T12:00')

    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(api.openTerm).toHaveBeenCalledTimes(2)
    expect(api.openTerm.mock.calls[0][2]).toBe(api.openTerm.mock.calls[1][2])
    expect(api.openTerm).toHaveBeenLastCalledWith(42, {
      plan_id: 3,
      group_id: 8,
      starts_at: null,
      mode: 'takeover',
      takeover: {
        current_base_balance_usd: '10',
        current_boost_balance_usd: '2',
        current_manual_balance_usd: '3',
        boost_used: 1,
        history_complete: true,
        historical_used_usd: '4',
        statistics_since: '2026-09-02T02:30:00.000Z',
        next_natural_reset_at: '2026-09-08T04:00:00.000Z',
      },
      notes: null,
      payment: null,
    }, expect.any(String))
    expect(api.openTerm.mock.calls[1][1]).not.toHaveProperty('scope_id')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('accepts all three used boosts when taking over a v1.4 term', async () => {
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.modes.takeover').trigger('click')
    const boostUsed = inputFor(wrapper, 'admin.carpool.takeover.boostUsed')
    expect(boostUsed.attributes('max')).toBe('3')
    await boostUsed.setValue('3')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(api.openTerm).toHaveBeenCalledWith(42, expect.objectContaining({
      takeover: expect.objectContaining({ boost_used: 3 }),
    }), expect.any(String))
  })

  it('discovers an active term and renews it without opening a second overlapping term', async () => {
    api.listTerms.mockResolvedValue({ items: [activeTerm], total: 1, page: 1, page_size: 20 })
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()

    expect(wrapper.text()).toContain('Four-seat')
    await button(wrapper, 'admin.carpool.actions.renew').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(api.openTerm).not.toHaveBeenCalled()
    expect(api.renewTerm).toHaveBeenCalledWith(9, { plan_id: 3, notes: null, payment: null }, expect.any(String))
  })

  it('renews a superseded plan through the latest enabled version of the same tier', async () => {
    const oldTerm = { ...activeTerm, plan_id: 2, plan_snapshot: { ...plan, plan_id: 2, version: 1 } }
    api.listTerms.mockResolvedValue({ items: [oldTerm], total: 1, page: 1, page_size: 20 })
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.actions.renew').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(api.renewTerm).toHaveBeenCalledWith(9, { plan_id: 3, notes: null, payment: null }, expect.any(String))
  })

  it('does not guess another tier when the original plan code is disabled', async () => {
    const disabledTerm = { ...activeTerm, plan_snapshot: { ...plan, code: 'disabled-tier' } }
    api.listPlans.mockResolvedValue([largerPlan, { ...plan, plan_id: 5, code: 'disabled-tier', enabled: false }])
    api.listTerms.mockResolvedValue({ items: [disabledTerm], total: 1, page: 1, page_size: 20 })
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.actions.renew').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(api.renewTerm).not.toHaveBeenCalled()
  })

  it('previews renewal from the existing expiry with new-term semantics', async () => {
    api.listTerms.mockResolvedValue({ items: [activeTerm], total: 1, page: 1, page_size: 20 })
    api.previewTerm.mockResolvedValue({ calculated_at: activeTerm.starts_at, mode: 'new', plan, starts_at: activeTerm.expires_at, expires_at: activeTerm.expires_at, cycles: [], warnings: [] })
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.actions.renew').trigger('click')
    await button(wrapper, 'admin.carpool.preview.action').trigger('click')
    await flushPromises()

    expect(api.previewTerm).toHaveBeenCalledWith(42, { plan_id: 3, starts_at: activeTerm.expires_at, mode: 'new', takeover: null })
  })

  it.each([{ quota: '450.00000000', days: 28, display: '$450.00' }, { quota: '1000.00000000', days: 28, display: '$1,000.00' }, { quota: '550.00000000', days: 7, display: '$550.00' }])('preserves customized $quota quota and $days days in default renewal and server preview', async ({ quota, days, display }) => {
    const snapshot = { ...plan, weekly_quota_usd: quota, duration_days: days, weekly_quota_customized: quota !== plan.weekly_quota_usd, duration_customized: days !== plan.duration_days }
    const term = { ...activeTerm, group_id: 2, plan_snapshot: snapshot }
    api.getAll.mockResolvedValue([{ id: 2, name: 'Original routing', subscription_type: 'standard', platform: 'openai', status: 'active' }])
    api.listTerms.mockResolvedValue({ items: [term], total: 1, page: 1, page_size: 20 })
    api.previewTerm.mockResolvedValue({ calculated_at: term.starts_at, mode: 'new', plan: snapshot, starts_at: term.expires_at, expires_at: '2026-10-06T12:00:00+08:00', cycles: [], warnings: [] })
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()
    await button(wrapper, 'admin.carpool.actions.renew').trigger('click')
    expect(wrapper.findComponent({ name: 'Select' }).props('modelValue')).toBe(0)
    await button(wrapper, 'admin.carpool.preview.action').trigger('click')
    await flushPromises()
    expect(api.previewTerm).toHaveBeenCalledWith(42, { plan_id: 0, renew_from_term_id: 9, starts_at: term.expires_at, mode: 'new', takeover: null })
    expect(wrapper.get('dl').text()).toContain(display)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.renewTerm).toHaveBeenCalledWith(9, { plan_id: 0, notes: null, payment: null }, expect.any(String))
    expect(api.openTerm).not.toHaveBeenCalled()
  })

  it('sends an explicit new plan when changing a customized renewal', async () => {
    api.listTerms.mockResolvedValue({ items: [{ ...activeTerm, plan_snapshot: { ...plan, weekly_quota_usd: '450.00000000', weekly_quota_customized: true } }], total: 1 })
    api.listPlans.mockResolvedValue([plan, largerPlan])
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()
    await button(wrapper, 'admin.carpool.actions.renew').trigger('click')
    await wrapper.findComponent({ name: 'Select' }).vm.$emit('update:modelValue', largerPlan.plan_id)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.renewTerm).toHaveBeenCalledWith(9, { plan_id: largerPlan.plan_id, notes: null, payment: null }, expect.any(String))
  })

  it('opens former members and new users on the original standard group', async () => {
    api.getAll.mockResolvedValue([{ id: 2, name: 'Original routing', subscription_type: 'standard', platform: 'openai', status: 'active' }])
    api.listTerms.mockResolvedValue({ items: [{ ...activeTerm, group_id: 2, status: 'expired' }], total: 1 })
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()
    await button(wrapper, 'admin.carpool.actions.open').trigger('click')
    expect(wrapper.findAllComponents({ name: 'Select' })[1].props('modelValue')).toBe(2)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.openTerm).toHaveBeenCalledWith(42, expect.objectContaining({ group_id: 2, plan_id: 3, mode: 'new' }), expect.any(String))
    api.openTerm.mockClear()
    api.listTerms.mockResolvedValue({ items: [], total: 0 })
    await wrapper.setProps({ user: { ...user, id: 43 } })
    await flushPromises()
    expect(wrapper.findAllComponents({ name: 'Select' })[1].props('options')).toEqual([{ value: 2, label: 'Original routing' }])
    expect(wrapper.findAllComponents({ name: 'Select' })[1].props('modelValue')).toBe(2)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.openTerm).toHaveBeenCalledWith(43, expect.objectContaining({ group_id: 2, plan_id: 3, mode: 'new', starts_at: null }), expect.any(String))
  })

  it('offers only active OpenAI standard or carpool groups for new membership', async () => {
    api.getAll.mockResolvedValue([
      { id: 2, name: 'Original routing', subscription_type: 'standard', platform: 'openai', status: 'active' },
      { id: 8, name: 'Existing carpool', subscription_type: 'carpool', platform: 'openai', status: 'active' },
      { id: 9, name: 'Inactive', subscription_type: 'standard', platform: 'openai', status: 'inactive' },
      { id: 10, name: 'Other platform', subscription_type: 'standard', platform: 'anthropic', status: 'active' },
      { id: 11, name: 'Subscription billing', subscription_type: 'subscription', platform: 'openai', status: 'active' },
    ])
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()
    expect(wrapper.findAllComponents({ name: 'Select' })[1].props('options')).toEqual([
      { value: 2, label: 'Original routing' }, { value: 8, label: 'Existing carpool' },
    ])
    expect(zhAdminCarpool.carpool.fields.group).toBe('分组')
    expect(enAdminCarpool.carpool.fields.group).toBe('Group')
  })

  it.each([
    { locale: 'zh', messages: zhAdminCarpool },
    { locale: 'en', messages: enAdminCarpool },
  ])('localizes every preview action in $locale while preserving unknown actions and warnings', async ({ messages }) => {
    const initialActionLabels = Object.entries(messages.carpool.preview.initialAction)
    initialActionLabels.forEach(([key, value]) => i18nTranslations.set(`admin.carpool.preview.initialAction.${key}`, value))
    api.previewTerm.mockResolvedValue({
      calculated_at: activeTerm.starts_at,
      mode: 'new',
      plan,
      starts_at: activeTerm.starts_at,
      expires_at: activeTerm.expires_at,
      cycles: [...initialActionLabels.map(([action]) => action), 'future_action'].map((initialAction, index) => ({
        cycle_no: index + 1,
        starts_at: activeTerm.starts_at,
        ends_at: activeTerm.expires_at,
        base_quota_usd: '550.00000000',
        initial_action: initialAction,
      })),
      warnings: ['Server warning remains unchanged'],
    })
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.preview.action').trigger('click')
    await flushPromises()

    const rows = wrapper.get('tbody').findAll('tr')
    initialActionLabels.forEach(([, label], index) => expect(rows[index].findAll('td')[3].text()).toBe(label))
    expect(rows[initialActionLabels.length].findAll('td')[3].text()).toBe('future_action')
    expect(wrapper.text()).toContain('Server warning remains unchanged')
  })

  it('previews only actual opening periods with membership dates and the next natural refill', async () => {
    i18nTranslations.set('admin.carpool.columns.nextGrant', 'Next natural refill')
    api.previewTerm.mockResolvedValue({
      calculated_at: activeTerm.starts_at,
      mode: 'new',
      plan,
      starts_at: activeTerm.starts_at,
      expires_at: activeTerm.expires_at,
      next_natural_reset_at: activeTerm.next_natural_reset_at,
      cycles: [{
        cycle_no: 1,
        starts_at: activeTerm.starts_at,
        ends_at: activeTerm.next_natural_reset_at!,
        base_quota_usd: plan.weekly_quota_usd,
        initial_action: 'grant',
      }],
      warnings: [],
    })
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.preview.action').trigger('click')
    await flushPromises()

    expect(wrapper.get('tbody').findAll('tr')).toHaveLength(1)
    expect(wrapper.get('[data-testid="admin-carpool-preview-next-refill"]').text()).toContain('Next natural refill')
    expect(wrapper.get('[data-testid="admin-carpool-preview-next-refill"]').text()).not.toContain('admin.carpool.noNaturalRefill')
  })

  it('removes fixed-cycle promises from both localized preview actions', () => {
    expect(zhAdminCarpool.carpool.preview.action).toBe('预览开通结果')
    expect(enAdminCarpool.carpool.preview.action).toBe('Preview opening')
  })

  it('tracks the selected plan price until the operator edits the payment amount', async () => {
    api.listPlans.mockResolvedValue([plan, largerPlan])
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()
    const selects = wrapper.findAllComponents({ name: 'Select' })
    const paymentToggle = inputFor(wrapper, 'admin.carpool.payment.recordNow')
    await paymentToggle.setValue(true)

    await selects[0].vm.$emit('update:modelValue', 4)
    await flushPromises()
    const amount = wrapper.get('input[min="0.01"]')
    expect(amount.element.value).toBe('700.00')

    await amount.setValue('650')
    await selects[0].vm.$emit('update:modelValue', 3)
    await flushPromises()
    expect(amount.element.value).toBe('650')
  })

  it('discards a previous user load when the open dialog switches identity', async () => {
    let resolveFirst!: (value: { items: CarpoolAdminTerm[]; total: number; page: number; page_size: number }) => void
    api.listTerms
      .mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20 })
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await wrapper.setProps({ user: { ...user, id: 43, email: 'next@example.invalid' } })
    await flushPromises()

    resolveFirst({ items: [activeTerm], total: 1, page: 1, page_size: 20 })
    await flushPromises()

    expect(api.listTerms).toHaveBeenLastCalledWith({ page: 1, page_size: 20, user_id: 43 })
    expect(wrapper.findAll('button').some((item) => item.text().includes('admin.carpool.actions.renew'))).toBe(false)
    expect(wrapper.find('form').exists()).toBe(true)
  })

  it('discards a slow preview after switching users', async () => {
    let resolvePreview!: (value: { calculated_at: string; mode: 'new'; plan: CarpoolPlan; starts_at: string; expires_at: string; cycles: []; warnings: string[] }) => void
    api.previewTerm.mockReturnValueOnce(new Promise((resolve) => { resolvePreview = resolve }))
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.carpool.preview.action').trigger('click')
    await wrapper.setProps({ user: { ...user, id: 43, email: 'next@example.invalid' } })
    await flushPromises()
    resolvePreview({ calculated_at: activeTerm.starts_at, mode: 'new', plan, starts_at: activeTerm.starts_at, expires_at: activeTerm.expires_at, cycles: [], warnings: ['OLD PREVIEW'] })
    await flushPromises()

    expect(wrapper.text()).not.toContain('OLD PREVIEW')
    expect(api.previewTerm).toHaveBeenCalledWith(42, expect.any(Object))
  })

  it('does not apply a completed submit to a different user dialog', async () => {
    let resolveOpening!: (value: CarpoolAdminTerm) => void
    api.openTerm.mockReturnValueOnce(new Promise((resolve) => { resolveOpening = resolve }))
    const wrapper = mount(CarpoolTermModal, { props: { show: true, user }, global: { stubs } })
    await flushPromises()

    await wrapper.get('form').trigger('submit')
    await wrapper.setProps({ user: { ...user, id: 43, email: 'next@example.invalid' } })
    await flushPromises()
    resolveOpening(activeTerm)
    await flushPromises()

    expect(api.openTerm).toHaveBeenCalledWith(42, expect.any(Object), expect.any(String))
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(api.listTerms).toHaveBeenLastCalledWith({ page: 1, page_size: 20, user_id: 43 })
    expect(wrapper.find('form').exists()).toBe(true)
  })
})
