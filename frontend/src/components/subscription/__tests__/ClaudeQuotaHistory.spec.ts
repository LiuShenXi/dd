import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ClaudeQuotaHistory from '../ClaudeQuotaHistory.vue'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'
import type { QuotaPage, SubscriptionQuotaTerm } from '@/types/subscriptionQuota'
import { deferred, quotaTerm } from '@/__tests__/subscriptionQuotaFixtures'
const api = vi.hoisted(() => ({ terms: vi.fn(), usage: vi.fn() }))
vi.mock('@/api/subscriptionQuota', () => ({ subscriptionQuotaAPI: api }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
let wrapper: VueWrapper
beforeEach(() => { setActivePinia(createPinia()); useAuthStore().user = { id: 1 } as User; vi.clearAllMocks(); api.terms.mockResolvedValue({ items: [quotaTerm()], next_cursor: null, has_more: false }); api.usage.mockResolvedValue({ items: [], next_cursor: null, has_more: false }); wrapper = mount(ClaudeQuotaHistory, { props: { groupId: 6 } }) })
afterEach(() => wrapper.unmount())
describe('card history ownership and pagination', () => {
  it('paginates card terms without requesting individual usage records', async () => {
    api.terms.mockResolvedValueOnce({ items: [quotaTerm()], next_cursor: 'opaque', has_more: true })
    await wrapper.get('button').trigger('click'); await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'subscriptionQuota.nextPage')!.trigger('click'); await flushPromises()
    expect(api.terms.mock.calls).toEqual([[6, undefined], [6, 'opaque']])
    expect(api.usage).not.toHaveBeenCalled()
    expect(wrapper.find('table').exists()).toBe(false)
  })
  it('rejects stale history after changing authenticated identity', async () => {
    const held = deferred<QuotaPage<SubscriptionQuotaTerm>>(); api.terms.mockReturnValueOnce(held.promise)
    await wrapper.get('button').trigger('click')
    useAuthStore().user = { id: 2 } as User; await flushPromises()
    held.resolve({ items: [quotaTerm({ term_no: 987654 })], next_cursor: null, has_more: false }); await flushPromises()
    expect(wrapper.text()).not.toContain('987654')
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
  })
})
