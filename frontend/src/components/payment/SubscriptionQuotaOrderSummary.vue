<template>
  <section v-if="snapshot" class="space-y-1 rounded-lg bg-orange-50 p-3 text-sm text-gray-800 dark:bg-orange-900/20 dark:text-gray-200" data-testid="order-quota-snapshot">
    <h4 v-if="!compact" class="font-semibold">{{ t('subscriptionQuota.orderSnapshot') }}</h4>
    <p class="break-words [overflow-wrap:anywhere]">{{ snapshot.product_name || snapshot.plan_name }} · {{ snapshot.group_name }}</p>
    <p class="break-words">{{ t('subscriptionQuota.quota') }}: {{ formatQuotaUSD(snapshot.quota_usd) }} USD</p>
    <p>{{ t('subscriptionQuota.fixedDuration') }}</p>
    <p v-if="!compact" class="text-xs text-gray-500 dark:text-gray-400">{{ t('subscriptionQuota.snapshotHint') }}</p>
  </section>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { SubscriptionQuotaOrderSnapshot } from '@/types/payment'
import { formatQuotaUSD } from '@/utils/claudeQuota'
defineProps<{ snapshot?: SubscriptionQuotaOrderSnapshot | null; compact?: boolean }>()
const { t } = useI18n()
</script>
