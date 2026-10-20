import { apiClient } from './client'
import type { QuotaPage, SubscriptionQuotaSummary, SubscriptionQuotaTerm, SubscriptionQuotaUsage } from '@/types/subscriptionQuota'

export const subscriptionQuotaAPI = {
  async summaries() {
    return (await apiClient.get<SubscriptionQuotaSummary[]>('/subscription-quota')).data ?? []
  },
  async summary(groupId: number) {
    return (await apiClient.get<SubscriptionQuotaSummary>(`/subscription-quota/groups/${groupId}`)).data
  },
  async terms(groupId: number, cursor?: string) {
    return (await apiClient.get<QuotaPage<SubscriptionQuotaTerm>>(`/subscription-quota/groups/${groupId}/terms`, { params: { limit: 20, ...(cursor ? { cursor } : {}) } })).data
  },
  async usage(termId: number, cursor?: string) {
    return (await apiClient.get<QuotaPage<SubscriptionQuotaUsage>>(`/subscription-quota/terms/${termId}/usage`, { params: { limit: 20, ...(cursor ? { cursor } : {}) } })).data
  }
}
