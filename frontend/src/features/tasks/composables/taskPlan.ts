import type { ArtifactState } from '@/shared/api/contract'
import type { Artifact, ArtifactSummary } from '@/features/artifacts/api/artifactsApi'

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

/** A T in the plan list: what `listArtifacts type=T` returns, with the situation (no header read). */
export interface TaskListItem {
  id: string
  title: string
  state: ArtifactState
  version: number
  situation: TaskSituation | undefined
}

export function taskListItem(summary: ArtifactSummary): TaskListItem {
  return {
    id: summary.id,
    title: summary.title,
    state: summary.state,
    version: summary.version,
    situation: (TASK_SITUATIONS as readonly unknown[]).includes(summary.situation)
      ? (summary.situation as TaskSituation)
      : undefined,
  }
}

/** Plan order by id (T-01, T-02, ..., T-10): the summary carries no dependencies. */
export function inIdOrder<TTask extends { id: string }>(tasks: TTask[]): TTask[] {
  return [...tasks].sort((a, b) => a.id.localeCompare(b.id, 'en', { numeric: true }))
}

/** "nenhum" (or empty) means the developer followed the task as planned (t.schema `desvios`). */
export function hasDeviation(task: TaskView): boolean {
  const deviations = task.devRecord?.deviations?.trim().toLowerCase()
  return deviations !== undefined && deviations !== '' && deviations !== 'nenhum'
}

/** The task selected when the screen opens: the one running, else the first not done, else the first. */
export function defaultTask<TTask extends { situation: TaskSituation | undefined }>(
  tasks: TTask[],
): TTask | undefined {
  return (
    tasks.find((task) => task.situation === 'em_andamento') ??
    tasks.find((task) => task.situation !== 'concluida' && task.situation !== 'justificada') ??
    tasks[0]
  )
}
