<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div><h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.carpool.title') }}</h1><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.carpool.description') }}</p></div>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary" @click="loadActive"><Icon name="refresh" size="sm" />{{ t('carpool.refresh') }}</button>
          <button type="button" class="btn btn-primary" @click="router.push('/admin/users')"><Icon name="plus" size="sm" />{{ t('admin.carpool.actions.open') }}</button>
        </div>
      </div>

      <div class="overflow-x-auto border-b border-gray-200 dark:border-dark-700">
        <div class="flex min-w-max gap-5" role="tablist">
          <button v-for="tab in tabs" :key="tab.key" type="button" role="tab" :aria-selected="activeTab === tab.key" class="border-b-2 px-1 py-3 text-sm font-medium" :class="activeTab === tab.key ? 'border-primary-500 text-primary-700 dark:text-primary-300' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'" @click="switchTab(tab.key)">{{ tab.label }}</button>
        </div>
      </div>

      <div v-if="activeTab !== 'users' && activeTab !== 'plans' && activeTab !== 'resets'" class="flex flex-wrap items-end gap-3">
        <div class="w-36"><label class="input-label">{{ t('admin.carpool.filters.searchUser') }}</label><input v-model.number="filters.user_id" type="number" min="1" class="input" /></div>
        <div v-if="activeTab !== 'ledger'" class="w-44"><label class="input-label">{{ t('admin.carpool.columns.status') }}</label><Select v-model="filters.status" :options="statusOptions" /></div>
        <button type="button" class="btn btn-secondary" @click="applyFilters">{{ t('admin.carpool.filters.apply') }}</button>
        <button type="button" class="btn btn-ghost" @click="clearFilters">{{ t('admin.carpool.filters.clear') }}</button>
      </div>

      <div v-if="loading" class="flex min-h-56 items-center justify-center"><LoadingSpinner /></div>
      <div v-else-if="loadError" class="flex min-h-56 flex-col items-center justify-center border-y border-gray-200 dark:border-dark-700"><p class="text-sm text-red-600 dark:text-red-400">{{ t('admin.carpool.loadFailed') }}</p><button class="btn btn-secondary mt-4" @click="loadActive">{{ t('carpool.retry') }}</button></div>

      <div v-else-if="activeTab === 'users'" class="overflow-x-auto">
        <table class="min-w-[1100px] w-full text-left text-sm">
          <thead class="table-head"><tr><th>{{ t('admin.carpool.columns.user') }}</th><th>{{ t('admin.carpool.columns.username') }}</th><th>{{ t('admin.carpool.columns.createdAt') }}</th><th>{{ t('admin.carpool.columns.defaultPeriod') }}</th><th>{{ t('admin.carpool.columns.mapping') }}</th><th>{{ t('admin.carpool.columns.plan') }}</th><th>{{ t('admin.carpool.columns.carpoolAvailable') }}</th><th>{{ t('admin.carpool.columns.ordinaryBalance') }}</th></tr></thead>
          <tbody class="table-body"><tr v-for="user in users" :key="user.id"><td><p class="font-medium text-gray-900 dark:text-white">{{ user.email }}</p><p class="text-xs text-gray-500">#{{ user.id }}</p></td><td>{{ user.username || '-' }}</td><td class="whitespace-nowrap text-xs">{{ dateSeconds(user.created_at) }}</td><td class="whitespace-nowrap text-xs">{{ date(user.created_at) }}<br>{{ date(termForUser(user.id)?.expires_at ?? defaultExpiry(user.created_at)) }}</td><td><span class="status-pill" :class="termForUser(user.id) ? 'text-emerald-700 dark:text-emerald-300' : ''">{{ termForUser(user.id) ? t('admin.carpool.mapping.unverified') : t('admin.carpool.mapping.pending') }}</span></td><td>{{ termForUser(user.id)?.plan_snapshot.name ?? '-' }}</td><td class="font-mono">${{ money(termForUser(user.id)?.current_cycle?.available_usd) }}</td><td class="font-mono">${{ money(String(user.balance)) }}</td></tr></tbody>
        </table>
        <EmptyState v-if="users.length === 0" :title="t('admin.carpool.emptyUsers')" />
      </div>

      <div v-else-if="activeTab === 'terms'" class="overflow-x-auto">
        <table class="min-w-[1120px] w-full text-left text-sm">
          <thead class="border-y border-gray-200 bg-gray-50 text-xs text-gray-500 dark:border-dark-700 dark:bg-dark-800"><tr><th class="px-3 py-3"></th><th class="px-3 py-3">{{ t('admin.carpool.columns.user') }}</th><th class="px-3 py-3">{{ t('admin.carpool.columns.plan') }}</th><th class="px-3 py-3">{{ t('admin.carpool.columns.period') }}</th><th class="px-3 py-3">{{ t('admin.carpool.columns.status') }}</th><th class="px-3 py-3">{{ t('admin.carpool.columns.currentCycle') }}</th><th class="px-3 py-3">{{ t('admin.carpool.columns.availableUsd') }}</th><th class="px-3 py-3">{{ t('admin.carpool.columns.boosts') }}</th><th class="px-3 py-3">{{ t('admin.carpool.columns.netPaidCny') }}</th><th class="px-3 py-3">{{ t('admin.carpool.columns.action') }}</th></tr></thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <template v-for="term in terms" :key="term.id">
              <tr class="hover:bg-gray-50 dark:hover:bg-dark-800/60"><td class="px-3 py-3"><button class="rounded p-1 hover:bg-gray-100 dark:hover:bg-dark-700" :aria-label="expandedTerms.has(term.id) ? t('admin.carpool.actions.collapse') : t('admin.carpool.actions.expand')" @click="toggleTerm(term)"><Icon :name="expandedTerms.has(term.id) ? 'chevronDown' : 'chevronRight'" size="sm" /></button></td><td class="px-3 py-3"><p class="font-medium text-gray-900 dark:text-white">{{ userLabel(term.user_id) }}</p><p class="text-xs text-gray-500">#{{ term.user_id }}</p></td><td class="px-3 py-3">{{ term.plan_snapshot.name }}</td><td class="whitespace-nowrap px-3 py-3 text-xs">{{ date(term.starts_at) }}<br>{{ date(term.expires_at) }}</td><td class="px-3 py-3"><span class="status-pill">{{ status(term.status) }}</span></td><td class="px-3 py-3 font-mono">{{ term.current_cycle?.cycle_no ?? '-' }}</td><td class="px-3 py-3 font-mono">${{ money(term.current_cycle?.available_usd) }}</td><td class="px-3 py-3 font-mono">{{ term.boost_remaining }}/{{ term.plan_snapshot.boost_count }}</td><td class="px-3 py-3 font-mono">¥{{ money(term.payment_net_cny) }}</td><td class="px-3 py-3"><div class="flex flex-wrap gap-1"><button v-if="term.status === 'active' && term.plan_snapshot.duration_days === 28 && !term.plan_snapshot.code.startsWith('legacy_')" class="table-action" @click="openRenew(term)">{{ t('admin.carpool.actions.renew') }}</button><button class="table-action" @click="openPayment(term)">{{ t('admin.carpool.actions.payment') }}</button><button v-if="term.status === 'active' || term.status === 'pending'" class="table-action text-red-600 dark:text-red-400" @click="openTerminate(term)">{{ t('admin.carpool.actions.terminate') }}</button></div></td></tr>
              <tr v-if="expandedTerms.has(term.id)"><td colspan="10" class="bg-gray-50 px-8 py-4 dark:bg-dark-800/50"><p v-if="cycleLoading.has(term.id)" class="text-xs text-gray-500">{{ t('common.loading') }}</p><div v-else class="grid gap-2 sm:grid-cols-2 lg:grid-cols-5"><div v-for="cycle in termCycles[term.id] ?? []" :key="cycle.id" class="rounded border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-800"><div class="flex justify-between"><span class="font-medium">{{ t('admin.carpool.columns.cycle') }} {{ cycle.cycle_no }}</span><span class="text-xs text-gray-500">{{ status(cycle.state) }}</span></div><p class="mt-2 font-mono text-sm">${{ money(cycle.available_usd) }}</p><p class="mt-1 text-[11px] text-gray-500">{{ date(cycle.starts_at) }} - {{ date(cycle.ends_at) }}</p></div></div></td></tr>
            </template>
          </tbody>
        </table>
        <EmptyState v-if="terms.length === 0" :title="t('admin.carpool.empty')" />
      </div>

      <div v-else-if="activeTab === 'cycles'" class="overflow-x-auto">
        <table class="min-w-[1680px] w-full text-left text-sm"><thead class="table-head"><tr><th>{{ t('admin.carpool.columns.user') }}</th><th>{{ t('admin.carpool.columns.cycle') }}</th><th>{{ t('admin.carpool.columns.period') }}</th><th>{{ t('admin.carpool.columns.baseQuotaUsd') }}</th><th>{{ t('admin.carpool.columns.initialGrantUsd') }}</th><th>{{ t('admin.carpool.columns.resetUsd') }}</th><th>{{ t('admin.carpool.columns.boostUsd') }}</th><th>{{ t('admin.carpool.columns.adjustmentNetUsd') }}</th><th>{{ t('admin.carpool.columns.usedUsd') }}</th><th>{{ t('admin.carpool.columns.expiredUsd') }}</th><th>{{ t('admin.carpool.columns.baseBalanceUsd') }}</th><th>{{ t('admin.carpool.columns.boostBalanceUsd') }}</th><th>{{ t('admin.carpool.columns.manualBalanceUsd') }}</th><th>{{ t('admin.carpool.columns.availableUsd') }}</th><th>{{ t('admin.carpool.columns.status') }}</th><th>{{ t('admin.carpool.columns.action') }}</th></tr></thead><tbody class="table-body"><tr v-for="cycle in cycles" :key="cycle.id"><td><p>{{ userLabel(cycle.user_id) }}</p><p class="text-xs text-gray-500">#{{ cycle.user_id }}</p></td><td>#{{ cycle.term_id }} / {{ cycle.cycle_no }}</td><td class="whitespace-nowrap text-xs">{{ date(cycle.starts_at) }}<br>{{ date(cycle.ends_at) }}</td><td class="font-mono">${{ money(cycle.base_quota_usd) }}</td><td class="font-mono">${{ money(cycle.initial_granted_usd) }}</td><td class="font-mono">${{ money(cycle.reset_granted_usd) }}</td><td class="font-mono">${{ money(cycle.boost_granted_usd) }}</td><td class="font-mono">${{ money(cycle.adjustment_net_usd) }}</td><td class="font-mono">${{ money(cycle.used_usd) }}</td><td class="font-mono">${{ money(cycle.expired_usd) }}</td><td class="font-mono">${{ money(cycle.base_balance_usd) }}</td><td class="font-mono">${{ money(cycle.boost_balance_usd) }}</td><td class="font-mono">${{ money(cycle.manual_balance_usd) }}</td><td class="font-mono">${{ money(cycle.available_usd) }}</td><td><span class="status-pill">{{ status(cycle.state) }}</span></td><td><button class="table-action" @click="openAdjustment(cycle)">{{ t('admin.carpool.actions.adjust') }}</button></td></tr></tbody></table>
      </div>

      <div v-else-if="activeTab === 'ledger'" class="overflow-x-auto">
        <table class="min-w-[1120px] w-full text-left text-sm"><thead class="table-head"><tr><th>{{ t('admin.carpool.columns.user') }}</th><th>{{ t('admin.carpool.columns.cycle') }}</th><th>{{ t('admin.carpool.columns.event') }}</th><th>{{ t('admin.carpool.columns.bucket') }}</th><th>{{ t('admin.carpool.columns.deltaUsd') }}</th><th>{{ t('admin.carpool.columns.reference') }}</th><th>{{ t('admin.carpool.columns.actor') }}</th><th>{{ t('admin.carpool.columns.reason') }}</th><th>{{ t('admin.carpool.columns.time') }}</th><th>{{ t('admin.carpool.columns.action') }}</th></tr></thead><tbody class="table-body"><tr v-for="entry in ledger" :key="entry.id"><td><p>{{ userLabel(entry.user_id) }}</p><p class="text-xs text-gray-500">#{{ entry.user_id }}</p></td><td>#{{ entry.term_id }} / #{{ entry.cycle_id }}</td><td>{{ ledgerEvent(entry.event_type) }}</td><td>{{ ledgerBucket(entry.bucket) }}</td><td class="font-mono" :class="Number(entry.delta_usd) < 0 ? 'text-red-600' : 'text-emerald-600'">{{ Number(entry.delta_usd) > 0 ? '+' : '' }}${{ money(entry.delta_usd) }}</td><td class="max-w-64 truncate font-mono text-xs">{{ ledgerReference(entry) }}</td><td>{{ entry.actor_id ? `#${entry.actor_id}` : '-' }}</td><td class="max-w-52 truncate">{{ entry.reason || '-' }}</td><td class="whitespace-nowrap text-xs">{{ date(entry.effective_at) }}</td><td><button class="table-action" @click="openReversal(entry)">{{ t('admin.carpool.actions.reverse') }}</button></td></tr></tbody></table>
      </div>

      <div v-else-if="activeTab === 'exceptions'" class="overflow-x-auto">
        <table class="min-w-[1160px] w-full text-left text-sm"><thead class="table-head"><tr><th>ID</th><th>{{ t('admin.carpool.columns.user') }}</th><th>{{ t('admin.carpool.columns.reference') }}</th><th>{{ t('admin.carpool.columns.cycle') }}</th><th>{{ t('admin.carpool.columns.status') }}</th><th>{{ t('admin.carpool.exceptions.retries') }}</th><th>{{ t('admin.carpool.exceptions.diagnostic') }}</th><th>{{ t('admin.carpool.columns.time') }}</th><th>{{ t('admin.carpool.columns.action') }}</th></tr></thead><tbody class="table-body"><tr v-for="item in billingExceptions" :key="item.id"><td>#{{ item.id }}</td><td>#{{ item.user_id }}</td><td class="max-w-64 truncate font-mono text-xs">{{ item.request_id }}</td><td>#{{ item.term_id }} / #{{ item.cycle_id }}</td><td><span class="status-pill">{{ status(item.status) }}</span></td><td>{{ item.retry_count }}</td><td class="max-w-72 truncate">{{ item.sanitized_error || '-' }}</td><td class="whitespace-nowrap text-xs">{{ dateSeconds(item.admitted_at) }}</td><td><button v-if="item.status === 'reconcile_required'" class="table-action" @click="openReconcile(item)">{{ t('admin.carpool.exceptions.reconcile') }}</button></td></tr></tbody></table>
        <EmptyState v-if="billingExceptions.length === 0" :title="t('admin.carpool.exceptions.empty')" />
      </div>

      <div v-else-if="activeTab === 'resets'" class="space-y-6">
        <div class="flex flex-wrap gap-2"><button class="btn btn-primary" @click="openResetRegister"><Icon name="plus" size="sm" />{{ t('admin.carpool.reset.register') }}</button><button class="btn btn-secondary" :disabled="scanning" @click="scanObservations"><Icon name="refresh" size="sm" />{{ t('admin.carpool.reset.scan') }}</button></div>
        <p v-if="scanResult" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t('admin.carpool.reset.scanResult', { scanned: scanResult.scanned, complete: scanResult.complete, incomplete: scanResult.incomplete, candidates: scanResult.new_candidates }) }}</p>
        <div class="overflow-x-auto"><table class="min-w-[980px] w-full text-left text-sm"><thead class="table-head"><tr><th>ID</th><th>{{ t('admin.carpool.columns.status') }}</th><th>{{ t('admin.carpool.columns.time') }}</th><th>{{ t('admin.carpool.reset.revision') }}</th><th>{{ t('admin.carpool.reset.targetCount') }}</th><th>{{ t('admin.carpool.reset.grantedUsd') }}</th><th>{{ t('admin.carpool.columns.reason') }}</th><th>{{ t('admin.carpool.columns.action') }}</th></tr></thead><tbody class="table-body"><tr v-for="batch in resetBatches" :key="batch.id"><td>#{{ batch.id }}</td><td><span class="status-pill">{{ resetStatus(batch.status) }}</span></td><td class="whitespace-nowrap text-xs">{{ batch.scheduled_at ? dateSeconds(batch.scheduled_at) : '-' }}</td><td>{{ batch.schedule_revision }}</td><td>{{ batch.target_count ?? '-' }}</td><td class="font-mono">${{ money(batch.granted_usd) }}</td><td>{{ resetDelayReason(batch.delay_reason) }}</td><td><div class="flex gap-1"><button v-if="batch.status === 'needs_review' || batch.status === 'scheduled'" class="table-action" @click="openSchedule(batch)">{{ t('admin.carpool.reset.schedule') }}</button><button v-if="batch.status === 'scheduled' || batch.status === 'running'" class="table-action" @click="executeBatch(batch)">{{ t('admin.carpool.reset.execute') }}</button></div></td></tr></tbody></table></div>
        <section class="border-t border-gray-200 pt-5 dark:border-dark-700"><h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.carpool.reset.observations') }}</h2><div class="mt-3 overflow-x-auto"><table class="min-w-[760px] w-full text-left text-sm"><thead class="table-head"><tr><th>{{ t('admin.carpool.reset.identityHash') }}</th><th>{{ t('admin.carpool.reset.baseline') }}</th><th>{{ t('admin.carpool.reset.health') }}</th><th>{{ t('admin.carpool.reset.knownCredits') }}</th><th>{{ t('admin.carpool.reset.unassigned') }}</th><th>{{ t('admin.carpool.columns.time') }}</th></tr></thead><tbody class="table-body"><tr v-for="observation in resetObservations" :key="observation.upstream_identity_hash"><td class="font-mono text-xs">{{ observation.upstream_identity_hash }}</td><td>{{ observation.baseline_complete ? t('common.yes') : t('common.no') }}</td><td>{{ observationHealth(observation.health_status) }}</td><td>{{ observation.known_credit_count }}</td><td>{{ observation.unassigned_credit_count }}</td><td>{{ observation.last_complete_at ? dateSeconds(observation.last_complete_at) : '-' }}</td></tr></tbody></table></div></section>
      </div>

      <div v-else class="overflow-x-auto">
        <table class="min-w-[840px] w-full text-left text-sm"><thead class="table-head"><tr><th>{{ t('admin.carpool.plans.code') }}</th><th>{{ t('admin.carpool.plans.name') }}</th><th>{{ t('admin.carpool.plans.priceCny') }}</th><th>{{ t('admin.carpool.plans.weeklyUsd') }}</th><th>{{ t('admin.carpool.plans.duration') }}</th><th>{{ t('admin.carpool.plans.boostUsd') }}</th><th>{{ t('admin.carpool.plans.version') }}</th><th>{{ t('admin.carpool.columns.action') }}</th></tr></thead><tbody class="table-body"><tr v-for="plan in plans" :key="plan.plan_id"><td class="font-mono">{{ plan.code }}</td><td>{{ plan.name }}</td><td class="font-mono">¥{{ money(plan.list_price_cny) }}</td><td class="font-mono">${{ money(plan.weekly_quota_usd) }}</td><td class="font-mono">{{ plan.duration_days }}</td><td class="font-mono">${{ money(plan.boost_amount_usd) }} × {{ plan.boost_count }}</td><td>{{ plan.version }}</td><td><button class="table-action" @click="openPlanVersion(plan)">{{ t('admin.carpool.actions.editPlan') }}</button></td></tr></tbody></table>
      </div>

      <Pagination v-if="activeTab !== 'plans' && activePagination.total > 0" :page="activePagination.page" :page-size="activePagination.page_size" :total="activePagination.total" @update:page="changePage" />
    </div>

    <BaseDialog :show="!!renewTarget" :title="t('admin.carpool.actions.renew')" @close="closeRenew"><form class="space-y-4" @submit.prevent="submitRenew"><div><label class="input-label">{{ t('admin.carpool.fields.plan') }}</label><Select v-model="renewForm.plan_id" :options="planOptions" /></div><div><label class="input-label">{{ t('admin.carpool.fields.notes') }}</label><input v-model.trim="renewForm.notes" class="input" /></div><p v-if="modalError" class="text-sm text-red-600">{{ modalError }}</p><div class="flex justify-end gap-2"><button type="button" class="btn btn-secondary" @click="closeRenew">{{ t('common.cancel') }}</button><button class="btn btn-primary" :disabled="submitting">{{ t('admin.carpool.actions.renew') }}</button></div></form></BaseDialog>
    <BaseDialog :show="!!terminateTarget" :title="t('admin.carpool.terminate.title')" @close="closeTerminate"><form class="space-y-4" @submit.prevent="submitTerminate"><div><label class="input-label">{{ t('admin.carpool.terminate.reason') }}</label><textarea v-model.trim="terminateReason" class="input min-h-24" required /></div><p v-if="modalError" class="text-sm text-red-600">{{ modalError }}</p><div class="flex justify-end gap-2"><button type="button" class="btn btn-secondary" @click="closeTerminate">{{ t('common.cancel') }}</button><button class="btn btn-danger" :disabled="submitting">{{ t('admin.carpool.terminate.confirm') }}</button></div></form></BaseDialog>
    <BaseDialog :show="!!paymentTarget" :title="t('admin.carpool.actions.payment')" width="wide" @close="closePayment"><div class="space-y-5"><div v-if="payments.length" class="max-h-48 overflow-auto"><table class="w-full text-left text-sm"><thead class="table-head"><tr><th>{{ t('admin.carpool.payment.kind') }}</th><th>{{ t('admin.carpool.payment.amountCny') }}</th><th>{{ t('admin.carpool.payment.paidAt') }}</th><th>{{ t('admin.carpool.payment.channel') }}</th></tr></thead><tbody class="table-body"><tr v-for="item in payments" :key="item.id"><td>{{ t(`admin.carpool.payment.${item.payment_kind}`) }}</td><td class="font-mono">¥{{ money(item.amount_cny) }}</td><td>{{ date(item.paid_at) }}</td><td>{{ item.channel }}</td></tr></tbody></table></div><form class="grid gap-4 sm:grid-cols-2" @submit.prevent="submitPayment"><div><label class="input-label">{{ t('admin.carpool.payment.kind') }}</label><Select v-model="paymentForm.payment_kind" :options="paymentKindOptions" /></div><div><label class="input-label">{{ t('admin.carpool.payment.amountCny') }}</label><input v-model="paymentForm.amount_cny" type="number" min="0.01" step="0.01" class="input" required /></div><div><label class="input-label">{{ t('admin.carpool.payment.paidAt') }}</label><input v-model="paymentForm.paid_at" type="datetime-local" class="input" required /></div><div><label class="input-label">{{ t('admin.carpool.payment.channel') }}</label><input v-model.trim="paymentForm.channel" class="input" required /></div><div><label class="input-label">{{ t('admin.carpool.payment.orderNo') }}</label><input v-model.trim="paymentForm.external_order_no" class="input" /></div><div><label class="input-label">{{ t('admin.carpool.payment.notes') }}</label><input v-model.trim="paymentForm.notes" class="input" /></div><p v-if="modalError" class="text-sm text-red-600 sm:col-span-2">{{ modalError }}</p><div class="flex justify-end sm:col-span-2"><button class="btn btn-primary" :disabled="submitting">{{ t('common.save') }}</button></div></form></div></BaseDialog>
    <BaseDialog :show="!!adjustmentTarget" :title="t('admin.carpool.adjustment.title')" @close="closeAdjustment"><form class="space-y-4" @submit.prevent="submitAdjustment"><div><label class="input-label">{{ t('admin.carpool.columns.bucket') }}</label><Select v-model="adjustmentForm.bucket" :options="bucketOptions" :disabled="adjustmentForm.reverses_ledger_id !== null" /></div><div><label class="input-label">{{ t('admin.carpool.adjustment.delta') }}</label><input v-model="adjustmentForm.delta_usd" type="number" step="0.00000001" class="input" required :readonly="adjustmentForm.reverses_ledger_id !== null" /></div><div><label class="input-label">{{ t('admin.carpool.adjustment.reason') }}</label><textarea v-model.trim="adjustmentForm.reason" class="input min-h-20" required /></div><div><label class="input-label">{{ t('admin.carpool.adjustment.reversal') }}</label><input v-model.number="adjustmentForm.reverses_ledger_id" type="number" min="1" class="input" readonly /></div><label class="flex items-center gap-2 text-sm"><input v-model="adjustmentConfirmed" type="checkbox" />{{ t('admin.carpool.adjustment.confirm') }}</label><p v-if="modalError" class="text-sm text-red-600">{{ modalError }}</p><div class="flex justify-end"><button class="btn btn-primary" :disabled="submitting || !adjustmentConfirmed">{{ t('common.save') }}</button></div></form></BaseDialog>
    <BaseDialog :show="!!reconcileTarget" :title="t('admin.carpool.exceptions.reconcile')" @close="closeReconcile"><form class="space-y-4" @submit.prevent="submitReconcile"><div><label class="input-label">{{ t('admin.carpool.exceptions.resolution') }}</label><Select v-model="reconcileForm.resolution" :options="reconcileOptions" /></div><div v-if="reconcileForm.resolution === 'actual_cost'"><label class="input-label">{{ t('admin.carpool.exceptions.actualCost') }}</label><input v-model="reconcileForm.actual_cost_usd" type="number" min="0.00000001" step="0.00000001" class="input" required /></div><div><label class="input-label">{{ t('admin.carpool.columns.reason') }}</label><textarea v-model.trim="reconcileForm.reason" class="input min-h-20" required /></div><label class="flex items-center gap-2 text-sm"><input v-model="reconcileForm.confirmed" type="checkbox" />{{ t('admin.carpool.exceptions.confirm') }}</label><p v-if="modalError" class="text-sm text-red-600">{{ modalError }}</p><div class="flex justify-end"><button class="btn btn-primary" :disabled="submitting || !reconcileForm.confirmed">{{ t('admin.carpool.exceptions.reconcile') }}</button></div></form></BaseDialog>
    <BaseDialog :show="showResetRegister" :title="t('admin.carpool.reset.register')" @close="closeResetRegister"><form class="space-y-4" @submit.prevent="submitResetRegister"><p class="text-sm text-amber-700 dark:text-amber-300">{{ t('admin.carpool.reset.noImmediate') }}</p><div><label class="input-label">{{ t('admin.carpool.reset.eventReference') }}</label><input v-model.trim="resetRegisterForm.source_event_key" class="input" required /><p class="input-hint">{{ t('admin.carpool.reset.eventHint') }}</p></div><div><label class="input-label">{{ t('admin.carpool.reset.reason') }}</label><textarea v-model.trim="resetRegisterForm.reason" class="input min-h-20" required /></div><label class="flex items-center gap-2 text-sm"><input v-model="resetRegisterForm.confirmed" type="checkbox" />{{ t('admin.carpool.reset.confirmed') }}</label><p v-if="modalError" class="text-sm text-red-600">{{ modalError }}</p><div class="flex justify-end"><button class="btn btn-primary" :disabled="submitting || !resetRegisterForm.confirmed">{{ t('admin.carpool.reset.registerAction') }}</button></div></form></BaseDialog>
    <BaseDialog :show="!!scheduleTarget" :title="t('admin.carpool.reset.schedule')" @close="closeSchedule"><form class="space-y-4" @submit.prevent="submitSchedule"><div><label class="input-label">{{ t('admin.carpool.reset.reason') }}</label><textarea v-model.trim="scheduleReason" class="input min-h-20" required /></div><p v-if="modalError" class="text-sm text-red-600">{{ modalError }}</p><div class="flex justify-end"><button class="btn btn-primary" :disabled="submitting">{{ t('admin.carpool.reset.schedule') }}</button></div></form></BaseDialog>
    <BaseDialog :show="!!planVersionTarget" :title="t('admin.carpool.actions.editPlan')" @close="closePlanVersion"><form class="grid gap-4 sm:grid-cols-2" @submit.prevent="submitPlanVersion"><div><label class="input-label">{{ t('admin.carpool.plans.name') }}</label><input v-model.trim="planForm.name" class="input" required /></div><div><label class="input-label">{{ t('admin.carpool.plans.priceCny') }}</label><input v-model="planForm.list_price_cny" type="number" step="0.01" min="0" class="input" required /></div><div><label class="input-label">{{ t('admin.carpool.plans.weeklyUsd') }}</label><input v-model="planForm.weekly_quota_usd" type="number" step="0.00000001" min="0" class="input" required /></div><div><label class="input-label">{{ t('admin.carpool.plans.ratio') }}</label><input v-model="planForm.boost_ratio" type="number" step="0.00000001" class="input" disabled /></div><div><label class="input-label">{{ t('admin.carpool.plans.count') }}</label><input v-model.number="planForm.boost_count" type="number" min="2" max="3" step="1" class="input" required /></div><label class="flex items-center gap-2 self-end pb-2 text-sm"><input v-model="planForm.enabled" type="checkbox" />{{ t('admin.carpool.plans.enabled') }}</label><p v-if="modalError" class="text-sm text-red-600 sm:col-span-2">{{ modalError }}</p><div class="flex justify-end sm:col-span-2"><button class="btn btn-primary" :disabled="submitting">{{ t('common.save') }}</button></div></form></BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { formatCarpoolAmount } from '@/utils/carpool'
import type { AdminUser } from '@/types'
import type { CarpoolAdminCycle, CarpoolAdminTerm, CarpoolBillingException, CarpoolBucket, CarpoolLedgerEntry, CarpoolPaymentKind, CarpoolPaymentRecord, CarpoolPlan, CarpoolResetBatch, CarpoolResetObservation, CarpoolResetScanResult } from '@/types/carpool'

type Tab = 'users' | 'terms' | 'cycles' | 'ledger' | 'exceptions' | 'resets' | 'plans'
const { t } = useI18n(); const router = useRouter(); const appStore = useAppStore()
const activeTab = ref<Tab>('users'); const loading = ref(false); const loadError = ref(false); const submitting = ref(false)
const users = ref<AdminUser[]>([]); const terms = ref<CarpoolAdminTerm[]>([]); const cycles = ref<CarpoolAdminCycle[]>([]); const ledger = ref<CarpoolLedgerEntry[]>([]); const billingExceptions = ref<CarpoolBillingException[]>([]); const plans = ref<CarpoolPlan[]>([]); const resetBatches = ref<CarpoolResetBatch[]>([]); const resetObservations = ref<CarpoolResetObservation[]>([])
const filters = reactive<{ user_id: number | null; status: string }>({ user_id: null, status: '' })
const paginations = reactive<Record<Exclude<Tab, 'plans'>, { page: number; page_size: number; total: number }>>({ users: { page: 1, page_size: 20, total: 0 }, terms: { page: 1, page_size: 20, total: 0 }, cycles: { page: 1, page_size: 20, total: 0 }, ledger: { page: 1, page_size: 20, total: 0 }, exceptions: { page: 1, page_size: 20, total: 0 }, resets: { page: 1, page_size: 20, total: 0 } })
const expandedTerms = reactive(new Set<number>()); const termCycles = reactive<Record<number, CarpoolAdminCycle[]>>({}); const cycleLoading = reactive(new Set<number>())
let activeLoadGeneration = 0
const tabs = computed(() => (['users', 'terms', 'cycles', 'ledger', 'exceptions', 'resets', 'plans'] as Tab[]).map((key) => ({ key, label: t(`admin.carpool.tabs.${key}`) })))
const statusOptions = computed(() => [{ value: '', label: t('admin.carpool.filters.allStatuses') }, ...['pending', 'active', 'expired', 'terminated', 'scheduled', 'missed', 'closing', 'closed', 'reconcile_required', 'settled'].map((value) => ({ value, label: status(value) }))])
const activePagination = computed(() => activeTab.value === 'plans' ? { page: 1, page_size: 20, total: 0 } : paginations[activeTab.value])
const planOptions = computed(() => plans.value.filter((plan) => plan.enabled).map((plan) => ({ value: plan.plan_id, label: plan.name })))
const paymentKindOptions = computed(() => (['payment', 'refund'] as CarpoolPaymentKind[]).map((value) => ({ value, label: t(`admin.carpool.payment.${value}`) })))
const bucketOptions = computed(() => (['base', 'boost', 'manual'] as CarpoolBucket[]).map((value) => ({ value, label: t(`admin.carpool.bucket.${value}`) })))
const reconcileOptions = computed(() => (['no_cost', 'actual_cost'] as const).map((value) => ({ value, label: t(`admin.carpool.exceptions.${value}`) })))

function key() { return typeof crypto !== 'undefined' && crypto.randomUUID ? crypto.randomUUID() : `carpool-${Date.now()}-${Math.random().toString(36).slice(2)}` }
function money(value?: string | null) { return formatCarpoolAmount(value) }
function date(value: string) { return new Intl.DateTimeFormat(undefined, { timeZone: 'Asia/Shanghai', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(value)) }
function dateSeconds(value: string) { return new Intl.DateTimeFormat(undefined, { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value)) }
function defaultExpiry(value: string) { return new Date(new Date(value).getTime() + 28 * 24 * 60 * 60 * 1000).toISOString() }
function termForUser(userID: number) { return terms.value.find((term) => term.user_id === userID) }
function userLabel(userID: number) { const user = users.value.find((item) => item.id === userID); return user?.email || user?.username || `#${userID}` }
function shanghai(value: string) { return new Date(`${value}:00+08:00`).toISOString() }
function localizedValue(namespace: string, value: string) { const key = `admin.carpool.${namespace}.${value}`; const translated = t(key); return translated === key ? value : translated }
function status(value: string) { return localizedValue('status', value) }
function ledgerEvent(value: string) { return localizedValue('ledgerEvent', value) }
function ledgerBucket(value: string) { return localizedValue('bucket', value) }
function localizedResetValue(group: 'batchStatus' | 'healthStatus' | 'delayReason', value: string) { return localizedValue(`reset.${group}`, value) }
function resetStatus(value: string) { return localizedResetValue('batchStatus', value) }
function observationHealth(value: string) { return localizedResetValue('healthStatus', value) }
function resetDelayReason(value?: string | null) { return value ? localizedResetValue('delayReason', value) : '-' }
function errorMessage(error: unknown) { return typeof error === 'object' && error !== null && 'message' in error ? String(error.message) : t('admin.carpool.saveFailed') }
function ledgerReference(entry: CarpoolLedgerEntry) { return entry.request_id || (entry.reset_batch_id ? `reset:${entry.reset_batch_id}` : entry.event_key) }
function negateDecimal(value: string) { return value.startsWith('-') ? value.slice(1) : `-${value}` }

async function loadActive() {
  const requestGeneration = ++activeLoadGeneration
  const requestedTab = activeTab.value
  const requestedUserID = filters.user_id ?? undefined
  const requestedStatus = filters.status || undefined
  const requestedPagination = requestedTab === 'plans' ? null : { page: paginations[requestedTab].page, page_size: paginations[requestedTab].page_size }
  loading.value = true; loadError.value = false
  try {
    const planData = plans.value.length === 0 || requestedTab === 'plans' ? await adminAPI.carpool.listPlans() : null
    let data: unknown = null
    if (requestedTab === 'users') data = await Promise.all([adminAPI.users.list(requestedPagination!.page, requestedPagination!.page_size, { role: 'user', sort_by: 'created_at', sort_order: 'asc' }), adminAPI.carpool.listTerms({ page: 1, page_size: 100 })])
    if (requestedTab === 'terms') data = await adminAPI.carpool.listTerms({ ...requestedPagination!, user_id: requestedUserID, status: requestedStatus })
    if (requestedTab === 'cycles') data = await adminAPI.carpool.listCycles({ ...requestedPagination!, user_id: requestedUserID, state: requestedStatus })
    if (requestedTab === 'ledger') data = await adminAPI.carpool.listLedger({ ...requestedPagination!, user_id: requestedUserID })
    if (requestedTab === 'exceptions') data = await adminAPI.carpool.listBillingExceptions({ ...requestedPagination!, user_id: requestedUserID, status: requestedStatus })
    if (requestedTab === 'resets') data = await Promise.all([adminAPI.carpool.listResetBatches({ ...requestedPagination!, scope_id: 1 }), adminAPI.carpool.listResetObservations({ page: 1, page_size: 50 })])
    if (requestGeneration !== activeLoadGeneration || activeTab.value !== requestedTab) return
    if (planData) plans.value = planData
    if (requestedTab === 'users') { const [userData, termData] = data as [Awaited<ReturnType<typeof adminAPI.users.list>>, Awaited<ReturnType<typeof adminAPI.carpool.listTerms>>]; users.value = userData.items; terms.value = termData.items; paginations.users.total = userData.total }
    if (requestedTab === 'terms') { const result = data as Awaited<ReturnType<typeof adminAPI.carpool.listTerms>>; terms.value = result.items; paginations.terms.total = result.total }
    if (requestedTab === 'cycles') { const result = data as Awaited<ReturnType<typeof adminAPI.carpool.listCycles>>; cycles.value = result.items; paginations.cycles.total = result.total }
    if (requestedTab === 'ledger') { const result = data as Awaited<ReturnType<typeof adminAPI.carpool.listLedger>>; ledger.value = result.items; paginations.ledger.total = result.total }
    if (requestedTab === 'exceptions') { const result = data as Awaited<ReturnType<typeof adminAPI.carpool.listBillingExceptions>>; billingExceptions.value = result.items; paginations.exceptions.total = result.total }
    if (requestedTab === 'resets') { const [batchData, observationData] = data as [Awaited<ReturnType<typeof adminAPI.carpool.listResetBatches>>, Awaited<ReturnType<typeof adminAPI.carpool.listResetObservations>>]; resetBatches.value = batchData.items; paginations.resets.total = batchData.total; resetObservations.value = observationData.items }
  } catch (error) { if (requestGeneration === activeLoadGeneration) { loadError.value = true; console.error('Failed to load carpool admin data:', error) } } finally { if (requestGeneration === activeLoadGeneration) loading.value = false }
}
function switchTab(tab: Tab) { activeTab.value = tab; void loadActive() }
function applyFilters() { if (activeTab.value !== 'plans') paginations[activeTab.value].page = 1; void loadActive() }
function clearFilters() { filters.user_id = null; filters.status = ''; applyFilters() }
function changePage(page: number) { if (activeTab.value === 'plans') return; paginations[activeTab.value].page = page; void loadActive() }
async function toggleTerm(term: CarpoolAdminTerm) { if (expandedTerms.has(term.id)) { expandedTerms.delete(term.id); return } expandedTerms.add(term.id); if (termCycles[term.id]) return; if (term.cycles?.length) { termCycles[term.id] = term.cycles; return } cycleLoading.add(term.id); try { termCycles[term.id] = (await adminAPI.carpool.listCycles({ page: 1, page_size: 10, term_id: term.id })).items } finally { cycleLoading.delete(term.id) } }

const renewTarget = ref<CarpoolAdminTerm | null>(null); const renewForm = reactive<{ plan_id: number | null; notes: string }>({ plan_id: null, notes: '' }); let renewKey = ''
const terminateTarget = ref<CarpoolAdminTerm | null>(null); const terminateReason = ref(''); let terminateKey = ''
const paymentTarget = ref<CarpoolAdminTerm | null>(null); const payments = ref<CarpoolPaymentRecord[]>([]); const paymentForm = reactive({ payment_kind: 'payment' as CarpoolPaymentKind, amount_cny: '', paid_at: '', channel: 'manual', external_order_no: '', notes: '' }); let paymentKey = ''; let paymentGeneration = 0
const adjustmentTarget = ref<{ id: number } | null>(null); const adjustmentForm = reactive<{ bucket: CarpoolBucket; delta_usd: string; reason: string; reverses_ledger_id: number | null }>({ bucket: 'manual', delta_usd: '', reason: '', reverses_ledger_id: null }); const adjustmentConfirmed = ref(false); let adjustmentKey = ''
const modalError = ref('')
let modalGeneration = 0
function beginModalAction() { modalGeneration += 1; paymentGeneration += 1; submitting.value = false; modalError.value = '' }
function invalidateModalAction() { modalGeneration += 1; paymentGeneration += 1; submitting.value = false; modalError.value = '' }
function openRenew(term: CarpoolAdminTerm) { beginModalAction(); renewTarget.value = term; renewForm.plan_id = plans.value.find((plan) => plan.enabled && plan.code === term.plan_snapshot.code)?.plan_id ?? null; renewForm.notes = ''; renewKey = key() }
function closeRenew() { invalidateModalAction(); renewTarget.value = null }
async function submitRenew() {
  const target = renewTarget.value; const planID = renewForm.plan_id
  if (!target || !planID) return
  const requestGeneration = modalGeneration; const requestKey = renewKey; const request = { plan_id: planID, notes: renewForm.notes || null, payment: null }
  submitting.value = true
  try { await adminAPI.carpool.renewTerm(target.id, request, requestKey); if (requestGeneration === modalGeneration && renewTarget.value?.id === target.id) { closeRenew(); appStore.showSuccess(t('admin.carpool.actions.renew')); await loadActive() } }
  catch (error) { if (requestGeneration === modalGeneration && renewTarget.value?.id === target.id) modalError.value = errorMessage(error) }
  finally { if (requestGeneration === modalGeneration) submitting.value = false }
}
function openTerminate(term: CarpoolAdminTerm) { beginModalAction(); terminateTarget.value = term; terminateReason.value = ''; terminateKey = key() }
function closeTerminate() { invalidateModalAction(); terminateTarget.value = null }
async function submitTerminate() {
  const target = terminateTarget.value; const reason = terminateReason.value
  if (!target || !reason) return
  const requestGeneration = modalGeneration; const requestKey = terminateKey
  submitting.value = true
  try { await adminAPI.carpool.terminateTerm(target.id, reason, requestKey); if (requestGeneration === modalGeneration && terminateTarget.value?.id === target.id) { closeTerminate(); await loadActive() } }
  catch (error) { if (requestGeneration === modalGeneration && terminateTarget.value?.id === target.id) modalError.value = errorMessage(error) }
  finally { if (requestGeneration === modalGeneration) submitting.value = false }
}
async function openPayment(term: CarpoolAdminTerm) { beginModalAction(); const requestGeneration = paymentGeneration; paymentTarget.value = term; paymentKey = key(); payments.value = []; Object.assign(paymentForm, { payment_kind: 'payment', amount_cny: '', paid_at: '', channel: 'manual', external_order_no: '', notes: '' }); try { const items = (await adminAPI.carpool.listPayments(term.id, { page: 1, page_size: 50 })).items; if (requestGeneration === paymentGeneration && paymentTarget.value?.id === term.id) payments.value = items } catch (error) { if (requestGeneration === paymentGeneration && paymentTarget.value?.id === term.id) modalError.value = errorMessage(error) } }
function closePayment() { invalidateModalAction(); paymentTarget.value = null; payments.value = []; Object.assign(paymentForm, { payment_kind: 'payment', amount_cny: '', paid_at: '', channel: 'manual', external_order_no: '', notes: '' }) }
async function submitPayment() { const target = paymentTarget.value; if (!target) return; const requestGeneration = paymentGeneration; const requestKey = paymentKey; const request = { amount_cny: String(paymentForm.amount_cny), payment_kind: paymentForm.payment_kind, paid_at: shanghai(paymentForm.paid_at), channel: paymentForm.channel, external_order_no: paymentForm.external_order_no || null, notes: paymentForm.notes || null }; submitting.value = true; modalError.value = ''; try { await adminAPI.carpool.addPayment(target.id, request, requestKey); if (requestGeneration === paymentGeneration && paymentTarget.value?.id === target.id) { closePayment(); appStore.showSuccess(t('admin.carpool.payment.saved')); await loadActive() } } catch (error) { if (requestGeneration === paymentGeneration && paymentTarget.value?.id === target.id) modalError.value = errorMessage(error) } finally { if (requestGeneration === paymentGeneration) submitting.value = false } }
function openAdjustment(cycle: CarpoolAdminCycle) { beginModalAction(); adjustmentTarget.value = cycle; Object.assign(adjustmentForm, { bucket: 'manual', delta_usd: '', reason: '', reverses_ledger_id: null }); adjustmentConfirmed.value = false; adjustmentKey = key() }
function openReversal(entry: CarpoolLedgerEntry) { beginModalAction(); adjustmentTarget.value = { id: entry.cycle_id }; Object.assign(adjustmentForm, { bucket: entry.bucket, delta_usd: negateDecimal(entry.delta_usd), reason: '', reverses_ledger_id: entry.id }); adjustmentConfirmed.value = false; adjustmentKey = key() }
function closeAdjustment() { invalidateModalAction(); adjustmentTarget.value = null }
async function submitAdjustment() {
  const target = adjustmentTarget.value
  if (!target || !adjustmentConfirmed.value) return
  const requestGeneration = modalGeneration; const requestKey = adjustmentKey; const request = { bucket: adjustmentForm.bucket, delta_usd: String(adjustmentForm.delta_usd), reason: adjustmentForm.reason, reverses_ledger_id: adjustmentForm.reverses_ledger_id }
  submitting.value = true
  try { await adminAPI.carpool.adjustCycle(target.id, request, requestKey); if (requestGeneration === modalGeneration && adjustmentTarget.value?.id === target.id) { closeAdjustment(); appStore.showSuccess(t('admin.carpool.adjustment.saved')); await loadActive() } }
  catch (error) { if (requestGeneration === modalGeneration && adjustmentTarget.value?.id === target.id) modalError.value = errorMessage(error) }
  finally { if (requestGeneration === modalGeneration) submitting.value = false }
}

const reconcileTarget = ref<CarpoolBillingException | null>(null); const reconcileForm = reactive<{ resolution: 'no_cost' | 'actual_cost'; actual_cost_usd: string; reason: string; confirmed: boolean }>({ resolution: 'no_cost', actual_cost_usd: '', reason: '', confirmed: false }); let reconcileKey = ''
function openReconcile(item: CarpoolBillingException) { beginModalAction(); reconcileTarget.value = item; Object.assign(reconcileForm, { resolution: 'no_cost', actual_cost_usd: '', reason: '', confirmed: false }); reconcileKey = key() }
function closeReconcile() { invalidateModalAction(); reconcileTarget.value = null }
async function submitReconcile() {
  const target = reconcileTarget.value
  if (!target || !reconcileForm.confirmed) return
  const requestGeneration = modalGeneration; const requestKey = reconcileKey; const request = { confirmed: true as const, resolution: reconcileForm.resolution, actual_cost_usd: reconcileForm.resolution === 'actual_cost' ? String(reconcileForm.actual_cost_usd) : null, reason: reconcileForm.reason }
  submitting.value = true
  try { await adminAPI.carpool.reconcileBillingException(target.id, request, requestKey); if (requestGeneration === modalGeneration && reconcileTarget.value?.id === target.id) { closeReconcile(); appStore.showSuccess(t('admin.carpool.exceptions.saved')); await loadActive() } }
  catch (error) { if (requestGeneration === modalGeneration && reconcileTarget.value?.id === target.id) modalError.value = errorMessage(error) }
  finally { if (requestGeneration === modalGeneration) submitting.value = false }
}

const showResetRegister = ref(false); const resetRegisterForm = reactive({ source_event_key: '', reason: '', confirmed: false }); let resetRegisterKey = ''; const scheduleTarget = ref<CarpoolResetBatch | null>(null); const scheduleReason = ref(''); let scheduleKey = ''; const executeKeys = new Map<number, string>(); const scanning = ref(false); const scanResult = ref<CarpoolResetScanResult | null>(null); let scanKey = key()
function openResetRegister() { beginModalAction(); showResetRegister.value = true; resetRegisterForm.source_event_key = ''; resetRegisterForm.reason = ''; resetRegisterForm.confirmed = false; resetRegisterKey = key() }
function closeResetRegister() { invalidateModalAction(); showResetRegister.value = false }
async function submitResetRegister() {
  if (!resetRegisterForm.confirmed) return
  const requestGeneration = modalGeneration; const requestKey = resetRegisterKey; const request = { scope_id: 1 as const, confirmed: true as const, source_event_key: resetRegisterForm.source_event_key, reason: resetRegisterForm.reason }
  submitting.value = true
  try { await adminAPI.carpool.registerResetQualification(request, requestKey); if (requestGeneration === modalGeneration && showResetRegister.value) { closeResetRegister(); await loadActive() } }
  catch (error) { if (requestGeneration === modalGeneration && showResetRegister.value) modalError.value = errorMessage(error) }
  finally { if (requestGeneration === modalGeneration) submitting.value = false }
}
function openSchedule(batch: CarpoolResetBatch) { beginModalAction(); scheduleTarget.value = batch; scheduleReason.value = ''; scheduleKey = key() }
function closeSchedule() { invalidateModalAction(); scheduleTarget.value = null }
async function submitSchedule() {
  const target = scheduleTarget.value
  if (!target) return
  const requestGeneration = modalGeneration; const requestKey = scheduleKey; const reason = scheduleReason.value
  submitting.value = true
  try { await adminAPI.carpool.scheduleResetBatch(target.id, reason, requestKey); if (requestGeneration === modalGeneration && scheduleTarget.value?.id === target.id) { closeSchedule(); await loadActive() } }
  catch (error) { if (requestGeneration === modalGeneration && scheduleTarget.value?.id === target.id) modalError.value = errorMessage(error) }
  finally { if (requestGeneration === modalGeneration) submitting.value = false }
}
async function executeBatch(batch: CarpoolResetBatch) { const requestKey = executeKeys.get(batch.id) ?? key(); executeKeys.set(batch.id, requestKey); try { await adminAPI.carpool.executeResetBatch(batch.id, requestKey); executeKeys.delete(batch.id); await loadActive() } catch (error) { appStore.showError(errorMessage(error)) } }
async function scanObservations() { scanning.value = true; try { scanResult.value = await adminAPI.carpool.scanResetObservations(scanKey); scanKey = key(); await loadActive() } catch (error) { appStore.showError(errorMessage(error)) } finally { scanning.value = false } }

const planVersionTarget = ref<CarpoolPlan | null>(null); const planForm = reactive({ name: '', list_price_cny: '', weekly_quota_usd: '', boost_ratio: '0.10000000', boost_count: 2, enabled: true }); let planKey = ''
function openPlanVersion(plan: CarpoolPlan) { beginModalAction(); planVersionTarget.value = plan; Object.assign(planForm, { name: plan.name, list_price_cny: plan.list_price_cny, weekly_quota_usd: plan.weekly_quota_usd, boost_ratio: '0.10000000', boost_count: plan.boost_count, enabled: plan.enabled }); planKey = key() }
function closePlanVersion() { invalidateModalAction(); planVersionTarget.value = null }
async function submitPlanVersion() {
  const target = planVersionTarget.value
  if (!target) return
  const requestGeneration = modalGeneration; const requestKey = planKey; const request = { ...planForm, list_price_cny: String(planForm.list_price_cny), weekly_quota_usd: String(planForm.weekly_quota_usd), boost_ratio: String(planForm.boost_ratio) }
  submitting.value = true
  try { await adminAPI.carpool.createPlanVersion(target.plan_id, request, requestKey); if (requestGeneration === modalGeneration && planVersionTarget.value?.plan_id === target.plan_id) { closePlanVersion(); appStore.showSuccess(t('admin.carpool.plans.saved')); await loadActive() } }
  catch (error) { if (requestGeneration === modalGeneration && planVersionTarget.value?.plan_id === target.plan_id) modalError.value = errorMessage(error) }
  finally { if (requestGeneration === modalGeneration) submitting.value = false }
}

void loadActive()
</script>

<style scoped>
.table-head { @apply border-y border-gray-200 bg-gray-50 text-xs text-gray-500 dark:border-dark-700 dark:bg-dark-800; }
.table-head th { @apply whitespace-nowrap px-3 py-3 font-medium; }
.table-body { @apply divide-y divide-gray-100 dark:divide-dark-700; }
.table-body td { @apply px-3 py-3 text-gray-700 dark:text-gray-200; }
.status-pill { @apply inline-flex whitespace-nowrap rounded-full bg-gray-100 px-2 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-200; }
.table-action { @apply whitespace-nowrap rounded px-2 py-1 text-xs font-medium text-primary-700 hover:bg-primary-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-primary-300 dark:hover:bg-primary-900/20; }
</style>
