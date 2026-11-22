import type { ArtifactState } from '@/shared/api/contract'
import type { Artifact } from '@/features/artifacts/api/artifactsApi'

/** `situacao` of a T (specs/dominio/artefatos/t.schema.json); tracking field, changes after approval. */
export const TASK_SITUATIONS = ['planejada', 'em_andamento', 'concluida', 'justificada'] as const
export type TaskSituation = (typeof TASK_SITUATIONS)[number]

/** A T as the stage screens show it (ESP-08 "Tarefas", ESP-14 "Tarefas"). */
export interface TaskView {
  id: string
  title: string
  state: ArtifactState
  version: number
  situation: TaskSituation | undefined
  justification: string | undefined
  nature: string | undefined
  size: string | undefined
  covers: Array<{ us: string; criteria: string[] }>
  follows: string[]
  dependsOn: string[]
  fileScope: string[]
  devRecord: { commits: string[]; tests: string[]; deviations: string | undefined } | undefined
}

function strings(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string')
    : []
}

function text(value: unknown): string | undefined {
  return typeof value === 'string' && value !== '' ? value : undefined
}

/**
 * Reads the T header (keys as in the artifact schema, in Portuguese) defensively: the header is
 * agent-written content (RNF-05), so anything outside the expected shape is dropped, never shown raw.
 */
export function taskFromArtifact(artifact: Artifact): TaskView {
  const header = artifact.header
  const situation = header.situacao
  const record = header.registro_dev
  const covers = Array.isArray(header.cobre)
    ? header.cobre.flatMap((item: unknown) => {
        if (typeof item !== 'object' || item === null) return []
        const entry = item as Record<string, unknown>
        const us = text(entry.us)
        return us ? [{ us, criteria: strings(entry.criterios) }] : []
      })
    : []
  return {
    id: artifact.id,
    title: artifact.title,
    state: artifact.state,
    version: artifact.version,
    situation: (TASK_SITUATIONS as readonly unknown[]).includes(situation)
      ? (situation as TaskSituation)
      : undefined,
    justification: text(header.justificativa),
    nature: text(header.natureza),
    size: text(header.tamanho),
    covers,
    follows: strings(header.segue),
    dependsOn: strings(header.depende_de),
    fileScope: strings(header.escopo_arquivos),
    devRecord:
      typeof record === 'object' && record !== null
        ? {
            commits: strings((record as Record<string, unknown>).commits),
            tests: strings((record as Record<string, unknown>).testes),
            deviations: text((record as Record<string, unknown>).desvios),
          }
        : undefined,
  }
}

/**
 * Plan order (ESP-08: topological by `depende_de`): a task comes after the tasks it depends on; ties
 * and cycles fall back to the id order, so the list is always complete and stable.
 */
export function inPlanOrder(tasks: TaskView[]): TaskView[] {
  const byId = new Map(tasks.map((task) => [task.id, task]))
  const sorted = [...tasks].sort((a, b) => a.id.localeCompare(b.id, 'en', { numeric: true }))
  const placed = new Set<string>()
  const visiting = new Set<string>()
  const result: TaskView[] = []
  function visit(task: TaskView): void {
    if (placed.has(task.id) || visiting.has(task.id)) return
    visiting.add(task.id)
    for (const dependency of task.dependsOn) {
      const before = byId.get(dependency)
      if (before) visit(before)
    }
    visiting.delete(task.id)
    placed.add(task.id)
    result.push(task)
  }
  for (const task of sorted) visit(task)
  return result
}

/** "nenhum" (or empty) means the developer followed the task as planned (t.schema `desvios`). */
export function hasDeviation(task: TaskView): boolean {
  const deviations = task.devRecord?.deviations?.trim().toLowerCase()
  return deviations !== undefined && deviations !== '' && deviations !== 'nenhum'
}

/** The task selected when the screen opens: the one running, else the first not done, else the first. */
export function defaultTask(tasks: TaskView[]): TaskView | undefined {
  return (
    tasks.find((task) => task.situation === 'em_andamento') ??
    tasks.find((task) => task.situation !== 'concluida' && task.situation !== 'justificada') ??
    tasks[0]
  )
}
