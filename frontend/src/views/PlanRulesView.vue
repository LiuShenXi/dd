<template>
  <div class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <header class="border-b border-gray-200/60 bg-white/70 px-6 py-4 backdrop-blur dark:border-dark-800 dark:bg-dark-900/70">
      <nav class="mx-auto flex max-w-3xl items-center justify-between">
        <router-link
          to="/home"
          class="inline-flex items-center gap-1.5 text-sm text-gray-600 hover:text-primary-600 dark:text-dark-300 dark:hover:text-primary-400"
        >
          <Icon name="arrowLeft" size="sm" :stroke-width="2" />
          {{ t('home.products.backHome') }}
        </router-link>
        <LocaleSwitcher />
      </nav>
    </header>

    <main class="mx-auto max-w-3xl px-6 py-12">
      <template v-if="product">
        <p class="mb-2 text-sm font-medium text-primary-600 dark:text-primary-400">
          {{ t('home.products.rulesDoc') }}
        </p>
        <h1 class="mb-3 text-3xl font-bold">
          {{ t(`home.products.${product.i18nKey}.name`) }}
        </h1>
        <p class="mb-10 text-gray-600 dark:text-dark-300">
          {{ t(`home.products.${product.i18nKey}.tagline`) }}
        </p>

        <section class="mb-8 rounded-2xl border border-gray-200/70 bg-white p-6 dark:border-dark-700 dark:bg-dark-800">
          <h2 class="mb-4 text-lg font-semibold">{{ t('home.products.summary') }}</h2>
          <ul class="space-y-2.5">
            <li
              v-for="(rule, index) in rules"
              :key="index"
              class="flex items-start gap-2 text-sm text-gray-700 dark:text-dark-200"
            >
              <Icon name="check" size="sm" class="mt-0.5 shrink-0 text-primary-500" :stroke-width="2.5" />
              <span>{{ rule }}</span>
            </li>
          </ul>
        </section>

        <div
          data-testid="plan-rules-pending"
          class="rounded-2xl border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400"
        >
          <Icon name="document" size="lg" class="mx-auto mb-3 text-gray-400" />
          {{ t('home.products.docPending') }}
        </div>
      </template>

      <div v-else-if="!loading" class="py-20 text-center">
        <p class="mb-6 text-gray-600 dark:text-dark-300">{{ t('home.products.notFound') }}</p>
        <router-link to="/home" class="btn btn-primary">{{ t('home.products.backHome') }}</router-link>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import { carpoolProductRules, findCarpoolProduct } from '@/constants/carpoolProducts'
import { useHomeProducts } from '@/composables/useHomeProducts'

const { t } = useI18n()
const route = useRoute()

const { products, loading } = useHomeProducts()
const product = computed(() => findCarpoolProduct(products.value, String(route.params.productId || '')))
const rules = computed(() => (product.value ? carpoolProductRules(product.value, t) : []))
</script>
