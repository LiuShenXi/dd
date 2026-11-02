import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ProductConfigView from '@/views/admin/ProductConfigView.vue'
import type { CarpoolPlan } from '@/types/carpool'

enableAutoUnmount(afterEach)

const carpool = vi.hoisted(() => ({ listPlans: vi.fn(), createPlanVersion: vi.fn() }))
const groups = vi.hoisted(() => ({ getAll: vi.fn(), update: vi.fn() }))
const payment = vi.hoisted(() => ({ getPlans: vi.fn(), updatePlan: vi.fn(), createPlan: vi.fn() }))
const homeProducts = vi.hoisted(() => ({ getHomeProductsSettings: vi.fn(), updateHomeProductsSettings: vi.fn() }))
const toast = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn() }))

vi.mock('@/api/admin', () => ({ default: { carpool, groups } }))
vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: payment }))
vi.mock('@/api/homeProducts', () => homeProducts)
vi.mock('@/stores/app', () => ({ useAppStore: () => toast }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string, values: Record<string, unknown> = {}) => `${key}${Object.keys(values).length ? JSON.stringify(values) : ''}` })
}))

const stubs = {
  AppLayout: { template: '<main><slot /></main>' },
  Icon: true,
  RouterLink: { template: '<a><slot /></a>' },
  ConfirmDialog: {
    props: ['show', 'message'],
    emits: ['confirm', 'cancel'],
    template: '<div v-if="show" class="confirm">{{ message }}<button class="do-confirm" @click="$emit(\'confirm\')">ok</button></div>'
  }
}

const fourSeat: CarpoolPlan = {
  plan_id: 16, code: 'four_seat', name: 'Four-seat', version: 5, list_price_cny: '330.00', weekly_quota_usd: '500.00000000',
  duration_days: 28, cycle_days: 7, boost_ratio: '0.10000000', boost_amount_usd: '50.00000000', boost_count: 2,
  rounding_mode: 'half_up', enabled: true, is_latest: true
}
const termGroup = { id: 7, name: 'Claude 周卡', platform: 'anthropic', subscription_quota_mode: 'term', rate_multiplier: 0.2 }
const termPlan = { id: 3, group_id: 7, name: '周卡', price: 100, default_quota_usd: '150.00000000', quota_mode: 'term', validity_days: 7, validity_unit: 'days', features: [], for_sale: false, sort_order: 0, description: '' }

function button(wrapper: VueWrapper, text: string) {
  const match = wrapper.findAll('button').find((b) => b.text().includes(text))
  if (!match) throw new Error(`button not found: ${text}`)
  return match
}

beforeEach(() => {
  vi.clearAllMocks()
  homeProducts.getHomeProductsSettings.mockResolvedValue({
    gpt_carpool: { enabled: true, status: 'available', rules: [] },
    claude_weekly: { enabled: true, status: 'pilot', plan_id: 0, rules: [] },
    contact: ''
  })
  carpool.listPlans.mockResolvedValue([fourSeat])
  groups.getAll.mockResolvedValue([termGroup])
  payment.getPlans.mockResolvedValue({ data: [termPlan] })
})

describe('ProductConfigView', () => {
  it('saves a GPT tier as a new real carpool plan version after confirmation', async () => {
    carpool.createPlanVersion.mockResolvedValue({ ...fourSeat, version: 6, list_price_cny: '350.00', weekly_quota_usd: '520.00000000' })
    const wrapper = mount(ProductConfigView, { global: { stubs } })
    await flushPromises()

    const row = wrapper.get('[data-testid="pc-tier-four_seat"]')
    const [price, quota] = row.findAll('input[inputmode="decimal"]')
    await price.setValue('350')
    await quota.setValue('520')
    await row.get('button').trigger('click')
    expect(carpool.createPlanVersion).not.toHaveBeenCalled()

    await wrapper.get('.do-confirm').trigger('click')
    await flushPromises()
    expect(carpool.createPlanVersion).toHaveBeenCalledWith(
      16,
      { name: 'Four-seat', list_price_cny: '350', weekly_quota_usd: '520', boost_ratio: '0.10000000', boost_count: 2, enabled: true },
      expect.any(String)
    )
    expect(row.text()).toContain('v6')
  })

  it('writes Claude price/quota to the subscription plan and rate to the group', async () => {
    payment.updatePlan.mockResolvedValue({ data: { ...termPlan, price: 120, default_quota_usd: '180.00000000' } })
    groups.update.mockResolvedValue({ ...termGroup, rate_multiplier: 0.25 })
    const wrapper = mount(ProductConfigView, { global: { stubs } })
    await flushPromises()

    await wrapper.get('#pc-claude-price').setValue('120')
    await wrapper.get('#pc-claude-quota').setValue('180')
    await wrapper.get('#pc-claude-rate').setValue('0.25')
    await wrapper.get('[data-testid="pc-claude-save"]').trigger('click')
    await wrapper.get('.do-confirm').trigger('click')
    await flushPromises()

    expect(payment.updatePlan).toHaveBeenCalledWith(3, { price: 120, default_quota_usd: '180' })
    expect(groups.update).toHaveBeenCalledWith(7, { rate_multiplier: 0.25 })
  })

  it('only touches the group when just the rate changes', async () => {
    groups.update.mockResolvedValue({ ...termGroup, rate_multiplier: 0.3 })
    const wrapper = mount(ProductConfigView, { global: { stubs } })
    await flushPromises()

    await wrapper.get('#pc-claude-rate').setValue('0.3')
    await wrapper.get('[data-testid="pc-claude-save"]').trigger('click')
    await wrapper.get('.do-confirm').trigger('click')
    await flushPromises()

    expect(payment.updatePlan).not.toHaveBeenCalled()
    expect(groups.update).toHaveBeenCalledWith(7, { rate_multiplier: 0.3 })
  })

  it('creates an unlisted 7-day term plan when none exists and links it', async () => {
    payment.getPlans.mockResolvedValueOnce({ data: [] }).mockResolvedValue({ data: [termPlan] })
    payment.createPlan.mockResolvedValue({ data: termPlan })
    homeProducts.updateHomeProductsSettings.mockImplementation(async (s) => s)
    const wrapper = mount(ProductConfigView, { global: { stubs } })
    await flushPromises()

    await button(wrapper, 'admin.productConfig.createPlan').trigger('click')
    await flushPromises()

    expect(payment.createPlan).toHaveBeenCalledWith(
      expect.objectContaining({ group_id: 7, price: 100, default_quota_usd: '150', validity_days: 7, validity_unit: 'days', for_sale: false })
    )
    expect(homeProducts.updateHomeProductsSettings).toHaveBeenCalledWith(
      expect.objectContaining({ claude_weekly: expect.objectContaining({ plan_id: 3 }) })
    )
  })

  it('saves display settings separately', async () => {
    homeProducts.updateHomeProductsSettings.mockImplementation(async (s) => s)
    const wrapper = mount(ProductConfigView, { global: { stubs } })
    await flushPromises()

    await wrapper.get('#pc-contact').setValue('微信 abc')
    await wrapper.get('[data-testid="pc-display-save"]').trigger('click')
    await flushPromises()

    expect(homeProducts.updateHomeProductsSettings).toHaveBeenCalledWith(expect.objectContaining({ contact: '微信 abc', claude_weekly: expect.objectContaining({ plan_id: 3 }) }))
    expect(carpool.createPlanVersion).not.toHaveBeenCalled()
    expect(payment.updatePlan).not.toHaveBeenCalled()
  })
})
