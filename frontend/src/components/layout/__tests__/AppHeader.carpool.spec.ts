import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AppHeader from '@/components/layout/AppHeader.vue'

const state = vi.hoisted(() => ({
  auth: {
    user: { id: 7, username: 'member', email: 'member@example.com', role: 'user', balance: 25, frozen_balance: 5 },
    isAdmin: false,
    isSimpleMode: false,
    logout: vi.fn(),
  },
  carpool: { availableQuotaUsd: null as string | null, billingMode: 'standard' as 'standard' | 'carpool' | null, detailsError: false },
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({ name: 'Dashboard', params: {}, meta: { titleKey: 'dashboard.title' } }),
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/stores', () => ({
  useAuthStore: () => state.auth,
  useAppStore: () => ({ contactInfo: '', docUrl: '', cachedPublicSettings: null, toggleMobileSidebar: vi.fn() }),
  useOnboardingStore: () => ({ replay: vi.fn() }),
}))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/stores/carpool', () => ({ useCarpoolStore: () => state.carpool }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: { modelPlaza: 'modelPlaza' }, isFeatureFlagEnabled: () => false }))

function mountHeader() {
  return mount(AppHeader, {
    global: {
      stubs: {
        AnnouncementBell: true,
        LocaleSwitcher: true,
        SubscriptionProgressMini: true,
        Icon: true,
        RouterLink: { template: '<a><slot /></a>' },
      },
    },
  })
}

describe('AppHeader carpool funding presentation', () => {
  beforeEach(() => {
    state.auth.user.balance = 25
    state.auth.user.frozen_balance = 5
    state.auth.user.role = 'user'
    state.carpool.availableQuotaUsd = null
    state.carpool.billingMode = 'standard'
    state.carpool.detailsError = false
  })

  it('shows carpool quota as a separate source without adding ordinary balance', () => {
    state.carpool.availableQuotaUsd = '838.00000000'
    state.carpool.billingMode = 'carpool'
    const wrapper = mountHeader()

    expect(wrapper.get('[data-testid="header-funding-label"]').text()).toBe('carpool.availableQuota')
    expect(wrapper.get('[data-testid="header-funding-amount"]').text()).toBe('$838.00')
    expect(wrapper.get('[data-testid="header-carpool-badge"]').text()).toBe('carpool.quotaBadge')
    expect(wrapper.text()).not.toContain('carpool.ordinaryBalance')
    expect(wrapper.text()).not.toContain('$25.00')
    expect(wrapper.text()).not.toContain('$863.00')
    wrapper.unmount()
  })

  it('keeps the last known carpool quota visible with an explicit stale label', () => {
    state.carpool.availableQuotaUsd = '838.00000000'
    state.carpool.billingMode = 'carpool'
    state.carpool.detailsError = true
    const wrapper = mountHeader()

    expect(wrapper.get('[data-testid="header-funding-amount"]').text()).toBe('$838.00')
    expect(wrapper.get('[data-testid="header-carpool-badge"]').text()).toContain('carpool.quotaStaleShort')
    expect(wrapper.text()).toContain('carpool.quotaStale')
    wrapper.unmount()
  })

  it('preserves ordinary available and frozen balance behavior without active carpool quota', () => {
    const wrapper = mountHeader()

    expect(wrapper.get('[data-testid="header-funding-label"]').text()).toBe('可用余额')
    expect(wrapper.get('[data-testid="header-funding-amount"]').text()).toBe('$25.00')
    expect(wrapper.text()).toContain('$30.00')
    wrapper.unmount()
  })

  it.each([false, true])('never exposes the retained balance while billing mode is unknown, error=%s', (error) => {
    state.carpool.billingMode = null
    state.carpool.detailsError = error
    const wrapper = mountHeader()
    expect(wrapper.get('[data-testid="header-funding-amount"]').text()).toBe('--')
    expect(wrapper.text()).not.toContain('$25.00')
    expect(wrapper.text()).not.toContain('$30.00')
    wrapper.unmount()
  })

  it('shows zero after expiry and keeps the administrator on ordinary billing', () => {
    state.carpool.billingMode = 'carpool'
    state.carpool.availableQuotaUsd = '0'
    const member = mountHeader()
    expect(member.get('[data-testid="header-funding-amount"]').text()).toBe('$0.00')
    expect(member.text()).not.toContain('$25.00')
    member.unmount()

    state.auth.user.role = 'admin'
    state.carpool.billingMode = null
    const admin = mountHeader()
    expect(admin.get('[data-testid="header-funding-amount"]').text()).toBe('$25.00')
    expect(admin.text()).toContain('$30.00')
    admin.unmount()
  })
})
