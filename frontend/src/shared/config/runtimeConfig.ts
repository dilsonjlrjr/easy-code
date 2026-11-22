/**
 * Runtime configuration. Nothing here is a fixed address (RNF-17): values come from
 * `window.__EASY_CODE_CONFIG__` (written by the container at startup), then from build-time
 * variables (`VITE_*`), and finally fall back to defaults.
 *
 * - `apiBaseUrl`: base of the versioned API (openapi `servers`: `/api/v1`, same origin).
 * - `devSignIn`: offers the fixed development identity on the login page. Only the `marco1`
 *   profile turns it on (ADR-12, ESP-02 ER-02-9); the API refuses the route outside that profile.
 */
export interface RuntimeConfig {
  apiBaseUrl: string
  devSignIn: boolean
}

interface RuntimeConfigSource {
  apiBaseUrl?: string
  devSignIn?: boolean | string
}

declare global {
  interface Window {
    __EASY_CODE_CONFIG__?: RuntimeConfigSource
  }
}

const SAME_ORIGIN_API_PATH = '/api/v1'

export function resolveRuntimeConfig(
  injected: RuntimeConfigSource | undefined,
  buildTimeApiBaseUrl: string | undefined,
  buildTimeDevSignIn?: string,
): RuntimeConfig {
  const apiBaseUrl =
    firstNonBlank(injected?.apiBaseUrl, buildTimeApiBaseUrl) ?? SAME_ORIGIN_API_PATH
  return {
    apiBaseUrl: withoutTrailingSlash(apiBaseUrl),
    devSignIn: isTrue(injected?.devSignIn) || isTrue(buildTimeDevSignIn),
  }
}

export function loadRuntimeConfig(): RuntimeConfig {
  return resolveRuntimeConfig(
    window.__EASY_CODE_CONFIG__,
    import.meta.env.VITE_API_BASE_URL,
    import.meta.env.VITE_DEV_SIGN_IN,
  )
}

function isTrue(value: boolean | string | undefined): boolean {
  return value === true || value === 'true'
}

function firstNonBlank(...values: Array<string | undefined>): string | undefined {
  return values.map((value) => value?.trim()).find((value) => value !== undefined && value !== '')
}

function withoutTrailingSlash(url: string): string {
  return url.endsWith('/') ? url.slice(0, -1) : url
}
