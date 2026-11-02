<template>
  <BaseDialog :show="show" :title="t('subscriptionQuota.review')" width="wide" @close="emit('close')">
    <form v-if="request" class="space-y-4" @submit.prevent="submit">
      <p class="break-all font-mono text-xs">{{ request.request_id }}</p><p class="text-xs text-gray-500">{{ t('subscriptionQuota.requestRevision') }}: {{ request.revision }} · #{{ request.user_id }} / #{{ request.group_id }} / #{{ request.term_id }}</p>
      <label class="block"><span class="input-label">{{ t('subscriptionQuota.resolution') }}</span><select v-model="resolution" class="input" data-testid="resolve-kind"><option value="no_charge">{{ t('subscriptionQuota.noCharge') }}</option><option value="verified_usage">{{ t('subscriptionQuota.verifiedUsage') }}</option></select></label>
      <label class="block"><span class="input-label">{{ t('subscriptionQuota.evidence') }}</span><input v-model="evidence" type="text" class="input" required data-testid="resolve-evidence" /></label>
      <label class="block"><span class="input-label">{{ t('subscriptionQuota.reason') }}</span><textarea v-model="reason" class="input" required data-testid="resolve-reason" /></label>
      <div v-if="resolution === 'verified_usage'" class="grid gap-3 sm:grid-cols-2"><label v-for="field in tokenFields" :key="field.key" class="block"><span class="input-label">{{ t(`subscriptionQuota.${field.label}`) }}</span><input v-model="tokens[field.key]" type="text" inputmode="numeric" class="input" required :data-testid="`resolve-${field.key}`" /></label><label v-for="field in modelFields" :key="field.key" class="block"><span class="input-label">{{ t(`subscriptionQuota.${field.label}`) }}</span><input v-model="models[field.key]" type="text" class="input" /></label></div>
      <p v-if="error" role="alert" class="text-sm text-red-600" data-testid="resolve-error">{{ error }}</p>
      <p v-if="accepted" role="status" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t('subscriptionQuota.accepted') }}</p>
      <button type="submit" class="btn btn-primary" :disabled="submitting || accepted || !evidence.trim() || !reason.trim() || request.status !== 'reconcile_required' || request.actual_cost_usd !== null" data-testid="resolve-submit">{{ submitting ? t('subscriptionQuota.saving') : t('subscriptionQuota.resolve') }}</button>
      <p class="text-xs text-gray-500">{{ t('subscriptionQuota.closeHint') }}</p>
    </form>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAuthStore } from '@/stores/auth'
import { useQuotaRequestIdentity } from '@/composables/useQuotaRequestIdentity'
import { adminSubscriptionQuotaAPI } from '@/api/admin/subscriptionQuota'
import { newQuotaOperationKey, quotaConflict, quotaError } from '@/utils/claudeQuota'
import type { QuotaResolveInput, QuotaVerifiedUsage, SubscriptionQuotaRequest } from '@/types/subscriptionQuota'
const props = defineProps<{ show: boolean; request: SubscriptionQuotaRequest | null }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'accepted'): void }>()
const { t } = useI18n(), auth = useAuthStore()
const resolution = ref<'no_charge' | 'verified_usage'>('no_charge'), evidence = ref(''), reason = ref('')
const tokenFields = [{ key: 'input_tokens', label: 'inputTokens' }, { key: 'output_tokens', label: 'outputTokens' }, { key: 'cache_creation_5m_tokens', label: 'cache5m' }, { key: 'cache_creation_1h_tokens', label: 'cache1h' }, { key: 'cache_read_tokens', label: 'cacheRead' }] as const
const modelFields = [{ key: 'requested_model', label: 'requestedModel' }, { key: 'upstream_model', label: 'upstreamModel' }, { key: 'response_model', label: 'responseModel' }, { key: 'service_tier', label: 'serviceTier' }] as const
const tokens = reactive<Record<typeof tokenFields[number]['key'], string>>({ input_tokens: '', output_tokens: '', cache_creation_5m_tokens: '', cache_creation_1h_tokens: '', cache_read_tokens: '' })
const models = reactive({ requested_model: '', upstream_model: '', response_model: '', service_tier: '' })
const submitting = ref(false), accepted = ref(false), error = ref('')
const payload = computed<QuotaResolveInput | null>(() => {
  if (!props.request) return null
  const common = { expected_request_revision: props.request.revision, evidence_reference: evidence.value, reason: reason.value }
  if (resolution.value === 'no_charge') return { ...common, resolution: 'no_charge' }
  const values = tokenFields.map(field => tokens[field.key])
  if (values.some(value => !/^(0|[1-9]\d*)$/.test(value) || !Number.isSafeInteger(Number(value)))) return null
  const verified = Object.fromEntries(tokenFields.map(field => [field.key, Number(tokens[field.key])])) as unknown as QuotaVerifiedUsage
  for (const field of modelFields) if (models[field.key]) verified[field.key] = models[field.key]
  return { ...common, resolution: 'verified_usage', verified_usage: verified }
})
const identity = useQuotaRequestIdentity(() => JSON.stringify([props.show, auth.user?.id, props.request?.id, props.request?.user_id, props.request?.group_id, props.request?.term_id, payload.value, tokens, models]))
let key = '', fingerprint = ''
async function submit() {
  if (!props.request || submitting.value || accepted.value || !evidence.value.trim() || !reason.value.trim()) return
  if (!payload.value) { error.value = t('subscriptionQuota.countersInvalid'); return }
  const input = structuredClone(payload.value), nextFingerprint = JSON.stringify([props.request.id, input])
  if (nextFingerprint !== fingerprint) { key = newQuotaOperationKey(); fingerprint = nextFingerprint }
  const current = identity.capture(), id = props.request.id, operationKey = key
  submitting.value = true; error.value = ''
  try { await adminSubscriptionQuotaAPI.resolve(id, input, operationKey); if (current()) { accepted.value = true; emit('accepted') } }
  catch (err) { if (current()) { error.value = quotaConflict(err) ? t('subscriptionQuota.conflict') : quotaError(err); if (quotaConflict(err)) { key = ''; fingerprint = ''; emit('accepted') } } }
  finally { if (current()) submitting.value = false }
}
watch(() => JSON.stringify([props.show, auth.user?.id, props.request?.id]), () => { identity.invalidate(); evidence.value = ''; reason.value = ''; resolution.value = 'no_charge'; for (const field of tokenFields) tokens[field.key] = ''; for (const field of modelFields) models[field.key] = ''; submitting.value = false; accepted.value = false; error.value = ''; key = ''; fingerprint = '' }, { immediate: true, flush: 'sync' })
watch(() => JSON.stringify([payload.value, tokens, models]), () => { identity.invalidate(); submitting.value = false; accepted.value = false; error.value = ''; key = ''; fingerprint = '' }, { flush: 'sync' })
</script>
