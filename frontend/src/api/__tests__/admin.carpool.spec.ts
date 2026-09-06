import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get, post } }))

import { carpoolAdminAPI } from '@/api/admin/carpool'

describe('carpool admin API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20 } })
    post.mockResolvedValue({ data: {} })
  })

  it('uses field-specific list filters without rewriting them', async () => {
    await carpoolAdminAPI.listTerms({ page: 2, page_size: 25, user_id: 7, plan_id: 3, status: 'active', starts_from: 'from', starts_to: 'to' })
    expect(get).toHaveBeenLastCalledWith('/admin/carpool/terms', { params: { page: 2, page_size: 25, user_id: 7, plan_id: 3, status: 'active', starts_from: 'from', starts_to: 'to' } })

    await carpoolAdminAPI.listCycles({ user_id: 7, term_id: 9, cycle_no: 2, state: 'active', starts_from: 'from', starts_to: 'to' })
    expect(get).toHaveBeenLastCalledWith('/admin/carpool/cycles', { params: { user_id: 7, term_id: 9, cycle_no: 2, state: 'active', starts_from: 'from', starts_to: 'to' } })

    await carpoolAdminAPI.listLedger({ user_id: 7, term_id: 9, cycle_id: 11, event_type: 'manual_adjustment', bucket: 'manual' })
    expect(get).toHaveBeenLastCalledWith('/admin/carpool/ledger', { params: { user_id: 7, term_id: 9, cycle_id: 11, event_type: 'manual_adjustment', bucket: 'manual' } })
  })

  it('sends exact term lifecycle bodies with the caller-owned idempotency key', async () => {
    const takeover = {
      current_base_balance_usd: '10.00000000',
      current_boost_balance_usd: '2.00000000',
      current_manual_balance_usd: '3.00000000',
      ordinary_balance_transfer_usd: '1.00000000',
      boost_used: 1,
      history_complete: true,
      historical_used_usd: '4.00000000',
      statistics_since: '2026-09-01T00:00:00+08:00',
    }
    const opening = { plan_id: 3, group_id: 8, starts_at: null, mode: 'takeover' as const, takeover, notes: null, payment: null }

    await carpoolAdminAPI.previewTerm(7, { plan_id: 3, starts_at: null, mode: 'takeover', takeover })
    expect(post).toHaveBeenLastCalledWith('/admin/users/7/carpool/preview', { plan_id: 3, starts_at: null, mode: 'takeover', takeover })

    await carpoolAdminAPI.openTerm(7, opening, 'open-key')
    expect(post).toHaveBeenLastCalledWith('/admin/users/7/carpool/terms', opening, { headers: { 'Idempotency-Key': 'open-key' } })
    expect(opening).not.toHaveProperty('scope_id')

    const renewal = { plan_id: 4, notes: 'renewal', payment: null }
    await carpoolAdminAPI.renewTerm(9, renewal, 'renew-key')
    expect(post).toHaveBeenLastCalledWith('/admin/carpool/terms/9/renew', renewal, { headers: { 'Idempotency-Key': 'renew-key' } })

    await carpoolAdminAPI.terminateTerm(9, 'operator request', 'terminate-key')
    expect(post).toHaveBeenLastCalledWith('/admin/carpool/terms/9/terminate', { reason: 'operator request', effective_at: null }, { headers: { 'Idempotency-Key': 'terminate-key' } })
  })

  it('keeps CNY payments and USD adjustments on separate contracts', async () => {
    const payment = { amount_cny: '330.00', payment_kind: 'refund' as const, paid_at: '2026-09-06T12:00:00+08:00', channel: 'manual', external_order_no: null, notes: null }
    await carpoolAdminAPI.addPayment(9, payment, 'payment-key')
    expect(post).toHaveBeenLastCalledWith('/admin/carpool/terms/9/payments', payment, { headers: { 'Idempotency-Key': 'payment-key' } })

    const adjustment = { bucket: 'manual' as const, delta_usd: '-5.00000000', reason: 'correction', reverses_ledger_id: 18 }
    await carpoolAdminAPI.adjustCycle(11, adjustment, 'adjust-key')
    expect(post).toHaveBeenLastCalledWith('/admin/carpool/cycles/11/adjustments', adjustment, { headers: { 'Idempotency-Key': 'adjust-key' } })
  })

  it('lists and explicitly reconciles sanitized billing exceptions', async () => {
    await carpoolAdminAPI.listBillingExceptions({ page: 1, page_size: 20, user_id: 7, status: 'reconcile_required' })
    expect(get).toHaveBeenLastCalledWith('/admin/carpool/billing-exceptions', { params: { page: 1, page_size: 20, user_id: 7, status: 'reconcile_required' } })

    const resolution = { confirmed: true as const, resolution: 'actual_cost' as const, actual_cost_usd: '1.25000000', reason: 'verified accounting correction' }
    await carpoolAdminAPI.reconcileBillingException(22, resolution, 'reconcile-key')
    expect(post).toHaveBeenLastCalledWith('/admin/carpool/billing-exceptions/22/reconcile', resolution, { headers: { 'Idempotency-Key': 'reconcile-key' } })
  })

  it('implements the reset registration, schedule, due execution, and read-only scan contracts', async () => {
    get.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20 } })
    await carpoolAdminAPI.listResetBatches({ page: 1, page_size: 20, scope_id: 1, status: 'scheduled' })
    expect(get).toHaveBeenLastCalledWith('/admin/carpool/reset-batches', { params: { page: 1, page_size: 20, scope_id: 1, status: 'scheduled' } })

    const qualification = { scope_id: 1 as const, confirmed: true as const, source_event_key: 'synthetic-event', reason: 'documented evidence' }
    await carpoolAdminAPI.registerResetQualification(qualification, 'register-key')
    expect(post).toHaveBeenLastCalledWith('/admin/carpool/reset-batches', qualification, { headers: { 'Idempotency-Key': 'register-key' } })

    await carpoolAdminAPI.scheduleResetBatch(12, 'reviewed', 'schedule-key')
    expect(post).toHaveBeenLastCalledWith('/admin/carpool/reset-batches/12/schedule', { reason: 'reviewed' }, { headers: { 'Idempotency-Key': 'schedule-key' } })

    await carpoolAdminAPI.executeResetBatch(12, 'execute-key')
    expect(post).toHaveBeenLastCalledWith('/admin/carpool/reset-batches/12/execute', {}, { headers: { 'Idempotency-Key': 'execute-key' } })

    await carpoolAdminAPI.scanResetObservations('scan-key')
    expect(post).toHaveBeenLastCalledWith('/admin/carpool/reset-observations/scan', {}, { headers: { 'Idempotency-Key': 'scan-key' } })
  })

  it('creates immutable plan versions with the exact public body', async () => {
    const input = { name: 'Four-seat', list_price_cny: '330.00', weekly_quota_usd: '550.00000000', boost_ratio: '0.10000000', boost_count: 3, enabled: true }
    await carpoolAdminAPI.createPlanVersion(3, input, 'plan-key')
    expect(post).toHaveBeenLastCalledWith('/admin/carpool/plans/3/versions', input, { headers: { 'Idempotency-Key': 'plan-key' } })
  })

  it('normalizes nullable empty-list payloads at the API boundary', async () => {
    get.mockResolvedValue({ data: { items: null, total: 0, page: 1, page_size: 20 } })
    const responses = await Promise.all([
      carpoolAdminAPI.listTerms(), carpoolAdminAPI.listCycles(), carpoolAdminAPI.listLedger(),
      carpoolAdminAPI.listBillingExceptions(), carpoolAdminAPI.listPayments(9),
      carpoolAdminAPI.listResetBatches(), carpoolAdminAPI.listResetObservations(),
    ])
    expect(responses.every((response) => Array.isArray(response.items) && response.items.length === 0)).toBe(true)

    get.mockResolvedValueOnce({ data: null })
    await expect(carpoolAdminAPI.listPlans()).resolves.toEqual([])
  })
})
