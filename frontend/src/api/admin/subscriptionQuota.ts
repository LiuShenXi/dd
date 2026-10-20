import { apiClient } from '../client'
import type { QuotaPage, QuotaRequestStatus, QuotaResolveInput, QuotaResolveResult, SubscriptionQuotaMutation, SubscriptionQuotaOpenInput, SubscriptionQuotaPreview, SubscriptionQuotaPreviewInput, SubscriptionQuotaRequest, SubscriptionQuotaSummary, SubscriptionQuotaTerm } from '@/types/subscriptionQuota'

const userPath = (id: number) => `/admin/users/${id}/subscription-quota`
const headers = (key: string) => ({ 'Idempotency-Key': key })
export const adminSubscriptionQuotaAPI = {
  async summary(userId: number, groupId: number) {
    return (await apiClient.get<SubscriptionQuotaSummary>(userPath(userId), { params: { group_id: groupId } })).data
  },
  async terms(userId: number, groupId: number, cursor?: string) {
    return (await apiClient.get<QuotaPage<SubscriptionQuotaTerm>>(`${userPath(userId)}/terms`, { params: { group_id: groupId, limit: 20, ...(cursor ? { cursor } : {}) } })).data
  },
  async preview(userId: number, input: SubscriptionQuotaPreviewInput) {
    return (await apiClient.post<SubscriptionQuotaPreview>(`${userPath(userId)}/terms/preview`, input)).data
  },
  async open(userId: number, input: SubscriptionQuotaOpenInput, key: string) {
    return (await apiClient.post<SubscriptionQuotaMutation>(`${userPath(userId)}/terms`, input, { headers: headers(key) })).data
  },
  async cancel(termId: number, expectedRevision: number, reason: string, key: string) {
    return (await apiClient.post<SubscriptionQuotaMutation>(`/admin/subscription-quota/terms/${termId}/cancel`, { expected_revision: expectedRevision, reason }, { headers: headers(key) })).data
  },
  async requests(params: { cursor?: string; limit?: number; status?: QuotaRequestStatus; user_id?: number; group_id?: number }) {
    return (await apiClient.get<QuotaPage<SubscriptionQuotaRequest>>('/admin/subscription-quota/requests', { params })).data
  },
  async resolve(rowId: number, input: QuotaResolveInput, key: string) {
    return (await apiClient.post<QuotaResolveResult>(`/admin/subscription-quota/requests/${rowId}/resolve`, input, { headers: headers(key) })).data
  }
}
export default adminSubscriptionQuotaAPI
