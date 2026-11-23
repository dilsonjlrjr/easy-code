import { describe, expect, it } from 'vitest'
import {
  budgetPayload,
  draftFrom,
  limitAtOrBelowSpend,
  parseAmount,
  validateBudget,
} from '../composables/budgetForm'

const BRL = (value: string) => ({ value, currency: 'BRL' })

describe('budget form (US-39, ESP-29 3.3)', () => {
  it('reads the amount in pt-BR and en conventions as decimal text', () => {
    expect(parseAmount('150')).toBe('150')
    expect(parseAmount('150,50')).toBe('150.50')
    expect(parseAmount('1.500,50')).toBe('1500.50')
    expect(parseAmount('1,500.50')).toBe('1500.50')
    expect(parseAmount('1.500')).toBe('1500')
    expect(parseAmount('0.5')).toBe('0.5')
    expect(parseAmount('abc')).toBeUndefined()
    expect(parseAmount('')).toBeUndefined()
  })

  it('RN-1, CA-29-9: the stop limit is required, positive and not above the ceiling', () => {
    expect(validateBudget({ limitText: '', alerts: [50, 80] }).limit).toBe('limitRequired')
    expect(validateBudget({ limitText: '0', alerts: [50, 80] }).limit).toBe('limitInvalid')
    expect(validateBudget({ limitText: 'x', alerts: [] }).limit).toBe('limitInvalid')
    expect(validateBudget({ limitText: '600', alerts: [] }, BRL('500')).limit).toBe(
      'limitAboveCeiling',
    )
    expect(validateBudget({ limitText: '500', alerts: [] }, BRL('500')).limit).toBeUndefined()
  })

  it('RN-2, CA-29-10: alerts are 1 to 99 with no repeats', () => {
    expect(validateBudget({ limitText: '10', alerts: [0] }).alerts).toBe('alertOutOfRange')
    expect(validateBudget({ limitText: '10', alerts: [100] }).alerts).toBe('alertOutOfRange')
    expect(validateBudget({ limitText: '10', alerts: [60, 60] }).alerts).toBe('alertRepeated')
    expect(validateBudget({ limitText: '10', alerts: [60, 90] }).alerts).toBeUndefined()
  })

  it('builds the Budget payload, alerts sorted, defaults 50 and 80 (CA-05-9)', () => {
    expect(draftFrom(null)).toEqual({ limitText: '', alerts: [50, 80] })
    expect(budgetPayload({ limitText: '200,00', alerts: [90, 60] }, 'BRL')).toEqual({
      stop_limit: BRL('200.00'),
      percentage_alert_limits: [60, 90],
    })
    expect(budgetPayload({ limitText: '', alerts: [] }, 'BRL')).toBeUndefined()
  })

  it('CA-5: tells when the new limit is at or below the spend', () => {
    const budget = { stop_limit: BRL('40') }
    expect(limitAtOrBelowSpend(budget, BRL('42'))).toBe(true)
    expect(limitAtOrBelowSpend(budget, BRL('40'))).toBe(true)
    expect(limitAtOrBelowSpend(budget, BRL('10'))).toBe(false)
  })
})
