import { loadRuntimeConfig } from '@/shared/config/runtimeConfig'
import { i18n } from '@/shared/i18n'
import { createHttpClient, type HttpClient } from './httpClient'
import type { ProblemDetails } from './problemDetails'

type UnauthorizedListener = (problem: ProblemDetails | undefined) => void

let unauthorizedListener: UnauthorizedListener | undefined

/**
 * Lets the application decide what a 401 means (go to the login page, keeping the address).
 * Set once by the router; kept here so `shared` does not depend on `app`.
 */
export function onApiUnauthorized(listener: UnauthorizedListener): void {
  unauthorizedListener = listener
}

/** The application's single API client. Feature `api/` modules build on it. */
export const apiClient: HttpClient = createHttpClient({
  baseUrl: loadRuntimeConfig().apiBaseUrl,
  currentLanguage: () => i18n.global.locale.value,
  onUnauthorized: (problem) => unauthorizedListener?.(problem),
})
