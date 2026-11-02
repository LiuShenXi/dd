import { computed, onMounted, ref } from 'vue'
import { getHomeProducts, type HomeProducts } from '@/api/homeProducts'
import { buildCarpoolProducts } from '@/constants/carpoolProducts'

/** 加载首页车次卡片；失败时保留待定卡片，不阻塞首页渲染。 */
export function useHomeProducts() {
  const data = ref<HomeProducts | null>(null)
  const loading = ref(true)

  const products = computed(() => buildCarpoolProducts(data.value))
  const contact = computed(() => data.value?.contact?.trim() || '')

  async function load() {
    loading.value = true
    try {
      data.value = await getHomeProducts()
    } catch (error) {
      console.warn('Failed to load home products', error)
    } finally {
      loading.value = false
    }
  }

  onMounted(load)

  return { products, contact, loading, reload: load }
}
