import { apiClient } from '@/shared/api/apiClient'
import type { HttpClient } from '@/shared/api/httpClient'
import type { Attachment } from '@/features/projects/api/projectsApi'

/** openapi `listAttachments` (US-03): the files attached to the business need, read-only after the start. */
export function createRequirementsApi(client: HttpClient = apiClient) {
  return {
    listAttachments: async (projectId: string): Promise<Attachment[]> =>
      (
        await client.get<{ items: Attachment[] }>(
          `/projects/${encodeURIComponent(projectId)}/attachments`,
        )
      ).items,
  }
}

export const requirementsApi = createRequirementsApi()
