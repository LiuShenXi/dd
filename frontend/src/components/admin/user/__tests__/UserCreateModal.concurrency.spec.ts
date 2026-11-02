import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import UserCreateModal from '../UserCreateModal.vue'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'
import { deferred } from '@/__tests__/subscriptionQuotaFixtures'
const api = vi.hoisted(() => ({ create: vi.fn(), showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { create: api.create } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => api }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
let wrapper: VueWrapper
beforeEach(() => { setActivePinia(createPinia()); useAuthStore().user = { id: 1 } as User; vi.clearAllMocks(); api.create.mockResolvedValue({}); wrapper = mount(UserCreateModal, { props: { show: true }, global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }, Icon: true, TotpStepUpDialog: true } } }) })
afterEach(() => wrapper.unmount())
describe('creating a user with independent Claude policy', () => {
  it('omits a blank Claude override and validates a separate positive override', async () => {
    await wrapper.get('input[type="email"]').setValue('user@example.invalid'); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.create.mock.calls[0][0]).not.toHaveProperty('claude_subscription_concurrency')
    await wrapper.get('[data-testid="claude-concurrency-input"]').setValue('0'); await wrapper.get('form').trigger('submit'); expect(api.create).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid="claude-concurrency-input"]').setValue('2'); await wrapper.get('form').trigger('submit'); await flushPromises(); expect(api.create.mock.calls[1][0]).toMatchObject({ concurrency: 1, claude_subscription_concurrency: 2 })
  })
  it('does not release an in-flight create mutex when the form changes', async () => {
    const pending = deferred<unknown>(); api.create.mockReturnValueOnce(pending.promise)
    await wrapper.get('input[type="email"]').setValue('a@example.invalid'); await wrapper.get('form').trigger('submit'); await flushPromises(); await wrapper.get('[data-testid="claude-concurrency-input"]').setValue('4'); await wrapper.get('form').trigger('submit'); expect(api.create).toHaveBeenCalledTimes(1)
    pending.resolve({}); await flushPromises(); expect(wrapper.emitted('close')).toBeUndefined()
  })
  it('old admin A finally cannot unlock a create started by admin B', async () => {
    const a = deferred<unknown>(), b = deferred<unknown>(); api.create.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    await wrapper.get('form').trigger('submit'); await flushPromises(); useAuthStore().user = { id: 2 } as User; await wrapper.get('input[type="email"]').setValue('b@example.invalid'); await wrapper.get('form').trigger('submit'); await flushPromises()
    a.reject(new Error('old admin error')); await flushPromises(); expect(api.showError).not.toHaveBeenCalled(); expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    b.resolve({}); await flushPromises(); expect(wrapper.emitted('success')).toHaveLength(1)
  })
})
