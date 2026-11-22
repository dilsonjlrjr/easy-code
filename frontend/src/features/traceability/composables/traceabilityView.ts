import type { CoverageMatrix, Traceability, TraceEdge, TraceNode } from '../api/traceabilityApi'

/**
 * ESP-24 3.1: the links of a root grouped by type (T, ADR, DIC, TELAS, CT, commit, ACH, others),
 * each node with the rules that tie it to the graph; 3.2: the coverage rows, gaps first.
 */
export const NODE_GROUPS = ['T', 'ADR', 'DIC', 'TELAS', 'CT', 'commit', 'ACH'] as const

export type NodeGroup = (typeof NODE_GROUPS)[number] | 'other'

export interface LinkedNode extends TraceNode {
  rules: string[]
}

export function groupNodes(graph: Traceability): Array<{ group: NodeGroup; nodes: LinkedNode[] }> {
  const rules = new Map<string, Set<string>>()
  const add = (id: string, edge: TraceEdge) => {
    const set = rules.get(id) ?? new Set<string>()
    set.add(edge.rule)
    rules.set(id, set)
  }
  for (const edge of graph.edges) {
    add(edge.from_state, edge)
    add(edge.to_state, edge)
  }
  const groups = new Map<NodeGroup, LinkedNode[]>()
  for (const node of graph.nodes) {
    if (node.id === graph.root) continue
    const group: NodeGroup = (NODE_GROUPS as readonly string[]).includes(node.type)
      ? (node.type as NodeGroup)
      : 'other'
    const list = groups.get(group) ?? []
    list.push({ ...node, rules: [...(rules.get(node.id) ?? [])].sort() })
    groups.set(group, list)
  }
  return [...NODE_GROUPS, 'other' as const]
    .filter((group) => groups.has(group))
    .map((group) => ({
      group,
      nodes: (groups.get(group) ?? []).sort((left, right) => left.id.localeCompare(right.id)),
    }))
}

/** ESP-24 3.2: gaps first, then by US id; the status is the api's, never recalculated (ER-24-2). */
export function coverageRows(matrix: CoverageMatrix): CoverageMatrix['items'] {
  return [...matrix.items].sort((left, right) => {
    if (left.status !== right.status) return left.status === 'gap' ? -1 : 1
    return left.us.localeCompare(right.us, undefined, { numeric: true })
  })
}

/** ESP-24 6: every CT "not executed" means QA has not run yet. */
export function qaNotExecuted(matrix: CoverageMatrix): boolean {
  const results = matrix.items.flatMap((item) =>
    item.criteria.flatMap((criterion) => criterion.cts.map((ct) => ct.result)),
  )
  return results.length > 0 && results.every((result) => result === 'not_executed')
}
