import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AdminPaymentPlansView from '../AdminPaymentPlansView.vue'

const { getPlans, getConfig, getGroups } = vi.hoisted(() => ({
  getPlans: vi.fn(),
  getConfig: vi.fn(),
  getGroups: vi.fn(),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    getPlans,
    getConfig,
  },
}))

vi.mock('@/api/admin', () => ({
  default: {
    groups: {
      getAll: getGroups,
    },
  },
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => ({
        'subscriptionQuota.fixedDuration': '有效期固定 168 小时（七天）',
        'payment.days': '天',
        'payment.weeks': '周',
        'payment.months': '个月',
        'payment.perMonth': '月',
      } as Record<string, string>)[key] ?? key,
    }),
  }
})

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id">
        <slot name="cell-price" :value="row.price" :row="row" />
        <span data-testid="validity"><slot name="cell-validity_days" :value="row.validity_days" :row="row" /></span>
      </div>
    </div>
  `,
}

function mountView() {
  return mount(AdminPaymentPlansView, {
    global: {
      plugins: [createPinia()],
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        DataTable: DataTableStub,
        ConfirmDialog: true,
        GroupBadge: true,
        Icon: true,
        PlanEditDialog: true,
      },
    },
  })
}

describe('AdminPaymentPlansView', () => {
  beforeEach(() => {
    getGroups.mockResolvedValue([])
    getConfig.mockResolvedValue({ data: {} })
    getPlans.mockResolvedValue({
      data: [
        {
          id: 1,
          name: 'CNY plan',
          group_id: 1,
          price: 499,
          original_price: 599,
          currency: 'CNY',
          validity_days: 30,
          validity_unit: 'day',
          sort_order: 0,
          for_sale: true,
          features: [],
        },
        {
          id: 2,
          name: 'Legacy plan',
          group_id: 1,
          price: 10,
          original_price: 0,
          currency: '',
          validity_days: 30,
          validity_unit: 'day',
          sort_order: 0,
          for_sale: true,
          features: [],
        },
      ],
    })
  })

  it('uses the configured currency symbol and keeps legacy prices in USD', async () => {
    const wrapper = mountView()

    await flushPromises()

    expect(wrapper.text()).toContain('¥499.00CNY')
    expect(wrapper.text()).toContain('¥599.00')
    expect(wrapper.text()).toContain('$10.00')
  })

  it('shows fixed term hours and formats legacy singular/plural windows durations', async () => {
    getPlans.mockResolvedValue({ data: [
      { id: 1, quota_mode: 'term', validity_days: 7, validity_unit: 'day' },
      { id: 2, quota_mode: 'term', validity_days: 7, validity_unit: 'days' },
      { id: 3, quota_mode: 'windows', validity_days: 30, validity_unit: 'day' },
      { id: 4, quota_mode: 'windows', validity_days: 30, validity_unit: 'days' },
      { id: 5, quota_mode: 'windows', validity_days: 2, validity_unit: 'week' },
      { id: 6, quota_mode: 'windows', validity_days: 2, validity_unit: 'weeks' },
      { id: 7, quota_mode: 'windows', validity_days: 3, validity_unit: 'month' },
      { id: 8, quota_mode: 'windows', validity_days: 3, validity_unit: 'months' },
    ] })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('[data-testid="validity"]').map(cell => cell.text())).toEqual([
      '有效期固定 168 小时（七天）', '有效期固定 168 小时（七天）',
      '30天', '30天', '2周', '2周', '3个月', '3个月',
    ])
    expect(wrapper.text()).not.toContain('payment.admin.day')
  })
})
