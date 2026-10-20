<template>
  <BaseDialog :show="show" :title="`${t('subscriptionQuota.rollout.title')} · ${group?.name ?? ''}`" width="wide" @close="emit('close')">
    <div class="space-y-5">
      <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('subscriptionQuota.rollout.introduction') }}</p>
      <p v-if="loading" role="status">{{ t('subscriptionQuota.loading') }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600" data-testid="rollout-error">{{ error }}</p>
      <button type="button" class="btn btn-secondary" :disabled="working" data-testid="rollout-refresh" @click="loadState()">{{ t('subscriptionQuota.refresh') }}</button>

      <template v-if="state">
        <section class="space-y-2 rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800" data-testid="rollout-state">
          <p>{{ t('subscriptionQuota.rollout.mode') }}: {{ t(state.quota_mode === 'term' ? 'subscriptionQuota.rollout.termMode' : 'subscriptionQuota.rollout.legacyMode') }}</p>
          <p>{{ t('subscriptionQuota.rollout.maintenance') }}: {{ t(state.maintenance ? 'common.enabled' : 'common.disabled') }} · {{ t('subscriptionQuota.rollout.revision') }}: {{ state.revision }}</p>
          <p>{{ t('subscriptionQuota.rollout.checkedAt') }}: {{ date(state.server_time) }}</p>
          <dl class="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <div><dt>{{ t('subscriptionQuota.rollout.protectedPending') }}</dt><dd class="font-semibold">{{ state.protected_pending }}</dd></div>
            <div><dt>{{ t('subscriptionQuota.rollout.pendingOrders') }}</dt><dd class="font-semibold">{{ state.pending_orders }}</dd></div>
          </dl>
          <p v-if="state.legacy_terminal_orders > 0 && !preview" role="note" class="text-amber-800 dark:text-amber-300" data-testid="rollout-legacy-orders-warning">
            {{ t('subscriptionQuota.rollout.legacyTerminalOrders') }}: {{ state.legacy_terminal_orders }}.
            {{ t('subscriptionQuota.rollout.legacyTerminalOrdersHint') }}
          </p>
          <ul v-if="state.blockers.length" class="list-inside list-disc space-y-1 text-amber-800 dark:text-amber-300" data-testid="rollout-blockers">
            <li v-for="blocker in state.blockers" :key="blocker" class="break-words">{{ blockerLabel(blocker) }}</li>
          </ul>
          <p v-else>{{ t('subscriptionQuota.rollout.noBlockers') }}</p>
        </section>

        <section class="space-y-2">
          <h4 class="text-sm font-semibold">{{ t('subscriptionQuota.rollout.replicas') }}</h4>
          <p v-if="!state.replicas.length" class="text-sm text-amber-700">{{ t('subscriptionQuota.rollout.noReplicas') }}</p>
          <div v-for="(replica, index) in state.replicas" :key="`${replica.replica_id}:${index}`" class="space-y-1 rounded-lg border border-gray-200 p-3 text-xs dark:border-dark-600" data-testid="rollout-replica">
            <p class="break-all font-medium">{{ replica.replica_id }} · {{ date(replica.heartbeat_at) }}</p>
            <p class="break-words">{{ t('subscriptionQuota.rollout.versions') }}: {{ replica.capability_version }} / {{ replica.receipt_version }} / {{ replica.lease_protocol_version }}</p>
            <p>{{ t('subscriptionQuota.rollout.defaultConcurrency') }}: {{ replica.claude_default_concurrency }}</p>
            <p>{{ t('subscriptionQuota.rollout.policies') }}: {{ flag(replica.issue_enabled) }} / {{ flag(replica.admission_enabled) }} / {{ flag(replica.recovery_enabled) }}</p>
            <p>{{ t('subscriptionQuota.rollout.protectedGroups') }}: {{ replica.protected_group_ids.join(', ') || '—' }}</p>
            <p>{{ t('subscriptionQuota.rollout.enabledGroups') }}: {{ replica.enabled_group_ids.join(', ') || '—' }}</p>
            <p class="break-words">{{ t('subscriptionQuota.rollout.expectedReplicas') }}: {{ replica.expected_replica_ids.join(', ') || '—' }}</p>
          </div>
        </section>

        <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
          <label class="block"><span class="input-label">{{ t('subscriptionQuota.rollout.maintenanceReason') }}</span><textarea v-model="maintenanceReason" class="input" rows="2" :disabled="mutationBusy" data-testid="rollout-reason" /></label>
          <p class="text-xs text-gray-500">{{ t('subscriptionQuota.rollout.maintenanceHint') }}</p>
          <button type="button" class="btn btn-secondary" :disabled="working || !maintenanceReason.trim()" data-testid="rollout-maintenance" @click="setMaintenance(!state.maintenance)">
            {{ t(state.maintenance ? 'subscriptionQuota.rollout.leaveMaintenance' : 'subscriptionQuota.rollout.enterMaintenance') }}
          </button>
        </section>

        <template v-if="state.quota_mode === 'windows' && !state.converted">
          <section class="space-y-2 border-t border-gray-200 pt-4 dark:border-dark-700">
            <h4 class="font-semibold">{{ t('subscriptionQuota.rollout.keyUsers') }}</h4>
            <p class="text-xs text-gray-500">{{ t('subscriptionQuota.rollout.keyUsersHint') }}</p>
            <p v-if="!state.key_users.length" class="text-sm text-gray-500">{{ t('subscriptionQuota.rollout.noKeyUsers') }}</p>
            <ul class="max-h-60 space-y-2 overflow-y-auto">
              <li v-for="user in state.key_users" :key="user.user_id" class="flex flex-wrap items-center justify-between gap-2 rounded border border-gray-200 p-2 text-sm dark:border-dark-700">
                <span class="min-w-0 break-words">#{{ user.user_id }} · {{ user.username || user.email }} · {{ t('subscriptionQuota.rollout.keyCount', { count: user.key_count }) }}</span>
                <button type="button" class="btn btn-secondary shrink-0" :disabled="mutationBusy || hasUser(user.user_id)" :data-testid="`rollout-add-user-${user.user_id}`" @click="addAllocation(user.user_id)">{{ t('subscriptionQuota.rollout.addAllocation') }}</button>
              </li>
            </ul>
          </section>

          <form class="space-y-3" data-testid="rollout-allocation-form" @submit.prevent="requestPreview">
            <h4 class="font-semibold">{{ t('subscriptionQuota.rollout.allocations') }}</h4>
            <p class="text-xs text-gray-500">{{ t('subscriptionQuota.rollout.allocationHint') }}</p>
            <fieldset :disabled="mutationBusy" class="space-y-3">
              <div v-for="row in rows" :key="row.id" class="grid grid-cols-1 gap-2 rounded-lg border border-gray-200 p-3 sm:grid-cols-[1fr_2fr_auto] sm:items-end dark:border-dark-700" data-testid="rollout-allocation-row">
                <label class="block"><span class="input-label">{{ t('subscriptionQuota.rollout.userId') }}</span><input v-model="row.userId" type="text" inputmode="numeric" class="input" required data-testid="rollout-user-id" /></label>
                <label class="block"><span class="input-label">{{ t('subscriptionQuota.amount') }}</span><input v-model="row.quota" type="text" inputmode="decimal" class="input" required data-testid="rollout-user-quota" /></label>
                <button type="button" class="btn btn-secondary" @click="removeAllocation(row.id)">{{ t('common.remove') }}</button>
              </div>
              <button type="button" class="btn btn-secondary" data-testid="rollout-add-row" @click="addAllocation()">{{ t('subscriptionQuota.rollout.addAllocation') }}</button>
            </fieldset>
            <p v-if="!rows.length" class="text-sm text-amber-700 dark:text-amber-300" data-testid="rollout-no-allocations">{{ t('subscriptionQuota.rollout.noAllocations') }}</p>
            <p v-else-if="!validAllocations" role="alert" class="text-sm text-red-600">{{ t('subscriptionQuota.rollout.invalidAllocations') }}</p>
            <button type="submit" class="btn btn-secondary" :disabled="working || !validAllocations" data-testid="rollout-preview">{{ t('subscriptionQuota.preview') }}</button>
          </form>

          <section v-if="preview" class="space-y-3 rounded-lg bg-orange-50 p-4 dark:bg-orange-900/20" data-testid="rollout-preview-result">
            <h4 class="font-semibold">{{ t('subscriptionQuota.rollout.previewTitle') }}</h4>
            <p v-if="preview.legacy_terminal_orders > 0" role="note" class="text-sm text-amber-800 dark:text-amber-300" data-testid="rollout-legacy-orders-warning">
              {{ t('subscriptionQuota.rollout.legacyTerminalOrders') }}: {{ preview.legacy_terminal_orders }}.
              {{ t('subscriptionQuota.rollout.legacyTerminalOrdersHint') }}
            </p>
            <p class="text-sm">{{ t('subscriptionQuota.fixedDuration') }}</p>
            <ul v-if="preview.allocations.length" class="space-y-1 text-sm"><li v-for="allocation in preview.allocations" :key="allocation.user_id">#{{ allocation.user_id }} · {{ formatQuotaUSD(allocation.quota_usd) }} USD</li></ul>
            <p v-else class="text-sm text-amber-700 dark:text-amber-300">{{ t('subscriptionQuota.rollout.noAllocations') }}</p>
            <label class="flex items-start gap-2 text-sm"><input v-model="confirmed" type="checkbox" class="mt-1" :disabled="!convertible || mutationBusy" data-testid="rollout-confirmed" /><span>{{ t('subscriptionQuota.rollout.confirmHint') }}</span></label>
            <button type="button" class="btn btn-primary" :disabled="working || !convertible || !confirmed" data-testid="rollout-convert" @click="convert">{{ t('subscriptionQuota.rollout.convert') }}</button>
          </section>
        </template>

        <section v-else-if="state.converted" class="space-y-2 border-t pt-4 text-sm dark:border-dark-700" data-testid="rollout-converted">
          <p class="font-semibold">{{ t(state.maintenance ? 'subscriptionQuota.rollout.convertedMaintained' : 'subscriptionQuota.rollout.convertedReleased') }}</p>
          <p v-for="allocation in state.allocations" :key="allocation.user_id">#{{ allocation.user_id }} · {{ formatQuotaUSD(allocation.quota_usd) }} USD</p>
          <p v-for="term in savedTerms" :key="term.id">#{{ term.id }} · {{ date(term.starts_at) }} — {{ date(term.expires_at) }} · {{ formatQuotaUSD(term.quota_usd) }} USD</p>
        </section>
      </template>
      <p class="text-xs text-gray-500">{{ t('subscriptionQuota.closeHint') }}</p>
    </div>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminSubscriptionQuotaRolloutAPI as api } from '@/api/admin/subscriptionQuotaRollout'
import { useQuotaRequestIdentity } from '@/composables/useQuotaRequestIdentity'
import { useQuotaMutationMutex } from '@/composables/useQuotaMutationMutex'
import { formatQuotaUSD, newQuotaOperationKey, quotaConflict, quotaError, validQuotaInput } from '@/utils/claudeQuota'
import { formatDateTimeToMinute as date } from '@/utils/format'
import type { AdminGroup } from '@/types'
import type { QuotaRolloutAllocation, QuotaRolloutState } from '@/types/subscriptionQuotaRollout'
import type { SubscriptionQuotaTerm } from '@/types/subscriptionQuota'

const props = defineProps<{ show: boolean; group: Pick<AdminGroup, 'id' | 'name' | 'platform'> | null }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t, te } = useI18n()
const auth = useAuthStore()
const state = ref<QuotaRolloutState | null>(null), preview = ref<QuotaRolloutState | null>(null)
const savedTerms = ref<SubscriptionQuotaTerm[]>([])
const rows = ref<Array<{ id: number; userId: string; quota: string }>>([])
const maintenanceReason = ref(''), confirmed = ref(false), error = ref('')
const loading = ref(false), previewing = ref(false)
let nextRowId = 0, previewFingerprint = '', maintenanceFingerprint = '', maintenanceKey = '', convertFingerprint = '', convertKey = ''
let previewAllocations: QuotaRolloutAllocation[] = []
const base = () => JSON.stringify([props.show, props.group?.id, auth.user?.id])
const identity = useQuotaRequestIdentity(base)
const draftFingerprint = computed(() => JSON.stringify(rows.value.map(row => [row.userId, row.quota])))
const draftIdentity = useQuotaRequestIdentity(() => JSON.stringify([base(), draftFingerprint.value]))
const maintenanceIdentity = useQuotaRequestIdentity(() => JSON.stringify([base(), maintenanceReason.value]))
const mutationLock = useQuotaMutationMutex('claude-rollout', () => JSON.stringify([auth.user?.id, props.group?.id]))
const mutationBusy = mutationLock.busy
const working = computed(() => loading.value || previewing.value || mutationBusy.value)
function validUserId(value: string) { return /^[1-9]\d*$/.test(value) && Number.isSafeInteger(Number(value)) }
const validAllocations = computed(() => rows.value.every(row => validUserId(row.userId) && validQuotaInput(row.quota)) && new Set(rows.value.map(row => row.userId)).size === rows.value.length)
const allocations = computed<QuotaRolloutAllocation[]>(() => rows.value.map(row => ({ user_id: Number(row.userId), quota_usd: row.quota })).sort((a, b) => a.user_id - b.user_id))
const convertible = computed(() => !!preview.value && preview.value.maintenance && !preview.value.converted && preview.value.quota_mode === 'windows' && preview.value.blockers.length === 0 && !!preview.value.preview_token && validAllocations.value && previewFingerprint === draftFingerprint.value)
function flag(value: boolean) { return t(value ? 'common.enabled' : 'common.disabled') }
function blockerLabel(value: string) { const key = `subscriptionQuota.rollout.blockers.${value}`; return te(key) ? t(key) : value }
function hasUser(userId: number) { return rows.value.some(row => row.userId === String(userId)) }
function addAllocation(userId?: number) { if (!mutationBusy.value && (!userId || !hasUser(userId))) rows.value.push({ id: ++nextRowId, userId: userId ? String(userId) : '', quota: '' }) }
function removeAllocation(id: number) { rows.value = rows.value.filter(row => row.id !== id) }
function invalidatePreview() { preview.value = null; confirmed.value = false; previewFingerprint = ''; previewAllocations = []; convertKey = ''; convertFingerprint = '' }
async function loadState(clearError = true) {
  if (!props.show || !props.group || props.group.platform !== 'anthropic' || mutationBusy.value) return
  const current = identity.capture('state'), groupId = props.group.id
  invalidatePreview(); loading.value = true; if (clearError) error.value = ''
  try { const result = await api.state(groupId); if (current()) state.value = result }
  catch (err) { if (current()) error.value = quotaError(err) }
  finally { if (current()) loading.value = false }
}
async function requestPreview() {
  if (!props.group || !state.value || working.value || !validAllocations.value || state.value.quota_mode !== 'windows') return
  const groupId = props.group.id, payload = allocations.value.map(row => ({ ...row })), fingerprint = draftFingerprint.value
  const currentState = identity.capture('state'), currentDraft = draftIdentity.capture()
  const current = () => currentState() && currentDraft()
  invalidatePreview(); previewing.value = true; error.value = ''
  try {
    const result = await api.preview(groupId, payload)
    if (current()) { state.value = result; preview.value = result; previewAllocations = payload; previewFingerprint = fingerprint }
  } catch (err) { if (current()) error.value = quotaError(err) }
  finally { if (current()) previewing.value = false }
}
async function setMaintenance(enabled: boolean) {
  if (!props.group || !state.value || working.value || !maintenanceReason.value.trim()) return
  const groupId = props.group.id, payload = { enabled, expected_revision: state.value.revision, reason: maintenanceReason.value }
  const fingerprint = JSON.stringify(payload)
  if (fingerprint !== maintenanceFingerprint) { maintenanceFingerprint = fingerprint; maintenanceKey = newQuotaOperationKey() }
  const key = maintenanceKey, currentState = identity.capture('state'), currentDraft = maintenanceIdentity.capture()
  const current = () => currentState() && currentDraft()
  const release = mutationLock.acquire(); if (!release) return
  invalidatePreview(); error.value = ''
  try {
    const result = await api.maintenance(groupId, payload, key)
    if (current()) { state.value = result; maintenanceKey = ''; maintenanceFingerprint = ''; emit('changed') }
  } catch (err) {
    if (!current()) return
    error.value = quotaConflict(err) ? t('subscriptionQuota.rollout.conflict') : quotaError(err)
    if (quotaConflict(err)) { maintenanceKey = ''; maintenanceFingerprint = ''; release(); void loadState(false) }
  } finally { release() }
}
async function convert() {
  if (!props.group || !preview.value || working.value || !convertible.value || !confirmed.value) return
  const groupId = props.group.id, payload = { allocations: previewAllocations.map(row => ({ ...row })), expected_revision: preview.value.revision, preview_token: preview.value.preview_token }
  const fingerprint = JSON.stringify(payload)
  if (fingerprint !== convertFingerprint) { convertFingerprint = fingerprint; convertKey = newQuotaOperationKey() }
  const key = convertKey, currentState = identity.capture('state'), currentDraft = draftIdentity.capture()
  const current = () => currentState() && currentDraft()
  const release = mutationLock.acquire(); if (!release) return
  error.value = ''
  try {
    const result = await api.convert(groupId, payload, key)
    if (current()) { state.value = result.state; savedTerms.value = result.terms; invalidatePreview(); emit('changed') }
  } catch (err) {
    if (!current()) return
    if (quotaConflict(err)) { invalidatePreview(); error.value = t('subscriptionQuota.rollout.conflict'); release(); void loadState(false) }
    else error.value = quotaError(err)
  } finally { release() }
}
watch(mutationBusy, busy => { if (!busy && props.show && !state.value) void loadState() })
watch(draftFingerprint, () => { draftIdentity.invalidate(); invalidatePreview(); previewing.value = false; error.value = '' }, { flush: 'sync' })
watch(base, () => {
  identity.invalidate(); draftIdentity.invalidate(); maintenanceIdentity.invalidate()
  state.value = null; rows.value = []; savedTerms.value = []; maintenanceReason.value = ''; loading.value = false; previewing.value = false; error.value = ''
  maintenanceKey = ''; maintenanceFingerprint = ''; invalidatePreview()
  void loadState()
}, { immediate: true, flush: 'sync' })
</script>
