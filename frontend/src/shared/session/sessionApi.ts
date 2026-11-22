import { apiClient } from '@/shared/api/apiClient'
import { joinUrl, withQuery, type HttpClient } from '@/shared/api/httpClient'

/** `getCurrentUser` (openapi `CurrentUser`). The platform keeps no e-mail (D-31). */
export interface CurrentUser {
  id: string
  name: string
  organization_roles: Array<'administrator'>
  access_medium: 'web' | 'local_client'
  session_expires_at?: string
  /** Last SSO authentication; critical decisions need it at most 10 min old (ADR-19). */
  authenticated_at?: string
  /** Sent as `X-CSRF-Token` on critical decisions (approve and reject a gate, ADR-19). */
  csrf_token?: string
  lead_in_any_project?: boolean
  organization?: { id: string; name: string }
  attachment_limit_bytes?: number
}

/**
 * The web session comes only from the corporate SSO (US-01 RN-1, ADR-12): the browser goes to
 * `startWebLogin`, the api runs the authorization code + PKCE flow with Keycloak and comes back to
 * the `callback` path with the session cookie. The web never sees a password or token.
 */
export interface SessionApi {
  getCurrentUser: () => Promise<CurrentUser>
  endSession: () => Promise<void>
}

export function createSessionApi(client: HttpClient = apiClient): SessionApi {
  return {
    getCurrentUser: () => client.get<CurrentUser>('/me'),
    endSession: async () => {
      await client.post('/session/logout')
    },
  }
}

/**
 * Address of `startWebLogin` (`GET /session/login`). `callback` is a same-origin path (ER-02-3);
 * `reauthenticate` asks Keycloak for a fresh login (step-up before a critical decision, ER-02-8).
 */
export function webLoginUrl(
  callback: string,
  options: { reauthenticate?: boolean } = {},
  baseUrl: string = apiClient.baseUrl,
): string {
  return withQuery(joinUrl(baseUrl, '/session/login'), {
    callback,
    reauthenticate: options.reauthenticate ? true : undefined,
  })
}

type Navigate = (url: string) => void

let navigate: Navigate = (url) => {
  window.location.assign(url)
}

/** Unit tests replace the full-page navigation; the application leaves the page for the SSO. */
export function setSsoNavigation(replacement: Navigate): void {
  navigate = replacement
}

/** Leaves the application for the SSO; the api brings the browser back to `callback`. */
export function redirectToSso(callback: string, options: { reauthenticate?: boolean } = {}): void {
  navigate(webLoginUrl(callback, options))
}
