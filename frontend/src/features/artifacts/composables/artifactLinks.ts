import type { GateRound, GateRoundSummary } from '@/features/gates/api/gatesApi'
import type { Traceability, TraceNode } from '@/features/traceability/api/traceabilityApi'

/** How the Artefato page opens an attachment: Markdown sanitized, other text as text, else the content origin. */
export function attachmentKind(mediaType: string): 'markdown' | 'text' | 'content-origin' {
  if (mediaType === 'text/markdown') return 'markdown'
  if (
    ['text/plain', 'application/yaml', 'application/json', 'application/sql', 'text/csv'].includes(
      mediaType,
    )
  )
    return 'text'
  return 'content-origin'
}

/** ESP-21 canvas: requirement → decision → data model → screen → code → test, in that order. */
const CHAIN_ORDER = [
  'VISAO',
  'US',
  'RNF',
  'BACKLOG',
  'ARQ',
  'ADR',
  'DIC',
  'PERSIST',
  'TELAS',
  'VALIDACAO',
  'T',
  'commit',
  'CT',
  'test',
  'REL-QA',
  'AMEACAS',
  'REL-SEG',
  'ACH',
  'RELEASE',
]

function rank(type: string): number {
  const index = CHAIN_ORDER.indexOf(type)
  return index === -1 ? CHAIN_ORDER.length : index
}

/** The chain of the artifact: its node among the linked ones, ordered by the place of the type in the flow. */
export function traceChain(graph: Traceability | undefined): TraceNode[] {
  if (!graph) return []
  return [...graph.nodes].sort((a, b) => rank(a.type) - rank(b.type) || a.id.localeCompare(b.id))
}

/**
 * ESP-21 3 "Depende deste": the artifacts that cite this one, by type with the count. The graph is the
 * one of `getArtifactTraceability` with `direction: input` and depth 1, so every other node cites it.
 */
export function dependents(
  graph: Traceability | undefined,
): Array<{ type: string; count: number; nodes: TraceNode[] }> {
  if (!graph) return []
  const nodes = graph.nodes.filter((node) => node.id !== graph.root)
  const groups = new Map<string, TraceNode[]>()
  for (const node of nodes) groups.set(node.type, [...(groups.get(node.type) ?? []), node])
  return [...groups.entries()]
    .map(([type, items]) => ({ type, count: items.length, nodes: items }))
    .sort((a, b) => rank(a.type) - rank(b.type))
}

/** The approved rounds of the gate, newest first: where the approval of a version is searched. */
export function approvedRounds(rounds: GateRoundSummary[]): GateRoundSummary[] {
  return rounds
    .filter((round) => round.decision?.result === 'approved')
    .sort((a, b) => b.round - a.round)
}

/** Whether a round approved this version of the artifact (its outputs carry it). */
export function roundCarries(round: GateRound, artifact: string, version: number): boolean {
  return round.outputs.some((output) => output.artifact === artifact && output.version === version)
}
