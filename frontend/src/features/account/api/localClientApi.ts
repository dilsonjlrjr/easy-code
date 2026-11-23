import { apiClient } from '@/shared/api/apiClient'
import type { HttpClient } from '@/shared/api/httpClient'

/** openapi `LocalClientSession` (US-45 CA-5): never a token; the last IP seen, no device name (PL-34). */
export interface LocalClientSession {
  id: string
  /** The app's User-Agent (`easycode-desktop/<version> (<os>; <arch>)`), printable text only. */
  device: string
  started_at: string
  last_use_at: string
  ip_address?: string
  /** False while the new-session notice was not seen (S-19): the person confirms "it was me". */
  acknowledged: boolean
  current: boolean
}

/** openapi `LocalClientConfiguration`: the device flow parameters of the desktop app (ADR-12). */
export interface LocalClientConfiguration {
  issuer: string
  client_id: string
  audience?: string
  scopes: string[]
  device_authorization_endpoint?: string
  endpoint_token?: string
  revocation_endpoint?: string
  code_validity_s?: number
}

export interface LocalClientApi {
  listSessions: () => Promise<LocalClientSession[]>
  /** US-45 CA-2: the app's next call fails with "session revoked" within 60 s (RNF-06). */
  revokeSession: (sessionId: string) => Promise<void>
  /** S-19 (RK-16): the person confirms a new desktop session was theirs. */
  acknowledgeSession: (sessionId: string) => Promise<void>
  getConfiguration: () => Promise<LocalClientConfiguration>
}

export function createLocalClientApi(client: HttpClient = apiClient): LocalClientApi {
  return {
    listSessions: async () =>
      (await client.get<{ items: LocalClientSession[] }>('/me/local-client-sessions')).items,
    revokeSession: async (sessionId) => {
      await client.delete(`/me/local-client-sessions/${encodeURIComponent(sessionId)}`)
    },
    acknowledgeSession: async (sessionId) => {
      await client.post(
        `/me/local-client-sessions/${encodeURIComponent(sessionId)}/acknowledgement`,
      )
    },
    getConfiguration: () => client.get<LocalClientConfiguration>('/session/local-client'),
  }
}

export const localClientApi = createLocalClientApi()

/**
 * Keycloak's device page under the issuer (RFC 8628 `verification_uri`), where a person types the
 * code the desktop app shows; the confirmation happens there, the web only links to it.
 */
export function ssoDevicePage(configuration: LocalClientConfiguration): string {
  return `${configuration.issuer.replace(/\/+$/, '')}/device`
}
