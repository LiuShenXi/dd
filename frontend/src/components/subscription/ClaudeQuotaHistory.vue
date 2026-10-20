<template>
  <section class="mt-4 space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
    <button type="button" class="btn btn-secondary" :aria-expanded="open" @click="toggle">{{ t('subscriptionQuota.history') }}</button>
    <template v-if="open"><p v-if="error" role="alert" class="text-xs text-red-600">{{ error }}</p><p v-if="loading" class="text-xs text-gray-500">{{ t('subscriptionQuota.loading') }}</p><div v-for="term in terms" :key="term.id" class="flex flex-wrap items-center justify-between gap-2 text-xs text-gray-700 dark:text-gray-300"><span>#{{ term.term_no }} · {{ statusLabel(term.status) }} · {{ date(term.starts_at) }} — {{ date(term.expires_at) }} · {{ formatQuotaUSD(term.quota_usd) }}</span><button type="button" class="btn btn-secondary" @click="selectTerm(term.id)">{{ t('subscriptionQuota.usage') }}</button></div><p v-if="!loading && !terms.length" class="text-xs text-gray-500">{{ t('subscriptionQuota.emptyHistory') }}</p><button v-if="termCursor" type="button" class="btn btn-secondary" :disabled="loading" @click="loadTerms(true)">{{ t('subscriptionQuota.nextPage') }}</button>
      <section v-if="selectedTerm" class="space-y-2 rounded-xl bg-gray-50 p-3 dark:bg-dark-900"><h4 class="text-sm font-medium">{{ t('subscriptionQuota.usage') }} · #{{ selectedTerm }}</h4><p v-if="usageError" role="alert" class="text-xs text-red-600">{{ usageError }}</p><p v-if="usageLoading" class="text-xs text-gray-500">{{ t('subscriptionQuota.loading') }}</p><div class="overflow-x-auto"><table class="w-full text-left text-xs"><thead><tr><th class="p-2">{{ t('subscriptionQuota.request') }}</th><th class="p-2">{{ t('subscriptionQuota.key') }}</th><th class="p-2">{{ t('subscriptionQuota.model') }}</th><th class="p-2">{{ t('subscriptionQuota.cost') }}</th><th class="p-2">{{ t('subscriptionQuota.tokens') }}</th></tr></thead><tbody><tr v-for="row in usage" :key="row.request_id"><td class="max-w-48 break-all p-2 font-mono">{{ row.request_id }}</td><td class="p-2">#{{ row.api_key_id }}</td><td class="p-2">{{ row.model }}</td><td class="p-2 font-mono">{{ formatQuotaUSD(row.actual_cost_usd) }}</td><td class="p-2">{{ row.input_tokens }} / {{ row.output_tokens }}</td></tr></tbody></table></div><p v-if="!usageLoading && !usage.length" class="text-xs text-gray-500">{{ t('subscriptionQuota.emptyUsage') }}</p><button v-if="usageCursor" type="button" class="btn btn-secondary" :disabled="usageLoading" @click="loadUsage(true)">{{ t('subscriptionQuota.nextPage') }}</button></section>
    </template>
  </section>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { subscriptionQuotaAPI } from '@/api/subscriptionQuota'
import { useQuotaRequestIdentity } from '@/composables/useQuotaRequestIdentity'
import { formatQuotaUSD, quotaError, quotaStatusLabel } from '@/utils/claudeQuota'
import { formatDateTimeToMinute as date } from '@/utils/format'
import type { SubscriptionQuotaTerm, SubscriptionQuotaUsage } from '@/types/subscriptionQuota'
const props = defineProps<{ groupId: number }>()
const { t } = useI18n(), auth = useAuthStore()
const open = ref(false), selectedTerm = ref<number | null>(null), terms = ref<SubscriptionQuotaTerm[]>([]), usage = ref<SubscriptionQuotaUsage[]>([])
const loading = ref(false), usageLoading = ref(false), error = ref(''), usageError = ref('')
const termCursor = ref<string | null>(null), usageCursor = ref<string | null>(null)
const identity = useQuotaRequestIdentity(() => JSON.stringify([auth.user?.id, props.groupId, open.value]))
const usageIdentity = useQuotaRequestIdentity(() => JSON.stringify([auth.user?.id, props.groupId, open.value, selectedTerm.value]))
function statusLabel(value: string) { return quotaStatusLabel(value, t) }
function toggle() { open.value = !open.value; if (open.value) void loadTerms() }
function selectTerm(id: number) { selectedTerm.value = id; usage.value = []; usageCursor.value = null; void loadUsage() }
async function loadTerms(more = false) {
  const current = identity.capture(); loading.value = true; error.value = ''
  try { const page = await subscriptionQuotaAPI.terms(props.groupId, more ? termCursor.value ?? undefined : undefined); if (current()) { terms.value = more ? [...terms.value, ...page.items] : page.items; termCursor.value = page.next_cursor } }
  catch (err) { if (current()) error.value = quotaError(err) }
  finally { if (current()) loading.value = false }
}
async function loadUsage(more = false) {
  if (!selectedTerm.value) return
  const current = usageIdentity.capture(); usageLoading.value = true; usageError.value = ''
  try { const page = await subscriptionQuotaAPI.usage(selectedTerm.value, more ? usageCursor.value ?? undefined : undefined); if (current()) { usage.value = more ? [...usage.value, ...page.items] : page.items; usageCursor.value = page.next_cursor } }
  catch (err) { if (current()) usageError.value = quotaError(err) }
  finally { if (current()) usageLoading.value = false }
}
watch(() => JSON.stringify([auth.user?.id, props.groupId]), () => { identity.invalidate(); usageIdentity.invalidate(); open.value = false; terms.value = []; usage.value = []; selectedTerm.value = null; loading.value = false; usageLoading.value = false; error.value = ''; usageError.value = '' }, { flush: 'sync' })
</script>
