<template>
  <article
    :data-testid="`carpool-product-${product.id}`"
    class="group relative flex h-full flex-col overflow-hidden rounded-2xl border bg-white/80 p-6 backdrop-blur-sm transition-all duration-300 hover:-translate-y-1 dark:bg-dark-800/70"
    :class="theme.card"
  >
    <!-- Accent strip -->
    <div class="absolute inset-x-0 top-0 h-1" :class="theme.strip"></div>

    <!-- Title row -->
    <div class="mb-5 flex items-start justify-between gap-3">
      <div class="flex items-center gap-3">
        <div
          class="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl text-base font-bold text-white shadow-lg"
          :class="theme.icon"
        >
          {{ t(`home.products.${product.i18nKey}.mark`) }}
        </div>
        <div>
          <h3 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t(`home.products.${product.i18nKey}.name`) }}
          </h3>
          <p class="text-xs text-gray-500 dark:text-dark-400">
            {{ t(`home.products.${product.i18nKey}.tagline`) }}
          </p>
        </div>
      </div>
      <span class="shrink-0 rounded-full px-2.5 py-1 text-xs font-medium" :class="theme.badge">
        {{ t(`home.products.status.${product.status}`) }}
      </span>
    </div>

    <!-- Price -->
    <div class="mb-5">
      <div class="flex items-baseline gap-1.5">
        <template v-if="product.priceCny !== null">
          <span class="text-sm font-medium text-gray-500 dark:text-dark-400">¥</span>
          <span class="text-4xl font-bold tracking-tight text-gray-900 dark:text-white">
            {{ product.priceCny }}
          </span>
        </template>
        <span v-else class="text-3xl font-bold tracking-tight text-gray-400 dark:text-dark-500">
          {{ t('home.products.priceTbd') }}
        </span>
        <span class="text-sm text-gray-500 dark:text-dark-400">
          {{ t(`home.products.${product.i18nKey}.priceUnit`) }}
        </span>
      </div>
      <p class="mt-1 text-sm font-medium" :class="theme.text">
        {{
          product.highlight
            ? t(`home.products.${product.i18nKey}.highlight`, product.highlight)
            : t(`home.products.${product.i18nKey}.highlightPending`)
        }}
      </p>
    </div>

    <!-- Tier table -->
    <div
      v-if="product.tiers?.length"
      class="mb-5 overflow-hidden rounded-xl border border-gray-100 dark:border-dark-700"
    >
      <table class="w-full text-sm">
        <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-900/60 dark:text-dark-400">
          <tr>
            <th scope="col" class="px-3 py-2 text-left font-medium">{{ t('home.products.tierSeats') }}</th>
            <th scope="col" class="px-3 py-2 text-right font-medium">{{ t('home.products.tierPrice') }}</th>
            <th scope="col" class="px-3 py-2 text-right font-medium">{{ t('home.products.tierQuota') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr v-for="tier in product.tiers" :key="tier.key" class="text-gray-700 dark:text-dark-200">
            <td class="px-3 py-2">{{ tier.seats ? t('home.products.seats', { n: tier.seats }) : tier.name }}</td>
            <td class="px-3 py-2 text-right tabular-nums">¥{{ tier.priceCny }}</td>
            <td class="px-3 py-2 text-right font-medium tabular-nums" :class="theme.text">
              ${{ tier.weeklyQuotaUsd }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Rules -->
    <ul class="mb-6 flex-1 space-y-2.5">
      <li
        v-for="(rule, index) in rules"
        :key="index"
        class="flex items-start gap-2 text-sm text-gray-600 dark:text-dark-300"
      >
        <Icon name="check" size="sm" class="mt-0.5 shrink-0" :class="theme.text" :stroke-width="2.5" />
        <span>{{ rule }}</span>
      </li>
    </ul>

    <!-- Details -->
    <router-link
      :to="`/plans/${product.id}`"
      class="inline-flex w-full items-center justify-center gap-1.5 rounded-xl px-4 py-2.5 text-sm font-medium transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-offset-2"
      :class="theme.button"
    >
      {{ t('home.products.viewRules') }}
      <Icon name="arrowRight" size="sm" :stroke-width="2" />
    </router-link>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { carpoolProductRules, type CarpoolProduct } from '@/constants/carpoolProducts'

const props = defineProps<{
  product: CarpoolProduct
}>()

const { t } = useI18n()

// Tailwind 需要完整类名，按强调色静态映射
const THEMES = {
  teal: {
    card: 'border-primary-200/70 hover:shadow-xl hover:shadow-primary-500/10 dark:border-primary-500/20',
    strip: 'bg-gradient-to-r from-primary-400 to-primary-600',
    icon: 'bg-gradient-to-br from-primary-500 to-primary-600 shadow-primary-500/30',
    badge: 'bg-primary-50 text-primary-700 dark:bg-primary-500/15 dark:text-primary-300',
    text: 'text-primary-600 dark:text-primary-400',
    button:
      'bg-primary-600 text-white hover:bg-primary-700 focus-visible:ring-primary-500 dark:bg-primary-500 dark:hover:bg-primary-600'
  },
  orange: {
    card: 'border-orange-200/70 hover:shadow-xl hover:shadow-orange-500/10 dark:border-orange-500/20',
    strip: 'bg-gradient-to-r from-orange-400 to-orange-600',
    icon: 'bg-gradient-to-br from-orange-400 to-orange-600 shadow-orange-500/30',
    badge: 'bg-orange-50 text-orange-700 dark:bg-orange-500/15 dark:text-orange-300',
    text: 'text-orange-600 dark:text-orange-400',
    button:
      'bg-orange-500 text-white hover:bg-orange-600 focus-visible:ring-orange-500 dark:bg-orange-500 dark:hover:bg-orange-600'
  },
  violet: {
    card: 'border-dashed border-violet-200 hover:shadow-xl hover:shadow-violet-500/10 dark:border-violet-500/30',
    strip: 'bg-gradient-to-r from-violet-300 to-violet-500',
    icon: 'bg-gradient-to-br from-violet-400 to-violet-600 shadow-violet-500/30',
    badge: 'bg-violet-50 text-violet-700 dark:bg-violet-500/15 dark:text-violet-300',
    text: 'text-violet-600 dark:text-violet-400',
    button:
      'border border-violet-200 bg-white text-violet-700 hover:bg-violet-50 focus-visible:ring-violet-500 dark:border-violet-500/30 dark:bg-transparent dark:text-violet-300 dark:hover:bg-violet-500/10'
  }
} as const

const theme = computed(() => THEMES[props.product.accent])
const rules = computed(() => carpoolProductRules(props.product, t))
</script>
