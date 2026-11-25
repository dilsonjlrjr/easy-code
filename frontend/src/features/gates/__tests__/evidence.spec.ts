import { describe, expect, it } from 'vitest'
import { evidenceTarget } from '../evidence'

describe('evidenceTarget (ESP-12: navigable evidence references)', () => {
  it('names waves, findings, artifacts and commits by the contract id patterns', () => {
    expect(evidenceTarget('DEV-03')).toEqual({ kind: 'wave', id: 'DEV-03' })
    expect(evidenceTarget('ACH-12')).toEqual({ kind: 'finding', id: 'ACH-12' })
    expect(evidenceTarget('US-01')).toEqual({ kind: 'artifact', id: 'US-01' })
    expect(evidenceTarget('VISAO')).toEqual({ kind: 'artifact', id: 'VISAO' })
    const sha = 'a'.repeat(40)
    expect(evidenceTarget(sha)).toEqual({ kind: 'commit', id: sha, short: 'aaaaaaa' })
  })

  it('opens the artifact of an item inside it and keeps anything else as text', () => {
    expect(evidenceTarget('TELAS/S-01')).toEqual({ kind: 'artifact', id: 'TELAS', item: 'S-01' })
    expect(evidenceTarget('docs/app.md')).toEqual({ kind: 'text', id: 'docs/app.md' })
    expect(evidenceTarget('<script>')).toEqual({ kind: 'text', id: '<script>' })
  })
})
