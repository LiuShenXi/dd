import type { SubscriptionQuotaTerm } from './subscriptionQuota'

export interface QuotaRolloutAllocation {
  user_id: number
  quota_usd: string
}
export interface QuotaRolloutKeyUser {
  user_id: number
  email: string
  username: string
  key_count: number
}
export interface QuotaRolloutReplica {
  replica_id: string
  heartbeat_at: string
  capability_version: number
  receipt_version: number
  lease_protocol_version: number
  claude_default_concurrency: number
  issue_enabled: boolean
  admission_enabled: boolean
  recovery_enabled: boolean
  protected_group_ids: number[]
  enabled_group_ids: number[]
  expected_replica_ids: string[]
}
export interface QuotaRolloutState {
  group_id: number
  quota_mode: 'windows' | 'term'
  group_status: string
  maintenance: boolean
  converted: boolean
  revision: number
  server_time: string
  allocations: QuotaRolloutAllocation[]
  key_users: QuotaRolloutKeyUser[]
  protected_pending: number
  pending_orders: number
  legacy_terminal_orders: number
  replicas: QuotaRolloutReplica[]
  blockers: string[]
  preview_token: string
}
export interface QuotaRolloutMaintenanceInput {
  enabled: boolean
  expected_revision: number
  reason: string
}
export interface QuotaRolloutConvertInput {
  allocations: QuotaRolloutAllocation[]
  expected_revision: number
  preview_token: string
}
export interface QuotaRolloutConversion {
  state: QuotaRolloutState
  terms: SubscriptionQuotaTerm[]
  replayed: boolean
}
