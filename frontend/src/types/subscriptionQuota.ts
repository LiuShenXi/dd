/** Financial amounts remain server decimal strings throughout the UI. */
import type { UserConcurrencyProjection } from './userConcurrency'
export type QuotaUSD = string
export interface SubscriptionQuotaTerm {
  id: number
  group_id: number
  subscription_id: number
  term_no: number
  quota_usd: QuotaUSD
  used_usd: QuotaUSD
  pending_usd: QuotaUSD
  remaining_usd: QuotaUSD
  status: string
  lifecycle_state: string
  starts_at: string
  expires_at: string
  cancelled_at: string | null
  revision: number
  unsettled_request_count: number
}
export interface SubscriptionQuotaSummary {
  group_id: number
  quota_mode: 'term'
  revision: number
  server_time: string
  current_term: SubscriptionQuotaTerm | null
  last_term: SubscriptionQuotaTerm | null
  next_terms: SubscriptionQuotaTerm[]
}
export interface QuotaProjection {
  quota_mode?: 'windows' | 'term'
  quota_summary?: SubscriptionQuotaSummary | null
}
/** Wire input may omit mode on old servers. Components consume this normalized union. */
export type NormalizedQuotaProjection =
  | { quota_mode: 'windows'; quota_summary: null }
  | { quota_mode: 'term'; quota_summary: SubscriptionQuotaSummary | null }
export type NormalizedUserSubscription = Omit<import('./index').UserSubscription, 'quota_mode' | 'quota_summary'> & NormalizedQuotaProjection
export interface SubscriptionQuotaPreviewInput {
  group_id: number
  operation: 'open' | 'renew'
  quota_usd?: QuotaUSD
  plan_id?: number
  notes?: string
}
export interface SubscriptionQuotaOpenInput extends SubscriptionQuotaPreviewInput {
  expected_revision: number
}
export interface SubscriptionQuotaPreview {
  group_id: number
  effective_quota_usd: QuotaUSD
  proposed_starts_at: string
  proposed_expires_at: string
  server_time: string
  current_revision: number
  warnings: string[]
  current_term: SubscriptionQuotaTerm | null
  next_terms: SubscriptionQuotaTerm[]
}
export interface SubscriptionQuotaMutation {
  term: SubscriptionQuotaTerm
  summary: SubscriptionQuotaSummary
  replayed: boolean
}
export interface QuotaPage<T> {
  items: T[]
  next_cursor: string | null
  has_more: boolean
}
export interface SubscriptionQuotaUsage {
  request_id: string
  api_key_id: number
  term_id: number
  model: string
  actual_cost_usd: QuotaUSD
  settled_at: string
  input_tokens: number
  output_tokens: number
}
export type QuotaRequestStatus = 'admitted' | 'usage_known' | 'settled' | 'no_charge' | 'reconcile_required'
export interface SubscriptionQuotaRequest {
  id: number
  request_id: string
  user_id: number
  group_id: number
  term_id: number
  api_key_id: number
  status: QuotaRequestStatus
  revision: number
  actual_cost_usd: QuotaUSD | null
  admitted_at: string
  updated_at: string
  settled_at: string | null
  last_error_category: string | null
}
export interface QuotaVerifiedUsage {
  requested_model?: string
  upstream_model?: string
  response_model?: string
  service_tier?: string
  search_call_count?: number
  input_tokens: number
  output_tokens: number
  cache_creation_5m_tokens: number
  cache_creation_1h_tokens: number
  cache_read_tokens: number
}
export type QuotaResolveInput = {
  expected_request_revision: number
  evidence_reference: string
  reason: string
} & ({ resolution: 'no_charge' } | { resolution: 'verified_usage'; verified_usage: QuotaVerifiedUsage })
export interface QuotaResolveResult { request: SubscriptionQuotaRequest; replayed: boolean }
export interface ClaudeKeyUsage extends UserConcurrencyProjection {
  billing_scope: 'claude_subscription'
  quota_mode: 'term'
  quota_summary: SubscriptionQuotaSummary | null
  mode: 'quota_limited' | 'unrestricted'
  remaining: QuotaUSD
  unit: 'USD'
  expires_at: string | null
  quota?: unknown
  rate_limits?: unknown
}
