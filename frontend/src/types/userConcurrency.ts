export interface UserConcurrencyProjection {
  concurrency?: number
  current_concurrency?: number | null
  legacy_current_concurrency?: number | null
  legacy_concurrency_status?: 'available' | 'unavailable'
  claude_subscription_concurrency?: number | null
  default_claude_subscription_concurrency?: number
  effective_claude_subscription_concurrency?: number | null
  claude_current_concurrency?: number | null
  claude_waiting_count?: number | null
  claude_concurrency_status?: 'available' | 'unavailable' | 'not_configured'
}
