import type { StageSummary } from '../api/projectsApi'

/**
 * Stages that run in parallel with the given one: the other stages of the workflow that wait on
 * exactly the same stages (workflow-padrao `depende_de`). After Architecture is approved, Data
 * Modeling and Design run side by side (US-20 CA-3, ESP-09 3, ESP-10 3).
 */
export function parallelStages(stages: readonly StageSummary[], stageId: string): StageSummary[] {
  const current = stages.find((stage) => stage.id === stageId)
  if (!current || current.depends_on.length === 0) return []
  const key = [...current.depends_on].sort().join(',')
  return stages.filter(
    (stage) =>
      stage.id !== stageId &&
      stage.type === current.type &&
      [...stage.depends_on].sort().join(',') === key,
  )
}
