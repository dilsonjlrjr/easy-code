import type { Money } from './types'

/** Money of the api (decimal text) in the interface language. */
export function formatMoney(money: Money | undefined, locale: string): string | undefined {
  if (!money) return undefined
  const value = Number(money.value)
  if (!Number.isFinite(value)) return `${money.value} ${money.currency}`
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: money.currency,
    maximumFractionDigits: value !== 0 && Math.abs(value) < 1 ? 4 : 2,
  }).format(value)
}

/** br.easycode.wave.completed.v1 → wave.completed */
export function shortType(type: string): string {
  return type.replace(/^br\.easycode\./, '').replace(/\.v\d+$/, '')
}

/** Whole minutes left until an instant, never negative. */
export function minutesLeft(until: string, now: number): number {
  return Math.max(0, Math.ceil((Date.parse(until) - now) / 60_000))
}
