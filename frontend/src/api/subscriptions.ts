/**
 * User Subscription API
 * API for regular users to view their own subscriptions and progress
 */

import { apiClient } from './client'
import type { UserSubscription, SubscriptionProgress } from '@/types'
import type { NormalizedQuotaProjection, NormalizedUserSubscription, QuotaProjection } from '@/types/subscriptionQuota'
import { normalizeQuotaProjection } from '@/utils/claudeQuota'

/**
 * Subscription summary for user dashboard
 */
export interface SubscriptionSummary {
  active_count: number
  total_used_usd: number
  subscriptions: Array<QuotaProjection & {
    id: number
    group_id: number
    group_name: string
    status: string
    daily_used_usd?: number
    daily_limit_usd?: number
    weekly_used_usd?: number
    weekly_limit_usd?: number
    monthly_used_usd?: number
    monthly_limit_usd?: number
    expires_at?: string | null
  }>
}

/**
 * Get list of current user's subscriptions
 */
export async function getMySubscriptions(): Promise<NormalizedUserSubscription[]> {
  const response = await apiClient.get<UserSubscription[]>('/subscriptions')
  return (response.data ?? []).map(normalizeQuotaProjection)
}

/**
 * Get current user's active subscriptions
 */
export async function getActiveSubscriptions(): Promise<NormalizedUserSubscription[]> {
  const response = await apiClient.get<UserSubscription[]>('/subscriptions/active')
  return (response.data ?? []).map(normalizeQuotaProjection)
}

/**
 * Get progress for all user's active subscriptions
 */
export type SubscriptionProgressInfo = { subscription: NormalizedUserSubscription; progress: SubscriptionProgress & NormalizedQuotaProjection }
export async function getSubscriptionsProgress(): Promise<SubscriptionProgressInfo[]> {
  const response = await apiClient.get<Array<{ subscription: UserSubscription; progress: SubscriptionProgress }>>('/subscriptions/progress')
  return (response.data ?? []).map(item => ({ subscription: normalizeQuotaProjection(item.subscription), progress: normalizeQuotaProjection(item.progress) }))
}

/**
 * Get subscription summary for dashboard display
 */
export async function getSubscriptionSummary(): Promise<SubscriptionSummary> {
  const response = await apiClient.get<SubscriptionSummary>('/subscriptions/summary')
  return { ...response.data, subscriptions: (response.data.subscriptions ?? []).map(normalizeQuotaProjection) }
}

/**
 * Get progress for a specific subscription
 */
export async function getSubscriptionProgress(
  subscriptionId: number
): Promise<SubscriptionProgress> {
  const response = await apiClient.get<SubscriptionProgress>(
    `/subscriptions/${subscriptionId}/progress`
  )
  return normalizeQuotaProjection(response.data)
}

export default {
  getMySubscriptions,
  getActiveSubscriptions,
  getSubscriptionsProgress,
  getSubscriptionSummary,
  getSubscriptionProgress
}
