export type DecimalString = string

export type CarpoolTermStatus = 'pending' | 'active' | 'terminated' | 'expired'
export type CarpoolCycleStatus = 'scheduled' | 'active' | 'closing' | 'closed' | 'missed'
export type CarpoolResetStatus = 'none' | 'scheduled' | 'executing' | 'delayed' | 'completed'
export type CarpoolOpeningMode = 'new' | 'takeover'
export type CarpoolPaymentKind = 'payment' | 'refund'
export type CarpoolBucket = 'base' | 'boost' | 'manual'

export interface CarpoolUserCycle {
  cycle_no: number
  starts_at: string
  ends_at: string
  status: CarpoolCycleStatus
}

export interface CarpoolUserTerm {
  status: CarpoolTermStatus
  starts_at: string
  expires_at: string
  reset_count: number
  reset_count_basis: 'current_term'
  current_cycle_no: number | null
  cycles: CarpoolUserCycle[]
  reset_events: CarpoolResetEvent[]
}

export interface CarpoolResetEvent {
  cycle_no: number
  occurred_at: string
  target_quota_usd: DecimalString
}

export interface CarpoolUserQuota {
  available_usd: DecimalString
}

export interface CarpoolResetWindow {
  status: CarpoolResetStatus
  scheduled_at: string | null
  schedule_revision: number
  eligible_for_me: boolean
  ineligible_reason: string | null
}

export interface CarpoolDetails {
  server_now: string
  timezone: 'Asia/Shanghai' | string
  quota: CarpoolUserQuota | null
  usage: {
    total_used_usd: DecimalString
    history_complete: boolean
    statistics_since: string | null
  }
  term: CarpoolUserTerm | null
  reset_window: CarpoolResetWindow
}

export interface CarpoolBoostStatus {
  eligible: boolean
  remaining: number
  total: number
  amount_usd: DecimalString
  help_text: string
  unavailable_reason: string | null
}

export interface CarpoolPlan {
  plan_id: number
  code: string
  name: string
  version: number
  list_price_cny: DecimalString
  weekly_quota_usd: DecimalString
  cycle_5_quota_usd?: DecimalString | null
  duration_days: number
  cycle_days: number
  boost_ratio: DecimalString
  boost_amount_usd: DecimalString
  boost_count: number
  rounding_mode: string
  enabled: boolean
  is_latest: boolean
}

export interface CarpoolPreviewCycle {
  id?: number
  cycle_no: number
  starts_at: string
  ends_at: string
  base_quota_usd: DecimalString
  initial_action: string
  status?: CarpoolCycleStatus
}

export interface CarpoolTakeoverInput {
  current_base_balance_usd: DecimalString
  current_boost_balance_usd: DecimalString
  current_manual_balance_usd: DecimalString
  boost_used: number
  history_complete: boolean
  historical_used_usd?: DecimalString | null
  statistics_since?: string | null
  ordinary_balance_transfer_usd?: DecimalString | null
}

export interface CarpoolPreviewRequest {
  plan_id: number
  starts_at: string | null
  mode: CarpoolOpeningMode
  takeover: CarpoolTakeoverInput | null
}

export interface CarpoolPreview {
  calculated_at: string
  mode: CarpoolOpeningMode
  plan: CarpoolPlan
  starts_at: string
  expires_at: string
  cycles: CarpoolPreviewCycle[]
  warnings: string[]
}

export interface CarpoolPaymentInput {
  amount_cny: DecimalString
  payment_kind: CarpoolPaymentKind
  paid_at: string
  channel: string
  external_order_no: string | null
  notes: string | null
}

export interface CarpoolOpenTermRequest extends CarpoolPreviewRequest {
  group_id: number
  scope_id?: number
  notes: string | null
  payment: CarpoolPaymentInput | null
}

export interface CarpoolRenewTermRequest {
  plan_id: number
  notes: string | null
  payment: CarpoolPaymentInput | null
}

export interface CarpoolAdminCycle {
  id: number
  term_id: number
  user_id: number
  cycle_no: number
  starts_at: string
  ends_at: string
  base_quota_usd: DecimalString
  base_balance_usd: DecimalString
  boost_balance_usd: DecimalString
  manual_balance_usd: DecimalString
  available_usd: DecimalString
  initial_granted_usd: DecimalString
  reset_granted_usd: DecimalString
  boost_granted_usd: DecimalString
  adjustment_net_usd: DecimalString
  used_usd: DecimalString
  expired_usd: DecimalString
  state: CarpoolCycleStatus
  revision: number
}

export interface CarpoolAdminTerm {
  id: number
  user_id: number
  scope_id: number
  group_id: number
  plan_id: number
  plan_snapshot: CarpoolPlan
  starts_at: string
  expires_at: string
  status: CarpoolTermStatus
  boost_used: number
  boost_remaining: number
  history_complete: boolean
  statistics_since: string | null
  payment_net_cny: DecimalString
  current_cycle: CarpoolAdminCycle | null
  cycles?: CarpoolAdminCycle[]
  payments?: CarpoolPaymentRecord[]
  created_at: string
}

export interface CarpoolPaymentRecord extends CarpoolPaymentInput {
  id: number
  term_id: number
  recorded_by: number
  recorded_at: string
}

export interface CarpoolLedgerEntry {
  id: number
  user_id: number
  user_email?: string
  term_id: number
  cycle_id: number
  cycle_no?: number
  event_type: string
  bucket: CarpoolBucket
  delta_usd: DecimalString
  event_key: string
  api_key_id: number | null
  request_id: string | null
  reset_batch_id: number | null
  boost_slot: number | null
  actor_id: number | null
  reverses_ledger_id: number | null
  reason: string | null
  effective_at: string
  recorded_at: string
}

export interface CarpoolBillingException {
  id: number
  request_id: string
  api_key_id: number
  user_id: number
  group_id: number
  term_id: number
  cycle_id: number
  admitted_at: string
  status: string
  known_cost_usd: DecimalString | null
  retry_count: number
  sanitized_error: string | null
  updated_at: string
  resolution: 'no_cost' | 'actual_cost' | null
  resolved_at: string | null
  resolved_by: number | null
  reason: string | null
  actual_cost_usd: DecimalString | null
  auxiliary_usage_reconstructed: false
}

export interface CarpoolBillingReconcileRequest {
  confirmed: true
  resolution: 'no_cost' | 'actual_cost'
  actual_cost_usd: DecimalString | null
  reason: string
}

export interface CarpoolResetBatch {
  id: number
  scope_id: number
  status: string
  detected_at?: string | null
  qualified_at?: string | null
  slot_at?: string | null
  scheduled_at?: string | null
  schedule_revision: number
  effective_at?: string | null
  completed_at?: string | null
  delay_reason?: string | null
  announcement_state?: string | null
  target_count?: number
  granted_usd?: DecimalString
}

export interface CarpoolResetObservation {
  upstream_identity_hash: string
  baseline_complete: boolean
  last_complete_at: string | null
  health_status: string
  known_credit_count: number
  unassigned_credit_count: number
}

export interface CarpoolResetScanResult {
  scanned: number
  complete: number
  incomplete: number
  new_candidates: number
}

export interface CarpoolListResponse<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export interface CarpoolListParams {
  page?: number
  page_size?: number
  user_id?: number
  plan_id?: number
  status?: string
  starts_from?: string
  starts_to?: string
  term_id?: number
  cycle_id?: number
  cycle_no?: number
  state?: string
  event_type?: string
  bucket?: CarpoolBucket
  scope_id?: number
}

export interface CarpoolPlanVersionRequest {
  name: string
  list_price_cny: DecimalString
  weekly_quota_usd: DecimalString
  boost_ratio: DecimalString
  boost_count: number
  enabled: boolean
}
