import { apiClient } from '../client'
import type { QuotaRolloutAllocation, QuotaRolloutConversion, QuotaRolloutConvertInput, QuotaRolloutMaintenanceInput, QuotaRolloutState } from '@/types/subscriptionQuotaRollout'

const path = (groupId: number) => `/admin/groups/${groupId}/subscription-quota/rollout`
const headers = (key: string) => ({ 'Idempotency-Key': key })
// Keep nullable list compatibility at this boundary; components always receive arrays.
function normalize(state: QuotaRolloutState): QuotaRolloutState {
  return { ...state, allocations: state.allocations ?? [], key_users: state.key_users ?? [], blockers: state.blockers ?? [], replicas: (state.replicas ?? []).map(replica => ({ ...replica, protected_group_ids: replica.protected_group_ids ?? [], enabled_group_ids: replica.enabled_group_ids ?? [], expected_replica_ids: replica.expected_replica_ids ?? [] })) }
}
export const adminSubscriptionQuotaRolloutAPI = {
  async state(groupId: number) {
    return normalize((await apiClient.get<QuotaRolloutState>(path(groupId))).data)
  },
  async maintenance(groupId: number, input: QuotaRolloutMaintenanceInput, key: string) {
    return normalize((await apiClient.post<QuotaRolloutState>(`${path(groupId)}/maintenance`, input, { headers: headers(key) })).data)
  },
  async preview(groupId: number, allocations: QuotaRolloutAllocation[]) {
    return normalize((await apiClient.post<QuotaRolloutState>(`${path(groupId)}/preview`, { allocations })).data)
  },
  async convert(groupId: number, input: QuotaRolloutConvertInput, key: string): Promise<QuotaRolloutConversion> {
    const { data } = await apiClient.post<QuotaRolloutConversion>(`${path(groupId)}/convert`, input, { headers: headers(key) })
    return { ...data, state: normalize(data.state), terms: data.terms ?? [] }
  }
}
