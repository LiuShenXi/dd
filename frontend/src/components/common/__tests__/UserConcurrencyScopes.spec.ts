import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UserConcurrencyScopes from '../UserConcurrencyScopes.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('independent user concurrency scopes', () => {
  it('shows independent default3/override2 alongside legacy5 and exact zero active/wait counts', () => {
    const wrapper = mount(UserConcurrencyScopes, { props: { source: { concurrency: 5, current_concurrency: 5, default_claude_subscription_concurrency: 3, claude_subscription_concurrency: 2, effective_claude_subscription_concurrency: 2, claude_current_concurrency: 0, claude_waiting_count: 0, claude_concurrency_status: 'available' } } })
    expect(wrapper.get('[data-testid="legacy-scope-stats"]').text()).toBe('5 / 5'); expect(wrapper.get('[data-testid="claude-scope-stats"]').text()).toBe('0 / 2'); expect(wrapper.get('[data-testid="claude-scope-policy"]').text()).toContain('3'); expect(wrapper.get('[data-testid="claude-scope-policy"]').text()).toContain('2'); wrapper.unmount()
  })
  it('shows unavailable counts with known effective cap and never fabricates a free slot', () => {
    const wrapper = mount(UserConcurrencyScopes, { props: { source: { concurrency: 5, current_concurrency: null, default_claude_subscription_concurrency: 3, effective_claude_subscription_concurrency: 3, claude_current_concurrency: null, claude_waiting_count: null, claude_concurrency_status: 'unavailable' } } })
    expect(wrapper.get('[data-testid="claude-scope-stats"]').text()).toBe('concurrencyScopes.unavailable / 3'); expect(wrapper.get('[data-testid="legacy-scope-stats"]').text()).toBe('concurrencyScopes.unavailable / 5'); expect(wrapper.text()).not.toContain('0 /'); wrapper.unmount()
  })
  it('does not infer Claude cap from legacy when not configured', () => {
    const wrapper = mount(UserConcurrencyScopes, { props: { source: { concurrency: 99, effective_claude_subscription_concurrency: null, claude_concurrency_status: 'not_configured' } } })
    expect(wrapper.get('[data-testid="claude-scope-stats"]').text()).not.toContain('99'); expect(wrapper.text()).toContain('concurrencyScopes.notConfigured'); wrapper.unmount()
  })
  it('uses the server legacy projection ahead of the old admin-only current field', () => {
    const wrapper = mount(UserConcurrencyScopes, { props: { source: { concurrency: 5, current_concurrency: 99, legacy_current_concurrency: 2, legacy_concurrency_status: 'available', claude_current_concurrency: null, effective_claude_subscription_concurrency: 3, claude_concurrency_status: 'unavailable' } } })
    expect(wrapper.get('[data-testid="legacy-scope-stats"]').text()).toBe('2 / 5')
    expect(wrapper.get('[data-testid="claude-scope-stats"]').text()).toBe('concurrencyScopes.unavailable / 3')
    wrapper.unmount()
  })
  it('does not fall back to a stale old count when the explicit legacy status is unavailable', () => {
    const wrapper = mount(UserConcurrencyScopes, { props: { source: { concurrency: 5, current_concurrency: 0, legacy_current_concurrency: null, legacy_concurrency_status: 'unavailable', claude_current_concurrency: 1, claude_waiting_count: 0, effective_claude_subscription_concurrency: 3, claude_concurrency_status: 'available' } } })
    expect(wrapper.get('[data-testid="legacy-scope-stats"]').text()).toBe('concurrencyScopes.unavailable / 5')
    expect(wrapper.get('[data-testid="claude-scope-stats"]').text()).toBe('1 / 3')
    wrapper.unmount()
  })
})
