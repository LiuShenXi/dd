import { computed, onMounted, shallowRef, ref, watch } from 'vue'
import type { User } from '@/types'
import type { UserConcurrencyProjection } from '@/types/userConcurrency'
import { useQuotaRequestIdentity } from './useQuotaRequestIdentity'

function project(user: User): UserConcurrencyProjection {
  return {
    concurrency: user.concurrency,
    current_concurrency: user.current_concurrency,
    legacy_current_concurrency: user.legacy_current_concurrency,
    legacy_concurrency_status: user.legacy_concurrency_status,
    claude_subscription_concurrency: user.claude_subscription_concurrency,
    default_claude_subscription_concurrency: user.default_claude_subscription_concurrency,
    effective_claude_subscription_concurrency: user.effective_claude_subscription_concurrency,
    claude_current_concurrency: user.claude_current_concurrency,
    claude_waiting_count: user.claude_waiting_count,
    claude_concurrency_status: user.claude_concurrency_status
  }
}
function unknown(user: User): UserConcurrencyProjection {
  return { ...project(user), current_concurrency: null, legacy_current_concurrency: null,
    legacy_concurrency_status: 'unavailable', claude_current_concurrency: null,
    claude_waiting_count: null, claude_concurrency_status: 'unavailable' }
}

// Bootstrap this page's display without mutating the authentication store.
// Later same-session /auth/me updates supersede this local profile snapshot.
export function useKeyPageConcurrency(getUser: () => User | null, getSession: () => string | null, getProfile: () => Promise<User>) {
  const scope = () => JSON.stringify([getUser()?.id ?? null, getSession()])
  const identity = useQuotaRequestIdentity(scope)
  const local = shallowRef<{ scope: string; value: UserConcurrencyProjection } | null>(null)
  const loading = ref(false)
  const projection = computed(() => {
    const user = getUser()
    if (!user) return null
    return local.value?.scope === scope() ? local.value.value : unknown(user)
  })
  async function refresh() {
    const user = getUser()
    if (!user || !getSession()) return
    const capturedScope = scope(), userID = user.id, current = identity.capture('profile')
    local.value = { scope: capturedScope, value: unknown(user) }
    loading.value = true
    try {
      const profile = await getProfile()
      if (!current()) return
      if (profile.id !== userID) throw new Error('Profile identity changed')
      local.value = { scope: capturedScope, value: project(profile) }
    } catch {
      if (current()) local.value = { scope: capturedScope, value: unknown(getUser()!) }
    } finally { if (current()) loading.value = false }
  }
  watch(scope, () => { local.value = null; loading.value = false; void refresh() }, { flush: 'post' })
  watch(getUser, (user, previous) => {
    if (user && previous && user.id === previous.id) {
      identity.invalidate()
      local.value = { scope: scope(), value: project(user) }
      loading.value = false
    }
  }, { flush: 'sync' })
  onMounted(() => { void refresh() })
  return { projection, loading, refresh }
}
