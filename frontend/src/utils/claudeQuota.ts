import type { NormalizedQuotaProjection, QuotaProjection } from '@/types/subscriptionQuota'

export function quotaAtoms(value: unknown): bigint | null {
  if (typeof value !== 'string' || !/^(0|[1-9]\d{0,11})(\.\d{1,8})?$/.test(value)) return null
  const [whole, fraction = ''] = value.split('.')
  return BigInt(whole) * 100000000n + BigInt(fraction.padEnd(8, '0'))
}
export function validQuotaInput(value: string): boolean {
  const atoms = quotaAtoms(value)
  return atoms !== null && atoms > 0n
}
export function formatQuotaUSD(value: unknown): string {
  if (quotaAtoms(value) === null) return '—'
  const [whole, fraction = ''] = (value as string).split('.')
  const decimals = fraction.replace(/0+$/, '').padEnd(2, '0')
  return `$${whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',')}.${decimals}`
}
/** Display ratio only. Never creates or changes the financial balance. */
export function quotaProgress(quota: string, used: string, pending: string): number | null {
  const q = quotaAtoms(quota), u = quotaAtoms(used), p = quotaAtoms(pending)
  if (q === null || u === null || p === null || q === 0n) return null
  const ratio = (u + p) * 10000n / q
  return Number(ratio > 10000n ? 10000n : ratio) / 100
}
export function isTermQuota(value: QuotaProjection): boolean { return value.quota_mode === 'term' }
export function normalizeQuotaProjection<T extends QuotaProjection>(value: T): Omit<T, 'quota_mode' | 'quota_summary'> & NormalizedQuotaProjection {
  return value.quota_mode === 'term'
    ? { ...value, quota_mode: 'term', quota_summary: value.quota_summary ?? null }
    : { ...value, quota_mode: 'windows', quota_summary: null }
}
export function newQuotaOperationKey(): string { return crypto.randomUUID() }
export function quotaError(error: unknown): string {
  const value = error as { message?: string }
  return value?.message || String(error)
}
export function quotaConflict(error: unknown): boolean {
  return (error as { status?: number })?.status === 409
}
const quotaStatuses = new Set(['scheduled', 'active', 'exhausted', 'reconciling', 'expired', 'cancelled', 'closing', 'closed', 'admitted', 'usage_known', 'settled', 'no_charge', 'reconcile_required'])
export function quotaStatusLabel(status: string, translate: (key: string) => string): string {
  return quotaStatuses.has(status) ? translate(`subscriptionQuota.statuses.${status}`) : status
}
