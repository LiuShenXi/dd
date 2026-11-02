import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, onMounted } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ClaudeQuotaCard from '../ClaudeQuotaCard.vue'
import { useSubscriptionStore } from '@/stores/subscriptions'
import type { SubscriptionQuotaSummary } from '@/types/subscriptionQuota'
import { deferred, quotaSummary, quotaTerm } from '@/__tests__/subscriptionQuotaFixtures'
import en from '@/i18n/locales/en/subscriptionQuota'
import zh from '@/i18n/locales/zh/subscriptionQuota'
const translations = vi.hoisted(() => ({ locale: 'en' }))
const api = vi.hoisted(() => ({ summaries: vi.fn() }))
vi.mock('@/api/subscriptionQuota', () => ({ subscriptionQuotaAPI: api }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => {
  const resource: unknown = translations.locale === 'en' ? en : zh
  return key.split('.').reduce<unknown>((value, part) => (value as Record<string, unknown>)?.[part], resource) as string || key
} }) }))
let wrappers: VueWrapper[] = []
beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks(); vi.useFakeTimers({ toFake: ['Date', 'performance', 'setInterval', 'clearInterval'] }); vi.setSystemTime(new Date('2035-01-01T00:00:00Z')) })
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers = []; vi.useRealTimers() })
describe('server-projected Claude card', () => {
  it.each(['en', 'zh'])('renders exact zero, pending and known/unknown status labels in %s', locale => {
    translations.locale = locale
    const wrapper = mount(ClaudeQuotaCard, { props: { summary: quotaSummary({ current_term: quotaTerm({ used_usd: '0.00000000', pending_usd: '0.00000001', remaining_usd: '0.00000000', status: 'reconciling' }) }) } }); wrappers.push(wrapper)
    expect(wrapper.get('[data-testid="quota-remaining"]').text()).toBe('$0.00'); expect(wrapper.get('[data-testid="quota-pending"]').text()).toBe('$0.00000001'); expect(wrapper.text()).toContain(locale === 'en' ? 'Usage under review' : '用量待核查')
    expect(wrapper.findAll('[data-testid="claude-quota-card"]')).toHaveLength(1)
  })
  it('does not invent a balance for unavailable/missing current cards or new statuses', async () => {
    const wrapper = mount(ClaudeQuotaCard, { props: { summary: null } }); wrappers.push(wrapper)
    expect(wrapper.find('[data-testid="quota-remaining"]').exists()).toBe(false)
    await wrapper.setProps({ summary: quotaSummary({ current_term: null }) }); expect(wrapper.find('[data-testid="quota-remaining"]').exists()).toBe(false)
    await wrapper.setProps({ summary: quotaSummary({ current_term: quotaTerm({ status: 'future_status' }) }) }); expect(wrapper.text()).toContain('future_status')
  })
  it('uses monotonic server time and only refetches at expiry; held refresh never clears used/pending or mints next', async () => {
    const initial = quotaSummary({ current_term: quotaTerm({ expires_at: '2026-10-20T00:00:02Z' }), next_terms: [quotaTerm({ id: 11, term_no: 2, quota_usd: '300.00000000', status: 'scheduled', starts_at: '2026-10-20T00:00:02Z' })] })
    const held = deferred<SubscriptionQuotaSummary[]>(); api.summaries.mockResolvedValueOnce([initial]).mockReturnValueOnce(held.promise)
    const host = defineComponent({ setup() { const store = useSubscriptionStore(); const refresh = () => { void store.fetchTermSummaries() }; onMounted(refresh); return () => h(ClaudeQuotaCard, { summary: store.termSummaries[0] ?? null, onRefresh: refresh }) } })
    const wrapper = mount(host); wrappers.push(wrapper); await flushPromises(); expect(wrapper.get('[data-testid="quota-used"]').text()).toBe('$12.00')
    await vi.advanceTimersByTimeAsync(2100); await flushPromises(); expect(api.summaries).toHaveBeenCalledTimes(2); expect(wrapper.get('[data-testid="quota-used"]').text()).toBe('$12.00'); expect(wrapper.get('[data-testid="quota-pending"]').text()).toBe('$3.00'); expect(wrapper.get('[data-testid="quota-remaining"]').text()).toBe('$85.00')
    await vi.advanceTimersByTimeAsync(3000); expect(api.summaries).toHaveBeenCalledTimes(2)
    held.resolve([quotaSummary({ server_time: '2026-10-20T00:00:05Z', current_term: quotaTerm({ id: 11, quota_usd: '300.00000000', used_usd: '1.00000000', pending_usd: '2.00000000', remaining_usd: '297.00000000', expires_at: '2026-10-27T00:00:02Z' }) })]); await flushPromises(); expect(wrapper.get('[data-testid="quota-used"]').text()).toBe('$1.00'); expect(wrapper.get('[data-testid="quota-remaining"]').text()).toBe('$297.00')
  })
})
