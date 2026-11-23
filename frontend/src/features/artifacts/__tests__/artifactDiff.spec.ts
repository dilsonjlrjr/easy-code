import { describe, expect, it } from 'vitest'
import type { ArtifactComparison } from '../api/artifactsApi'
import {
  comparedPair,
  defaultPair,
  diffCounts,
  diffValue,
  pointerText,
} from '../composables/artifactDiff'

const comparison: ArtifactComparison = {
  artifact: 'US-01',
  header: [
    { path: '/titulo', operation: 'modified', before: 'A', after: 'B' },
    { path: '/vinculos/1', operation: 'added', after: 'US-02' },
    { path: '/rnf/0', operation: 'removed', before: 'RNF-09' },
  ],
  body: [
    { operation: 'equal', line_from: 1, line_to: 1, text: '# US-01' },
    { operation: 'removed', line_from: 2, line_to: null, text: 'old' },
    { operation: 'added', line_from: null, line_to: 2, text: 'new' },
    { operation: 'added', line_from: null, line_to: 3, text: 'more' },
  ],
  driving_findings: ['ACH-03'],
}

describe('artifact comparison (US-27, ESP-21)', () => {
  it('counts header fields and body lines by kind of change (ESP-21 8)', () => {
    expect(diffCounts(comparison)).toEqual({ added: 3, removed: 2, modified: 1 })
  })

  it('shows header values and pointers as text', () => {
    expect(diffValue('x')).toBe('x')
    expect(diffValue(3)).toBe('3')
    expect(diffValue(undefined)).toBe('')
    expect(diffValue(['a'])).toBe('[\n  "a"\n]')
    expect(pointerText('/criterios_aceite/0/entao')).toBe('criterios_aceite › 0 › entao')
    expect(pointerText('/a~1b')).toBe('a/b')
    expect(pointerText('')).toBe('/')
  })

  it('compares any two existing versions from the address (ER-21-6)', () => {
    expect(comparedPair({ from: '1', to: '3' }, [1, 2, 3])).toEqual({ from: 1, to: 3 })
    expect(comparedPair({ from: '3', to: '3' }, [1, 2, 3])).toBeUndefined()
    expect(comparedPair({ from: '1', to: '9' }, [1, 2, 3])).toBeUndefined()
    expect(comparedPair({}, [1, 2])).toBeUndefined()
  })

  it('suggests the previous version against the current one', () => {
    expect(defaultPair([1, 3, 2])).toEqual({ from: 2, to: 3 })
    expect(defaultPair([1])).toBeUndefined()
  })
})
