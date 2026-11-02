import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import UserEditModal from '../UserEditModal.vue'
import { deferred } from '@/__tests__/subscriptionQuotaFixtures'

const { update, updateUserAttributeValues, showSuccess, showError } = vi.hoisted(() => ({
  update: vi.fn(),
  updateUserAttributeValues: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: { update },
    userAttributes: { updateUserAttributeValues }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn() })
}))

// useStepUp pulls in the API client, which needs the real i18n instance.
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key
  })
}))

const mountModal = (concurrency: number) => mount(UserEditModal, {
  props: {
    show: true,
    user: { id: 7, email: 'user@example.test', username: 'user', notes: '', role: 'user', concurrency, rpm_limit: 0 } as never
  },
  global: {
    stubs: {
      BaseDialog: {
        props: ['show', 'title'],
        template: '<div v-if="show"><slot /><slot name="footer" /></div>'
      },
      Select: true,
      Icon: true,
      UserAttributeForm: true,
      TotpStepUpDialog: true
    }
  }
})

describe('UserEditModal concurrency', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    update.mockReset()
    updateUserAttributeValues.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    update.mockResolvedValue({})
  })

  // Regression coverage for issue #5977: the gateway treats concurrency <= 0 as
  // unlimited (AcquireUserSlot) and both the batch limits endpoint and the bulk
  // edit modal accept 0, so this dialog must not be the only place that rejects
  // it — doing so blocked every other edit on such a user.
  it('saves an unlimited (0) concurrency instead of blocking the whole form', async () => {
    const wrapper = mountModal(0)

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
    expect(update).toHaveBeenCalledWith(7, expect.objectContaining({ concurrency: 0 }))
    expect(wrapper.emitted('success')).toBeTruthy()
  })

  it('still rejects a negative concurrency', async () => {
    const wrapper = mountModal(3)

    await wrapper.get('[data-test="concurrency-input"]').setValue('-1')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.users.concurrencyNonNegative')
    expect(update).not.toHaveBeenCalled()
  })
  it('does not alter Claude policy when only the legacy setting changes', async () => {
    const wrapper = mountModal(5)
    await wrapper.get('[data-test="concurrency-input"]').setValue('8')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(update.mock.calls[0][1].concurrency).toBe(8)
    expect(update.mock.calls[0][1]).not.toHaveProperty('claude_subscription_concurrency')
    wrapper.unmount()
  })
  it.each(['0', '-1', '1.5', '9007199254740993'])('rejects an invalid Claude-only override %s', async value => {
    const wrapper = mountModal(5)
    await wrapper.get('[data-testid="claude-concurrency-input"]').setValue(value)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(update).not.toHaveBeenCalled(); expect(showError).toHaveBeenCalledWith('concurrencyScopes.invalid')
    wrapper.unmount()
  })
  it('submits an explicit Claude override without deriving it from GPT concurrency', async () => {
    const wrapper = mountModal(5)
    await wrapper.get('[data-testid="claude-concurrency-input"]').setValue('2')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(update.mock.calls[0][1]).toMatchObject({ concurrency: 5, claude_subscription_concurrency: 2 })
    wrapper.unmount()
  })
  it('keeps the same target mutation locked after edits until the original update finishes', async () => {
    const pending = deferred<unknown>(); update.mockReturnValueOnce(pending.promise)
    const wrapper = mountModal(5)
    await wrapper.get('[data-testid="claude-concurrency-input"]').setValue('2'); await wrapper.get('form').trigger('submit'); await flushPromises()
    await wrapper.get('[data-testid="claude-concurrency-input"]').setValue('4'); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(update).toHaveBeenCalledTimes(1); expect(update.mock.calls[0][1].claude_subscription_concurrency).toBe(2); expect(wrapper.get('[data-testid="user-edit-submit"]').attributes('disabled')).toBeDefined()
    pending.resolve({}); await flushPromises(); expect(wrapper.emitted('close')).toBeUndefined(); expect(wrapper.get('[data-testid="user-edit-submit"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
  it.each(['success', 'error'])('old A %s/finally cannot clear B submitting state', async outcome => {
    const a = deferred<unknown>(), b = deferred<unknown>(); update.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const wrapper = mountModal(5); await wrapper.get('form').trigger('submit'); await flushPromises()
    await wrapper.setProps({ user: { id: 8, email: 'b@example.test', role: 'user', concurrency: 5 } as never }); await wrapper.get('[data-testid="claude-concurrency-input"]').setValue('4'); await wrapper.get('form').trigger('submit'); await flushPromises()
    if (outcome === 'success') a.resolve({}); else a.reject(new Error('old A failure'))
    await flushPromises(); expect(wrapper.get('[data-testid="user-edit-submit"]').attributes('disabled')).toBeDefined(); expect(showError).not.toHaveBeenCalled(); expect(wrapper.emitted('success')).toBeUndefined()
    b.resolve({}); await flushPromises(); expect(wrapper.emitted('success')).toHaveLength(1)
    wrapper.unmount()
  })
})
