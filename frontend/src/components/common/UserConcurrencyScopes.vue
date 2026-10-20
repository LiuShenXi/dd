<template>
  <section class="grid min-w-0 gap-3 text-xs sm:grid-cols-2" data-testid="user-concurrency-scopes">
    <div class="min-w-0 rounded-lg bg-gray-50 p-3 dark:bg-dark-900"><p class="font-medium text-gray-700 dark:text-gray-200">{{ t('concurrencyScopes.legacy') }}</p><p class="mt-1 font-mono" data-testid="legacy-scope-stats">{{ legacyCurrent }} / {{ source.concurrency === 0 ? '∞' : count(source.concurrency) }}</p></div>
    <div class="min-w-0 rounded-lg bg-orange-50 p-3 dark:bg-orange-900/20"><p class="font-medium text-gray-700 dark:text-gray-200">{{ t('concurrencyScopes.claude') }}</p><p class="mt-1 font-mono" data-testid="claude-scope-stats">{{ available ? count(source.claude_current_concurrency) : t('concurrencyScopes.unavailable') }} / {{ positive(source.effective_claude_subscription_concurrency) }}</p><p class="mt-1 text-gray-500">{{ t('concurrencyScopes.waiting') }}: {{ available ? count(source.claude_waiting_count) : t('concurrencyScopes.unavailable') }}</p><p class="mt-1 text-gray-500" data-testid="claude-scope-policy">{{ t('concurrencyScopes.default') }}: {{ positive(source.default_claude_subscription_concurrency) }} · {{ t('concurrencyScopes.override') }}: {{ source.claude_subscription_concurrency == null ? t('concurrencyScopes.inherit') : positive(source.claude_subscription_concurrency) }}</p><p v-if="source.claude_concurrency_status === 'not_configured'" class="mt-1 text-amber-700 dark:text-amber-300">{{ t('concurrencyScopes.notConfigured') }}</p></div>
    <p v-if="!compact" class="text-gray-500 sm:col-span-2">{{ t('concurrencyScopes.shared') }}</p>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserConcurrencyProjection } from '@/types/userConcurrency'
const props = defineProps<{ source: UserConcurrencyProjection; compact?: boolean }>()
const { t } = useI18n()
const available = computed(() => props.source.claude_concurrency_status === 'available')
const legacyCurrent = computed(() => {
  const source = props.source
  if (source.legacy_concurrency_status !== undefined || source.legacy_current_concurrency !== undefined) {
    return source.legacy_concurrency_status === 'available' ? count(source.legacy_current_concurrency) : t('concurrencyScopes.unavailable')
  }
  return count(source.current_concurrency)
})
function count(value: number | null | undefined) { return value != null && Number.isSafeInteger(value) && value >= 0 ? String(value) : t('concurrencyScopes.unavailable') }
function positive(value: number | null | undefined) { return value != null && value > 0 ? count(value) : t('concurrencyScopes.unavailable') }
</script>
