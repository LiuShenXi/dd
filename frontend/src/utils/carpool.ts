import type { CarpoolUserCycle } from '@/types/carpool'

const DAY_MS = 86_400_000

export function parseCarpoolAmount(value: string | null | undefined): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}

export function formatCarpoolAmount(value: string | null | undefined): string {
  return new Intl.NumberFormat('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(parseCarpoolAmount(value))
}

export function formatCarpoolTargetAmount(value: string | null | undefined): string {
  return new Intl.NumberFormat('en-US', {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  }).format(parseCarpoolAmount(value))
}

export function cycleProgress(cycle: CarpoolUserCycle, nowMs: number): number {
  const start = Date.parse(cycle.starts_at)
  const end = Date.parse(cycle.ends_at)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return 0
  return Math.min(1, Math.max(0, (nowMs - start) / (end - start)))
}

export function cycleGridTemplate(cycles: CarpoolUserCycle[]): string {
  return cycles.map((cycle) => {
    const durationDays = (Date.parse(cycle.ends_at) - Date.parse(cycle.starts_at)) / DAY_MS
    return `${Number.isFinite(durationDays) && durationDays > 0 ? Number(durationDays.toFixed(6)) : 1}fr`
  }).join(' ')
}

export function termEventPosition(occurredAt: string, startsAt: string, expiresAt: string): number | null {
  const occurred = Date.parse(occurredAt)
  const starts = Date.parse(startsAt)
  const expires = Date.parse(expiresAt)
  if (![occurred, starts, expires].every(Number.isFinite) || expires <= starts || occurred < starts || occurred >= expires) return null
  return ((occurred - starts) / (expires - starts)) * 100
}

export interface CountdownParts {
  days: number
  hours: number
  minutes: number
  seconds: number
}

export function countdownParts(target: string | null, nowMs: number): CountdownParts | null {
  if (!target) return null
  const targetMs = Date.parse(target)
  if (!Number.isFinite(targetMs)) return null
  let remaining = Math.max(0, targetMs - nowMs)
  const days = Math.floor(remaining / 86_400_000)
  remaining -= days * 86_400_000
  const hours = Math.floor(remaining / 3_600_000)
  remaining -= hours * 3_600_000
  const minutes = Math.floor(remaining / 60_000)
  remaining -= minutes * 60_000
  return { days, hours, minutes, seconds: Math.floor(remaining / 1000) }
}
