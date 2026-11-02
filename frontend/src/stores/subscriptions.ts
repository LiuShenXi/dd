/**
 * Subscription Store
 * Global state management for user subscriptions with caching and deduplication
 */

import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import subscriptionsAPI from '@/api/subscriptions'
import type { UserSubscription } from '@/types'
import { subscriptionQuotaAPI } from '@/api/subscriptionQuota'
import { useAuthStore } from './auth'
import type { NormalizedUserSubscription, SubscriptionQuotaSummary } from '@/types/subscriptionQuota'

// Cache TTL: 60 seconds
const CACHE_TTL_MS = 60_000

// Request generation counter to invalidate stale in-flight responses
let requestGeneration = 0

export const useSubscriptionStore = defineStore('subscriptions', () => {
  const auth = useAuthStore()
  const termSummaries = ref<SubscriptionQuotaSummary[]>([])
  const termsLoading = ref(false)
  let termGeneration = 0
  let currentTermResult: SubscriptionQuotaSummary[] | null = null
  let currentTermError: unknown = null
  let currentActiveResult: UserSubscription[] | null = null
  let currentActiveError: unknown = null
  let activeIdentity: number | undefined
  let termIdentity: number | undefined
  // State
  const activeSubscriptions = ref<NormalizedUserSubscription[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const lastFetchedAt = ref<number | null>(null)

  // In-flight request deduplication
  let activePromise: Promise<UserSubscription[]> | null = null

  // Auto-refresh interval
  let pollerInterval: ReturnType<typeof setInterval> | null = null

  // Computed
  const hasActiveSubscriptions = computed(() => activeSubscriptions.value.length > 0)

  /**
   * Fetch active subscriptions with caching and deduplication
   * @param force - Force refresh even if cache is valid
   */
  async function fetchActiveSubscriptions(force = false): Promise<UserSubscription[]> {
    const now = Date.now()

    // Return cached data if valid
    if (
      !force &&
      activeIdentity === auth.user?.id &&
      loaded.value &&
      lastFetchedAt.value &&
      now - lastFetchedAt.value < CACHE_TTL_MS
    ) {
      currentActiveResult = activeSubscriptions.value
      return activeSubscriptions.value
    }

    // Return in-flight request if exists (deduplication)
    if (activePromise && !force) {
      return activePromise
    }

    const currentGeneration = ++requestGeneration
    const userId = auth.user?.id
    const current = () => currentGeneration === requestGeneration && auth.user?.id === userId
    currentActiveResult = null
    currentActiveError = null

    // Start new request
    loading.value = true
    const requestPromise = subscriptionsAPI
      .getActiveSubscriptions()
      .then((data) => {
        if (current()) {
          activeSubscriptions.value = data
          loaded.value = true
          lastFetchedAt.value = Date.now()
          currentActiveResult = data
          activeIdentity = userId
        }
        return data
      })
      .catch((error) => {
        if (current()) { activeIdentity = userId; currentActiveError = error; console.error('Failed to fetch active subscriptions:', error) }
        throw error
      })
      .finally(() => {
        if (current() && activePromise === requestPromise) {
          loading.value = false
          activePromise = null
        }
      })

    activePromise = requestPromise

    return activePromise
  }

  /**
   * Start auto-refresh polling 
   */
  function startPolling() {
    if (pollerInterval) return

    pollerInterval = setInterval(() => {
      fetchActiveSubscriptions(true).catch((error) => {
        console.error('Subscription polling failed:', error)
      })
    }, 5 * 60 * 1000)
  }

  /**
   * Stop auto-refresh polling
   */
  function stopPolling() {
    if (pollerInterval) {
      clearInterval(pollerInterval)
      pollerInterval = null
    }
  }

  /**
   * Clear all subscription data and stop polling
   */
  function clear() {
    requestGeneration++
    activePromise = null
    loading.value = false
    activeSubscriptions.value = []
    loaded.value = false
    lastFetchedAt.value = null
    termGeneration++
    termSummaries.value = []
    termsLoading.value = false
    currentTermResult = null
    currentTermError = null
    currentActiveResult = null
    currentActiveError = null
    stopPolling()
  }

  /**
   * Invalidate cache (force next fetch to reload)
   */
  function invalidateCache() {
    lastFetchedAt.value = null
  }

  async function fetchTermSummaries(): Promise<SubscriptionQuotaSummary[]> {
    const generation = ++termGeneration, userId = auth.user?.id
    const current = () => generation === termGeneration && auth.user?.id === userId
    currentTermResult = null; currentTermError = null; termsLoading.value = true
    try {
      const data = await subscriptionQuotaAPI.summaries()
      if (current()) { termSummaries.value = data; currentTermResult = data; termIdentity = userId }
      return data
    } catch (error) { if (current()) { currentTermError = error; termIdentity = userId } throw error }
    finally { if (current()) termsLoading.value = false }
  }

  function isCurrentTermSummaries(result: SubscriptionQuotaSummary[]) { return currentTermResult === result && termIdentity === auth.user?.id }
  function isCurrentTermError(error: unknown) { return currentTermError !== null && currentTermError === error && termIdentity === auth.user?.id }
  function isCurrentActiveSubscriptions(result: UserSubscription[]) { return currentActiveResult === result && activeIdentity === auth.user?.id }
  function isCurrentActiveError(error: unknown) { return currentActiveError !== null && currentActiveError === error && activeIdentity === auth.user?.id }
  watch(() => auth.user?.id, clear, { flush: 'sync' })

  return {
    // State
    activeSubscriptions,
    loading,
    hasActiveSubscriptions,
    termSummaries,
    termsLoading,

    // Actions
    fetchActiveSubscriptions,
    startPolling,
    stopPolling,
    clear,
    invalidateCache,
    fetchTermSummaries, isCurrentTermSummaries, isCurrentTermError, isCurrentActiveSubscriptions, isCurrentActiveError
  }
})
