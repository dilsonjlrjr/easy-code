import type { Money } from '@/shared/api/contract'

/**
 * Exact arithmetic on `Money` (decimal text, never floating point: openapi `Money`, RNF-16). Only
 * comparison and difference are needed on the screens; prices are never computed here.
 */

const SCALE = 10
const DECIMAL = /^(-?)(\d+)(?:\.(\d{1,10}))?$/

function toUnits(value: string): bigint | undefined {
  const match = DECIMAL.exec(value.trim())
  if (!match) return undefined
  const [, sign, whole = '0', fraction = ''] = match
  const units = BigInt(whole + fraction.padEnd(SCALE, '0'))
  return sign === '-' ? -units : units
}

function fromUnits(units: bigint): string {
  const negative = units < 0n
  const digits = (negative ? -units : units).toString().padStart(SCALE + 1, '0')
  const whole = digits.slice(0, -SCALE)
  const fraction = digits.slice(-SCALE).replace(/0+$/, '')
  return `${negative ? '-' : ''}${whole}${fraction ? `.${fraction}` : ''}`
}

/** -1, 0 or 1; undefined when a value is missing, malformed or in another currency. */
export function compareMoney(
  left: Money | null | undefined,
  right: Money | null | undefined,
): -1 | 0 | 1 | undefined {
  if (!left || !right || left.currency !== right.currency) return undefined
  const a = toUnits(left.value)
  const b = toUnits(right.value)
  if (a === undefined || b === undefined) return undefined
  return a === b ? 0 : a < b ? -1 : 1
}

/** `left - right` in the same currency; undefined otherwise. */
export function subtractMoney(
  left: Money | null | undefined,
  right: Money | null | undefined,
): Money | undefined {
  if (!left || !right || left.currency !== right.currency) return undefined
  const a = toUnits(left.value)
  const b = toUnits(right.value)
  if (a === undefined || b === undefined) return undefined
  return { value: fromUnits(a - b), currency: left.currency }
}

/** Sum of values in one currency; undefined when empty, malformed or mixed. */
export function sumMoney(values: Array<Money | null | undefined>): Money | undefined {
  const present = values.filter((value): value is Money => !!value)
  const first = present[0]
  if (!first) return undefined
  let total = 0n
  for (const value of present) {
    const units = value.currency === first.currency ? toUnits(value.value) : undefined
    if (units === undefined) return undefined
    total += units
  }
  return { value: fromUnits(total), currency: first.currency }
}

export function isZeroMoney(value: Money | null | undefined): boolean {
  return !value || toUnits(value.value) === 0n
}
