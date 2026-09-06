import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { announcementsAPI } from '@/api'
import type { UserAnnouncement } from '@/types'

const THROTTLE_MS = 20 * 60 * 1000 // 20 minutes
const CARPOOL_MEMBER_POLL_MS = 45 * 1000

export const useAnnouncementStore = defineStore('announcements', () => {
  // State
  const announcements = ref<UserAnnouncement[]>([])
  const loading = ref(false)
  const lastFetchTime = ref(0)
  const popupQueue = ref<UserAnnouncement[]>([])
  const currentPopup = ref<UserAnnouncement | null>(null)

  // Session-scoped dedup set — not reactive, used as plain lookup only
  let shownPopupIds = new Set<number>()
  let carpoolMemberTimer: ReturnType<typeof setInterval> | null = null
  let identity: number | null = null
  let generation = 0
  let lastVersion: string | null = null
  let lastUnreadCount: number | null = null
  let listRequestSequence = 0
  let versionRequestSequence = 0
  let popupDelayTimer: ReturnType<typeof setTimeout> | null = null

  // Getters
  const unreadCount = computed(() =>
    announcements.value.filter((a) => !a.read_at).length
  )

  // Actions
  async function fetchAnnouncements(force = false) {
    const requestGeneration = generation
    const now = Date.now()
    if (!force && lastFetchTime.value > 0 && now - lastFetchTime.value < THROTTLE_MS) {
      return false
    }
    const requestSequence = ++listRequestSequence

    // Set immediately to prevent concurrent duplicate requests
    lastFetchTime.value = now

    try {
      loading.value = true
      const all = await announcementsAPI.list(false)
      if (requestGeneration !== generation || requestSequence !== listRequestSequence) return false
      announcements.value = all.slice(0, 20)
      lastUnreadCount = unreadCount.value
      enqueueNewPopups()
      return true
    } catch (err: any) {
      // Revert throttle timestamp on failure so retry is allowed
      if (requestGeneration === generation && requestSequence === listRequestSequence) lastFetchTime.value = 0
      console.error('Failed to fetch announcements:', err)
      return false
    } finally {
      if (requestGeneration === generation && requestSequence === listRequestSequence) loading.value = false
    }
  }

  async function checkForUpdates() {
    const requestGeneration = generation
    const requestSequence = ++versionRequestSequence
    try {
      const status = await announcementsAPI.getVersion()
      if (requestGeneration !== generation || requestSequence !== versionRequestSequence) return
      const changed = lastVersion !== null && lastVersion !== status.version
      const unreadChanged = lastUnreadCount !== null && lastUnreadCount !== status.unread_count
      const needsInitialList = lastVersion === null && announcements.value.length === 0
      if (changed || unreadChanged || needsInitialList) {
        const refreshed = await fetchAnnouncements(true)
        if (!refreshed || requestGeneration !== generation || requestSequence !== versionRequestSequence) return
      }
      lastVersion = status.version
      lastUnreadCount = status.unread_count
    } catch (err) {
      if (requestGeneration === generation) console.error('Failed to check announcement version:', err)
    }
  }

  function enqueueNewPopups() {
    const newPopups = announcements.value.filter(
      (a) => a.notify_mode === 'popup' && !a.read_at && !shownPopupIds.has(a.id)
    )
    if (newPopups.length === 0) return

    for (const p of newPopups) {
      if (!popupQueue.value.some((q) => q.id === p.id)) {
        popupQueue.value.push(p)
      }
    }

    if (!currentPopup.value) {
      showNextPopup()
    }
  }

  function showNextPopup() {
    if (popupQueue.value.length === 0) {
      currentPopup.value = null
      return
    }
    currentPopup.value = popupQueue.value.shift()!
    shownPopupIds.add(currentPopup.value.id)
  }

  async function dismissPopup() {
    if (!currentPopup.value) return
    const id = currentPopup.value.id
    currentPopup.value = null

    // Mark as read (fire-and-forget, UI already updated)
    markAsRead(id)

    // Show next popup after a short delay
    if (popupQueue.value.length > 0) {
      const requestGeneration = generation
      if (popupDelayTimer) clearTimeout(popupDelayTimer)
      popupDelayTimer = setTimeout(() => {
        popupDelayTimer = null
        if (requestGeneration === generation) showNextPopup()
      }, 300)
    }
  }

  async function markAsRead(id: number) {
    const requestGeneration = generation
    try {
      await announcementsAPI.markRead(id)
      if (requestGeneration !== generation) return
      const ann = announcements.value.find((a) => a.id === id)
      if (ann) {
        ann.read_at = new Date().toISOString()
        lastUnreadCount = unreadCount.value
      }
    } catch (err: any) {
      console.error('Failed to mark announcement as read:', err)
    }
  }

  async function markAllAsRead() {
    const unread = announcements.value.filter((a) => !a.read_at)
    if (unread.length === 0) return

    const requestGeneration = generation
    try {
      loading.value = true
      await Promise.all(unread.map((a) => announcementsAPI.markRead(a.id)))
      if (requestGeneration !== generation) return
      announcements.value.forEach((a) => {
        if (!a.read_at) {
          a.read_at = new Date().toISOString()
        }
      })
      lastUnreadCount = 0
    } catch (err: any) {
      console.error('Failed to mark all as read:', err)
      throw err
    } finally {
      if (requestGeneration === generation) loading.value = false
    }
  }

  function startCarpoolMemberPolling() {
    if (carpoolMemberTimer) return
    carpoolMemberTimer = setInterval(() => {
      if (document.visibilityState === 'visible') void checkForUpdates()
    }, CARPOOL_MEMBER_POLL_MS)
  }

  function stopCarpoolMemberPolling() {
    if (carpoolMemberTimer) clearInterval(carpoolMemberTimer)
    carpoolMemberTimer = null
  }

  function clearSession() {
    stopCarpoolMemberPolling()
    announcements.value = []
    lastFetchTime.value = 0
    shownPopupIds = new Set()
    popupQueue.value = []
    currentPopup.value = null
    if (popupDelayTimer) clearTimeout(popupDelayTimer)
    popupDelayTimer = null
    listRequestSequence += 1
    versionRequestSequence += 1
    loading.value = false
    lastVersion = null
    lastUnreadCount = null
  }

  function setIdentity(userId: number | null) {
    if (identity === userId) return
    identity = userId
    generation += 1
    clearSession()
  }

  function reset() {
    identity = null
    generation += 1
    clearSession()
  }

  return {
    // State
    announcements,
    loading,
    currentPopup,
    // Getters
    unreadCount,
    // Actions
    fetchAnnouncements,
    checkForUpdates,
    dismissPopup,
    markAsRead,
    markAllAsRead,
    startCarpoolMemberPolling,
    stopCarpoolMemberPolling,
    setIdentity,
    reset,
  }
})
