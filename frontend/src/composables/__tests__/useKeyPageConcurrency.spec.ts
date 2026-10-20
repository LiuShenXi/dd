import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { User } from '@/types'
import { deferred } from '@/__tests__/subscriptionQuotaFixtures'
import { useKeyPageConcurrency } from '../useKeyPageConcurrency'

const profile = (id: number, current = 2): User => ({ id, concurrency: 5, legacy_current_concurrency: current, legacy_concurrency_status: 'available', default_claude_subscription_concurrency: 3, effective_claude_subscription_concurrency: 3, claude_current_concurrency: current, claude_waiting_count: 0, claude_concurrency_status: 'available' }) as User
let wrapper: VueWrapper
afterEach(() => wrapper?.unmount())
function setup() {
  const user = ref<User | null>(profile(1)), session = ref<string | null>('session-A'), fetch = vi.fn<() => Promise<User>>()
  let state!: ReturnType<typeof useKeyPageConcurrency>
  const start = () => { wrapper = mount(defineComponent({ setup() { state = useKeyPageConcurrency(() => user.value, () => session.value, fetch); return () => null } })); return state }
  return { user, session, fetch, start }
}
describe('Key page profile bootstrap and session identity', () => {
  it('fetches immediately on mount without waiting for auth polling or changing global auth data', async () => {
    const fixture = setup(), response = deferred<User>(); fixture.fetch.mockReturnValue(response.promise)
    const initialUser = fixture.user.value, state = fixture.start()
    expect(fixture.fetch).toHaveBeenCalledOnce(); expect(state.loading.value).toBe(true)
    expect(state.projection.value?.legacy_current_concurrency).toBeNull()
    response.resolve(profile(1, 1)); await flushPromises()
    expect(state.projection.value?.claude_current_concurrency).toBe(1); expect(state.loading.value).toBe(false)
    expect(fixture.user.value).toBe(initialUser); expect(fixture.user.value?.claude_current_concurrency).toBe(2)
  })
  it('late A success/finally cannot replace B or clear B loading', async () => {
    const fixture = setup(), a = deferred<User>(), b = deferred<User>(); fixture.fetch.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const state = fixture.start(); fixture.session.value = 'session-B'; fixture.user.value = profile(2); await flushPromises()
    expect(fixture.fetch).toHaveBeenCalledTimes(2)
    a.resolve(profile(1, 99)); await flushPromises()
    expect(state.loading.value).toBe(true); expect(state.projection.value?.claude_current_concurrency).toBeNull()
    b.resolve(profile(2, 1)); await flushPromises(); expect(state.projection.value?.claude_current_concurrency).toBe(1)
  })
  it('same-session auth refresh supersedes a local bootstrap and invalidates older page requests', async () => {
    const fixture = setup(), first = deferred<User>(), old = deferred<User>(); fixture.fetch.mockReturnValueOnce(first.promise).mockReturnValueOnce(old.promise)
    const state = fixture.start(); first.resolve(profile(1, 1)); await flushPromises()
    const pending = state.refresh(); fixture.user.value = profile(1, 3)
    expect(state.projection.value?.claude_current_concurrency).toBe(3); expect(state.loading.value).toBe(false)
    old.resolve(profile(1, 0)); await pending
    expect(state.projection.value?.claude_current_concurrency).toBe(3)
  })
  it('A to B to A does not revive the first A response', async () => {
    const fixture = setup(), firstA = deferred<User>(), secondA = deferred<User>(); fixture.fetch.mockReturnValueOnce(firstA.promise).mockReturnValueOnce(secondA.promise)
    const state = fixture.start(); fixture.user.value = profile(2); fixture.session.value = 'session-B'; fixture.user.value = profile(1); fixture.session.value = 'session-A-again'; await flushPromises()
    firstA.resolve(profile(1, 99)); await flushPromises()
    expect(state.loading.value).toBe(true); expect(state.projection.value?.claude_current_concurrency).toBeNull()
    secondA.resolve(profile(1, 1)); await flushPromises(); expect(state.projection.value?.claude_current_concurrency).toBe(1)
  })
  it('old A catch/finally does not change B; B failure reports unknown instead of old counts or fake zero', async () => {
    const fixture = setup(), a = deferred<User>(), b = deferred<User>(); fixture.fetch.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const state = fixture.start(); fixture.user.value = profile(2); fixture.session.value = 'session-B'; await flushPromises()
    a.reject(new Error('old A failed')); await flushPromises(); expect(state.loading.value).toBe(true)
    b.reject(new Error('statistics unavailable')); await flushPromises()
    expect(state.loading.value).toBe(false)
    expect(state.projection.value).toMatchObject({ legacy_current_concurrency: null, legacy_concurrency_status: 'unavailable', claude_current_concurrency: null, claude_waiting_count: null, claude_concurrency_status: 'unavailable', effective_claude_subscription_concurrency: 3 })
    expect(fixture.user.value?.id).toBe(2)
  })
  it('rejects a response belonging to a different user and ignores completion after unmount', async () => {
    const fixture = setup(), mismatch = deferred<User>(), late = deferred<User>(); fixture.fetch.mockReturnValueOnce(mismatch.promise).mockReturnValueOnce(late.promise)
    const state = fixture.start(); mismatch.resolve(profile(99, 99)); await flushPromises()
    expect(state.projection.value?.claude_current_concurrency).toBeNull()
    const pending = state.refresh(); wrapper.unmount(); late.resolve(profile(1, 99)); await pending
    expect(state.projection.value?.claude_current_concurrency).toBeNull()
  })
})
