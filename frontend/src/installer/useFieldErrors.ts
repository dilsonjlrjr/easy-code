import { useInstallerStore } from './installerStore'
import { useReason } from './useReason'

/** Messages of the last test for one field ("database.host"), as v-text-field error-messages. */
export function useFieldErrors() {
  const store = useInstallerStore()
  const { reasonText } = useReason()
  return (field: string): string[] => {
    const problem = store.fieldProblem(field)
    return problem ? [reasonText(problem.reason)] : []
  }
}
