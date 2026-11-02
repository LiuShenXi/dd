// 首页展示的拼车产品（车次）。价格与额度来自 /api/v1/home/products（真实套餐），
// 这里只保留卡片的静态外观和默认规则文案；接口不可用时卡片显示「待定」。
import type { HomeProducts, HomeProductStatus } from '@/api/homeProducts'

export type CarpoolProductId = 'gpt-carpool' | 'claude-weekly' | 'claude-monthly'

export type CarpoolProductStatus = HomeProductStatus

export interface CarpoolProductTier {
  key: string
  seats: number
  name: string
  priceCny: number
  weeklyQuotaUsd: number
}

export interface CarpoolProduct {
  id: CarpoolProductId
  // home.products.<i18nKey>.*
  i18nKey: 'gpt' | 'claudeWeekly' | 'claudeMonthly'
  status: CarpoolProductStatus
  accent: 'teal' | 'orange' | 'violet'
  // 起售价，null 表示价格待定
  priceCny: number | null
  tiers?: CarpoolProductTier[]
  // 卡片亮点文案的插值参数，缺失时使用 home.products.<i18nKey>.highlightPending
  highlight?: Record<string, string | number>
  // home.products.<i18nKey>.rules.<key> 默认规则
  ruleKeys: string[]
  // 需要 ruleParams 插值的默认规则；参数缺失（接口未返回）时不展示
  paramRuleKeys?: string[]
  // 管理端配置的规则，非空时替代默认规则
  customRules?: string[]
  // 默认规则文案的插值参数
  ruleParams?: Record<string, string | number>
}

export const DEFAULT_CARPOOL_CONTACT = {
  wechat: 'lfx916182218',
  qq: '916182218'
}

const BASE_PRODUCTS: CarpoolProduct[] = [
  {
    id: 'gpt-carpool',
    i18nKey: 'gpt',
    status: 'available',
    accent: 'teal',
    priceCny: null,
    ruleKeys: ['term', 'refill', 'boost', 'officialReset', 'noCarry'],
    paramRuleKeys: ['term', 'refill', 'boost', 'officialReset']
  },
  {
    id: 'claude-weekly',
    i18nKey: 'claudeWeekly',
    status: 'pilot',
    accent: 'orange',
    priceCny: null,
    ruleKeys: ['quota', 'term', 'models', 'rate', 'tokens'],
    paramRuleKeys: ['quota', 'rate']
  },
  {
    id: 'claude-monthly',
    i18nKey: 'claudeMonthly',
    status: 'soon',
    accent: 'violet',
    priceCny: null,
    ruleKeys: ['term', 'models', 'pricing', 'notify']
  }
]

function num(value: string | number | null | undefined): number | null {
  if (value === null || value === undefined || value === '') return null
  const n = Number(value)
  return Number.isFinite(n) ? n : null
}

// 去掉多余小数位用于展示：150.00000000 → 150
function plain(n: number): number {
  return Number(n.toFixed(2))
}

/** 将接口数据合并进卡片定义；data 为 null（加载中或失败）时返回待定卡片。 */
export function buildCarpoolProducts(data: HomeProducts | null): CarpoolProduct[] {
  const [gptBase, weeklyBase, monthly] = BASE_PRODUCTS
  if (!data) return BASE_PRODUCTS.map((p) => ({ ...p }))

  const products: CarpoolProduct[] = []

  if (data.gpt_carpool.enabled) {
    const tiers: CarpoolProductTier[] = data.gpt_carpool.tiers
      .map((tier) => ({
        key: tier.code,
        seats: tier.seats,
        name: tier.name,
        priceCny: plain(num(tier.price_cny) ?? 0),
        weeklyQuotaUsd: plain(num(tier.weekly_quota_usd) ?? 0)
      }))
      .sort((a, b) => a.priceCny - b.priceCny)
    const first = data.gpt_carpool.tiers[0]
    const maxQuota = tiers.reduce((max, tier) => Math.max(max, tier.weeklyQuotaUsd), 0)
    products.push({
      ...gptBase,
      status: data.gpt_carpool.status,
      priceCny: tiers.length ? tiers[0].priceCny : null,
      tiers,
      highlight: tiers.length ? { quota: maxQuota } : undefined,
      customRules: data.gpt_carpool.rules,
      ruleParams: first
        ? {
            days: first.duration_days,
            cycle: first.cycle_days,
            boosts: first.boost_count,
            percent: plain((num(first.boost_ratio) ?? 0) * 100)
          }
        : undefined
    })
  }

  if (data.claude_weekly.enabled) {
    const w = data.claude_weekly
    const quota = num(w.quota_usd)
    const rate = num(w.rate_multiplier)
    const priced = w.price_cny !== null && quota !== null && rate !== null
    products.push({
      ...weeklyBase,
      status: w.status,
      priceCny: priced ? plain(w.price_cny as number) : null,
      highlight: priced ? { quota: plain(quota as number), rate } : undefined,
      customRules: w.rules,
      ruleParams: priced ? { quota: plain(quota as number), rate: rate as number } : undefined
    })
  }

  products.push({ ...monthly })
  return products
}

export function findCarpoolProduct(products: CarpoolProduct[], id: string): CarpoolProduct | undefined {
  return products.find((product) => product.id === id)
}

/** 卡片规则：优先使用管理端配置，否则使用默认文案；缺少数值参数的默认规则不展示。 */
export function carpoolProductRules(
  product: CarpoolProduct,
  t: (key: string, params?: Record<string, string | number>) => string
): string[] {
  if (product.customRules?.length) return product.customRules
  return product.ruleKeys
    .filter((key) => product.ruleParams || !product.paramRuleKeys?.includes(key))
    .map((key) => t(`home.products.${product.i18nKey}.rules.${key}`, product.ruleParams ?? {}))
}
