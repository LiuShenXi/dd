import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { OrderStatus, PaymentOrder, SubscriptionQuotaOrderSnapshot } from '@/types/payment'
import AdminOrdersView from '../AdminOrdersView.vue'

const { api, showError, showSuccess } = vi.hoisted(() => ({
  api: {
    getOrders: vi.fn(), getOrder: vi.fn(), refundOrder: vi.fn(), queryRefund: vi.fn(),
    cancelOrder: vi.fn(), retryRecharge: vi.fn(),
  },
  showError: vi.fn(), showSuccess: vi.fn(),
}))
vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: api, default: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key === 'subscriptionQuota.fixedDuration' ? 'Fixed validity: 168 hours (7 days)' : key }),
}))

const snapshot: SubscriptionQuotaOrderSnapshot = {
  schema_version: 1, mode: 'term', platform: 'anthropic', group_id: 7,
  duration_seconds: 604800, quota_usd: '125.12345678', source_plan_id: 1,
  plan_name: 'Purchased plan', product_name: 'Frozen Claude card', group_name: 'Claude group',
}
const order = (id: number, status: OrderStatus, term = false): PaymentOrder => ({
  id, user_id: 7, amount: 12.34, pay_amount: 86.38, currency: 'CNY', fee_rate: 0,
  payment_type: 'alipay', out_trade_no: `order-${id}`, status, order_type: 'subscription',
  created_at: '2026-10-20T02:00:00Z', expires_at: '2026-10-20T03:00:00Z',
  refund_amount: 0, plan_id: 1, subscription_quota_snapshot: term ? { ...snapshot } : null,
})
const refundInput = { amount: 1, reason: 'Reviewed legacy refund', deduct_balance: true, force: false }

const DataTableStub = {
  props: ['data', 'columns'],
  template: `<table><tbody><tr v-for="row in data" :key="row.id" :data-testid="'order-' + row.id">
    <td v-for="column in columns" :key="column.key"><slot :name="'cell-' + column.key" :row="row" :value="row[column.key]" /></td>
  </tr></tbody></table>`,
}
const BaseDialogStub = {
  props: ['show', 'title'], emits: ['close'],
  template: '<section v-if="show" role="dialog" :aria-label="title"><slot /></section>',
}
const RefundDialogStub = {
  name: 'AdminRefundDialog',
  props: ['show', 'order', 'submitting', 'requireForce', 'warning'], emits: ['confirm', 'cancel'],
  template: '<section v-if="show" data-testid="refund-dialog">{{ order.id }}</section>',
}

async function mountView(rows: PaymentOrder[]) {
  api.getOrders.mockResolvedValue({ data: { items: rows, total: rows.length } })
  const wrapper = mount(AdminOrdersView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' }, DataTable: DataTableStub,
    BaseDialog: BaseDialogStub, AdminRefundDialog: RefundDialogStub,
    Icon: true, Pagination: true, Select: true, OrderStatusBadge: true,
  } } })
  await flushPromises()
  return wrapper
}

const refundStatuses: [OrderStatus, string][] = [
  ['COMPLETED', 'payment.admin.refund'],
  ['PARTIALLY_REFUNDED', 'payment.admin.refund'],
  ['REFUND_REQUESTED', 'payment.admin.approveRefund'],
  ['REFUND_FAILED', 'payment.admin.retryRefund'],
  ['REFUND_PENDING', 'payment.admin.queryRefundStatus'],
]

describe('AdminOrdersView actual routed order actions and detail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getOrder.mockResolvedValue({ data: {} })
    api.refundOrder.mockResolvedValue({ data: { success: true } })
    api.queryRefund.mockResolvedValue({ data: { success: true } })
    api.cancelOrder.mockResolvedValue({ data: {} })
    api.retryRecharge.mockResolvedValue({ data: {} })
  })

  it.each(refundStatuses)('hides every legacy refund action for a term order in %s', async (status) => {
    const wrapper = await mountView([order(3, status, true)])
    const row = wrapper.get('[data-testid="order-3"]')
    expect(row.findAll('button').map(button => button.text())).toEqual(['common.view'])
    expect(row.text()).toContain('subscriptionQuota.manualRefund')
    expect(row.text()).toContain('125.12345678 USD')
    expect(row.text()).not.toContain('payment.orders.creditedAmount')
    expect(wrapper.find('[data-testid="refund-dialog"]').exists()).toBe(false)
    expect(api.refundOrder).not.toHaveBeenCalled()
    expect(api.queryRefund).not.toHaveBeenCalled()
  })

  it.each(refundStatuses)('keeps the legacy windows refund operation for %s', async (status, action) => {
    const wrapper = await mountView([order(4, status)])
    const row = wrapper.get('[data-testid="order-4"]')
    expect(row.text()).not.toContain('subscriptionQuota.manualRefund')
    await row.findAll('button').find(button => button.text() === action)!.trigger('click')
    if (status === 'REFUND_PENDING') {
      await flushPromises()
      expect(api.queryRefund).toHaveBeenCalledWith(4)
    } else {
      const dialog = wrapper.getComponent(RefundDialogStub)
      expect(dialog.props('show')).toBe(true)
      expect(dialog.props('order').id).toBe(4)
      dialog.vm.$emit('confirm', refundInput)
      await flushPromises()
      expect(api.refundOrder).toHaveBeenCalledWith(4, refundInput)
    }
  })

  it.each([['PENDING', 'payment.orders.cancel', 'cancelOrder'], ['FAILED', 'payment.admin.retry', 'retryRecharge']] as const)(
    'preserves non-refund term order operation for %s', async (status, action, endpoint) => {
      const wrapper = await mountView([order(3, status, true)])
      await wrapper.get('[data-testid="order-3"]').findAll('button').find(button => button.text() === action)!.trigger('click')
      await flushPromises()
      expect(api[endpoint]).toHaveBeenCalledWith(3)
    },
  )

  it('shows the fetched frozen snapshot, sale price and manual-refund instruction in the actual detail dialog', async () => {
    const purchased = order(3, 'COMPLETED', true)
    api.getOrder.mockResolvedValue({ data: { order: purchased, auditLogs: [] } })
    const wrapper = await mountView([purchased])
    await wrapper.get('[data-testid="order-3"]').findAll('button').find(button => button.text() === 'common.view')!.trigger('click')
    await flushPromises()
    const detail = wrapper.get('[role="dialog"]')
    expect(api.getOrder).toHaveBeenCalledWith(3)
    expect(detail.get('[data-testid="order-quota-snapshot"]').text()).toContain('125.12345678 USD')
    expect(detail.text()).toContain('168 hours (7 days)')
    expect(detail.text()).toContain('subscriptionQuota.manualRefund')
    expect(detail.text()).toContain('subscriptionQuota.salePrice')
    expect(detail.text()).toContain('$12.34')
    expect(detail.text()).toContain('¥86.38')
    expect(detail.text()).not.toContain('payment.orders.amount')
    expect(detail.text()).not.toContain('payment.orders.creditedAmount')
  })

  it.each([['COMPLETED', 'payment.admin.refund'], ['REFUND_PENDING', 'payment.admin.queryRefundStatus']] as const)(
    'guards a stale %s action when the row receives a quota snapshot before the click', async (status, action) => {
      const existing = order(4, status)
      const wrapper = await mountView([existing])
      const button = wrapper.get('[data-testid="order-4"]').findAll('button').find(candidate => candidate.text() === action)!
      // Simulate server hydration reaching the row before its old action paints.
      existing.subscription_quota_snapshot = { ...snapshot }
      await button.trigger('click')
      expect(wrapper.getComponent(RefundDialogStub).props('show')).toBe(false)
      expect(api.refundOrder).not.toHaveBeenCalled()
      expect(api.queryRefund).not.toHaveBeenCalled()
      expect(showError).toHaveBeenCalledWith('subscriptionQuota.manualRefund')
    },
  )

  it('rejects a retained refund submit after a snapshot arrives and clears the dialog target', async () => {
    const existing = order(4, 'COMPLETED')
    const wrapper = await mountView([existing])
    await wrapper.get('[data-testid="order-4"]').findAll('button').find(button => button.text() === 'payment.admin.refund')!.trigger('click')
    existing.subscription_quota_snapshot = { ...snapshot }
    const dialog = wrapper.getComponent(RefundDialogStub)
    dialog.vm.$emit('confirm', refundInput)
    await flushPromises()
    expect(api.refundOrder).not.toHaveBeenCalled()
    expect(dialog.props('show')).toBe(false)
    expect(dialog.props('order')).toBeNull()
    expect(showError).toHaveBeenCalledWith('subscriptionQuota.manualRefund')
  })

  it('ignores a residual confirm after closing a legacy refund dialog', async () => {
    const wrapper = await mountView([order(4, 'COMPLETED')])
    await wrapper.get('[data-testid="order-4"]').findAll('button').find(button => button.text() === 'payment.admin.refund')!.trigger('click')
    const dialog = wrapper.getComponent(RefundDialogStub)
    dialog.vm.$emit('cancel')
    dialog.vm.$emit('confirm', refundInput)
    await flushPromises()
    expect(api.refundOrder).not.toHaveBeenCalled()
    expect(dialog.props('order')).toBeNull()
  })

  it('does not let a late detail response replace a separately selected legacy refund order', async () => {
    const term = order(3, 'COMPLETED', true)
    let resolveDetail!: (value: unknown) => void
    api.getOrder.mockReturnValue(new Promise(resolve => { resolveDetail = resolve }))
    const wrapper = await mountView([term, order(4, 'COMPLETED')])
    await wrapper.get('[data-testid="order-3"]').findAll('button').find(button => button.text() === 'common.view')!.trigger('click')
    wrapper.getComponent(BaseDialogStub).vm.$emit('close')
    await wrapper.get('[data-testid="order-4"]').findAll('button').find(button => button.text() === 'payment.admin.refund')!.trigger('click')
    resolveDetail({ data: { order: term, auditLogs: [] } })
    await flushPromises()
    const dialog = wrapper.getComponent(RefundDialogStub)
    expect(dialog.props('order').id).toBe(4)
    dialog.vm.$emit('confirm', refundInput)
    await flushPromises()
    expect(api.refundOrder).toHaveBeenCalledWith(4, refundInput)
  })
})
