<template>
  <BaseDialog :show="show" :title="t('admin.carpool.userDialog.title', { user: user?.email ?? '' })" width="wide" @close="emit('close')">
    <div v-if="loading" class="flex min-h-48 items-center justify-center"><LoadingSpinner /></div>
    <div v-else-if="loadError" class="py-10 text-center">
      <p class="text-sm text-red-600 dark:text-red-400">{{ t('admin.carpool.loadFailed') }}</p>
      <button type="button" class="btn btn-secondary mt-4" @click="load">{{ t('carpool.retry') }}</button>
    </div>
    <div v-else class="space-y-5">
      <section v-if="relevantTerm" class="border-b border-gray-200 pb-5 dark:border-dark-700">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <p class="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('admin.carpool.userDialog.currentTerm') }}</p>
            <p class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ relevantTerm.plan_snapshot.name }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ formatDate(relevantTerm.starts_at) }} - {{ formatDate(relevantTerm.expires_at) }}</p>
          </div>
          <span class="rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-200">{{ termStatus(relevantTerm.status) }}</span>
        </div>
        <div class="mt-4 grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
          <div><p class="text-xs text-gray-500">{{ t('admin.carpool.columns.currentCycle') }}</p><p class="mt-1 font-mono text-gray-900 dark:text-white">{{ relevantTerm.current_cycle?.cycle_no ?? '-' }}</p></div>
          <div><p class="text-xs text-gray-500">{{ t('admin.carpool.columns.availableUsd') }}</p><p class="mt-1 font-mono text-gray-900 dark:text-white">${{ money(relevantTerm.current_cycle?.available_usd) }}</p></div>
          <div><p class="text-xs text-gray-500">{{ t('admin.carpool.columns.boosts') }}</p><p class="mt-1 font-mono text-gray-900 dark:text-white">{{ relevantTerm.boost_remaining }}/{{ relevantTerm.plan_snapshot.boost_count }}</p></div>
          <div><p class="text-xs text-gray-500">{{ t('admin.carpool.columns.netPaidCny') }}</p><p class="mt-1 font-mono text-gray-900 dark:text-white">¥{{ money(relevantTerm.payment_net_cny) }}</p></div>
        </div>
        <div class="mt-4 flex flex-wrap gap-2">
          <button v-if="relevantTerm.status === 'active'" type="button" class="btn btn-secondary" @click="startRenew">{{ t('admin.carpool.actions.renew') }}</button>
          <button type="button" class="btn btn-secondary" @click="router.push('/admin/carpool'); emit('close')">{{ t('admin.carpool.actions.openManagement') }}</button>
        </div>
      </section>

      <form v-if="!relevantTerm || formMode === 'renew'" class="space-y-5" @submit.prevent="submit">
        <div v-if="formMode === 'open'" class="inline-flex rounded-lg bg-gray-100 p-1 dark:bg-dark-700">
          <button v-for="mode in openingModes" :key="mode.value" type="button" class="rounded-md px-3 py-1.5 text-sm font-medium" :class="openingMode === mode.value ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-800 dark:text-white' : 'text-gray-500 dark:text-gray-300'" @click="openingMode = mode.value">
            {{ mode.label }}
          </button>
        </div>

        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.carpool.fields.plan') }}</label>
            <Select v-model="selectedPlanId" :options="planOptions" />
          </div>
          <div v-if="formMode === 'open'">
            <label class="input-label">{{ t('admin.carpool.fields.group') }}</label>
            <Select v-model="selectedGroupId" :options="groupOptions" />
          </div>
          <div v-if="formMode === 'open'">
            <label class="input-label">{{ t('admin.carpool.fields.startsAt') }}</label>
            <input v-model="startsAtLocal" type="datetime-local" class="input" />
            <p class="input-hint">{{ t('admin.carpool.fields.startsAtHint') }}</p>
          </div>
          <div>
            <label class="input-label">{{ t('admin.carpool.fields.notes') }}</label>
            <input v-model.trim="notes" type="text" class="input" />
          </div>
        </div>

        <div v-if="formMode === 'open' && openingMode === 'takeover'" class="border-l-2 border-amber-300 pl-4 dark:border-amber-700">
          <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.carpool.takeover.title') }}</p>
          <p class="mt-1 text-xs text-amber-700 dark:text-amber-300">{{ t('admin.carpool.takeover.warning') }}</p>
          <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <label v-for="field in takeoverBalanceFields" :key="field.key" class="block">
              <span class="input-label">{{ field.label }}</span>
              <input v-model="takeover[field.key]" type="number" min="0" step="0.00000001" class="input" required />
            </label>
            <label class="block"><span class="input-label">{{ t('admin.carpool.takeover.boostUsed') }}</span><input v-model.number="takeover.boost_used" type="number" min="0" :max="selectedPlan?.boost_count ?? 0" class="input" required /></label>
            <label class="flex items-center gap-2 self-end pb-2 text-sm text-gray-700 dark:text-gray-200"><input v-model="takeover.history_complete" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600" />{{ t('admin.carpool.takeover.historyComplete') }}</label>
            <label v-if="takeover.history_complete" class="block"><span class="input-label">{{ t('admin.carpool.takeover.historicalUsed') }}</span><input v-model="takeover.historical_used_usd" type="number" min="0" step="0.00000001" class="input" required /></label>
            <label v-if="takeover.history_complete" class="block"><span class="input-label">{{ t('admin.carpool.takeover.statisticsSince') }}</span><input v-model="takeover.statistics_since" type="datetime-local" class="input" required /></label>
          </div>
        </div>

        <label class="flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-200">
          <input v-model="recordPayment" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600" />
          {{ t('admin.carpool.payment.recordNow') }}
        </label>
        <div v-if="recordPayment" class="grid gap-4 border-l-2 border-emerald-300 pl-4 sm:grid-cols-2 lg:grid-cols-3 dark:border-emerald-700">
          <div><label class="input-label">{{ t('admin.carpool.payment.amountCny') }}</label><input v-model="payment.amount_cny" type="number" min="0.01" step="0.01" class="input" required @input="paymentAmountDirty = true" /></div>
          <div><label class="input-label">{{ t('admin.carpool.payment.paidAt') }}</label><input v-model="payment.paid_at" type="datetime-local" class="input" required /></div>
          <div><label class="input-label">{{ t('admin.carpool.payment.channel') }}</label><input v-model.trim="payment.channel" type="text" class="input" required /></div>
          <div><label class="input-label">{{ t('admin.carpool.payment.orderNo') }}</label><input v-model.trim="payment.external_order_no" type="text" class="input" /></div>
          <div class="sm:col-span-2"><label class="input-label">{{ t('admin.carpool.payment.notes') }}</label><input v-model.trim="payment.notes" type="text" class="input" /></div>
        </div>

        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary" :disabled="previewing || !canPreview" @click="requestPreview">
            <Icon name="eye" size="sm" />{{ previewing ? t('admin.carpool.preview.loading') : t('admin.carpool.preview.action') }}
          </button>
        </div>

        <section v-if="preview" class="border-y border-gray-200 py-4 dark:border-dark-700">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.carpool.preview.title') }}</h4>
            <span class="text-xs text-gray-500">{{ t('admin.carpool.preview.serverCalculated', { date: formatDate(preview.calculated_at) }) }}</span>
          </div>
          <div class="mt-3 overflow-x-auto">
            <table class="min-w-full text-left text-xs">
              <thead class="text-gray-500"><tr><th class="px-2 py-2">{{ t('admin.carpool.columns.cycle') }}</th><th class="px-2 py-2">{{ t('admin.carpool.columns.period') }}</th><th class="px-2 py-2">{{ t('admin.carpool.columns.baseQuotaUsd') }}</th><th class="px-2 py-2">{{ t('admin.carpool.columns.action') }}</th></tr></thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="cycle in preview.cycles" :key="cycle.cycle_no"><td class="px-2 py-2 font-medium">{{ cycle.cycle_no }}</td><td class="whitespace-nowrap px-2 py-2">{{ formatDate(cycle.starts_at) }} - {{ formatDate(cycle.ends_at) }}</td><td class="px-2 py-2 font-mono">${{ money(cycle.base_quota_usd) }}</td><td class="px-2 py-2">{{ previewAction(cycle.initial_action) }}</td></tr></tbody>
            </table>
          </div>
          <p v-for="warning in preview.warnings" :key="warning" class="mt-2 text-xs text-amber-700 dark:text-amber-300">{{ warning }}</p>
        </section>

        <p v-if="submitError" class="text-sm text-red-600 dark:text-red-400">{{ submitError }}</p>
        <div class="flex justify-end gap-2 border-t border-gray-200 pt-4 dark:border-dark-700">
          <button v-if="formMode === 'renew'" type="button" class="btn btn-secondary" @click="formMode = 'view'">{{ t('common.cancel') }}</button>
          <button type="submit" class="btn btn-primary" :disabled="submitting || !canSubmit">
            {{ submitting ? t('common.saving') : formMode === 'renew' ? t('admin.carpool.actions.renew') : t('admin.carpool.actions.open') }}
          </button>
        </div>
      </form>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { formatCarpoolAmount } from '@/utils/carpool'
import type { AdminUser, Group } from '@/types'
import type { CarpoolAdminTerm, CarpoolOpeningMode, CarpoolPaymentInput, CarpoolPlan, CarpoolPreview, CarpoolPreviewRequest, CarpoolTakeoverInput } from '@/types/carpool'

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'success'): void }>()
const { t } = useI18n()
const router = useRouter()
const loading = ref(false)
const loadError = ref(false)
const submitting = ref(false)
const previewing = ref(false)
const submitError = ref('')
const plans = ref<CarpoolPlan[]>([])
const groups = ref<Group[]>([])
const terms = ref<CarpoolAdminTerm[]>([])
const preview = ref<CarpoolPreview | null>(null)
const formMode = ref<'view' | 'open' | 'renew'>('open')
const openingMode = ref<CarpoolOpeningMode>('new')
const selectedPlanId = ref<number | null>(null)
const selectedGroupId = ref<number | null>(null)
const startsAtLocal = ref('')
const notes = ref('')
const recordPayment = ref(false)
let operationKey = ''
let loadGeneration = 0
let previewGeneration = 0
let submitGeneration = 0
const paymentAmountDirty = ref(false)

const takeover = reactive<Record<'current_base_balance_usd' | 'current_boost_balance_usd' | 'current_manual_balance_usd' | 'ordinary_balance_transfer_usd' | 'historical_used_usd' | 'statistics_since', string> & { boost_used: number; history_complete: boolean }>({
  current_base_balance_usd: '0', current_boost_balance_usd: '0', current_manual_balance_usd: '0', ordinary_balance_transfer_usd: '0', historical_used_usd: '0', statistics_since: '', boost_used: 0, history_complete: false,
})
const payment = reactive({ amount_cny: '', paid_at: '', channel: 'manual', external_order_no: '', notes: '' })

const relevantTerm = computed(() => terms.value.find((term) => term.status === 'active' || term.status === 'pending') ?? terms.value[0] ?? null)
const enabledPlans = computed(() => plans.value.filter((plan) => plan.enabled))
const planOptions = computed(() => enabledPlans.value.map((plan) => ({ value: plan.plan_id, label: `${plan.name} · ¥${money(plan.list_price_cny)} · $${money(plan.weekly_quota_usd)}/7d` })))
const groupOptions = computed(() => groups.value.filter((group) => group.subscription_type === 'carpool' && group.platform === 'openai' && group.status === 'active').map((group) => ({ value: group.id, label: group.name })))
const selectedPlan = computed(() => plans.value.find((plan) => plan.plan_id === selectedPlanId.value) ?? null)
const takeoverBoostUsedValid = computed(() => openingMode.value !== 'takeover' || (
  Number.isInteger(takeover.boost_used)
  && takeover.boost_used >= 0
  && takeover.boost_used <= (selectedPlan.value?.boost_count ?? -1)
))
const canPreview = computed(() => !!props.user && !!selectedPlanId.value && takeoverBoostUsedValid.value)
const canSubmit = computed(() => !!props.user && !!selectedPlanId.value && takeoverBoostUsedValid.value && (formMode.value === 'renew' || !!selectedGroupId.value))
const openingModes = computed(() => [{ value: 'new' as const, label: t('admin.carpool.modes.new') }, { value: 'takeover' as const, label: t('admin.carpool.modes.takeover') }])
const takeoverBalanceFields = computed(() => [
  { key: 'current_base_balance_usd' as const, label: t('admin.carpool.takeover.baseBalance') },
  { key: 'current_boost_balance_usd' as const, label: t('admin.carpool.takeover.boostBalance') },
  { key: 'current_manual_balance_usd' as const, label: t('admin.carpool.takeover.manualBalance') },
  { key: 'ordinary_balance_transfer_usd' as const, label: t('admin.carpool.takeover.transfer') },
])

function createKey() { return typeof crypto !== 'undefined' && crypto.randomUUID ? crypto.randomUUID() : `carpool-admin-${Date.now()}-${Math.random().toString(36).slice(2)}` }
function money(value?: string | null) { return formatCarpoolAmount(value) }
function formatDate(value: string) { return new Intl.DateTimeFormat(undefined, { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(value)) }
function localizedValue(namespace: string, value: string) { const key = `admin.carpool.${namespace}.${value}`; const translated = t(key); return translated === key ? value : translated }
function termStatus(status: string) { return localizedValue('status', status) }
function previewAction(action: string) { return localizedValue('preview.initialAction', action) }
function shanghaiInstant(value: string): string | null { return value ? new Date(`${value}:00+08:00`).toISOString() : null }
function paymentInput(): CarpoolPaymentInput | null {
  if (!recordPayment.value) return null
  return { amount_cny: String(payment.amount_cny), payment_kind: 'payment', paid_at: shanghaiInstant(payment.paid_at)!, channel: payment.channel, external_order_no: payment.external_order_no || null, notes: payment.notes || null }
}
function takeoverInput(): CarpoolTakeoverInput | null {
  if (openingMode.value !== 'takeover') return null
  return {
    current_base_balance_usd: String(takeover.current_base_balance_usd),
    current_boost_balance_usd: String(takeover.current_boost_balance_usd),
    current_manual_balance_usd: String(takeover.current_manual_balance_usd),
    ordinary_balance_transfer_usd: takeover.ordinary_balance_transfer_usd === '' ? null : String(takeover.ordinary_balance_transfer_usd),
    boost_used: takeover.boost_used,
    history_complete: takeover.history_complete,
    historical_used_usd: takeover.history_complete ? String(takeover.historical_used_usd) : null,
    statistics_since: takeover.history_complete ? shanghaiInstant(takeover.statistics_since) : null,
  }
}

function resetForm() {
  preview.value = null; submitError.value = ''; formMode.value = relevantTerm.value ? 'view' : 'open'; openingMode.value = 'new'; startsAtLocal.value = ''; notes.value = ''; recordPayment.value = false; operationKey = createKey()
  Object.assign(takeover, { current_base_balance_usd: '0', current_boost_balance_usd: '0', current_manual_balance_usd: '0', ordinary_balance_transfer_usd: '0', historical_used_usd: '0', statistics_since: '', boost_used: 0, history_complete: false })
  Object.assign(payment, { amount_cny: '', paid_at: '', channel: 'manual', external_order_no: '', notes: '' })
  paymentAmountDirty.value = false
  const defaultPlan = enabledPlans.value[0]
  selectedPlanId.value = defaultPlan?.plan_id ?? null
  payment.amount_cny = defaultPlan?.list_price_cny ?? ''
  selectedGroupId.value = groupOptions.value[0]?.value ?? null
}

async function load() {
  if (!props.user) return
  const userId = props.user.id
  const requestGeneration = ++loadGeneration
  terms.value = []
  resetForm()
  loading.value = true; loadError.value = false
  try {
    const [planData, groupData, termData] = await Promise.all([adminAPI.carpool.listPlans(), adminAPI.groups.getAll(), adminAPI.carpool.listTerms({ page: 1, page_size: 20, user_id: userId })])
    if (requestGeneration !== loadGeneration || !props.show || props.user?.id !== userId) return
    plans.value = planData; groups.value = groupData; terms.value = termData.items.sort((a, b) => Date.parse(b.starts_at) - Date.parse(a.starts_at)); resetForm()
  } catch (error) { if (requestGeneration === loadGeneration) { loadError.value = true; console.error('Failed to load user carpool:', error) } } finally { if (requestGeneration === loadGeneration) loading.value = false }
}

function startRenew() { formMode.value = 'renew'; const planCode = relevantTerm.value?.plan_snapshot.code; selectedPlanId.value = enabledPlans.value.find((plan) => plan.code === planCode)?.plan_id ?? null; preview.value = null; submitError.value = ''; operationKey = createKey() }

async function requestPreview() {
  if (!props.user || !selectedPlanId.value) return
  const requestGeneration = ++previewGeneration
  const userId = props.user.id
  const request = buildPreviewRequest()
  if (!request) return
  const fingerprint = JSON.stringify(request)
  previewing.value = true; submitError.value = ''
  try {
    const result = await adminAPI.carpool.previewTerm(userId, request)
    const current = buildPreviewRequest()
    if (requestGeneration !== previewGeneration || !props.show || props.user?.id !== userId || !current || JSON.stringify(current) !== fingerprint) return
    preview.value = result
  }
  catch (error) { if (requestGeneration === previewGeneration && props.user?.id === userId) { submitError.value = errorMessage(error); preview.value = null } } finally { if (requestGeneration === previewGeneration) previewing.value = false }
}

function buildPreviewRequest(): CarpoolPreviewRequest | null {
  if (!selectedPlanId.value) return null
  const renewal = formMode.value === 'renew' && relevantTerm.value
  return { plan_id: selectedPlanId.value, starts_at: renewal ? relevantTerm.value!.expires_at : shanghaiInstant(startsAtLocal.value), mode: renewal ? 'new' : openingMode.value, takeover: renewal ? null : takeoverInput() }
}

async function submit() {
  if (!props.user || !selectedPlanId.value || !canSubmit.value) return
  const requestGeneration = ++submitGeneration
  const userId = props.user.id
  const requestKey = operationKey
  const renewalTermId = formMode.value === 'renew' ? relevantTerm.value?.id : null
  const renewalRequest = renewalTermId ? { plan_id: selectedPlanId.value, notes: notes.value || null, payment: paymentInput() } : null
  const openingRequest = !renewalTermId ? { plan_id: selectedPlanId.value, group_id: selectedGroupId.value!, starts_at: shanghaiInstant(startsAtLocal.value), mode: openingMode.value, takeover: takeoverInput(), notes: notes.value || null, payment: paymentInput() } : null
  submitting.value = true; submitError.value = ''
  try {
    if (renewalTermId && renewalRequest) {
      await adminAPI.carpool.renewTerm(renewalTermId, renewalRequest, requestKey)
    } else {
      await adminAPI.carpool.openTerm(userId, openingRequest!, requestKey)
    }
    if (requestGeneration !== submitGeneration || !props.show || props.user?.id !== userId) return
    emit('success'); await load()
  } catch (error) { if (requestGeneration === submitGeneration && props.show && props.user?.id === userId) submitError.value = errorMessage(error) } finally { if (requestGeneration === submitGeneration) submitting.value = false }
}

function errorMessage(error: unknown): string { return typeof error === 'object' && error !== null && 'message' in error ? String(error.message) : t('admin.carpool.saveFailed') }

watch(selectedPlan, (plan) => { if (plan && !paymentAmountDirty.value) payment.amount_cny = plan.list_price_cny })
watch([() => props.show, () => props.user?.id], ([visible]) => { previewGeneration += 1; submitGeneration += 1; previewing.value = false; submitting.value = false; preview.value = null; if (visible) void load(); else loadGeneration += 1 }, { immediate: true })
</script>
