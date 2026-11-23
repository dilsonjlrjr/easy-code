import type { Money } from '@/shared/api/contract'
import { DEFAULT_ALERT_PERCENTAGES, type Budget } from '../api/projectsApi'

/**
 * The budget form (US-39, ESP-05 3.2, ESP-29 3.3): the stop limit is required and never above the
 * organization's ceiling (RN-1); alerts are percentages of the limit, 1 to 99, no repeats, 50 and
 * 80 by default (RN-2). Money is decimal text on the wire, never a float (openapi `Money`).
 */
export interface BudgetDraft {
  /** The limit as typed: `150`, `150,50` or `1.500,50` (pt-BR) and `1,500.50` (en) all work. */
  limitText: string
  alerts: number[]
}

export type BudgetFieldError = 'limitRequired' | 'limitInvalid' | 'limitAboveCeiling'
export type AlertsError = 'alertOutOfRange' | 'alertRepeated'

export interface BudgetErrors {
  limit?: BudgetFieldError
  alerts?: AlertsError
}

const DECIMAL = /^[0-9]+(\.[0-9]{1,10})?$/

/**
 * Decimal text of an amount typed in either convention. The last `,` or `.` followed by one to
 * ten digits (and not by exactly three, which is a thousands group) is the decimal separator.
 */
export function parseAmount(text: string): string | undefined {
  const compact = text.trim().replace(/\s/g, '')
  if (compact === '') return undefined
  if (!/^[0-9.,]+$/.test(compact)) return undefined
  const lastSeparator = Math.max(compact.lastIndexOf(','), compact.lastIndexOf('.'))
  let integer = compact
  let fraction = ''
  if (lastSeparator >= 0) {
    const tail = compact.slice(lastSeparator + 1)
    const separators = compact.match(/[.,]/g) ?? []
    const isThousandsGroup = tail.length === 3 && separators.length > 1
    const isOnlyThousands =
      tail.length === 3 &&
      separators.length === 1 &&
      compact[lastSeparator] === '.' &&
      compact.slice(0, lastSeparator) !== '0'
    if (!isThousandsGroup && !isOnlyThousands) {
      integer = compact.slice(0, lastSeparator)
      fraction = tail
    }
  }
  integer = integer.replace(/[.,]/g, '')
  if (integer === '') integer = '0'
  const value = fraction ? `${integer}.${fraction}` : integer
  if (!DECIMAL.test(value)) return undefined
  return value.replace(/^0+(?=\d)/, '')
}

export function validateBudget(draft: BudgetDraft, ceiling?: Money | null): BudgetErrors {
  const errors: BudgetErrors = {}
  const amount = parseAmount(draft.limitText)
  if (draft.limitText.trim() === '') errors.limit = 'limitRequired'
  else if (amount === undefined || Number(amount) <= 0) errors.limit = 'limitInvalid'
  else if (ceiling && Number(amount) > Number(ceiling.value)) errors.limit = 'limitAboveCeiling'
  if (draft.alerts.some((value) => !Number.isInteger(value) || value < 1 || value > 99))
    errors.alerts = 'alertOutOfRange'
  else if (new Set(draft.alerts).size !== draft.alerts.length) errors.alerts = 'alertRepeated'
  return errors
}

export function hasBudgetErrors(errors: BudgetErrors): boolean {
  return errors.limit !== undefined || errors.alerts !== undefined
}

/** The `Budget` the api takes; undefined while the draft is invalid. */
export function budgetPayload(
  draft: BudgetDraft,
  currency: string,
  ceiling?: Money | null,
): Budget | undefined {
  if (hasBudgetErrors(validateBudget(draft, ceiling))) return undefined
  const value = parseAmount(draft.limitText)
  if (value === undefined) return undefined
  return {
    stop_limit: { value, currency },
    percentage_alert_limits: [...draft.alerts].sort((left, right) => left - right),
  }
}

export function draftFrom(budget: Budget | null | undefined): BudgetDraft {
  return {
    limitText: budget?.stop_limit.value ?? '',
    alerts: [...(budget?.percentage_alert_limits ?? DEFAULT_ALERT_PERCENTAGES)],
  }
}

/** US-39 CA-5 / ER-29-21: a limit at or below the spend pauses the project by budget. */
export function limitAtOrBelowSpend(budget: Budget | undefined, spend: Money | undefined): boolean {
  if (!budget || !spend) return false
  return Number(budget.stop_limit.value) <= Number(spend.value)
}
