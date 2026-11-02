<template>
  <section class="min-w-0 rounded-2xl border border-orange-200 bg-white p-4 dark:border-orange-900 dark:bg-dark-800" data-testid="claude-quota-card">
    <div class="flex flex-wrap items-start justify-between gap-2">
      <div><h3 class="font-semibold text-gray-900 dark:text-white">{{ title || t('subscriptionQuota.title') }}</h3><p class="mt-1 text-xs text-gray-500">{{ t('subscriptionQuota.shared') }}</p></div>
      <span v-if="term" class="rounded-lg bg-orange-50 px-2 py-1 text-xs text-orange-800 dark:bg-orange-900/30 dark:text-orange-200">{{ statusLabel(term.status) }}</span>
    </div>
    <p v-if="!summary" role="status" class="mt-4 text-sm text-amber-700 dark:text-amber-300">{{ t('subscriptionQuota.unavailable') }}</p>
    <template v-else>
      <p v-if="!summary.current_term" class="mt-4 text-sm text-gray-500">{{ t('subscriptionQuota.noCurrent') }}</p>
      <template v-if="term">
        <p v-if="!summary.current_term" class="mt-3 text-xs font-medium text-gray-500">{{ t('subscriptionQuota.last') }}</p>
        <dl class="mt-4 grid grid-cols-2 gap-4" :class="compact ? '' : 'sm:grid-cols-4'">
          <div v-for="item in amounts" :key="item.key" class="min-w-0"><dt class="text-xs text-gray-500">{{ t(`subscriptionQuota.${item.key}`) }}</dt><dd class="mt-1 break-words font-mono text-sm font-semibold text-gray-900 dark:text-gray-100" :data-testid="`quota-${item.key}`">{{ formatQuotaUSD(item.value) }}</dd></div>
        </dl>
        <div v-if="progress !== null" class="mt-4 h-2 overflow-hidden rounded-full bg-orange-50 dark:bg-dark-700" role="progressbar" :aria-label="t('subscriptionQuota.used')" :aria-valuenow="progress" aria-valuemin="0" aria-valuemax="100"><div class="h-full rounded-full bg-orange-500" :style="{ width: `${progress}%` }" /></div>
        <p v-if="term.unsettled_request_count > 0" class="mt-3 text-xs text-amber-700 dark:text-amber-300">{{ t('subscriptionQuota.unsettled', { count: term.unsettled_request_count }) }}</p>
        <dl class="mt-4 grid gap-2 text-xs sm:grid-cols-2"><div><dt class="text-gray-500">{{ t('subscriptionQuota.starts') }}</dt><dd class="mt-1 text-gray-700 dark:text-gray-300">{{ date(term.starts_at) }}</dd></div><div><dt class="text-gray-500">{{ t('subscriptionQuota.expires') }}</dt><dd class="mt-1 text-gray-700 dark:text-gray-300">{{ date(term.expires_at) }}</dd></div></dl>
        <p v-if="summary.current_term" class="mt-2 text-xs text-gray-500" data-testid="quota-countdown">{{ countdown }}</p>
      </template>
      <div v-if="summary.next_terms.length" class="mt-4 border-t border-gray-100 pt-3 dark:border-dark-700"><h4 class="text-xs font-medium text-gray-500">{{ t('subscriptionQuota.next') }}</h4><div v-for="next in summary.next_terms" :key="next.id" class="mt-2 flex flex-wrap justify-between gap-1 text-xs text-gray-700 dark:text-gray-300"><span>#{{ next.term_no }} · {{ statusLabel(next.status) }} · {{ date(next.starts_at) }}</span><span class="font-mono">{{ formatQuotaUSD(next.quota_usd) }}</span></div></div>
    </template>
    <slot />
  </section>
</template>
<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useServerClock } from '@/composables/useServerClock'
import { formatQuotaUSD, quotaProgress, quotaStatusLabel } from '@/utils/claudeQuota'
import { formatDateTimeToMinute } from '@/utils/format'
import type { SubscriptionQuotaSummary } from '@/types/subscriptionQuota'
const props = defineProps<{ summary: SubscriptionQuotaSummary | null; title?: string; compact?: boolean }>()
const emit = defineEmits<{ (e: 'refresh'): void }>()
const { t } = useI18n()
const clock = useServerClock()
const term = computed(() => props.summary?.current_term ?? props.summary?.last_term ?? null)
const amounts = computed(() => term.value ? [{ key: 'quota', value: term.value.quota_usd }, { key: 'used', value: term.value.used_usd }, { key: 'pending', value: term.value.pending_usd }, { key: 'remaining', value: term.value.remaining_usd }] : [])
const progress = computed(() => term.value ? quotaProgress(term.value.quota_usd, term.value.used_usd, term.value.pending_usd) : null)
const countdown = computed(() => {
  const end = Date.parse(props.summary?.current_term?.expires_at ?? '')
  const seconds = Math.ceil((end - clock.nowMs.value) / 1000)
  if (!Number.isFinite(seconds) || seconds <= 0) return t('subscriptionQuota.awaitingRefresh')
  return `${t('subscriptionQuota.countdown')}: ${Math.floor(seconds / 86400)}d ${Math.floor(seconds % 86400 / 3600)}h ${Math.floor(seconds % 3600 / 60)}m`
})
function statusLabel(value: string) { return quotaStatusLabel(value, t) }
function date(value: string) { return formatDateTimeToMinute(value) }
let boundarySent = ''
watch(() => props.summary, (summary) => { if (summary) { clock.sync(summary.server_time); boundarySent = '' } else clock.stop() }, { immediate: true })
watch(() => clock.nowMs.value, (now) => {
  const summary = props.summary
  if (!summary) return
  const boundaries = [summary.current_term?.expires_at, ...summary.next_terms.map(next => next.starts_at)].filter((value): value is string => !!value)
  const passed = boundaries.filter(value => Date.parse(value) <= now).join('|')
  if (passed && passed !== boundarySent) { boundarySent = passed; emit('refresh') }
})
</script>
