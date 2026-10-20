import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SubscriptionQuotaResolveModal from '../SubscriptionQuotaResolveModal.vue'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'
import type { QuotaResolveResult, SubscriptionQuotaRequest } from '@/types/subscriptionQuota'
import { deferred } from '@/__tests__/subscriptionQuotaFixtures'
const api = vi.hoisted(() => ({ resolve: vi.fn() }))
vi.mock('@/api/admin/subscriptionQuota', () => ({ adminSubscriptionQuotaAPI: api, default: api }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const request = (id = 9): SubscriptionQuotaRequest => ({ id, request_id: `subquota:request-${id}`, user_id: 42, group_id: 6, term_id: 10, api_key_id: 5, status: 'reconcile_required', revision: 3, actual_cost_usd: null, admitted_at: '2026-10-20T00:00:00Z', updated_at: '2026-10-20T00:00:00Z', settled_at: null, last_error_category: 'incomplete_usage' })
const result = (id = 9): QuotaResolveResult => ({ request: { ...request(id), status: 'usage_known' }, replayed: false })
let wrappers: VueWrapper[] = []
async function setup() { const wrapper = mount(SubscriptionQuotaResolveModal, { props: { show: true, request: request() }, global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } } }); wrappers.push(wrapper); await fill(wrapper); return wrapper }
async function fill(wrapper: VueWrapper) { await wrapper.get('[data-testid="resolve-evidence"]').setValue('ticket:123'); await wrapper.get('[data-testid="resolve-reason"]').setValue('原始审计说明') }
beforeEach(() => { setActivePinia(createPinia()); useAuthStore().user = { id: 1 } as User; vi.clearAllMocks(); api.resolve.mockResolvedValue(result()) })
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers = [] })
describe('audited request resolution UI', () => {
  it('omits verified fields for no_charge, keeps audit text, and reuses the key on exact retry', async () => {
    api.resolve.mockRejectedValueOnce(new Error('offline')); const wrapper = await setup(); await wrapper.get('form').trigger('submit'); await flushPromises(); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.resolve.mock.calls[0]).toEqual(api.resolve.mock.calls[1]); expect(api.resolve.mock.calls[0][1]).toEqual({ expected_request_revision: 3, evidence_reference: 'ticket:123', reason: '原始审计说明', resolution: 'no_charge' }); expect(wrapper.text()).toContain('subscriptionQuota.accepted'); expect(wrapper.text()).not.toContain('statuses.settled')
  })
  it('requires every exact safe token counter, preserves explicit zero and rejects rounded int64', async () => {
    const wrapper = await setup(); await wrapper.get('[data-testid="resolve-kind"]').setValue('verified_usage'); await wrapper.get('form').trigger('submit'); expect(api.resolve).not.toHaveBeenCalled()
    const fields = ['input_tokens', 'output_tokens', 'cache_creation_5m_tokens', 'cache_creation_1h_tokens', 'cache_read_tokens']
    for (const field of fields) await wrapper.get(`[data-testid="resolve-${field}"]`).setValue('0')
    await wrapper.get('[data-testid="resolve-input_tokens"]').setValue('9007199254740993'); await wrapper.get('form').trigger('submit'); expect(api.resolve).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="resolve-input_tokens"]').setValue('100'); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.resolve.mock.calls[0][1].verified_usage).toEqual({ input_tokens: 100, output_tokens: 0, cache_creation_5m_tokens: 0, cache_creation_1h_tokens: 0, cache_read_tokens: 0 })
  })
  it.each(['success', 'error'])('ignores old A %s and finally while B is submitting', async outcome => {
    const a = deferred<QuotaResolveResult>(), b = deferred<QuotaResolveResult>(); api.resolve.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const wrapper = await setup(); await wrapper.get('form').trigger('submit'); await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true, request: request(11) }); await fill(wrapper); await wrapper.get('form').trigger('submit')
    if (outcome === 'success') a.resolve(result()); else a.reject(new Error('old A error'))
    await flushPromises(); expect(wrapper.get('[data-testid="resolve-submit"]').text()).toBe('subscriptionQuota.saving'); expect(wrapper.emitted('accepted')).toBeUndefined(); expect(wrapper.text()).not.toContain('old A error')
    b.resolve(result(11)); await flushPromises(); expect(wrapper.emitted('accepted')).toHaveLength(1)
  })
  it('keeps evidence/reason after revision conflict and requests a canonical refresh', async () => {
    api.resolve.mockRejectedValueOnce({ status: 409 }); const wrapper = await setup(); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect((wrapper.get('[data-testid="resolve-evidence"]').element as HTMLInputElement).value).toBe('ticket:123'); expect(wrapper.get('[data-testid="resolve-error"]').text()).toContain('subscriptionQuota.conflict'); expect(wrapper.emitted('accepted')).toHaveLength(1)
  })
})
