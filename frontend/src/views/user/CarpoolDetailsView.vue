<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-5">
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
            <div ref="timeline" class="carpool-timeline">
              <div v-if="hasDenseResetHistory" class="mb-3">
                <button
                  type="button"
                  data-testid="carpool-reset-history-toggle"
                  class="inline-flex items-center gap-1.5 text-xs font-medium text-gray-600 hover:text-gray-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:text-gray-300 dark:hover:text-white dark:focus-visible:ring-offset-dark-950"
                  :aria-expanded="resetHistoryExpanded"
                  aria-controls="carpool-reset-history"
                  @click="resetHistoryExpanded = !resetHistoryExpanded"
                >
                  <Icon name="chevronDown" size="xs" class="transition-transform motion-reduce:transition-none" :class="{ 'rotate-180': resetHistoryExpanded }" />
                  {{ t('carpool.currentTermResets') }} · {{ resetAnnotations.length }}
                </button>
                <div
                  v-show="resetHistoryExpanded"
                  id="carpool-reset-history"
                  data-testid="carpool-reset-history"
                  class="mt-2 max-h-48 overflow-y-auto border-y border-gray-200 py-1 pr-2 dark:border-dark-700"
                >
                  <ol :aria-label="t('carpool.currentTermResets')" class="divide-y divide-gray-100 dark:divide-dark-800">
                    <li
                      v-for="annotation in resetAnnotations"
                      :key="`history:${annotation.event.cycle_no}:${annotation.event.occurred_at}`"
                      data-testid="carpool-reset-history-item"
                      class="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 py-2 text-xs"
                    >
                      <time class="font-mono tabular-nums text-gray-500 dark:text-gray-400" :datetime="annotation.event.occurred_at">
                        {{ formatShanghaiDate(annotation.event.occurred_at) }}
                      </time>
                      <span class="font-semibold text-emerald-700 dark:text-emerald-300">
                        {{ t('carpool.resetFilledTo', { amount: formatCarpoolTargetAmount(annotation.event.target_quota_usd) }) }}
                      </span>
                    </li>
                  </ol>
                </div>
              </div>
              <div
                v-if="positionedResetAnnotations.length"
                data-testid="carpool-reset-targets"
                class="carpool-timeline__annotations"
                :style="{ height: `${annotationRegionHeight}px` }"
              >
                <svg
                  class="carpool-timeline__leaders"
                  :viewBox="`0 0 ${timelineWidth} ${annotationRegionHeight}`"
                  preserveAspectRatio="none"
                  aria-hidden="true"
                >
                  <defs>
                    <mask id="carpool-timeline-label-mask" maskUnits="userSpaceOnUse">
                      <rect width="100%" height="100%" fill="white" />
                      <rect
                        v-for="annotation in positionedResetAnnotations"
                        :key="`mask:${annotation.event.cycle_no}:${annotation.event.occurred_at}`"
                        :x="annotation.labelLeft"
                        :y="annotation.labelTop"
                        :width="annotation.labelWidth"
                        :height="annotation.labelHeight"
                        fill="black"
                      />
                    </mask>
                  </defs>
                  <g mask="url(#carpool-timeline-label-mask)">
                    <path
                      v-for="annotation in positionedResetAnnotations"
                      :key="`leader:${annotation.event.cycle_no}:${annotation.event.occurred_at}`"
                      :d="annotation.leaderPath"
                    />
                  </g>
                </svg>
                <ol data-testid="carpool-reset-times">
                  <li
                    v-for="annotation in positionedResetAnnotations"
                    :key="`annotation:${annotation.event.cycle_no}:${annotation.event.occurred_at}`"
                    class="carpool-timeline__annotation"
                    :data-annotation-lane="annotation.lane"
                    :style="annotation.labelStyle"
                  >
                    <span class="carpool-timeline__annotation-dot" aria-hidden="true" />
                    <span class="min-w-0">
                      <time class="block font-mono text-xs leading-4 tabular-nums text-gray-500 dark:text-gray-400" :datetime="annotation.event.occurred_at">
                        {{ formatShanghaiDate(annotation.event.occurred_at) }}
                      </time>
                      <span class="block break-words text-[13px] font-semibold leading-4 text-emerald-700 dark:text-emerald-300">
                        {{ t('carpool.resetFilledTo', { amount: formatCarpoolTargetAmount(annotation.event.target_quota_usd) }) }}
                      </span>
                    </span>
                  </li>
                </ol>
              </div>

              <div class="relative">
                <div class="grid h-3 gap-px overflow-hidden rounded-full bg-gray-200 p-px dark:bg-dark-600" :style="{ gridTemplateColumns: timelineColumns }" data-testid="carpool-time-rail">
                  <div
                    v-for="cycle in details.term.cycles"
                    :key="cycle.cycle_no"
                    class="relative min-w-0 overflow-hidden first:rounded-l-full last:rounded-r-full bg-gray-50 dark:bg-dark-800"
                    :aria-label="`${t('carpool.cycle', { cycle: cycle.cycle_no })}: ${cycleStatusLabel(cycle)}`"
                  >
                    <div
                      class="absolute inset-y-0 left-0 transition-[width] duration-300 motion-reduce:transition-none"
                      :class="cycleFillClass(cycle)"
                      :style="{ width: `${cycleProgress(cycle, nowMs) * 100}%` }"
                    />
                  </div>
                </div>
                <div v-if="resetAnnotations.length" class="pointer-events-none absolute inset-x-0 top-0 h-3" aria-hidden="true">
                  <span
                    v-for="annotation in resetAnnotations"
                    :key="`marker:${annotation.event.cycle_no}:${annotation.event.occurred_at}`"
                    data-testid="carpool-reset-marker"
                    class="carpool-timeline__node"
                    :style="{ left: `${annotation.position}%` }"
                  >
                    <span />
                  </span>
                </div>
              </div>
              <div data-testid="carpool-cycle-labels" class="mt-2 grid gap-px" :style="{ gridTemplateColumns: timelineColumns }" aria-hidden="true">
                <span v-for="cycle in details.term.cycles" :key="`label:${cycle.cycle_no}`" class="text-center font-mono text-[10px] leading-3 tabular-nums text-gray-400 dark:text-gray-500">
                  {{ cycle.cycle_no }}
                </span>
              </div>
            </div>
            <p v-if="currentCycle && currentCycleRemaining" class="mt-3 text-sm font-medium text-primary-700 dark:text-primary-300">
              {{ t('carpool.cycleRemaining', { cycle: currentCycle.cycle_no, time: formatCountdown(currentCycleRemaining) }) }}
            </p>
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
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useCarpoolStore } from '@/stores/carpool'
import { useServerClock } from '@/composables/useServerClock'
import { countdownParts, cycleGridTemplate, cycleProgress, formatCarpoolAmount, formatCarpoolTargetAmount, termEventPosition, type CountdownParts } from '@/utils/carpool'
import type { CarpoolResetEvent, CarpoolUserCycle } from '@/types/carpool'

const { t, locale } = useI18n()
const appStore = useAppStore()
const store = useCarpoolStore()
const { nowMs, sync } = useServerClock()
const details = computed(() => store.details)
let boundaryRefreshKey: string | null = null
const loadError = ref(false)
const timeline = ref<HTMLElement | null>(null)
const measuredTimelineWidth = ref(0)
const resetHistoryExpanded = ref(false)
let timelineResizeObserver: ResizeObserver | null = null

const currentCycle = computed(() => details.value?.term?.cycles.find((cycle) => cycle.cycle_no === details.value?.term?.current_cycle_no) ?? null)
const currentCycleRemaining = computed(() => countdownParts(currentCycle.value?.ends_at ?? null, nowMs.value))
const resetCountdown = computed(() => countdownParts(details.value?.reset_window.scheduled_at ?? null, nowMs.value))
const timelineColumns = computed(() => cycleGridTemplate(details.value?.term?.cycles ?? []) || '1fr')
const resetAnnotations = computed(() => {
  const term = details.value?.term
  if (!term) return []
  return term.reset_events
    .map((event) => ({ event, position: termEventPosition(event.occurred_at, term.starts_at, term.expires_at) }))
    .filter((annotation): annotation is { event: CarpoolResetEvent; position: number } => annotation.position !== null)
    .sort((a, b) => Date.parse(a.event.occurred_at) - Date.parse(b.event.occurred_at))
})
const timelineWidth = computed(() => measuredTimelineWidth.value || 360)
type ResetAnnotation = (typeof resetAnnotations.value)[number]

function layoutResetAnnotations(annotations: ResetAnnotation[], width: number) {
  const labelWidth = Math.min(width, width < 520 ? 168 : 188)
  const labelHeight = 64
  const laneGap = 8
  const horizontalGap = 10
  const rowCapacity = Math.max(1, Math.floor((width + horizontalGap) / (labelWidth + horizontalGap)))

  return annotations.flatMap((_, rowStart) => {
    if (rowStart % rowCapacity !== 0) return []
    const row = annotations.slice(rowStart, rowStart + rowCapacity)
    const lane = rowStart / rowCapacity
    const positions: number[] = []

    row.forEach((annotation, index) => {
      const nodeX = (annotation.position / 100) * width
      const desiredLeft = Math.min(Math.max(nodeX - labelWidth / 2, 0), Math.max(width - labelWidth, 0))
      positions[index] = index === 0 ? desiredLeft : Math.max(desiredLeft, positions[index - 1] + labelWidth + horizontalGap)
    })

    const lastIndex = positions.length - 1
    positions[lastIndex] = Math.min(positions[lastIndex], width - labelWidth)
    for (let index = lastIndex - 1; index >= 0; index -= 1) {
      positions[index] = Math.min(positions[index], positions[index + 1] - labelWidth - horizontalGap)
    }

    return row.map((annotation, index) => {
      const nodeX = (annotation.position / 100) * width
      const left = positions[index]
      const top = lane * (labelHeight + laneGap)
      const labelAnchorX = Math.min(Math.max(nodeX, left + 10), left + labelWidth - 10)
      const elbowY = top + labelHeight + 5

      return {
        ...annotation,
        lane,
        labelStyle: { left: `${left}px`, top: `${top}px`, width: `${labelWidth}px`, height: `${labelHeight}px` },
        labelLeft: left,
        labelTop: top,
        labelWidth,
        labelHeight,
        labelAnchorX,
        labelBottomY: top + labelHeight,
        elbowY,
        nodeX,
      }
    })
  })
}

const allResetAnnotationLayout = computed(() => layoutResetAnnotations(resetAnnotations.value, timelineWidth.value))
const hasDenseResetHistory = computed(() => allResetAnnotationLayout.value.some((annotation) => annotation.lane > 1))
const visibleResetAnnotations = computed(() => hasDenseResetHistory.value ? resetAnnotations.value.slice(-2) : resetAnnotations.value)
const resetAnnotationLayout = computed(() => {
  return layoutResetAnnotations(visibleResetAnnotations.value, timelineWidth.value)
})
const annotationRegionHeight = computed(() => {
  const laneCount = resetAnnotationLayout.value.reduce((count, annotation) => Math.max(count, annotation.lane + 1), 0)
  return laneCount ? laneCount * 72 + 18 : 0
})
const positionedResetAnnotations = computed(() => resetAnnotationLayout.value.map((annotation) => ({
  ...annotation,
  leaderPath: `M ${annotation.labelAnchorX} ${annotation.labelBottomY} V ${annotation.elbowY} H ${annotation.nodeX} V ${annotationRegionHeight.value}`,
})))
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

function cycleFillClass(cycle: CarpoolUserCycle): string {
  const status = cycleStatus(cycle)
  if (status === 'completed') return 'bg-emerald-500 dark:bg-emerald-600'
  if (status === 'active') return 'bg-primary-500 dark:bg-primary-600'
  return 'bg-transparent'
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
    const activeBoundary = result.term?.cycles.find((cycle) => cycle.cycle_no === result.term?.current_cycle_no)?.ends_at
    if (boundaryRefreshKey?.startsWith('cycle:') && !boundaryRefreshKey.endsWith(activeBoundary ?? '')) boundaryRefreshKey = null
    const scheduledBoundary = result.reset_window.status === 'scheduled' ? result.reset_window.scheduled_at : null
    if (boundaryRefreshKey?.startsWith('reset:') && !boundaryRefreshKey.endsWith(scheduledBoundary ?? '')) boundaryRefreshKey = null
  } catch (error) {
    if (!store.isCurrentDetailsError(error)) return
    loadError.value = true
    console.error('Failed to load carpool details:', error)
    appStore.showError(t('carpool.loadFailed'))
  }
}

watch(nowMs, () => {
  const boundary = currentCycle.value?.ends_at
  const resetAt = details.value?.reset_window.status === 'scheduled' ? details.value.reset_window.scheduled_at : null
  const dueKey = boundary && Date.parse(boundary) <= nowMs.value ? `cycle:${boundary}` : resetAt && Date.parse(resetAt) <= nowMs.value ? `reset:${resetAt}` : null
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

watch(timeline, (element) => {
  timelineResizeObserver?.disconnect()
  timelineResizeObserver = null
  if (!element) return

  measuredTimelineWidth.value = element.clientWidth
  if (typeof ResizeObserver === 'undefined') return
  timelineResizeObserver = new ResizeObserver(([entry]) => {
    measuredTimelineWidth.value = entry?.contentRect.width || element.clientWidth
  })
  timelineResizeObserver.observe(element)
})

watch(() => details.value?.term?.starts_at, () => {
  resetHistoryExpanded.value = false
})

onBeforeUnmount(() => {
  timelineResizeObserver?.disconnect()
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<style scoped>
.carpool-timeline__annotations {
  position: relative;
  width: 100%;
}

.carpool-timeline__leaders {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  overflow: visible;
  pointer-events: none;
}

.carpool-timeline__leaders path {
  fill: none;
  stroke: rgb(148 163 184 / 55%);
  stroke-width: 1;
  stroke-linecap: round;
  stroke-linejoin: round;
  vector-effect: non-scaling-stroke;
}

.carpool-timeline__annotation {
  position: absolute;
  display: flex;
  min-width: 0;
  align-items: flex-end;
  gap: 0.5rem;
}

.carpool-timeline__annotation-dot {
  width: 0.375rem;
  height: 0.375rem;
  margin-bottom: 0.3125rem;
  flex: none;
  border-radius: 9999px;
  background: #0d9488;
  box-shadow: 0 0 0 2px rgb(13 148 136 / 12%);
}

.carpool-timeline__node {
  position: absolute;
  top: 50%;
  display: flex;
  width: 0.75rem;
  height: 0.75rem;
  transform: translate(-50%, -50%);
  align-items: center;
  justify-content: center;
  border: 2px solid #0f766e;
  border-radius: 9999px;
  background: #f8fafc;
  box-shadow: 0 0 0 2px rgb(248 250 252 / 85%);
}

.carpool-timeline__node > span {
  width: 0.1875rem;
  height: 0.1875rem;
  border-radius: 9999px;
  background: #0f766e;
}

:global(.dark) .carpool-timeline__leaders path {
  stroke: rgb(100 116 139 / 70%);
}

:global(.dark) .carpool-timeline__node {
  background: #020617;
  box-shadow: 0 0 0 2px rgb(2 6 23 / 85%);
}
</style>
