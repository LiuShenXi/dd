<template>
  <BaseDialog :show="show" :title="`${t('subscriptionQuota.manage')} · ${user?.email ?? ''}`" width="wide" @close="emit('close')">
    <div class="space-y-5">
      <p class="text-xs text-gray-500">{{ t('subscriptionQuota.fixedDuration') }}</p>
      <label class="block"><span class="input-label">{{ t('subscriptionQuota.group') }}</span><select v-model.number="groupId" class="input" data-testid="quota-group"><option :value="0">{{ t('subscriptionQuota.chooseGroup') }}</option><option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option></select></label>
      <p v-if="!groupsLoading && !groups.length" class="text-sm text-gray-500">{{ t('subscriptionQuota.noGroups') }}</p>
      <p v-if="loading || groupsLoading" role="status" class="text-sm text-gray-500">{{ t('subscriptionQuota.loading') }}</p>
      <p v-if="loadError" role="alert" class="text-sm text-red-600">{{ loadError }} <button type="button" class="underline" @click="loadSummary">{{ t('subscriptionQuota.retry') }}</button></p>
      <ClaudeQuotaCard v-if="groupId" :summary="summary" @refresh="loadSummary" />
      <form v-if="groupId" class="space-y-4" @submit.prevent="submit">
        <label class="block"><span class="input-label">{{ t('subscriptionQuota.resolution') }}</span><select v-model="operation" class="input" data-testid="quota-operation"><option value="open">{{ t('subscriptionQuota.open') }}</option><option value="renew">{{ t('subscriptionQuota.renew') }}</option></select></label>
        <p v-if="operation === 'renew'" class="text-sm text-amber-700 dark:text-amber-300">{{ t('subscriptionQuota.renewHint') }}</p>
        <label class="block"><span class="input-label">{{ t('subscriptionQuota.amount') }}</span><input v-model="amount" inputmode="decimal" type="text" class="input" required aria-describedby="claude-quota-amount-hint" data-testid="quota-amount" /><span id="claude-quota-amount-hint" class="input-hint">{{ t('subscriptionQuota.amountHint') }}</span></label>
        <label class="block"><span class="input-label">{{ t('subscriptionQuota.notes') }}</span><textarea v-model="notes" class="input" rows="2" data-testid="quota-notes" /></label>
        <button type="button" class="btn btn-secondary" :disabled="previewing || mutationBusy || !valid" data-testid="quota-preview" @click="requestPreview">{{ previewing ? t('subscriptionQuota.loading') : t('subscriptionQuota.preview') }}</button>
        <section v-if="preview" class="space-y-2 rounded-xl bg-orange-50 p-4 text-sm dark:bg-orange-900/20" data-testid="quota-preview-result"><h4 class="font-semibold">{{ t('subscriptionQuota.previewTitle') }}</h4><p>{{ t('subscriptionQuota.effectiveAmount') }}: {{ formatQuotaUSD(preview.effective_quota_usd) }}</p><p>{{ date(preview.proposed_starts_at) }} — {{ date(preview.proposed_expires_at) }}</p><p v-for="warning in preview.warnings" :key="warning" class="text-amber-700 dark:text-amber-300">{{ warning }}</p></section>
        <p v-if="error" role="alert" class="text-sm text-red-600" data-testid="quota-error">{{ error }}</p>
        <button type="submit" class="btn btn-primary" :disabled="mutationBusy || !preview || !valid" data-testid="quota-submit">{{ submitting ? t('subscriptionQuota.saving') : t('subscriptionQuota.submit') }}</button>
      </form>
      <section v-if="saved" class="space-y-2 border-t pt-4 text-sm dark:border-dark-700" role="status" data-testid="quota-saved"><p>{{ t(saved.replayed ? 'subscriptionQuota.replayed' : 'subscriptionQuota.saved') }}</p><p class="font-semibold">{{ t('subscriptionQuota.actual') }}</p><p>{{ date(saved.term.starts_at) }} — {{ date(saved.term.expires_at) }} · {{ formatQuotaUSD(saved.term.quota_usd) }}</p></section>
      <section v-if="terms.length" class="space-y-3 border-t pt-4 dark:border-dark-700"><h4 class="text-sm font-semibold">{{ t('subscriptionQuota.history') }}</h4><div v-for="term in terms" :key="term.id" class="flex flex-wrap items-center justify-between gap-2 text-xs"><span>#{{ term.term_no }} · {{ label(term.status) }} · {{ date(term.starts_at) }} — {{ date(term.expires_at) }} · {{ formatQuotaUSD(term.quota_usd) }}</span><button v-if="!term.cancelled_at && ['active', 'scheduled'].includes(term.lifecycle_state)" type="button" class="btn btn-secondary" @click="cancelTarget = term.id; cancelReason = ''">{{ t('subscriptionQuota.cancel') }}</button></div><button v-if="nextCursor" type="button" class="btn btn-secondary" :disabled="loading" @click="loadMoreTerms">{{ t('subscriptionQuota.nextPage') }}</button></section>
      <form v-if="cancelTarget" class="space-y-3 rounded-xl border border-red-200 p-4" data-testid="quota-cancel-form" @submit.prevent="cancelCard"><p class="text-xs text-amber-700">{{ t('subscriptionQuota.cancelHint') }}</p><label class="block"><span class="input-label">{{ t('subscriptionQuota.cancelReason') }}</span><textarea v-model="cancelReason" class="input" required /></label><p v-if="cancelError" role="alert" class="text-sm text-red-600">{{ cancelError }}</p><button type="submit" class="btn btn-danger" :disabled="mutationBusy || !cancelReason.trim()">{{ t('subscriptionQuota.cancelConfirm') }}</button></form>
      <p class="text-xs text-gray-500">{{ t('subscriptionQuota.closeHint') }}</p>
    </div>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ClaudeQuotaCard from '@/components/subscription/ClaudeQuotaCard.vue'
import groupsAPI from '@/api/admin/groups'
import { adminSubscriptionQuotaAPI as api } from '@/api/admin/subscriptionQuota'
import { useAuthStore } from '@/stores/auth'
import { useQuotaRequestIdentity } from '@/composables/useQuotaRequestIdentity'
import { useQuotaMutationMutex } from '@/composables/useQuotaMutationMutex'
import { formatQuotaUSD, newQuotaOperationKey, quotaConflict, quotaError, quotaStatusLabel, validQuotaInput } from '@/utils/claudeQuota'
import { formatDateTimeToMinute as date } from '@/utils/format'
import type { AdminGroup } from '@/types'
import type { SubscriptionQuotaMutation, SubscriptionQuotaPreview, SubscriptionQuotaSummary, SubscriptionQuotaTerm } from '@/types/subscriptionQuota'
const props = defineProps<{ show: boolean; user: { id: number; email: string } | null; initialGroupId?: number }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'success'): void }>()
const { t } = useI18n()
const auth = useAuthStore()
const groups = ref<AdminGroup[]>([]), groupId = ref(0)
const amount = ref(''), notes = ref(''), operation = ref<'open' | 'renew'>('open')
const summary = ref<SubscriptionQuotaSummary | null>(null), preview = ref<SubscriptionQuotaPreview | null>(null)
const terms = ref<SubscriptionQuotaTerm[]>([]), nextCursor = ref<string | null>(null)
const saved = ref<SubscriptionQuotaMutation | null>(null)
const groupsLoading = ref(false), loading = ref(false), previewing = ref(false), submitting = ref(false)
const loadError = ref(''), error = ref('')
const cancelTarget = ref<number | null>(null), cancelReason = ref(''), cancelError = ref(''), cancelling = ref(false)
const base = () => JSON.stringify([props.show, props.user?.id, auth.user?.id, groupId.value])
// A read/form edit must not release an already-sent mutation on the same scope.
const mutationLock = useQuotaMutationMutex('claude-term', () => JSON.stringify([auth.user?.id, props.user?.id, groupId.value]))
const mutationBusy = mutationLock.busy
const draft = computed(() => ({ group_id: groupId.value, operation: operation.value, quota_usd: amount.value, ...(notes.value ? { notes: notes.value } : {}) }))
const identity = useQuotaRequestIdentity(base)
const actions = useQuotaRequestIdentity(() => JSON.stringify([base(), draft.value]))
const cancellation = useQuotaRequestIdentity(() => JSON.stringify([base(), cancelTarget.value, cancelReason.value]))
const valid = computed(() => validQuotaInput(amount.value))
let previewFingerprint = '', keyFingerprint = '', operationKey = '', cancelFingerprint = '', cancelKey = ''
function label(value: string) { return quotaStatusLabel(value, t) }
async function loadSummary() {
  if (!props.show || !props.user || !groupId.value) return
  const current = identity.capture('summary'), userId = props.user.id, id = groupId.value
  loading.value = true; loadError.value = ''
  try {
    const [data, page] = await Promise.all([api.summary(userId, id), api.terms(userId, id)])
    if (!current()) return
    summary.value = data; terms.value = page.items; nextCursor.value = page.next_cursor
  } catch (err) { if (current()) loadError.value = quotaError(err) }
  finally { if (current()) loading.value = false }
}
async function loadMoreTerms() {
  if (!props.user || !nextCursor.value || loading.value) return
  const current = identity.capture('summary'), cursor = nextCursor.value
  loading.value = true
  try { const page = await api.terms(props.user.id, groupId.value, cursor); if (current()) { terms.value.push(...page.items); nextCursor.value = page.next_cursor } }
  catch (err) { if (current()) loadError.value = quotaError(err) }
  finally { if (current()) loading.value = false }
}
async function requestPreview() {
  if (!props.user || !valid.value || mutationBusy.value) return
  const payload = { ...draft.value }, current = actions.capture('preview')
  previewing.value = true; error.value = ''; preview.value = null
  try { const data = await api.preview(props.user.id, payload); if (current()) { preview.value = data; previewFingerprint = JSON.stringify(payload) } }
  catch (err) { if (current()) error.value = quotaError(err) }
  finally { if (current()) previewing.value = false }
}
async function submit() {
  if (!props.user || !preview.value || !valid.value || mutationBusy.value || previewFingerprint !== JSON.stringify(draft.value)) return
  const payload = { ...draft.value, expected_revision: preview.value.current_revision }, fingerprint = JSON.stringify(payload)
  if (keyFingerprint !== fingerprint) { keyFingerprint = fingerprint; operationKey = newQuotaOperationKey() }
  const current = actions.capture('submit'), userId = props.user.id, key = operationKey
  const releaseMutation = mutationLock.acquire()
  if (!releaseMutation) return
  submitting.value = true; error.value = ''
  try {
    const data = await api.open(userId, payload, key)
    if (!current()) return
    saved.value = data; preview.value = null; summary.value = data.summary; operationKey = ''; keyFingerprint = ''
    emit('success'); void loadSummary()
  } catch (err) {
    if (!current()) return
    if (quotaConflict(err)) { preview.value = null; operationKey = ''; keyFingerprint = ''; error.value = t('subscriptionQuota.conflict'); void loadSummary() }
    else error.value = quotaError(err)
  } finally { releaseMutation(); if (current()) submitting.value = false }
}
async function cancelCard() {
  if (!cancelTarget.value || !summary.value || !cancelReason.value.trim() || mutationBusy.value) return
  const id = cancelTarget.value, revision = summary.value.revision, reason = cancelReason.value
  const fingerprint = JSON.stringify([id, revision, reason])
  if (cancelFingerprint !== fingerprint) { cancelFingerprint = fingerprint; cancelKey = newQuotaOperationKey() }
  const current = cancellation.capture(), key = cancelKey
  const releaseMutation = mutationLock.acquire()
  if (!releaseMutation) return
  cancelling.value = true; cancelError.value = ''
  try { const data = await api.cancel(id, revision, reason, key); if (current()) { saved.value = data; emit('success'); void loadSummary() } }
  catch (err) { if (current()) { cancelError.value = quotaConflict(err) ? t('subscriptionQuota.conflict') : quotaError(err); if (quotaConflict(err)) { cancelKey = ''; cancelFingerprint = ''; void loadSummary() } } }
  finally { releaseMutation(); if (current()) cancelling.value = false }
}
watch(() => JSON.stringify([props.show, props.user?.id, auth.user?.id]), async () => {
  identity.invalidate(); actions.invalidate(); cancellation.invalidate()
  groupId.value = 0; groups.value = []; summary.value = null; terms.value = []; preview.value = null; saved.value = null
  amount.value = ''; notes.value = ''; operation.value = 'open'; loading.value = false; groupsLoading.value = false; previewing.value = false; submitting.value = false; cancelling.value = false; loadError.value = ''; error.value = ''; cancelTarget.value = null
  if (!props.show || !props.user) return
  const current = identity.capture('groups'); groupsLoading.value = true
  try { const data = await groupsAPI.getAll('anthropic'); if (current()) { groups.value = data.filter(group => group.subscription_quota_mode === 'term' && group.subscription_type === 'subscription'); groupsLoading.value = false; groupId.value = groups.value.some(group => group.id === props.initialGroupId) ? props.initialGroupId! : groups.value[0]?.id ?? 0 } }
  catch (err) { if (current()) loadError.value = quotaError(err) }
  finally { if (current()) groupsLoading.value = false }
}, { immediate: true, flush: 'sync' })
watch(groupId, () => { identity.invalidate(); summary.value = null; terms.value = []; saved.value = null; cancelTarget.value = null; loading.value = false; void loadSummary() })
watch(() => JSON.stringify(draft.value), () => { actions.invalidate(); preview.value = null; error.value = ''; previewing.value = false; submitting.value = false; previewFingerprint = ''; keyFingerprint = ''; operationKey = '' }, { flush: 'sync' })
watch(() => JSON.stringify([cancelTarget.value, cancelReason.value]), () => { cancellation.invalidate(); cancelling.value = false; cancelError.value = '' }, { flush: 'sync' })
</script>
