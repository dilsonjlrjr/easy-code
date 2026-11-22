import { apiClient } from '@/shared/api/apiClient'
import type { HttpClient } from '@/shared/api/httpClient'

/** `getCurrentUser` (openapi `CurrentUser`). The platform keeps no e-mail (D-31). */
export interface CurrentUser {
  id: string
  name: string
  organization_roles: Array<'administrator'>
  access_medium: 'web' | 'local_client'
  session_expires_at?: string
  authenticated_at?: string
  csrf_token?: string
  lead_in_any_project?: boolean
  organization?: { id: string; name: string }
  attachment_limit_bytes?: number
}

/**
 * Ways to obtain a web session. MARCO-1 only has the fixed development identity (`POST /dev/session`,
 * outside openapi.yaml, profile `marco1`: ARQ 7, ADR-12); MARCO-2 adds the SSO (`startWebLogin`) as
 * another method here, without touching the pages that use the session.
 */
export interface SessionApi {
  getCurrentUser: () => Promise<CurrentUser>
  startDevSession: () => Promise<void>
  endSession: () => Promise<void>
}

export function createSessionApi(client: HttpClient = apiClient): SessionApi {
  return {
    getCurrentUser: () => client.get<CurrentUser>('/me'),
    startDevSession: async () => {
      await client.post('/dev/session')
    },
    endSession: async () => {
      await client.post('/session/logout')
    },
  }
}
