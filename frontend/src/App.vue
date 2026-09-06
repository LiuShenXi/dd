<script setup lang="ts">
import { RouterView, useRouter, useRoute } from 'vue-router'
import { onMounted, onBeforeUnmount, watch } from 'vue'
import Toast from '@/components/common/Toast.vue'
import NavigationProgress from '@/components/common/NavigationProgress.vue'
import AdminComplianceDialog from '@/components/admin/AdminComplianceDialog.vue'
import { resolveRouteDocumentTitle } from '@/router/title'
import AnnouncementPopup from '@/components/common/AnnouncementPopup.vue'
import { useAppStore, useAuthStore, useSubscriptionStore, useAnnouncementStore, useAdminComplianceStore, useAdminSettingsStore } from '@/stores'
import { useCarpoolStore } from '@/stores/carpool'
import { getSetupStatus } from '@/api/setup'
import { updateFavicon } from '@/utils/branding'

const router = useRouter()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const subscriptionStore = useSubscriptionStore()
const announcementStore = useAnnouncementStore()
const adminComplianceStore = useAdminComplianceStore()
const adminSettingsStore = useAdminSettingsStore()
const carpoolStore = useCarpoolStore()
let announcementLoginTimer: ReturnType<typeof setTimeout> | null = null

function clearAnnouncementLoginTimer() {
  if (announcementLoginTimer) clearTimeout(announcementLoginTimer)
  announcementLoginTimer = null
}

watch(
  () => authStore.user?.id ?? null,
  (userId) => {
    clearAnnouncementLoginTimer()
    carpoolStore.setIdentity(userId)
    announcementStore.setIdentity(userId)
    if (userId !== null && authStore.isAuthenticated) void refreshCarpoolAnnouncementPolling()
  },
  { immediate: true }
)

async function refreshCarpoolAnnouncementPolling() {
  const userId = authStore.user?.id ?? null
  if (!authStore.isAuthenticated || userId === null) {
    announcementStore.stopCarpoolMemberPolling()
    return
  }
  try {
    const result = await carpoolStore.fetchDetails()
    if (!authStore.isAuthenticated || authStore.user?.id !== userId) return
    if (!carpoolStore.isCurrentDetails(result)) return
    const details = result
    const isCurrentOrFutureMember = details.term?.status === 'active' || details.term?.status === 'pending'
    if (isCurrentOrFutureMember) announcementStore.startCarpoolMemberPolling()
    else announcementStore.stopCarpoolMemberPolling()
  } catch (error) {
    if (authStore.user?.id === userId && carpoolStore.isCurrentDetailsError(error)) console.error('Failed to refresh carpool announcement eligibility:', error)
  }
}

function updateDocumentTitle() {
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(route, appStore.siteName, customMenuItems)
}

// Watch for site settings changes and update favicon/title
watch(
  () => appStore.siteLogo,
  (newLogo) => {
    if (newLogo) {
      updateFavicon(newLogo)
    }
  },
  { immediate: true }
)

watch(
  [
    () => route.fullPath,
    () => route.meta.title,
    () => route.meta.titleKey,
    () => appStore.siteName,
    () => appStore.cachedPublicSettings?.custom_menu_items,
    () => authStore.isAdmin,
    () => adminSettingsStore.customMenuItems,
  ],
  updateDocumentTitle,
  { deep: true }
)

// Watch for authentication state and manage subscription data + announcements
function onVisibilityChange() {
  if (document.visibilityState === 'visible' && authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
    void announcementStore.checkForUpdates()
    void refreshCarpoolAnnouncementPolling()
  }
}

function onAdminComplianceRequired(event: Event) {
  const detail = (event as CustomEvent<Record<string, string>>).detail || {}
  adminComplianceStore.requireAcknowledgement(detail)
}

watch(
  () => authStore.isAuthenticated,
  (isAuthenticated, oldValue) => {
    if (isAuthenticated) {
      if (authStore.isAdmin) {
        adminComplianceStore.fetchStatus().catch((error) => {
          console.error('Failed to fetch admin compliance status:', error)
        })
      }

      // User logged in: preload subscriptions and start polling
      subscriptionStore.fetchActiveSubscriptions().catch((error) => {
        console.error('Failed to preload subscriptions:', error)
      })
      subscriptionStore.startPolling()
      carpoolStore.startDetailsPolling()

      void refreshCarpoolAnnouncementPolling()

      // Announcements: new login vs page refresh restore
      if (oldValue === false) {
        // New login: delay 3s then force fetch
        const loginUserId = authStore.user?.id ?? null
        announcementLoginTimer = setTimeout(() => {
          announcementLoginTimer = null
          if (authStore.isAuthenticated && loginUserId !== null && authStore.user?.id === loginUserId) {
            void announcementStore.fetchAnnouncements(true)
          }
        }, 3000)
      } else {
        // Page refresh restore (oldValue was undefined)
        announcementStore.fetchAnnouncements()
      }

      // Register visibility change listener
      document.addEventListener('visibilitychange', onVisibilityChange)
    } else {
      // User logged out: clear data and stop polling
      clearAnnouncementLoginTimer()
      subscriptionStore.clear()
      announcementStore.reset()
      carpoolStore.reset()
      adminComplianceStore.reset()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  },
  { immediate: true }
)

// Route change trigger (throttled by store)
router.afterEach(() => {
  if (authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
    void announcementStore.checkForUpdates()
    void refreshCarpoolAnnouncementPolling()
  }
})

onBeforeUnmount(() => {
  clearAnnouncementLoginTimer()
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('admin-compliance-required', onAdminComplianceRequired)
})

onMounted(async () => {
  window.addEventListener('admin-compliance-required', onAdminComplianceRequired)

  // Check if setup is needed
  try {
    const status = await getSetupStatus()
    if (status.needs_setup && route.path !== '/setup') {
      router.replace('/setup')
      return
    }
  } catch {
    // If setup endpoint fails, assume normal mode and continue
  }

  // Load public settings into appStore (will be cached for other components)
  await appStore.fetchPublicSettings()

  // Re-resolve document title now that site settings are available
  updateDocumentTitle()
})
</script>

<template>
  <NavigationProgress />
  <RouterView />
  <Toast />
  <AnnouncementPopup />
  <AdminComplianceDialog />
</template>
