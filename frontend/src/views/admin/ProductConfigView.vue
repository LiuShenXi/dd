<template>
  <AppLayout>
    <div class="mx-auto max-w-5xl space-y-6">
      <div
        class="flex items-start gap-3 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200"
        role="note"
      >
        <Icon name="exclamationTriangle" size="md" class="mt-0.5 shrink-0" />
        <p>{{ t('admin.productConfig.realConfigNotice') }}</p>
      </div>

      <div v-if="loading" class="py-16 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <div v-else-if="loadError" class="py-16 text-center">
        <p class="mb-4 text-sm text-red-600">{{ t('admin.productConfig.loadFailed') }}</p>
        <button type="button" class="btn btn-secondary" @click="loadAll">{{ t('common.refresh') }}</button>
      </div>

      <template v-else>
        <!-- ==================== GPT 拼车 ==================== -->
        <section class="card p-6" aria-labelledby="pc-gpt-title" data-testid="product-config-gpt">
          <header class="mb-5 flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 id="pc-gpt-title" class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('home.products.gpt.name') }}
              </h2>
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('admin.productConfig.gptHint') }}</p>
            </div>
            <div class="flex items-center gap-4">
              <ToggleSwitch
                :label="t('admin.productConfig.showOnHome')"
                :checked="display.gpt_carpool.enabled"
                @toggle="display.gpt_carpool.enabled = !display.gpt_carpool.enabled"
              />
              <div class="w-36">
                <label class="input-label" for="pc-gpt-status">{{ t('admin.productConfig.status') }}</label>
                <select id="pc-gpt-status" v-model="display.gpt_carpool.status" class="input">
                  <option v-for="s in statusOptions" :key="s.value" :value="s.value">{{ s.label }}</option>
                </select>
              </div>
            </div>
          </header>

          <div class="overflow-x-auto rounded-xl border border-gray-100 dark:border-dark-700">
            <table class="w-full min-w-[720px] text-left text-sm">
              <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-900/60 dark:text-dark-400">
                <tr>
                  <th scope="col" class="px-3 py-2 font-medium">{{ t('admin.productConfig.tier') }}</th>
                  <th scope="col" class="px-3 py-2 font-medium">{{ t('admin.productConfig.priceCny') }}</th>
                  <th scope="col" class="px-3 py-2 font-medium">{{ t('admin.productConfig.weeklyQuotaUsd') }}</th>
                  <th scope="col" class="px-3 py-2 font-medium">{{ t('admin.productConfig.boost') }}</th>
                  <th scope="col" class="px-3 py-2 font-medium">{{ t('admin.productConfig.onSale') }}</th>
                  <th scope="col" class="px-3 py-2 font-medium">{{ t('admin.productConfig.version') }}</th>
                  <th scope="col" class="px-3 py-2"><span class="sr-only">{{ t('common.actions') }}</span></th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                <tr v-for="row in carpoolRows" :key="row.plan.plan_id" :data-testid="`pc-tier-${row.plan.code}`">
                  <td class="px-3 py-2">
                    <div class="font-medium text-gray-900 dark:text-white">{{ tierLabel(row.plan) }}</div>
                    <div class="font-mono text-xs text-gray-400">{{ row.plan.code }}</div>
                  </td>
                  <td class="px-3 py-2">
                    <input v-model="row.form.list_price_cny" type="text" inputmode="decimal" class="input w-28" :aria-label="`${tierLabel(row.plan)} ${t('admin.productConfig.priceCny')}`" />
                  </td>
                  <td class="px-3 py-2">
                    <input v-model="row.form.weekly_quota_usd" type="text" inputmode="decimal" class="input w-28" :aria-label="`${tierLabel(row.plan)} ${t('admin.productConfig.weeklyQuotaUsd')}`" />
                  </td>
                  <td class="px-3 py-2">
                    <div class="flex items-center gap-1">
                      <input v-model.number="row.form.boost_count" type="number" min="0" max="3" class="input w-16" :aria-label="`${tierLabel(row.plan)} ${t('admin.productConfig.boostCount')}`" />
                      <span class="text-xs text-gray-400">×</span>
                      <input v-model="row.form.boost_ratio_percent" type="text" inputmode="decimal" class="input w-16" :aria-label="`${tierLabel(row.plan)} ${t('admin.productConfig.boostRatio')}`" />
                      <span class="text-xs text-gray-400">%</span>
                    </div>
                  </td>
                  <td class="px-3 py-2">
                    <input v-model="row.form.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" :aria-label="`${tierLabel(row.plan)} ${t('admin.productConfig.onSale')}`" />
                  </td>
                  <td class="px-3 py-2 text-xs text-gray-500">v{{ row.plan.version }}</td>
                  <td class="px-3 py-2 text-right">
                    <button
                      type="button"
                      class="btn btn-primary btn-sm"
                      :disabled="!rowDirty(row) || row.saving"
                      @click="saveCarpoolRow(row)"
                    >
                      {{ row.saving ? t('common.saving') : t('common.save') }}
                    </button>
                  </td>
                </tr>
                <tr v-if="carpoolRows.length === 0">
                  <td colspan="7" class="px-3 py-6 text-center text-sm text-gray-400">{{ t('admin.productConfig.noCarpoolPlans') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.productConfig.carpoolVersionHint') }}</p>

          <RulesEditor v-model="display.gpt_carpool.rules" class="mt-5" :placeholder="t('admin.productConfig.rulesPlaceholderGpt')" />
        </section>

        <!-- ==================== Claude 周卡 ==================== -->
        <section class="card p-6" aria-labelledby="pc-claude-title" data-testid="product-config-claude">
          <header class="mb-5 flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 id="pc-claude-title" class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('home.products.claudeWeekly.name') }}
              </h2>
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('admin.productConfig.claudeHint') }}</p>
            </div>
            <div class="flex items-center gap-4">
              <ToggleSwitch
                :label="t('admin.productConfig.showOnHome')"
                :checked="display.claude_weekly.enabled"
                @toggle="display.claude_weekly.enabled = !display.claude_weekly.enabled"
              />
              <div class="w-36">
                <label class="input-label" for="pc-claude-status">{{ t('admin.productConfig.status') }}</label>
                <select id="pc-claude-status" v-model="display.claude_weekly.status" class="input">
                  <option v-for="s in statusOptions" :key="s.value" :value="s.value">{{ s.label }}</option>
                </select>
              </div>
            </div>
          </header>

          <!-- 尚无周卡套餐：在 term 分组下一键创建 -->
          <div v-if="termPlans.length === 0" class="rounded-xl border border-dashed border-gray-300 p-5 dark:border-dark-600">
            <p class="mb-4 text-sm text-gray-600 dark:text-dark-300">{{ t('admin.productConfig.noClaudePlan') }}</p>
            <div class="grid gap-4 sm:grid-cols-4">
              <div class="sm:col-span-2">
                <label class="input-label" for="pc-new-group">{{ t('admin.productConfig.group') }}</label>
                <select id="pc-new-group" v-model.number="newPlan.group_id" class="input">
                  <option :value="0" disabled>{{ t('admin.productConfig.selectGroup') }}</option>
                  <option v-for="g in termGroups" :key="g.id" :value="g.id">{{ g.name }}</option>
                </select>
              </div>
              <div>
                <label class="input-label" for="pc-new-price">{{ t('admin.productConfig.priceCny') }}</label>
                <input id="pc-new-price" v-model="newPlan.price" type="text" inputmode="decimal" class="input" />
              </div>
              <div>
                <label class="input-label" for="pc-new-quota">{{ t('admin.productConfig.quotaUsd') }}</label>
                <input id="pc-new-quota" v-model="newPlan.quota" type="text" inputmode="decimal" class="input" />
              </div>
            </div>
            <p v-if="termGroups.length === 0" class="mt-3 text-xs text-red-600">{{ t('admin.productConfig.noTermGroup') }}</p>
            <div class="mt-4 flex justify-end">
              <button type="button" class="btn btn-primary" :disabled="!newPlanValid || creatingPlan" @click="createClaudePlan">
                {{ creatingPlan ? t('common.saving') : t('admin.productConfig.createPlan') }}
              </button>
            </div>
          </div>

          <template v-else>
            <div class="mb-4 max-w-md">
              <label class="input-label" for="pc-claude-plan">{{ t('admin.productConfig.linkedPlan') }}</label>
              <select id="pc-claude-plan" v-model.number="display.claude_weekly.plan_id" class="input" @change="syncClaudeForm">
                <option v-for="p in termPlans" :key="p.id" :value="p.id">#{{ p.id }} {{ p.name }}</option>
              </select>
            </div>

            <div v-if="linkedPlan" class="grid gap-4 sm:grid-cols-4">
              <div>
                <label class="input-label" for="pc-claude-price">{{ t('admin.productConfig.priceCny') }}</label>
                <input id="pc-claude-price" v-model="claudeForm.price" type="text" inputmode="decimal" class="input" />
              </div>
              <div>
                <label class="input-label" for="pc-claude-quota">{{ t('admin.productConfig.quotaUsd') }}</label>
                <input id="pc-claude-quota" v-model="claudeForm.quota" type="text" inputmode="decimal" class="input" />
              </div>
              <div>
                <label class="input-label" for="pc-claude-rate">{{ t('admin.productConfig.rateMultiplier') }}</label>
                <input id="pc-claude-rate" v-model="claudeForm.rate" type="text" inputmode="decimal" class="input" />
              </div>
              <div>
                <span class="input-label">{{ t('admin.productConfig.duration') }}</span>
                <p class="py-2 text-sm text-gray-700 dark:text-dark-200">{{ t('admin.productConfig.durationFixed') }}</p>
              </div>
            </div>
            <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.productConfig.claudeApplyHint', { group: linkedGroup?.name || '-' }) }}
            </p>
            <div class="mt-4 flex justify-end">
              <button
                type="button"
                class="btn btn-primary"
                :disabled="!claudeDirty || savingClaude"
                data-testid="pc-claude-save"
                @click="saveClaude"
              >
                {{ savingClaude ? t('common.saving') : t('admin.productConfig.saveClaude') }}
              </button>
            </div>
          </template>

          <RulesEditor v-model="display.claude_weekly.rules" class="mt-5" :placeholder="t('admin.productConfig.rulesPlaceholderClaude')" />
        </section>

        <!-- ==================== 展示配置 ==================== -->
        <section class="card p-6" aria-labelledby="pc-display-title">
          <h2 id="pc-display-title" class="mb-4 text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('admin.productConfig.displayTitle') }}
          </h2>
          <label class="input-label" for="pc-contact">{{ t('admin.productConfig.contact') }}</label>
          <input id="pc-contact" v-model="display.contact" type="text" maxlength="200" class="input" :placeholder="t('admin.productConfig.contactPlaceholder')" />
          <p class="input-hint">{{ t('admin.productConfig.displayHint') }}</p>
          <div class="mt-4 flex justify-end gap-2">
            <router-link to="/home" target="_blank" class="btn btn-secondary">{{ t('admin.productConfig.previewHome') }}</router-link>
            <button type="button" class="btn btn-primary" :disabled="savingDisplay" data-testid="pc-display-save" @click="saveDisplay">
              {{ savingDisplay ? t('common.saving') : t('admin.productConfig.saveDisplay') }}
            </button>
          </div>
        </section>
      </template>
    </div>

    <ConfirmDialog
      :show="!!confirmState"
      :title="t('admin.productConfig.confirmTitle')"
      :message="confirmState?.message || ''"
      :confirm-text="t('common.confirm')"
      @confirm="runConfirmed"
      @cancel="confirmState = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ToggleSwitch from '@/components/payment/ToggleSwitch.vue'
import RulesEditor from '@/components/admin/product/RulesEditor.vue'
import adminAPI from '@/api/admin'
import { adminPaymentAPI } from '@/api/admin/payment'
import { getHomeProductsSettings, updateHomeProductsSettings, type HomeProductsSettings, type HomeProductStatus } from '@/api/homeProducts'
import { useAppStore } from '@/stores/app'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { formatQuotaUSD, validQuotaInput } from '@/utils/claudeQuota'
import type { CarpoolPlan } from '@/types/carpool'
import type { SubscriptionPlan } from '@/types/payment'
import type { AdminGroup } from '@/types'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const loadError = ref(false)

const statusOptions = computed(() =>
  (['available', 'pilot', 'soon', 'sold_out'] as HomeProductStatus[]).map((value) => ({
    value,
    label: t(`home.products.status.${value}`)
  }))
)

// ==================== Display settings ====================

const display = reactive<HomeProductsSettings>({
  gpt_carpool: { enabled: true, status: 'available', rules: [] },
  claude_weekly: { enabled: true, status: 'pilot', plan_id: 0, rules: [] },
  contact: ''
})
const savingDisplay = ref(false)

async function saveDisplay() {
  savingDisplay.value = true
  try {
    Object.assign(display, await updateHomeProductsSettings(JSON.parse(JSON.stringify(display))))
    appStore.showSuccess(t('admin.productConfig.saved'))
  } catch (err) {
    appStore.showError(extractI18nErrorMessage(err, t, 'admin.productConfig.errors', t('common.error')))
  } finally {
    savingDisplay.value = false
  }
}

// ==================== GPT carpool plans ====================

interface CarpoolRow {
  plan: CarpoolPlan
  form: { list_price_cny: string; weekly_quota_usd: string; boost_count: number; boost_ratio_percent: string; enabled: boolean }
  saving: boolean
}
const carpoolRows = ref<CarpoolRow[]>([])
const SEATS: Record<string, number> = { two_seat: 2, three_seat: 3, four_seat: 4 }

function tierLabel(plan: CarpoolPlan) {
  const seats = SEATS[plan.code]
  return seats ? t('home.products.seats', { n: seats }) : plan.name
}

function trimDecimal(value: string | number) {
  const n = Number(value)
  return Number.isFinite(n) ? String(Number(n.toFixed(8))) : String(value)
}

function carpoolForm(plan: CarpoolPlan): CarpoolRow['form'] {
  return {
    list_price_cny: trimDecimal(plan.list_price_cny),
    weekly_quota_usd: trimDecimal(plan.weekly_quota_usd),
    boost_count: plan.boost_count,
    boost_ratio_percent: trimDecimal(Number(plan.boost_ratio) * 100),
    enabled: plan.enabled
  }
}

function rowDirty(row: CarpoolRow) {
  return JSON.stringify(row.form) !== JSON.stringify(carpoolForm(row.plan))
}

function validMoney(value: string, allowZero = false) {
  if (!/^\d+(\.\d{1,8})?$/.test(value.trim())) return false
  return allowZero ? Number(value) >= 0 : Number(value) > 0
}

function newKey() {
  return typeof crypto !== 'undefined' && crypto.randomUUID ? crypto.randomUUID() : `pc-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function saveCarpoolRow(row: CarpoolRow) {
  const f = row.form
  const ratio = Number(f.boost_ratio_percent) / 100
  if (!validMoney(f.list_price_cny, true) || !validMoney(f.weekly_quota_usd) || !Number.isInteger(f.boost_count) || f.boost_count < 0 || f.boost_count > 3 || !(ratio >= 0 && ratio <= 1)) {
    appStore.showError(t('admin.productConfig.invalidCarpool'))
    return
  }
  askConfirm(
    t('admin.productConfig.confirmCarpool', {
      tier: tierLabel(row.plan),
      price: f.list_price_cny,
      quota: f.weekly_quota_usd,
      version: row.plan.version + 1
    }),
    async () => {
      row.saving = true
      try {
        const next = await adminAPI.carpool.createPlanVersion(
          row.plan.plan_id,
          {
            name: row.plan.name,
            list_price_cny: f.list_price_cny.trim(),
            weekly_quota_usd: f.weekly_quota_usd.trim(),
            boost_ratio: ratio.toFixed(8),
            boost_count: f.boost_count,
            enabled: f.enabled
          },
          newKey()
        )
        row.plan = next
        row.form = carpoolForm(next)
        appStore.showSuccess(t('admin.productConfig.saved'))
      } catch (err) {
        appStore.showError(extractI18nErrorMessage(err, t, 'admin.carpool.errors', t('common.error')))
      } finally {
        row.saving = false
      }
    }
  )
}

// ==================== Claude weekly plan ====================

const groups = ref<AdminGroup[]>([])
const subscriptionPlans = ref<SubscriptionPlan[]>([])
const termGroups = computed(() => groups.value.filter((g) => g.subscription_quota_mode === 'term' && g.platform === 'anthropic'))
const termPlans = computed(() => subscriptionPlans.value.filter((p) => p.quota_mode === 'term'))
const linkedPlan = computed(() => termPlans.value.find((p) => p.id === display.claude_weekly.plan_id))
const linkedGroup = computed(() => groups.value.find((g) => g.id === linkedPlan.value?.group_id))

const claudeForm = reactive({ price: '', quota: '', rate: '' })
const savingClaude = ref(false)

function claudeBaseline() {
  return {
    price: linkedPlan.value ? trimDecimal(linkedPlan.value.price) : '',
    quota: linkedPlan.value?.default_quota_usd ? trimDecimal(linkedPlan.value.default_quota_usd) : '',
    rate: linkedGroup.value ? trimDecimal(linkedGroup.value.rate_multiplier) : ''
  }
}
function syncClaudeForm() {
  Object.assign(claudeForm, claudeBaseline())
}
const claudeDirty = computed(() => JSON.stringify({ ...claudeForm }) !== JSON.stringify(claudeBaseline()))

function saveClaude() {
  const plan = linkedPlan.value
  const group = linkedGroup.value
  if (!plan || !group) return
  if (!validMoney(claudeForm.price, true) || !validQuotaInput(claudeForm.quota) || !validMoney(claudeForm.rate)) {
    appStore.showError(t('admin.productConfig.invalidClaude'))
    return
  }
  const baseline = claudeBaseline()
  askConfirm(
    t('admin.productConfig.confirmClaude', {
      price: claudeForm.price,
      quota: claudeForm.quota,
      rate: claudeForm.rate,
      group: group.name
    }),
    async () => {
      savingClaude.value = true
      try {
        if (claudeForm.price !== baseline.price || claudeForm.quota !== baseline.quota) {
          const res = await adminPaymentAPI.updatePlan(plan.id, {
            price: Number(claudeForm.price),
            default_quota_usd: claudeForm.quota.trim()
          })
          Object.assign(plan, res.data)
        }
        if (claudeForm.rate !== baseline.rate) {
          const updated = await adminAPI.groups.update(group.id, { rate_multiplier: Number(claudeForm.rate) })
          Object.assign(group, updated)
        }
        syncClaudeForm()
        appStore.showSuccess(t('admin.productConfig.saved'))
      } catch (err) {
        appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
      } finally {
        savingClaude.value = false
      }
    }
  )
}

// 首次使用：在 term 分组下创建周卡套餐（不上架，只作为开通/展示的价格与额度来源）
const newPlan = reactive({ group_id: 0, price: '100', quota: '150' })
const creatingPlan = ref(false)
const newPlanValid = computed(() => newPlan.group_id > 0 && validMoney(newPlan.price, true) && validQuotaInput(newPlan.quota))

async function createClaudePlan() {
  if (!newPlanValid.value) return
  creatingPlan.value = true
  try {
    const res = await adminPaymentAPI.createPlan({
      group_id: newPlan.group_id,
      name: t('home.products.claudeWeekly.name'),
      description: '',
      price: Number(newPlan.price),
      validity_days: 7,
      validity_unit: 'days',
      default_quota_usd: newPlan.quota.trim(),
      for_sale: false,
      sort_order: 0,
      features: ''
    })
    await loadClaude()
    display.claude_weekly.plan_id = res.data.id
    syncClaudeForm()
    await saveDisplay()
  } catch (err) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    creatingPlan.value = false
  }
}

async function loadClaude() {
  const [groupList, plans] = await Promise.all([adminAPI.groups.getAll(), adminPaymentAPI.getPlans()])
  groups.value = groupList
  subscriptionPlans.value = plans.data || []
  if (newPlan.group_id === 0 && termGroups.value.length > 0) newPlan.group_id = termGroups.value[0].id
}

// ==================== Confirm + load ====================

const confirmState = ref<{ message: string; run: () => Promise<void> } | null>(null)
function askConfirm(message: string, run: () => Promise<void>) {
  confirmState.value = { message, run }
}
async function runConfirmed() {
  const state = confirmState.value
  confirmState.value = null
  if (state) await state.run()
}

async function loadAll() {
  loading.value = true
  loadError.value = false
  try {
    const [settings, plans] = await Promise.all([getHomeProductsSettings(), adminAPI.carpool.listPlans(), loadClaude()])
    Object.assign(display, settings)
    carpoolRows.value = plans
      .slice()
      .sort((a, b) => Number(a.list_price_cny) - Number(b.list_price_cny))
      .map((plan) => ({ plan, form: carpoolForm(plan), saving: false }))
    if (!linkedPlan.value && termPlans.value.length > 0) display.claude_weekly.plan_id = termPlans.value[0].id
    syncClaudeForm()
  } catch {
    loadError.value = true
  } finally {
    loading.value = false
  }
}

onMounted(loadAll)

defineExpose({ formatQuotaUSD })
</script>
