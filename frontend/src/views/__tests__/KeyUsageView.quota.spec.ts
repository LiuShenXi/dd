import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import KeyUsageView from '../KeyUsageView.vue'
import { deferred, quotaSummary } from '@/__tests__/subscriptionQuotaFixtures'
const app = vi.hoisted(() => ({ showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ ...app, cachedPublicSettings: null, publicSettingsLoaded: true, siteName: 'Synthetic' }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
let wrappers: VueWrapper[] = []
const data = () => ({ billing_scope: 'claude_subscription', quota_mode: 'term', quota_summary: quotaSummary(), remaining: '85.00000000', mode: 'quota_limited', quota: { limit: 999, used: 0, remaining: 999 }, rate_limits: [], balance: 12345 })
function response(body = data()) { return { ok: true, json: async () => body } as Response }
function setup() { const wrapper = mount(KeyUsageView, { global: { stubs: { RouterLink: { template: '<a><slot /></a>' }, LocaleSwitcher: true, Icon: true } } }); wrappers.push(wrapper); return wrapper }
beforeEach(() => { vi.clearAllMocks(); Object.defineProperty(window, 'matchMedia', { configurable: true, value: vi.fn().mockReturnValue({ matches: false }) }); vi.stubGlobal('requestAnimationFrame', () => 0); vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response())) })
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers = []; vi.unstubAllGlobals() })
describe('Key usage canonical shared card', () => {
  it('renders one shared allowance independently of Key quota and never substitutes ordinary money', async () => {
    const wrapper = setup(); const input = wrapper.get('input'); await input.setValue('key-A'); await input.trigger('keydown.enter'); await flushPromises()
    expect(wrapper.findAll('[data-testid="claude-quota-card"]')).toHaveLength(1); expect(wrapper.get('[data-testid="quota-remaining"]').text()).toBe('$85.00'); expect(wrapper.get('[data-testid="quota-pending"]').text()).toBe('$3.00'); expect(wrapper.text()).toContain('subscriptionQuota.keyLimit'); expect(wrapper.text()).not.toContain('12345')
    await input.setValue('key-B'); await input.trigger('keydown.enter'); await flushPromises(); expect(wrapper.findAll('[data-testid="claude-quota-card"]')).toHaveLength(1); expect(wrapper.get('[data-testid="quota-quota"]').text()).toBe('$100.00')
  })
  it.each(['success', 'error'])('ignores stale Key A %s and finally while B query is pending', async outcome => {
    const a = deferred<Response>(), b = deferred<Response>(); vi.mocked(fetch).mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const wrapper = setup(), input = wrapper.get('input'); await input.setValue('key-A'); await input.trigger('keydown.enter'); await input.setValue('key-B'); await input.trigger('keydown.enter')
    if (outcome === 'success') a.resolve(response()); else a.reject(new Error('old secret key A failure'))
    await flushPromises(); expect(app.showError).not.toHaveBeenCalled(); expect(app.showSuccess).not.toHaveBeenCalled(); expect(wrapper.find('[data-testid="claude-quota-card"]').exists()).toBe(false); expect(wrapper.text()).toContain('keyUsage.querying')
    b.resolve(response()); await flushPromises(); expect(app.showSuccess).toHaveBeenCalledTimes(1); expect(wrapper.get('[data-testid="quota-remaining"]').text()).toBe('$85.00')
  })
})
