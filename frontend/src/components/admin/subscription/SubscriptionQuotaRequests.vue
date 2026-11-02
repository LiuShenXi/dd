<template>
  <section class="card space-y-4 p-4" data-testid="quota-requests">
    <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('subscriptionQuota.requests') }}</h2>
    <form class="flex flex-wrap items-end gap-3" @submit.prevent="load(false)">
      <label><span class="input-label">{{ t('subscriptionQuota.status') }}</span><select v-model="status" class="input"><option value="">{{ t('subscriptionQuota.allStatuses') }}</option><option v-for="value in statuses" :key="value" :value="value">{{ statusLabel(value) }}</option></select></label>
      <label><span class="input-label">{{ t('subscriptionQuota.filterUser') }}</span><input v-model="userId" type="number" min="1" step="1" class="input w-32" /></label>
      <label><span class="input-label">{{ t('subscriptionQuota.filterGroup') }}</span><input v-model="groupId" type="number" min="1" step="1" class="input w-32" /></label>
      <button type="submit" class="btn btn-secondary" :disabled="loading">{{ t('subscriptionQuota.refresh') }}</button>
    </form>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('subscriptionQuota.loading') }}</p>
    <div class="overflow-x-auto"><table class="w-full text-left text-xs"><thead><tr class="border-b dark:border-dark-700"><th class="p-2">{{ t('subscriptionQuota.request') }}</th><th class="p-2">{{ t('subscriptionQuota.user') }} / {{ t('subscriptionQuota.group') }}</th><th class="p-2">{{ t('subscriptionQuota.status') }}</th><th class="p-2">{{ t('subscriptionQuota.cost') }}</th><th class="p-2">{{ t('subscriptionQuota.admitted') }}</th><th class="p-2">{{ t('subscriptionQuota.category') }}</th><th class="p-2" /></tr></thead><tbody><tr v-for="row in rows" :key="row.id" class="border-b dark:border-dark-700"><td class="max-w-64 break-all p-2 font-mono">{{ row.request_id }}</td><td class="p-2">#{{ row.user_id }} / #{{ row.group_id }}</td><td class="p-2">{{ statusLabel(row.status) }}</td><td class="p-2 font-mono">{{ row.actual_cost_usd === null ? t('subscriptionQuota.unknownCost') : formatQuotaUSD(row.actual_cost_usd) }}</td><td class="whitespace-nowrap p-2">{{ date(row.admitted_at) }}</td><td class="p-2">{{ row.last_error_category ?? '—' }}</td><td class="p-2"><button v-if="row.status === 'reconcile_required' && row.actual_cost_usd === null" type="button" class="btn btn-secondary" @click="target = row; showResolve = true">{{ t('subscriptionQuota.review') }}</button></td></tr></tbody></table></div>
    <p v-if="!loading && !rows.length" class="text-sm text-gray-500">{{ t('subscriptionQuota.noRequests') }}</p>
    <button v-if="cursor" type="button" class="btn btn-secondary" :disabled="loading" @click="load(true)">{{ t('subscriptionQuota.nextPage') }}</button>
    <SubscriptionQuotaResolveModal :show="showResolve" :request="target" @close="showResolve = false" @accepted="load(false)" />
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useQuotaRequestIdentity } from '@/composables/useQuotaRequestIdentity'
import { adminSubscriptionQuotaAPI } from '@/api/admin/subscriptionQuota'
import { formatQuotaUSD, quotaError } from '@/utils/claudeQuota'
import { formatDateTimeToMinute as date } from '@/utils/format'
import SubscriptionQuotaResolveModal from './SubscriptionQuotaResolveModal.vue'
import type { QuotaRequestStatus, SubscriptionQuotaRequest } from '@/types/subscriptionQuota'
const { t } = useI18n(), auth = useAuthStore()
const statuses: QuotaRequestStatus[] = ['admitted', 'usage_known', 'settled', 'no_charge', 'reconcile_required']
const status = ref<QuotaRequestStatus | ''>('reconcile_required'), userId = ref(''), groupId = ref('')
const rows = ref<SubscriptionQuotaRequest[]>([]), cursor = ref<string | null>(null), loading = ref(false), error = ref('')
const target = ref<SubscriptionQuotaRequest | null>(null), showResolve = ref(false)
const scope = () => JSON.stringify([auth.user?.id, status.value, userId.value, groupId.value])
const identity = useQuotaRequestIdentity(scope)
function statusLabel(value: string) { return statuses.includes(value as QuotaRequestStatus) ? t(`subscriptionQuota.statuses.${value}`) : value }
async function load(more = false) {
  const current = identity.capture(), next = more ? cursor.value : null
  loading.value = true; error.value = ''
  try {
    const page = await adminSubscriptionQuotaAPI.requests({ limit: 20, ...(next ? { cursor: next } : {}), ...(status.value ? { status: status.value } : {}), ...(userId.value ? { user_id: Number(userId.value) } : {}), ...(groupId.value ? { group_id: Number(groupId.value) } : {}) })
    if (!current()) return
    rows.value = more ? [...rows.value, ...page.items] : page.items; cursor.value = page.next_cursor
    if (target.value) target.value = page.items.find(row => row.id === target.value?.id) ?? target.value
  } catch (err) { if (current()) error.value = quotaError(err) }
  finally { if (current()) loading.value = false }
}
watch(scope, () => { identity.invalidate(); rows.value = []; cursor.value = null; loading.value = false; error.value = ''; showResolve.value = false }, { flush: 'sync' })
onMounted(() => { void load() })
</script>
