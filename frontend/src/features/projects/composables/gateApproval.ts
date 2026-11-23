import type { GateApprovalMode, GateApprovalModeValue, StageSummary } from '../api/projectsApi'

/** One gate of the project in the "Gates" section of the configuration (ESP-29 3.2). */
export interface GateModeRow {
  gate: string
  /** The stage that owns the gate, when the workflow names it. */
  stage?: string
  mode: GateApprovalModeValue
  /** Both modes allowed: only gate.validation and gate.implementation (US-07 CA-3, RN-1). */
  configurable: boolean
  alwaysHuman: boolean
}

/** The stage of a gate: the workflow's `gate` field, or the `gate.<stage>` naming convention. */
export function stageOfGate(gate: string, stages: StageSummary[]): string | undefined {
  return (
    stages.find((stage) => stage.gate === gate)?.id ??
    stages.find((stage) => `gate.${stage.id}` === gate)?.id
  )
}

/**
 * The gates of the configuration, in the order of the workflow stages (ESP-29 3.2); gates the
 * workflow does not list go last, by name. Only a gate that allows both modes gets a control.
 */
export function gateModeRows(
  gateApproval: Record<string, GateApprovalMode> | undefined,
  stages: StageSummary[],
): GateModeRow[] {
  const position = (gate: string): number => {
    const stage = stageOfGate(gate, stages)
    const index = stages.findIndex((item) => item.id === stage)
    return index < 0 ? stages.length : index
  }
  return Object.entries(gateApproval ?? {})
    .map(([gate, value]) => ({
      gate,
      stage: stageOfGate(gate, stages),
      mode: value.mode,
      configurable:
        !value.always_human &&
        value.allowed.includes('human') &&
        value.allowed.includes('automatic'),
      alwaysHuman: value.always_human ?? false,
    }))
    .sort(
      (left, right) =>
        position(left.gate) - position(right.gate) || left.gate.localeCompare(right.gate),
    )
}

/**
 * US-07 RN-3 (PD-05): the stricter mode always wins. To `human` the change applies at once, also to
 * the open round; to `automatic` only from the next round on.
 */
export function modeChangeTakesEffect(next: GateApprovalModeValue): 'now' | 'next_round' {
  return next === 'human' ? 'now' : 'next_round'
}
