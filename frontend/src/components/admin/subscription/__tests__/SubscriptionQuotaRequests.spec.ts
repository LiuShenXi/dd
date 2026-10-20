import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SubscriptionQuotaRequests from '../SubscriptionQuotaRequests.vue'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'
import type { QuotaPage, SubscriptionQuotaRequest } from '@/types/subscriptionQuota'
import { deferred } from '@/__tests__/subscriptionQuotaFixtures'
const api = vi.hoisted(() => ({ requests: vi.fn() }))
vi.mock('@/api/admin/subscriptionQuota', () => ({ adminSubscriptionQuotaAPI: api, default: api }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
let wrapper: VueWrapper
beforeEach(() => { setActivePinia(createPinia()); useAuthStore().user = { id: 1 } as User; vi.clearAllMocks(); api.requests.mockResolvedValue({ items: [], next_cursor: null, has_more: false }) })
afterEach(() => wrapper.unmount())
function setup() { wrapper = mount(SubscriptionQuotaRequests, { global: { stubs: { SubscriptionQuotaResolveModal: true } } }); return wrapper }
describe('sanitized administrator request review', () => {
  it('shows unknown cost explicitly and offers resolution only for unknown reconcile rows', async () => {
    const base = { id: 1, request_id: 'subquota:unknown', user_id: 42, group_id: 6, term_id: 10, api_key_id: 9, status: 'reconcile_required', revision: 1, actual_cost_usd: null, admitted_at: '', last_error_category: 'usage_missing' }
    api.requests.mockResolvedValueOnce({ items: [base, { ...base, id: 2, request_id: 'subquota:zero', actual_cost_usd: '0.00000000', status: 'settled' }], next_cursor: null, has_more: false })
    setup(); await flushPromises(); expect(wrapper.text()).toContain('subscriptionQuota.unknownCost'); expect(wrapper.text()).toContain('$0.00'); expect(wrapper.findAll('button').filter(button => button.text() === 'subscriptionQuota.review')).toHaveLength(1); expect(api.requests).toHaveBeenCalledWith({ limit: 20, status: 'reconcile_required' })
  })
  it('ignores an old administrator list response after a new auth identity starts its own request', async () => {
    const old = deferred<QuotaPage<SubscriptionQuotaRequest>>(); api.requests.mockReturnValueOnce(old.promise)
    setup(); useAuthStore().user = { id: 2 } as User; await flushPromises(); await wrapper.get('form').trigger('submit'); await flushPromises()
    old.reject(new Error('old admin error')); await flushPromises(); expect(wrapper.text()).not.toContain('old admin error'); expect(wrapper.text()).toContain('subscriptionQuota.noRequests')
  })
})
