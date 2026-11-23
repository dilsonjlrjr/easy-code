import type { LocationQuery } from 'vue-router'
import type { ArtifactComparison } from '../api/artifactsApi'

export interface DiffCounts {
  added: number
  removed: number
  modified: number
}

/** ESP-21 8: the count of changes is announced; header fields and body lines both count. */
export function diffCounts(comparison: ArtifactComparison): DiffCounts {
  const counts: DiffCounts = { added: 0, removed: 0, modified: 0 }
  for (const change of comparison.header) counts[change.operation] += 1
  for (const line of comparison.body) {
    if (line.operation === 'added') counts.added += 1
    if (line.operation === 'removed') counts.removed += 1
  }
  return counts
}

/** A header value as text: scalars as they are, lists and objects as indented JSON. */
export function diffValue(value: unknown): string {
  if (value === undefined || value === null) return ''
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  return JSON.stringify(value, null, 2)
}

/** `/criterios_aceite/0/entao` → `criterios_aceite › 0 › entao` (JSON pointer, RFC 6901). */
export function pointerText(pointer: string): string {
  const parts = pointer
    .split('/')
    .slice(1)
    .map((part) => part.replace(/~1/g, '/').replace(/~0/g, '~'))
  return parts.length ? parts.join(' › ') : '/'
}

function positive(value: LocationQuery[string] | undefined): number | undefined {
  const raw = Array.isArray(value) ? value[0] : value
  const number = Number(raw)
  return Number.isInteger(number) && number >= 1 ? number : undefined
}

/**
 * The versions to compare (ESP-21 2: `?from=1&to=3` in the address). Any two existing versions
 * (ER-21-6); a missing or unknown pair is undefined.
 */
export function comparedPair(
  query: LocationQuery,
  versions: number[],
): { from: number; to: number } | undefined {
  const from = positive(query.from)
  const to = positive(query.to)
  if (from === undefined || to === undefined || from === to) return undefined
  if (!versions.includes(from) || !versions.includes(to)) return undefined
  return { from, to }
}

/** The default choice of the comparison: the version before the current one, and the current. */
export function defaultPair(versions: number[]): { from: number; to: number } | undefined {
  const sorted = [...versions].sort((a, b) => b - a)
  const [to, from] = sorted
  return to !== undefined && from !== undefined ? { from, to } : undefined
}
