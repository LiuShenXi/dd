<template>
  <AppLayout>
    <WaterTrainDecoration />
    <div class="relative z-10 mx-auto max-w-6xl space-y-5">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('carpool.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('carpool.description') }}</p>
        </div>
        <button class="btn btn-secondary inline-flex items-center gap-2" :disabled="store.detailsLoading" @click="loadDetails">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': store.detailsLoading }" />
          {{ t('carpool.refresh') }}
        </button>
      </div>

      <div v-if="store.detailsLoading && !details" class="flex min-h-56 items-center justify-center">
        <LoadingSpinner />
      </div>

      <div v-else-if="loadError && !details" class="flex min-h-56 flex-col items-center justify-center border-y border-gray-200 text-center dark:border-dark-700">
        <Icon name="exclamationCircle" size="xl" class="text-red-500" />
        <p class="mt-3 text-sm font-medium text-gray-900 dark:text-white">{{ t('carpool.loadFailed') }}</p>
        <button type="button" class="btn btn-secondary mt-4" @click="loadDetails">{{ t('carpool.retry') }}</button>
      </div>

      <template v-else-if="details">
        <section
          class="grid grid-cols-1 gap-px overflow-hidden rounded-lg border border-gray-200 bg-gray-200 dark:border-dark-700 dark:bg-dark-700"
          :class="details.quota !== null ? 'sm:grid-cols-3' : 'sm:grid-cols-2'"
        >
          <div class="bg-white p-5 dark:bg-dark-800">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('carpool.totalUsed') }}</p>
            <p class="mt-2 font-mono text-2xl font-semibold text-gray-900 dark:text-white">${{ formatCarpoolAmount(details.usage.total_used_usd) }}</p>
            <p v-if="!details.usage.history_complete" class="mt-2 text-xs text-amber-700 dark:text-amber-300">
              {{ details.usage.statistics_since ? t('carpool.historySince', { date: formatDate(details.usage.statistics_since) }) : t('carpool.historyIncomplete') }}
            </p>
          </div>
          <div class="bg-white p-5 dark:bg-dark-800">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('carpool.currentTermResets') }}</p>
            <p class="mt-2 font-mono text-2xl font-semibold text-gray-900 dark:text-white">{{ details.term?.reset_count ?? 0 }}</p>
          </div>
          <div v-if="details.quota !== null" data-testid="carpool-available-quota" class="bg-white p-5 dark:bg-dark-800">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('carpool.availableQuota') }}</p>
            <p class="mt-2 font-mono text-2xl font-semibold text-emerald-600 dark:text-emerald-400">${{ formatCarpoolAmount(details.quota.available_usd) }}</p>
            <p class="mt-2 text-xs" :class="store.detailsError ? 'text-amber-700 dark:text-amber-300' : 'text-gray-500 dark:text-gray-400'">
              {{ store.detailsError ? t('carpool.quotaStale') : t('carpool.quotaIndependent') }}
            </p>
          </div>
        </section>

        <section v-if="details.term" class="border-y border-gray-200 py-5 dark:border-dark-700">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('carpool.termTimeline') }}</h2>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('carpool.termPeriod', { start: formatDate(details.term.starts_at), end: formatDate(details.term.expires_at) }) }}
              </p>
              <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500">{{ t('carpool.timezone', { timezone: details.timezone }) }}</p>
            </div>
            <span :class="termStatusClass">{{ termStatusLabel }}</span>
          </div>

          <div class="mt-5">
            <div data-testid="carpool-cycle-quota">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('carpool.weeklyQuotaLimit') }}</p>
              <p v-if="remainingPercent !== null" class="mt-1 flex items-baseline gap-1.5 text-gray-900 dark:text-white">
                <span class="font-mono text-3xl font-semibold tabular-nums">{{ displayRemainingPercent }}%</span>
                <span class="text-sm font-medium text-gray-500 dark:text-gray-400">{{ t('carpool.quotaRemaining') }}</span>
              </p>
              <p v-else class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('carpool.quotaUnavailable') }}</p>
              <div
                v-if="remainingPercent !== null"
                data-testid="carpool-quota-progress"
                class="mt-2 h-2.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600"
                role="progressbar"
                :aria-label="t('carpool.weeklyQuotaLimit')"
                aria-valuemin="0"
                aria-valuemax="100"
                :aria-valuenow="remainingPercent"
              >
                <div
                  data-testid="carpool-quota-progress-fill"
                  class="h-full rounded-full bg-emerald-500 transition-[width] duration-300 motion-reduce:transition-none dark:bg-emerald-500"
                  :style="{ width: `${remainingPercent}%` }"
                />
              </div>
              <p v-if="details.term.current_cycle_no !== null" data-testid="carpool-current-cycle" class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                {{ t('carpool.currentCycle', { cycle: details.term.current_cycle_no }) }}
              </p>
            </div>
            <dl class="mt-4 grid gap-x-6 gap-y-3 border-y border-gray-100 py-3 text-sm sm:grid-cols-2 dark:border-dark-700">
              <div data-testid="carpool-next-natural-refill">
                <dt class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('carpool.nextNaturalRefill') }}</dt>
                <dd v-if="nextNaturalReset" class="mt-1 text-gray-900 dark:text-white">
                  <span class="font-medium">{{ formatDate(nextNaturalReset) }}</span>
                  <span v-if="naturalResetCountdown" class="mt-0.5 block font-mono text-xs tabular-nums text-primary-700 dark:text-primary-300">
                    {{ t('carpool.refillRemaining', { time: formatCountdown(naturalResetCountdown) }) }}
                  </span>
                </dd>
                <dd v-else class="mt-1 text-gray-600 dark:text-gray-300">{{ t('carpool.noNaturalRefill') }}</dd>
              </div>
              <div data-testid="carpool-membership-expiry">
                <dt class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('carpool.membershipExpiry') }}</dt>
                <dd class="mt-1 font-medium text-gray-900 dark:text-white">{{ formatDate(details.term.expires_at) }}</dd>
              </div>
            </dl>
          </div>

          <div class="mt-6">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('carpool.cycleDetails') }}</h3>
            <div class="mt-3 divide-y divide-gray-100 border-y border-gray-100 dark:divide-dark-700 dark:border-dark-700">
              <div v-for="cycle in details.term.cycles" :key="cycle.cycle_no" class="grid gap-1 py-3 text-sm sm:grid-cols-[7rem_1fr_auto] sm:items-center">
                <span class="font-medium text-gray-900 dark:text-white">{{ t('carpool.cycle', { cycle: cycle.cycle_no }) }}</span>
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ formatDate(cycle.starts_at) }} - {{ formatDate(cycle.ends_at) }}</span>
                <span class="text-xs font-medium" :class="cycleStatusTextClass(cycle)">{{ cycleStatusLabel(cycle) }}</span>
              </div>
            </div>
          </div>
        </section>

        <section v-else class="border-y border-gray-200 py-10 text-center dark:border-dark-700">
          <Icon name="clock" size="xl" class="mx-auto text-gray-300 dark:text-dark-500" />
          <h2 class="mt-3 text-base font-semibold text-gray-900 dark:text-white">{{ t('carpool.noTerm') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('carpool.noTermDescription') }}</p>
        </section>

        <section class="border-b border-gray-200 pb-5 dark:border-dark-700">
          <div class="flex items-center gap-2">
            <Icon name="clock" size="sm" class="text-gray-500" />
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('carpool.resetWindow') }}</h2>
          </div>
          <div class="mt-3 rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800">
            <template v-if="details.reset_window.status === 'none' || !details.reset_window.scheduled_at">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('carpool.noResetWindow') }}</p>
            </template>
            <template v-else>
              <div class="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ resetStatusLabel }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                    {{ t('carpool.plannedAt', { date: formatBeijingDate(details.reset_window.scheduled_at) }) }}
                  </p>
                  <p v-if="!details.reset_window.eligible_for_me" class="mt-2 text-xs text-amber-700 dark:text-amber-300">
                    {{ resetIneligibleReason }}
                  </p>
                </div>
                <p v-if="resetCountdown && details.reset_window.status === 'scheduled'" class="font-mono text-lg font-semibold tabular-nums text-primary-700 dark:text-primary-300">
                  {{ formatCountdown(resetCountdown) }}
                </p>
              </div>
            </template>
          </div>
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import WaterTrainDecoration from '@/components/common/WaterTrainDecoration.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useCarpoolStore } from '@/stores/carpool'
import { useServerClock } from '@/composables/useServerClock'
import { countdownParts, formatCarpoolAmount, nextNaturalResetAt, type CountdownParts } from '@/utils/carpool'
import type { CarpoolUserCycle } from '@/types/carpool'

const { t, locale } = useI18n()
const appStore = useAppStore()
const store = useCarpoolStore()
const { nowMs, sync } = useServerClock()
const details = computed(() => store.details)
let boundaryRefreshKey: string | null = null
const loadError = ref(false)

const resetCountdown = computed(() => countdownParts(details.value?.reset_window.scheduled_at ?? null, nowMs.value))
const remainingPercent = computed(() => {
  if (details.value?.term?.status !== 'active' || details.value.term.current_cycle_no === null) return null
  const raw = details.value?.quota?.remaining_percent
  if (raw === null || raw === undefined || raw.trim() === '') return null
  const parsed = Number(raw)
  return Number.isFinite(parsed) ? Math.min(100, Math.max(0, parsed)) : null
})
const displayRemainingPercent = computed(() => remainingPercent.value === null
  ? ''
  : new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(remainingPercent.value))
const nextNaturalReset = computed(() => details.value?.term ? nextNaturalResetAt(details.value.term) : null)
const naturalResetCountdown = computed(() => {
  const target = nextNaturalReset.value
  return target && Date.parse(target) > nowMs.value ? countdownParts(target, nowMs.value) : null
})
const resetIneligibleReason = computed(() => {
  const reason = details.value?.reset_window.ineligible_reason
  const keys: Record<string, string> = {
    no_term: 'noTerm',
    term_not_started: 'termPending',
    term_expired: 'termExpired',
    term_terminated: 'termTerminated',
    no_active_cycle: 'noActiveCycle',
  }
  return t(`carpool.resetReasons.${keys[reason ?? ''] ?? 'notEligible'}`)
})

const termStatusLabel = computed(() => {
  const status = details.value?.term?.status
  if (status === 'pending') return t('carpool.termScheduled')
  if (status === 'expired') return t('carpool.termExpired')
  if (status === 'terminated') return t('carpool.termTerminated')
  return t('carpool.inProgress')
})

const termStatusClass = computed(() => [
  'rounded-full px-2.5 py-1 text-xs font-medium',
  details.value?.term?.status === 'active'
    ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
    : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300',
])

const resetStatusLabel = computed(() => {
  if (details.value?.reset_window.status === 'executing' || isResetDue.value) return t('carpool.resetExecuting')
  if (details.value?.reset_window.status === 'delayed') return t('carpool.resetDelayed')
  if (details.value?.reset_window.status === 'completed') return t('carpool.resetCompleted')
  return t('carpool.resetScheduled')
})

const isResetDue = computed(() => {
  const scheduledAt = details.value?.reset_window.scheduled_at
  return !!scheduledAt && details.value?.reset_window.status === 'scheduled' && Date.parse(scheduledAt) <= nowMs.value
})

function cycleStatus(cycle: CarpoolUserCycle): 'completed' | 'active' | 'upcoming' {
  const start = Date.parse(cycle.starts_at)
  const end = Date.parse(cycle.ends_at)
  if (nowMs.value >= end || ['closed', 'missed', 'closing'].includes(cycle.status)) return 'completed'
  if (nowMs.value >= start && nowMs.value < end && cycle.status !== 'scheduled') return 'active'
  return 'upcoming'
}

function cycleStatusLabel(cycle: CarpoolUserCycle): string {
  const status = cycleStatus(cycle)
  return status === 'completed' ? t('carpool.completed') : status === 'active' ? t('carpool.inProgress') : t('carpool.upcoming')
}

function cycleStatusTextClass(cycle: CarpoolUserCycle): string {
  const status = cycleStatus(cycle)
  if (status === 'completed') return 'text-emerald-600 dark:text-emerald-400'
  if (status === 'active') return 'text-primary-600 dark:text-primary-400'
  return 'text-gray-500 dark:text-gray-400'
}

function formatCountdown(parts: CountdownParts): string {
  const values = { days: parts.days, hours: parts.hours, minutes: parts.minutes, seconds: parts.seconds }
  if (parts.days > 0) return t('carpool.duration', values)
  return t('carpool.durationShort', values)
}

function formatDate(value: string): string {
  const timezone = details.value?.timezone || 'Asia/Shanghai'
  return new Intl.DateTimeFormat(undefined, { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(value))
}

function formatBeijingDate(value: string): string {
  return formatShanghaiDate(value)
}

function formatShanghaiDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value === 'zh' ? 'zh-CN' : 'en-US', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value))
}

async function loadDetails() {
  try {
    const result = await store.fetchDetails()
    if (!store.isCurrentDetails(result)) return
    loadError.value = false
    sync(result.server_now)
    if (boundaryRefreshKey && !refreshBoundaryKeys(result).includes(boundaryRefreshKey)) boundaryRefreshKey = null
  } catch (error) {
    if (!store.isCurrentDetailsError(error)) return
    loadError.value = true
    console.error('Failed to load carpool details:', error)
    appStore.showError(t('carpool.loadFailed'))
  }
}

function refreshBoundaryKeys(result: NonNullable<typeof details.value>): string[] {
  const term = result.term
  if (!term) return []
  const keys: string[] = []
  const natural = nextNaturalResetAt(term)
  const cycleEnd = term.cycles.find((cycle) => cycle.cycle_no === term.current_cycle_no)?.ends_at
  const resetAt = result.reset_window.status === 'scheduled' ? result.reset_window.scheduled_at : null
  if (natural) keys.push(`natural:${natural}`)
  if (cycleEnd) keys.push(`cycle:${cycleEnd}`)
  if (resetAt) keys.push(`reset:${resetAt}`)
  if (term.status === 'pending') keys.push(`term-start:${term.starts_at}`)
  if (term.status === 'active' || term.status === 'pending') keys.push(`term:${term.expires_at}`)
  return keys
}

watch(nowMs, () => {
  const dueKey = details.value
    ? refreshBoundaryKeys(details.value).find((key) => Date.parse(key.slice(key.indexOf(':') + 1)) <= nowMs.value) ?? null
    : null
  if (dueKey && boundaryRefreshKey !== dueKey && !store.detailsLoading) {
    boundaryRefreshKey = dueKey
    void loadDetails()
  }
})

function onVisibilityChange() {
  if (document.visibilityState === 'visible') void loadDetails()
}

onMounted(() => {
  void loadDetails()
  document.addEventListener('visibilitychange', onVisibilityChange)
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>
