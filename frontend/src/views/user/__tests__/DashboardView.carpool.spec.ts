import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DashboardView from '@/views/user/DashboardView.vue'

const authStore = vi.hoisted(() => ({
  user: { id: 101, balance: 0 },
  isSimpleMode: false,
  refreshUser: vi.fn(),
}))
const fetchDetails = vi.hoisted(() => vi.fn())
const usageAPI = vi.hoisted(() => ({
  getDashboardStats: vi.fn(),
  getDashboardTrend: vi.fn(),
  getDashboardModels: vi.fn(),
  getByDateRange: vi.fn(),
}))
const getMyPlatformQuotas = vi.hoisted(() => vi.fn())

vi.mock('@/stores/auth', () => ({ useAuthStore: () => authStore }))
vi.mock('@/stores/carpool', () => ({ useCarpoolStore: () => ({ fetchDetails }) }))
vi.mock('@/api/usage', () => ({ usageAPI }))
vi.mock('@/api/user', () => ({ getMyPlatformQuotas }))

describe('DashboardView carpool refresh', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    authStore.refreshUser.mockResolvedValue(authStore.user)
    fetchDetails.mockResolvedValue({})
    usageAPI.getDashboardStats.mockResolvedValue({ by_platform: [] })
    usageAPI.getDashboardTrend.mockResolvedValue({ trend: [] })
    usageAPI.getDashboardModels.mockResolvedValue({ models: [] })
    usageAPI.getByDateRange.mockResolvedValue({ items: [] })
    getMyPlatformQuotas.mockResolvedValue({ platform_quotas: [] })
  })

  it('refreshes authoritative carpool details on initial and explicit dashboard refreshes', async () => {
    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<main><slot /></main>' },
          LoadingSpinner: true,
          UserDashboardStats: true,
          UserDashboardCharts: { template: '<button data-testid="refresh-dashboard" @click="$emit(\'refresh\')">Refresh</button>' },
          UserDashboardRecentUsage: true,
          UserDashboardQuickActions: true,
        },
      },
    })
    await flushPromises()

    expect(fetchDetails).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid="refresh-dashboard"]').trigger('click')
    await flushPromises()
    expect(fetchDetails).toHaveBeenCalledTimes(2)
  })
})
