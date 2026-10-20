import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SubscriptionQuotaTermModal from '../SubscriptionQuotaTermModal.vue'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'
import type { SubscriptionQuotaMutation, SubscriptionQuotaPreview } from '@/types/subscriptionQuota'
import { deferred, quotaSummary, quotaTerm } from '@/__tests__/subscriptionQuotaFixtures'
const api = vi.hoisted(() => ({ getAll: vi.fn(), summary: vi.fn(), terms: vi.fn(), preview: vi.fn(), open: vi.fn(), cancel: vi.fn() }))
vi.mock('@/api/admin/groups', () => ({ default: { getAll: api.getAll } }))
vi.mock('@/api/admin/subscriptionQuota', () => ({ adminSubscriptionQuotaAPI: api, default: api }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const preview = (): SubscriptionQuotaPreview => ({ group_id: 6, effective_quota_usd: '100.00000000', proposed_starts_at: '2026-10-20T00:00:00Z', proposed_expires_at: '2026-10-27T00:00:00Z', server_time: '2026-10-20T00:00:00Z', current_revision: 4, warnings: [], current_term: quotaTerm(), next_terms: [] })
const mutation = (): SubscriptionQuotaMutation => ({ term: quotaTerm(), summary: quotaSummary(), replayed: false })
let wrappers: VueWrapper[] = []
async function setup() {
  const wrapper = mount(SubscriptionQuotaTermModal, { props: { show: true, user: { id: 42, email: 'a@example.invalid' } }, global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' }, ClaudeQuotaCard: true } } })
  wrappers.push(wrapper); await flushPromises(); return wrapper
}
async function prepare(wrapper: VueWrapper, amount = '100') { await wrapper.get('[data-testid="quota-amount"]').setValue(amount); await wrapper.get('[data-testid="quota-preview"]').trigger('click'); await flushPromises() }
beforeEach(() => {
  setActivePinia(createPinia()); useAuthStore().user = { id: 1 } as User
  vi.clearAllMocks()
  api.getAll.mockResolvedValue([{ id: 6, name: 'Claude', subscription_type: 'subscription', subscription_quota_mode: 'term' }])
  api.summary.mockResolvedValue(quotaSummary()); api.terms.mockResolvedValue({ items: [], next_cursor: null, has_more: false }); api.preview.mockResolvedValue(preview()); api.open.mockResolvedValue(mutation())
})
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers = [] })
describe('Claude manual card strict asynchronous identity', () => {
  it.each(['success', 'error'])('ignores late preview %s and finally after A closes and B starts preview', async outcome => {
    const old = deferred<SubscriptionQuotaPreview>(), newer = deferred<SubscriptionQuotaPreview>()
    api.preview.mockReturnValueOnce(old.promise).mockReturnValueOnce(newer.promise)
    const wrapper = await setup(); await prepare(wrapper)
    await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true, user: { id: 43, email: 'b@example.invalid' } }); await flushPromises(); await prepare(wrapper, '300')
    if (outcome === 'success') old.resolve(preview()); else old.reject(new Error('old A failure'))
    await flushPromises(); expect(wrapper.find('[data-testid="quota-preview-result"]').exists()).toBe(false); expect(wrapper.get('[data-testid="quota-preview"]').attributes('disabled')).toBeDefined(); expect(wrapper.text()).not.toContain('old A failure')
    newer.reject(new Error('current B failure')); await flushPromises(); expect(wrapper.get('[data-testid="quota-error"]').text()).toContain('current B failure'); expect(wrapper.get('[data-testid="quota-preview"]').attributes('disabled')).toBeUndefined()
  })
  it.each(['success', 'error'])('ignores late submission %s including emitted success and finally', async outcome => {
    const old = deferred<SubscriptionQuotaMutation>(), newer = deferred<SubscriptionQuotaMutation>()
    api.open.mockReturnValueOnce(old.promise).mockReturnValueOnce(newer.promise)
    const wrapper = await setup(); await prepare(wrapper); await wrapper.get('form').trigger('submit'); await flushPromises()
    await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true, user: { id: 43, email: 'b@example.invalid' } }); await flushPromises(); await prepare(wrapper, '300'); await wrapper.get('form').trigger('submit'); await flushPromises()
    if (outcome === 'success') old.resolve(mutation()); else old.reject(new Error('old A failure'))
    await flushPromises(); expect(wrapper.get('[data-testid="quota-submit"]').attributes('disabled')).toBeDefined(); expect(wrapper.find('[data-testid="quota-saved"]').exists()).toBe(false); expect(wrapper.emitted('success')).toBeUndefined(); expect(wrapper.text()).not.toContain('old A failure')
    newer.resolve(mutation()); await flushPromises(); expect(wrapper.emitted('success')).toHaveLength(1)
  })
  it('reuses a copied payload and key on exact network retry; edits require preview and a new key', async () => {
    api.open.mockRejectedValueOnce(new Error('connection lost')).mockRejectedValueOnce(new Error('still offline'))
    const wrapper = await setup(); await prepare(wrapper, '0.00000001'); await wrapper.get('form').trigger('submit'); await flushPromises(); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.open.mock.calls[0]).toEqual(api.open.mock.calls[1]); expect(api.open.mock.calls[0][1]).toEqual({ group_id: 6, operation: 'open', quota_usd: '0.00000001', expected_revision: 4 })
    expect(api.open.mock.calls[0][2]).toMatch(/^[a-f0-9-]{36}$/)
    await prepare(wrapper, '300'); await wrapper.get('form').trigger('submit'); await flushPromises(); expect(api.open.mock.calls[2][2]).not.toBe(api.open.mock.calls[0][2])
  })
  it('preserves quota and notes after 409, invalidates preview, and refreshes current server state', async () => {
    api.open.mockRejectedValueOnce({ status: 409, message: 'revision conflict' })
    const wrapper = await setup(); await wrapper.get('[data-testid="quota-notes"]').setValue('operator original text'); await prepare(wrapper, '300'); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect((wrapper.get('[data-testid="quota-amount"]').element as HTMLInputElement).value).toBe('300'); expect((wrapper.get('[data-testid="quota-notes"]').element as HTMLTextAreaElement).value).toBe('operator original text'); expect(wrapper.find('[data-testid="quota-preview-result"]').exists()).toBe(false); expect(wrapper.get('[data-testid="quota-error"]').text()).toContain('subscriptionQuota.conflict'); expect(api.summary).toHaveBeenCalledTimes(2)
  })
  it('drops preview and catch effects after payload or auth identity changes', async () => {
    const old = deferred<SubscriptionQuotaPreview>(); api.preview.mockReturnValueOnce(old.promise)
    const wrapper = await setup(); await prepare(wrapper); useAuthStore().user = { id: 2 } as User; await flushPromises(); old.reject(new Error('wrong actor')); await flushPromises(); expect(wrapper.text()).not.toContain('wrong actor'); expect(wrapper.find('[data-testid="quota-preview-result"]').exists()).toBe(false)
  })
  it('serializes open/renew and cancellation on the same scope even after form edits', async () => {
    api.terms.mockResolvedValue({ items: [quotaTerm()], next_cursor: null, has_more: false })
    const opening = deferred<SubscriptionQuotaMutation>(), cancelling = deferred<SubscriptionQuotaMutation>()
    api.open.mockReturnValueOnce(opening.promise); api.cancel.mockReturnValueOnce(cancelling.promise)
    const wrapper = await setup(); await prepare(wrapper); await wrapper.findAll('form')[0].trigger('submit'); await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'subscriptionQuota.cancel')!.trigger('click')
    await wrapper.get('[data-testid="quota-cancel-form"] textarea').setValue('verified reason'); await wrapper.get('[data-testid="quota-cancel-form"]').trigger('submit'); expect(api.cancel).not.toHaveBeenCalled()
    opening.reject(new Error('offline')); await flushPromises()
    await wrapper.get('[data-testid="quota-cancel-form"]').trigger('submit'); await flushPromises(); expect(api.cancel).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid="quota-amount"]').setValue('300'); await wrapper.findAll('form')[0].trigger('submit'); expect(api.open).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="quota-preview"]').attributes('disabled')).toBeDefined()
    cancelling.resolve(mutation()); await flushPromises(); expect(wrapper.emitted('success')).toHaveLength(1)
  })
})
