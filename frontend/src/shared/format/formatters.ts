import type { Money } from '@/shared/api/contract'

/**
 * Locale-aware formatting (RNF-21, ESP-00 3.3): dates dd/mm/aaaa 24 h in pt-BR, numbers and money
 * by the interface language. Money arrives as decimal text and is never computed as a price here.
 */

export function formatMoney(locale: string, money: Money | null | undefined): string {
  if (!money) return ''
  const amount = Number(money.value)
  if (!Number.isFinite(amount)) return `${money.value} ${money.currency}`
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: money.currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 4,
  }).format(amount)
}

/** A price per million tokens as decimal text, in the given currency. */
export function formatDecimalAmount(locale: string, value: string, currency: string): string {
  return formatMoney(locale, { value, currency })
}

/** A percentage given in points (`80` → "80%"), at most one decimal. */
export function formatPercent(locale: string, points: number | null | undefined): string {
  if (points === null || points === undefined || !Number.isFinite(points)) return ''
  return new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 1 }).format(
    points / 100,
  )
}

/** Share of `part` in `whole`, in points; undefined when the whole is not a positive amount. */
export function moneyShare(part: Money | null | undefined, whole: Money | null | undefined) {
  const total = Number(whole?.value)
  const value = Number(part?.value)
  if (!Number.isFinite(total) || total <= 0 || !Number.isFinite(value)) return undefined
  return (value / total) * 100
}

export function formatInteger(locale: string, value: number | null | undefined): string {
  if (value === null || value === undefined) return ''
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(value)
}

export function formatDateTime(locale: string, iso: string | null | undefined): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(locale, {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(date)
}

export function formatDate(locale: string, iso: string | null | undefined): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(locale, {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    timeZone: /^\d{4}-\d{2}-\d{2}$/.test(iso) ? 'UTC' : undefined,
  }).format(date)
}

const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** "há 25 min" style age; the full date goes in the accessible name next to it. */
export function formatRelativeAge(locale: string, iso: string, now: Date = new Date()): string {
  const elapsed = now.getTime() - new Date(iso).getTime()
  const format = new Intl.RelativeTimeFormat(locale, { numeric: 'auto', style: 'short' })
  if (elapsed < MINUTE) return format.format(-Math.max(0, Math.round(elapsed / SECOND)), 'second')
  if (elapsed < HOUR) return format.format(-Math.round(elapsed / MINUTE), 'minute')
  if (elapsed < DAY) return format.format(-Math.round(elapsed / HOUR), 'hour')
  return format.format(-Math.round(elapsed / DAY), 'day')
}

/** Duration in milliseconds as "1 h 05 min", "3 min 20 s" or "12 s". */
export function formatDuration(locale: string, milliseconds: number): string {
  const format = (value: number, unit: 'hour' | 'minute' | 'second') =>
    new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: 'narrow' }).format(value)
  const hours = Math.floor(milliseconds / HOUR)
  const minutes = Math.floor((milliseconds % HOUR) / MINUTE)
  const seconds = Math.round((milliseconds % MINUTE) / SECOND)
  if (hours > 0) return `${format(hours, 'hour')} ${format(minutes, 'minute')}`
  if (minutes > 0) return `${format(minutes, 'minute')} ${format(seconds, 'second')}`
  return format(seconds, 'second')
}

/** ESP-25 3: cycle and execution time in days, hours and minutes once they pass a day. */
export function formatLongDuration(locale: string, milliseconds: number): string {
  if (milliseconds < DAY) return formatDuration(locale, milliseconds)
  const format = (value: number, unit: 'day' | 'hour' | 'minute') =>
    new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: 'narrow' }).format(value)
  const days = Math.floor(milliseconds / DAY)
  const hours = Math.floor((milliseconds % DAY) / HOUR)
  const minutes = Math.floor((milliseconds % HOUR) / MINUTE)
  return `${format(days, 'day')} ${format(hours, 'hour')} ${format(minutes, 'minute')}`
}

export function formatBytes(locale: string, bytes: number): string {
  const units = ['byte', 'kilobyte', 'megabyte', 'gigabyte'] as const
  let value = bytes
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex += 1
  }
  return new Intl.NumberFormat(locale, {
    style: 'unit',
    unit: units[unitIndex],
    unitDisplay: 'short',
    maximumFractionDigits: unitIndex === 0 ? 0 : 1,
  }).format(value)
}
