import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SubscriptionQuotaOrderSummary from '../SubscriptionQuotaOrderSummary.vue'
import { quotaOrderSnapshot } from '@/__tests__/subscriptionQuotaFixtures'
import en from '@/i18n/locales/en/subscriptionQuota'
import zh from '@/i18n/locales/zh/subscriptionQuota'
const translations = vi.hoisted(() => ({ locale: 'en' }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => {
  const resource: unknown = translations.locale === 'en' ? en : zh
  return key.split('.').reduce<unknown>((value, part) => (value as Record<string, unknown>)?.[part], resource) as string || key
} }) }))
describe('frozen order entitlement', () => {
  it.each(['en', 'zh'])('renders the exact historical quota and product names in %s', locale => {
    translations.locale = locale
    const wrapper = mount(SubscriptionQuotaOrderSummary, { props: { snapshot: quotaOrderSnapshot() } })
    expect(wrapper.text()).toContain('$100.00000001 USD')
    expect(wrapper.text()).toContain('Original weekly card · Claude group')
    expect(wrapper.text()).toContain(locale === 'en' ? 'Fixed validity: 168 hours (7 days)' : '有效期固定 168 小时（七天）')
    expect(wrapper.text()).not.toContain('subscriptionQuota.')
    wrapper.unmount()
  })
  it('does not invent an entitlement for a legacy order', () => {
    const wrapper = mount(SubscriptionQuotaOrderSummary, { props: { snapshot: null } })
    expect(wrapper.find('[data-testid="order-quota-snapshot"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
