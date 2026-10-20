import type { SubscriptionQuotaSummary, SubscriptionQuotaTerm } from '@/types/subscriptionQuota'
export function quotaTerm(overrides: Partial<SubscriptionQuotaTerm> = {}): SubscriptionQuotaTerm {
  return { id: 10, group_id: 6, subscription_id: 2, term_no: 1, quota_usd: '100.00000000', used_usd: '12.00000000', pending_usd: '3.00000000', remaining_usd: '85.00000000', status: 'active', lifecycle_state: 'active', starts_at: '2026-10-20T00:00:00Z', expires_at: '2026-10-27T00:00:00Z', cancelled_at: null, revision: 1, unsettled_request_count: 1, ...overrides }
}
export function quotaSummary(overrides: Partial<SubscriptionQuotaSummary> = {}): SubscriptionQuotaSummary {
  return { group_id: 6, quota_mode: 'term', revision: 4, server_time: '2026-10-20T00:00:00Z', current_term: quotaTerm(), last_term: null, next_terms: [], ...overrides }
}
export function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
