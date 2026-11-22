import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Page } from '@/shared/api/contract'
import {
  projectsApi,
  type Project,
  type ProjectsApi,
  type WaveSummary,
  type Workflow,
} from '../api/projectsApi'

let api: ProjectsApi = projectsApi

/** Tests replace the API; the application uses the default one. */
export function setCurrentProjectApi(replacement: ProjectsApi): void {
  api = replacement
}

export function currentProjectApi(): ProjectsApi {
  return api
}

async function allWaves(projectId: string): Promise<WaveSummary[]> {
  const waves: WaveSummary[] = []
  let cursor: string | undefined
  do {
    const page: Page<WaveSummary> = await api.listWaves(projectId, cursor)
    waves.push(...page.items)
    cursor = page.next_cursor ?? undefined
  } while (cursor)
  return waves
}

/**
 * The project open in the project pages (ESP-06): project, workflow and waves read by REST. The page
 * refreshes it when domain events arrive on the project stream, so the pipeline and the waves change
 * without reloading the page (ER-06-4).
 */
export const useCurrentProjectStore = defineStore('current-project', () => {
  const project = ref<Project>()
  const workflow = ref<Workflow>()
  const waves = ref<WaveSummary[]>([])
  /** Grows on every reload, so nested views (stage detail) re-read their own data (US-49 CA-3). */
  const revision = ref(0)

  async function load(projectId: string): Promise<number | undefined> {
    const loadedProject = await api.getProject(projectId)
    const [loadedWorkflow, loadedWaves] = await Promise.all([
      api.getWorkflow(projectId),
      // Waves exist only after the workflow starts.
      loadedProject.state === 'created' ? Promise.resolve([]) : allWaves(projectId),
    ])
    project.value = loadedProject
    workflow.value = loadedWorkflow
    // ESP-06 3.5: newest first.
    waves.value = [...loadedWaves].sort((left, right) =>
      right.created_at.localeCompare(left.created_at),
    )
    revision.value += 1
    return loadedProject.last_sequence
  }

  function clear(): void {
    project.value = undefined
    workflow.value = undefined
    waves.value = []
  }

  return { project, workflow, waves, revision, load, clear }
})
