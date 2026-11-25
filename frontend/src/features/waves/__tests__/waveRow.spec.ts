import { describe, expect, it } from 'vitest'
import { waveElapsedMs, waveTokenTotals } from '../composables/waveRow'

describe('waveElapsedMs (ESP-06 CA-06-11)', () => {
  it('is undefined before the wave starts', () => {
    expect(waveElapsedMs({ started_at: null, ended_at: null })).toBeUndefined()
  })

  it('runs from start to end of an ended wave', () => {
    expect(
      waveElapsedMs({ started_at: '2026-11-24T10:00:00Z', ended_at: '2026-11-24T10:41:00Z' }),
    ).toBe(41 * 60_000)
  })

  it('runs to now while the wave is open', () => {
    expect(
      waveElapsedMs(
        { started_at: '2026-11-24T10:00:00Z', ended_at: null },
        new Date('2026-11-24T10:12:00Z'),
      ),
    ).toBe(12 * 60_000)
  })
})

describe('waveTokenTotals', () => {
  it('adds input and output, and the two cache kinds apart', () => {
    expect(waveTokenTotals({ input: 100, output: 20, read_cache: 5, write_cache: 7 })).toEqual({
      main: 120,
      cache: 12,
    })
  })

  it('is zero without tokens', () => {
    expect(waveTokenTotals(undefined)).toEqual({ main: 0, cache: 0 })
  })
})
