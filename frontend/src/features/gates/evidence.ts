/**
 * ESP-12 "Checagens": the reference of an evidence item is navigable when it names an artifact, a
 * finding, a wave or a commit. Evidence comes from the checks and is untrusted (RNF-05): it is only
 * matched against the id patterns of the contract, never interpreted.
 */
export type EvidenceTarget =
  | { kind: 'wave'; id: string }
  | { kind: 'finding'; id: string }
  | { kind: 'artifact'; id: string; item?: string }
  | { kind: 'commit'; id: string; short: string }
  | { kind: 'text'; id: string }

/** openapi `WaveId`. */
const WAVE = /^(REQ|ARQ|DBA|DSG|VAL|DEV|QA|SEG|REL)-[0-9]{2,}$/
/** openapi `FindingId`. */
const FINDING = /^ACH-[0-9]{2,}$/
/** openapi `ArtifactId`, plus VALIDACAO (the single artifact of the technical validation). */
const ARTIFACT =
  /^([A-Z]+(-[A-Z]+)?-[0-9]{2,}|VISAO|ARQ|DIC|PERSIST|AMEACAS|BACKLOG|TELAS|VALIDACAO)$/
/** A git commit SHA-1. */
const COMMIT = /^[0-9a-f]{40}$/

export function evidenceTarget(reference: string): EvidenceTarget {
  if (WAVE.test(reference)) return { kind: 'wave', id: reference }
  if (FINDING.test(reference)) return { kind: 'finding', id: reference }
  if (COMMIT.test(reference)) return { kind: 'commit', id: reference, short: reference.slice(0, 7) }
  if (ARTIFACT.test(reference)) return { kind: 'artifact', id: reference }
  // "TELAS/S-01", "AMEACAS/T-3": an item inside an artifact opens the artifact.
  const slash = reference.indexOf('/')
  if (slash > 0) {
    const artifact = reference.slice(0, slash)
    if (ARTIFACT.test(artifact)) {
      return { kind: 'artifact', id: artifact, item: reference.slice(slash + 1) }
    }
  }
  return { kind: 'text', id: reference }
}
