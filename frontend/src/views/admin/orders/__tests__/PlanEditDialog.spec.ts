import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

import { adminPaymentAPI } from '@/api/admin/payment'
import { deferred } from '@/__tests__/subscriptionQuotaFixtures'
import PlanEditDialog from '../PlanEditDialog.vue'
import type { AdminGroup } from '@/types'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (key === 'payment.admin.subscriptionCnyPayPreview') return `preview ${params?.amount}`
      if (key === 'payment.admin.subscriptionCnyPayPreviewWithFee') return `fee ${params?.feeRate} ${params?.total}`
      return key
    },
  }),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    createPlan: vi.fn(),
    updatePlan: vi.fn(),
  },
}))

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: Boolean,
    title: String,
    width: String,
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: [String, Number],
    options: {
      type: Array,
      default: () => [],
    },
    placeholder: String,
  },
  emits: ['update:modelValue'],
  setup(_props, { emit }) {
    const onChange = (event: Event) => {
      const value = (event.target as HTMLSelectElement).value
      emit('update:modelValue', value === '' ? null : Number(value))
    }
    return { onChange }
  },
  template: `
    <select
      :value="modelValue ?? ''"
      @change="onChange"
    >
      <option value="">{{ placeholder }}</option>
      <option
        v-for="option in options"
        :key="option.value"
        :value="option.value"
        :data-platform="option.platform"
      >
        {{ option.label }}
      </option>
    </select>
  `,
})

const groupFixture = (overrides: Partial<AdminGroup>): AdminGroup => ({
  id: 1,
  name: 'OpenAI',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'subscription',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: false,
  sort_order: 0,
  ...overrides,
})

function mountDialog({
  groups = [],
  paymentConfig = null,
}: {
  groups?: AdminGroup[]
  paymentConfig?: Record<string, unknown> | null
} = {}) {
  return mount(PlanEditDialog, {
    props: {
      show: true,
      plan: null,
      groups,
      paymentConfig,
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
        GroupBadge: true,
      },
    },
  })
}

describe('PlanEditDialog', () => {
  it('shows CNY channel charge using the configured subscription rate and fee', async () => {
    const wrapper = mountDialog({
      paymentConfig: {
        subscription_usd_to_cny_rate: 7.15,
        recharge_fee_rate: 2.5,
      },
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).toContain('preview')
    expect(wrapper.text()).toContain('¥71.43')
    expect(wrapper.text()).toContain('fee 2.5')
    expect(wrapper.text()).toContain('¥73.22')
  })

  it('hides the preview when the subscription rate is not configured', async () => {
    const wrapper = mountDialog({
      paymentConfig: {
        subscription_usd_to_cny_rate: 0,
        recharge_fee_rate: 2.5,
      },
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).not.toContain('preview')
    expect(wrapper.text()).not.toContain('¥71.43')
  })

  it('allows composite subscription groups for payment plans', () => {
    const wrapper = mountDialog({
      groups: [
        groupFixture({
          id: 10,
          name: 'OpenAI + Claude + Gemini + Grok',
          platform: 'composite',
          rate_multiplier: 1.2,
          subscription_type: 'subscription',
        }),
        groupFixture({
          id: 11,
          name: 'Standard OpenAI',
          platform: 'openai',
          subscription_type: 'standard',
        }),
      ],
    })

    const options = wrapper.findAll('option').map(option => option.text())

    expect(options).toContain('OpenAI + Claude + Gemini + Grok — composite (1.2x)')
    expect(options).not.toContain('Standard OpenAI — openai (1x)')
  })
})

beforeEach(() => { vi.clearAllMocks() })
describe('Claude plan configuration', () => {
  const termGroup = () => groupFixture({ id: 6, name: 'Claude', platform: 'anthropic', subscription_quota_mode: 'term' })
  async function prepareTerm() {
    const wrapper = mountDialog({ groups: [termGroup()] })
    await wrapper.get('select').setValue('6')
    await wrapper.find('input[type="number"]').setValue('9.99')
    return wrapper
  }
  it.each(['', '0', '-1', '1e2', '1.123456789', ' 10', '01'])('rejects invalid exact-money input %j before sending', async amount => {
    const wrapper = await prepareTerm(); await wrapper.get('[data-testid="plan-default-quota"]').setValue(amount)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(adminPaymentAPI.createPlan).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('subscriptionQuota.invalidAmount')
    wrapper.unmount()
  })
  it('separates the sale price from an exact positive allowance and fixes seven days without client mode input', async () => {
    const wrapper = await prepareTerm()
    expect(wrapper.find('[data-testid="plan-fixed-duration"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('payment.admin.unlimited')
    await wrapper.get('[data-testid="plan-default-quota"]').setValue('0.00000001')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    const payload = vi.mocked(adminPaymentAPI.createPlan).mock.calls[0][0]
    expect(payload).toMatchObject({ group_id: 6, price: 9.99, default_quota_usd: '0.00000001', validity_days: 7, validity_unit: 'days' })
    expect(payload).not.toHaveProperty('quota_mode')
    wrapper.unmount()
  })
  it('preserves native windows validity and sends no term allowance', async () => {
    const wrapper = mountDialog({ groups: [groupFixture({ id: 1 })] })
    await wrapper.get('select').setValue('1'); await wrapper.find('input[type="number"]').setValue('10')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    const payload = vi.mocked(adminPaymentAPI.createPlan).mock.calls[0][0]
    expect(payload).toMatchObject({ price: 10, validity_days: 30, validity_unit: 'days' })
    expect(payload).not.toHaveProperty('default_quota_usd')
    expect(wrapper.find('[data-testid="plan-default-quota"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it.each(['resolve', 'reject'])('ignores a stale save %s after closing and reopening another form', async outcome => {
    const old = deferred<never>(), current = deferred<never>()
    vi.mocked(adminPaymentAPI.createPlan).mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = await prepareTerm(); await wrapper.get('[data-testid="plan-default-quota"]').setValue('100'); await wrapper.get('form').trigger('submit')
    await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true }); await wrapper.get('select').setValue('6'); await wrapper.find('input[type="number"]').setValue('20'); await wrapper.get('[data-testid="plan-default-quota"]').setValue('300'); await wrapper.get('form').trigger('submit')
    if (outcome === 'resolve') old.resolve(undefined as never); else old.reject(new Error('old failure'))
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    current.resolve(undefined as never); await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })
})

it('can repair an existing term plan without a hidden windows duration blocking the fixed seven days', async () => {
  const wrapper = mountDialog({ groups: [groupFixture({ id: 6, platform: 'anthropic', subscription_quota_mode: 'term' })] })
  await wrapper.setProps({ plan: { id: 7, name: 'Existing Claude', group_id: 6, description: 'Card', quota_mode: 'term', default_quota_usd: '123.45678901', price: 9.99, validity_days: 0, validity_unit: 'months', sort_order: 0, for_sale: false, features: [] } })
  expect((wrapper.get('[data-testid="plan-default-quota"]').element as HTMLInputElement).value).toBe('123.45678901')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(adminPaymentAPI.updatePlan).toHaveBeenCalledWith(7, expect.objectContaining({ default_quota_usd: '123.45678901', validity_days: 7, validity_unit: 'days' }))
  wrapper.unmount()
})
