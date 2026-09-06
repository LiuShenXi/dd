import { defineStore } from 'pinia'
import { computed, ref, toRaw } from 'vue'
import { carpoolAPI } from '@/api/carpool'
import type { CarpoolBoostStatus, CarpoolDetails } from '@/types/carpool'

function createRequestKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return `carpool-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export const useCarpoolStore = defineStore('carpool', () => {
  const details = ref<CarpoolDetails | null>(null)
  const boosts = ref<CarpoolBoostStatus | null>(null)
  const detailsLoading = ref(false)
  const detailsError = ref(false)
  const boostsLoading = ref(false)
  const claiming = ref(false)
  let claimKey: string | null = null
  let identity: number | null = null
  let generation = 0
  let detailsRequestSequence = 0
  let boostsRequestSequence = 0
  let latestDetailsError: unknown = null
  let detailsRefreshTimer: ReturnType<typeof setInterval> | null = null

  const availableQuotaUsd = computed(() => details.value?.quota?.available_usd ?? null)

  function isCurrentDetails(result: CarpoolDetails): boolean {
    return details.value !== null && toRaw(details.value) === result
  }

  function isCurrentDetailsError(error: unknown): boolean {
    return latestDetailsError === error
  }

  function setIdentity(userId: number | null) {
    if (identity === userId) return
    identity = userId
    generation += 1
    details.value = null
    boosts.value = null
    detailsLoading.value = false
    detailsError.value = false
    boostsLoading.value = false
    claiming.value = false
    claimKey = null
    latestDetailsError = null
  }

  async function fetchDetails(): Promise<CarpoolDetails> {
    const requestGeneration = generation
    const requestSequence = ++detailsRequestSequence
    latestDetailsError = null
    detailsLoading.value = true
    try {
      const result = await carpoolAPI.getDetails()
      if (requestGeneration === generation && requestSequence === detailsRequestSequence) {
        details.value = result
        detailsError.value = false
        latestDetailsError = null
      }
      return result
    } catch (error) {
      if (requestGeneration === generation && requestSequence === detailsRequestSequence) {
        detailsError.value = true
        latestDetailsError = error
      }
      throw error
    } finally {
      if (requestGeneration === generation && requestSequence === detailsRequestSequence) detailsLoading.value = false
    }
  }

  async function fetchBoosts(): Promise<CarpoolBoostStatus> {
    const requestGeneration = generation
    const requestSequence = ++boostsRequestSequence
    boostsLoading.value = true
    try {
      const result = await carpoolAPI.getBoosts()
      if (requestGeneration === generation && requestSequence === boostsRequestSequence) boosts.value = result
      return result
    } finally {
      if (requestGeneration === generation && requestSequence === boostsRequestSequence) boostsLoading.value = false
    }
  }

  async function claimBoost(): Promise<CarpoolBoostStatus> {
    if (claiming.value) throw new Error('CARPOOL_BOOST_IN_PROGRESS')
    claiming.value = true
    claimKey ||= createRequestKey()
    const requestGeneration = generation
    try {
      const result = await carpoolAPI.claimBoost(claimKey)
      if (requestGeneration === generation) {
        boosts.value = result
        claimKey = null
      }
      return result
    } finally {
      if (requestGeneration === generation) claiming.value = false
    }
  }

  function startDetailsPolling() {
    if (detailsRefreshTimer) return
    detailsRefreshTimer = setInterval(() => {
      if (document.visibilityState === 'visible') {
        void fetchDetails().catch((error) => {
          if (isCurrentDetailsError(error)) console.error('Failed to refresh carpool quota:', error)
        })
      }
    }, 45_000)
  }

  function stopDetailsPolling() {
    if (detailsRefreshTimer) clearInterval(detailsRefreshTimer)
    detailsRefreshTimer = null
  }

  function reset() {
    stopDetailsPolling()
    identity = null
    generation += 1
    detailsRequestSequence += 1
    boostsRequestSequence += 1
    details.value = null
    boosts.value = null
    detailsLoading.value = false
    detailsError.value = false
    boostsLoading.value = false
    claiming.value = false
    claimKey = null
    latestDetailsError = null
  }

  return {
    details,
    boosts,
    availableQuotaUsd,
    isCurrentDetails,
    isCurrentDetailsError,
    detailsLoading,
    detailsError,
    boostsLoading,
    claiming,
    setIdentity,
    fetchDetails,
    fetchBoosts,
    claimBoost,
    startDetailsPolling,
    stopDetailsPolling,
    reset,
  }
})
