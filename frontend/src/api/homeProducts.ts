/**
 * 首页车次卡片 API
 * 公开接口返回真实套餐的价格/额度；管理端接口只读写展示配置（状态、规则文案、关联套餐）。
 */

import { apiClient } from './client'

export type HomeProductStatus = 'available' | 'pilot' | 'soon' | 'sold_out'

export interface HomeCarpoolTier {
  code: string
  name: string
  seats: number
  price_cny: string
  weekly_quota_usd: string
  duration_days: number
  cycle_days: number
  boost_ratio: string
  boost_count: number
}

export interface HomeProducts {
  gpt_carpool: {
    enabled: boolean
    status: HomeProductStatus
    rules: string[]
    tiers: HomeCarpoolTier[]
  }
  claude_weekly: {
    enabled: boolean
    status: HomeProductStatus
    rules: string[]
    plan_id: number
    plan_name: string
    price_cny: number | null
    original_price: number | null
    quota_usd: string | null
    rate_multiplier: number | null
    duration_days: number
  }
  contact: string
}

export interface HomeProductsSettings {
  gpt_carpool: {
    enabled: boolean
    status: HomeProductStatus
    rules: string[]
  }
  claude_weekly: {
    enabled: boolean
    status: HomeProductStatus
    plan_id: number
    rules: string[]
  }
  contact: string
}

export async function getHomeProducts(): Promise<HomeProducts> {
  const { data } = await apiClient.get<HomeProducts>('/home/products')
  return data
}

export async function getHomeProductsSettings(): Promise<HomeProductsSettings> {
  const { data } = await apiClient.get<HomeProductsSettings>('/admin/settings/home-products')
  return data
}

export async function updateHomeProductsSettings(settings: HomeProductsSettings): Promise<HomeProductsSettings> {
  const { data } = await apiClient.put<HomeProductsSettings>('/admin/settings/home-products', settings)
  return data
}
