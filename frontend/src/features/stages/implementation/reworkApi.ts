import { apiClient } from '@/shared/api/apiClient'
import type { Page } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'
import type { WaveSummary } from '@/features/projects/api/projectsApi'
import { findingsApi, type FindingSummary } from '@/features/findings/api/findingsApi'
import type { ReworkPurpose } from './implementationView'

async function readAllPages<TItem>(
  read: (cursor: string | undefined) => Promise<Page<TItem>>,
): Promise<TItem[]> {
  const items: TItem[] = []
  let cursor: string | undefined
  do {
    const page = await read(cursor)
    items.push(...page.items)
    cursor = page.next_cursor ?? undefined
  } while (cursor)
  return items
}

/**
 * Reads of the ESP-14 "Retrabalho" block: openapi `listFindings` (`destination_stage=implementation`)
 * and `listWaves` by `purpose` (rework of the Developer, revalidation of the emitter, which runs in the
 * emitter's stage). The waves are filtered by purpose here too, so the block stays right while a
 * server ignores the filter.
 */
export function createReworkApi(client: HttpClient = apiClient) {
  return {
    findings: (projectId: string): Promise<FindingSummary[]> =>
      readAllPages((cursor) =>
        findingsApi.list(projectId, { destination_stage: 'implementation' }, cursor),
      ),
    waves: async (projectId: string, purpose: ReworkPurpose): Promise<WaveSummary[]> =>
      (
        await readAllPages((cursor) =>
          client.get<Page<WaveSummary>>(`/projects/${encodeURIComponent(projectId)}/waves`, {
            query: { purpose, cursor, limit: 100 },
          }),
        )
      ).filter((wave) => wave.purpose === purpose),
  }
}

export const reworkApi = createReworkApi()
