import { defineStore } from 'pinia'
import { ref } from 'vue'

/**
 * Hand-over from the new project wizard to the project page: when a step after the creation fails,
 * the project page shows the reason (ESP-05 ER-05-9). Kept only in memory, never in storage.
 */
export const useProjectNoticesStore = defineStore('project-notices', () => {
  const startFailures = ref<Record<string, unknown>>({})

  function rememberStartFailure(projectId: string, error: unknown): void {
    startFailures.value = { ...startFailures.value, [projectId]: error }
  }

  function takeStartFailure(projectId: string): unknown {
    const error = startFailures.value[projectId]
    startFailures.value = Object.fromEntries(
      Object.entries(startFailures.value).filter(([id]) => id !== projectId),
    )
    return error
  }

  return { startFailures, rememberStartFailure, takeStartFailure }
})
