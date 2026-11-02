import type { QuotaRolloutState } from '@/types/subscriptionQuotaRollout'
export function rolloutState(overrides: Partial<QuotaRolloutState> = {}): QuotaRolloutState {
  return {
    group_id: 6, quota_mode: 'windows', group_status: 'quota_maintenance', maintenance: true, converted: false,
    revision: 1, server_time: '2026-10-20T00:00:00Z', allocations: [],
    key_users: [{ user_id: 42, email: 'a@example.invalid', username: 'User A', key_count: 2 }, { user_id: 43, email: 'b@example.invalid', username: 'User B', key_count: 1 }],
    protected_pending: 0, pending_orders: 0, legacy_terminal_orders: 0,
    replicas: [{ replica_id: 'test-one', heartbeat_at: '2026-10-20T00:00:00Z', capability_version: 1, receipt_version: 1, lease_protocol_version: 1, claude_default_concurrency: 3, issue_enabled: true, admission_enabled: true, recovery_enabled: true, protected_group_ids: [6], enabled_group_ids: [6], expected_replica_ids: ['test-one'] }],
    blockers: [], preview_token: '', ...overrides
  }
}
