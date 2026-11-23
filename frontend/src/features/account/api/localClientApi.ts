import { apiClient } from '@/shared/api/apiClient'
import type { HttpClient } from '@/shared/api/httpClient'

/** openapi `LocalClientSession` (US-45 CA-5): never a token; the last IP seen, no device name (PL-34). */
export interface LocalClientSession {
  id: string
  started_at: string
  last_use_at?: string
  ip_address?: string
  current?: boolean
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
  /**
   * Not in the contract yet: the page the desktop app opens for the person to confirm the code; the
   * web's `/device` when the platform publishes it.
   */
  verification_uri?: string
}

export interface LocalClientApi {
  listSessions: () => Promise<LocalClientSession[]>
  /** US-45 CA-2: the app's next call fails with "session revoked" within 60 s (RNF-06). */
  revokeSession: (sessionId: string) => Promise<void>
  getConfiguration: () => Promise<LocalClientConfiguration>
}

export function createLocalClientApi(client: HttpClient = apiClient): LocalClientApi {
  return {
    listSessions: async () =>
      (await client.get<{ items: LocalClientSession[] }>('/me/local-client-sessions')).items,
    revokeSession: async (sessionId) => {
      await client.delete(`/me/local-client-sessions/${encodeURIComponent(sessionId)}`)
    },
    getConfiguration: () => client.get<LocalClientConfiguration>('/session/local-client'),
  }
}

export const localClientApi = createLocalClientApi()

/**
 * The code the desktop app shows (RFC 8628 `user_code`): letters and digits, maybe split by a hyphen.
 * Spaces and case are forgiven; anything else is not a code.
 */
export function normalizeUserCode(value: string): string | undefined {
  const code = value.replace(/\s+/g, '').toUpperCase()
  return /^[A-Z0-9]{2,8}(-[A-Z0-9]{2,8})?$/.test(code) ? code : undefined
}

/**
 * Where the person confirms the code in the SSO: Keycloak's device page under the issuer
 * (`<issuer>/device`, RFC 8628), with the code filled. The contract's `verification_uri` is not used
 * here: it is the address the desktop app opens, which may be this very web page (`/device`).
 */
export function verificationUrl(configuration: LocalClientConfiguration, code: string): string {
  const url = new URL(`${configuration.issuer.replace(/\/+$/, '')}/device`)
  url.searchParams.set('user_code', code)
  return url.toString()
}
