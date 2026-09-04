import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const apiMocks = vi.hoisted(() => ({
  updateBalance: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      updateBalance: apiMocks.updateBalance,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/components/common/BaseDialog.vue', () => ({
  default: {
    name: 'BaseDialog',
    props: ['show', 'title', 'width'],
    template: '<div v-if="show"><slot /><slot name="footer" /></div>',
  },
}))

import UserBalanceModal from '../UserBalanceModal.vue'

function makeUser(balance: number) {
  return { id: 7, email: 'user@example.com', balance } as any
}

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.updateBalance.mockResolvedValue({})
})

describe('UserBalanceModal', () => {
  it('submits withdraw all as an atomic set-to-zero operation when the displayed balance becomes stale', async () => {
    const wrapper = mount(UserBalanceModal, {
      props: { show: true, user: makeUser(276.80589308), operation: 'subtract' },
    })

    const withdrawAllButton = wrapper.findAll('button').find((button) => button.text() === 'admin.users.withdrawAll')
    expect(withdrawAllButton).toBeTruthy()
    await withdrawAllButton!.trigger('click')
    await wrapper.setProps({ user: makeUser(275.58) })
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(apiMocks.updateBalance).toHaveBeenCalledWith(7, 0, 'set', '')
  })

  it('uses a regular subtraction after the admin edits an amount filled by withdraw all', async () => {
    const wrapper = mount(UserBalanceModal, {
      props: { show: true, user: makeUser(276.80589308), operation: 'subtract' },
    })

    const withdrawAllButton = wrapper.findAll('button').find((button) => button.text() === 'admin.users.withdrawAll')
    await withdrawAllButton!.trigger('click')
    await wrapper.get('input[type="number"]').setValue('100')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(apiMocks.updateBalance).toHaveBeenCalledWith(7, 100, 'subtract', '')
  })
})
