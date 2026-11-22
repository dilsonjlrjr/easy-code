import { describe, expect, it } from 'vitest'
import {
  formatBytes,
  formatDateTime,
  formatDuration,
  formatInteger,
  formatMoney,
  formatRelativeAge,
} from '../formatters'

describe('formatters (RNF-21, ESP-00 3.3)', () => {
  it('formats money from decimal text by the interface language, never with a fixed price', () => {
    expect(formatMoney('pt-BR', { value: '1234.5', currency: 'BRL' }).replace(/\s/g, ' ')).toBe(
      'R$ 1.234,50',
    )
    expect(formatMoney('en', { value: '0.4127', currency: 'USD' })).toBe('$0.4127')
    expect(formatMoney('pt-BR', undefined)).toBe('')
  })

  it('formats dates as dd/mm/aaaa with 24 h in pt-BR', () => {
    const text = formatDateTime('pt-BR', '2026-11-19T16:05:41-03:00')
    expect(text).toMatch(/^\d{2}\/11\/2026,? \d{2}:\d{2}$/)
  })

  it('formats integers with the thousands separator of the language', () => {
    expect(formatInteger('pt-BR', 48210)).toBe('48.210')
    expect(formatInteger('en', 48210)).toBe('48,210')
  })

  it('formats durations, ages and sizes', () => {
    expect(formatDuration('en', 412_300)).toBe('6m 52s')
    expect(formatDuration('en', 12_000)).toBe('12s')
    const now = new Date('2026-11-19T16:30:00Z')
    expect(formatRelativeAge('en', '2026-11-19T16:05:00Z', now)).toBe('25 min. ago')
    expect(formatBytes('en', 10 * 1024 * 1024)).toBe('10 MB')
  })
})
