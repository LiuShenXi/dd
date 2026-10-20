<template>
  <section class="mt-4 space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
    <button type="button" class="btn btn-secondary" :aria-expanded="open" @click="toggle">{{ t('subscriptionQuota.history') }}</button>
    <template v-if="open"><p v-if="error" role="alert" class="text-xs text-red-600">{{ error }}</p><p v-if="loading" class="text-xs text-gray-500">{{ t('subscriptionQuota.loading') }}</p><div v-for="term in terms" :key="term.id" class="flex flex-wrap items-center justify-between gap-2 text-xs text-gray-700 dark:text-gray-300"><span>#{{ term.term_no }} · {{ statusLabel(term.status) }} · {{ date(term.starts_at) }} — {{ date(term.expires_at) }} · {{ formatQuotaUSD(term.quota_usd) }}</span></div><p v-if="!loading && !terms.length" class="text-xs text-gray-500">{{ t('subscriptionQuota.emptyHistory') }}</p><button v-if="termCursor" type="button" class="btn btn-secondary" :disabled="loading" @click="loadTerms(true)">{{ t('subscriptionQuota.nextPage') }}</button>

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
import type { SubscriptionQuotaTerm } from '@/types/subscriptionQuota'
const props = defineProps<{ groupId: number }>()
const { t } = useI18n(), auth = useAuthStore()
const open = ref(false), terms = ref<SubscriptionQuotaTerm[]>([])
const loading = ref(false), error = ref('')
const termCursor = ref<string | null>(null)
const identity = useQuotaRequestIdentity(() => JSON.stringify([auth.user?.id, props.groupId, open.value]))
function statusLabel(value: string) { return quotaStatusLabel(value, t) }
function toggle() { open.value = !open.value; if (open.value) void loadTerms() }
async function loadTerms(more = false) {
  const current = identity.capture(); loading.value = true; error.value = ''
  try { const page = await subscriptionQuotaAPI.terms(props.groupId, more ? termCursor.value ?? undefined : undefined); if (current()) { terms.value = more ? [...terms.value, ...page.items] : page.items; termCursor.value = page.next_cursor } }
  catch (err) { if (current()) error.value = quotaError(err) }
  finally { if (current()) loading.value = false }
}
watch(() => JSON.stringify([auth.user?.id, props.groupId]), () => { identity.invalidate(); open.value = false; terms.value = []; termCursor.value = null; loading.value = false; error.value = '' }, { flush: 'sync' })
</script>
