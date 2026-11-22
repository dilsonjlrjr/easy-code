/**
 * Runtime configuration. Nothing here is a fixed address (RNF-17): values come from
 * `window.__EASY_CODE_CONFIG__` (written by the container at startup), then from build-time
 * variables (`VITE_*`), and finally fall back to defaults.
 *
 * - `apiBaseUrl`: base of the versioned API (openapi `servers`: `/api/v1`, same origin).
 *
 * The sign-in is only the corporate SSO (MARCO-2, ADR-12): there is no development identity here.
 */
export interface RuntimeConfig {
  apiBaseUrl: string
}

interface RuntimeConfigSource {
  apiBaseUrl?: string
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
): RuntimeConfig {
  const apiBaseUrl =
    firstNonBlank(injected?.apiBaseUrl, buildTimeApiBaseUrl) ?? SAME_ORIGIN_API_PATH
  return {
    apiBaseUrl: withoutTrailingSlash(apiBaseUrl),
  }
}

export function loadRuntimeConfig(): RuntimeConfig {
  return resolveRuntimeConfig(window.__EASY_CODE_CONFIG__, import.meta.env.VITE_API_BASE_URL)
}

function firstNonBlank(...values: Array<string | undefined>): string | undefined {
  return values.map((value) => value?.trim()).find((value) => value !== undefined && value !== '')
}

function withoutTrailingSlash(url: string): string {
  return url.endsWith('/') ? url.slice(0, -1) : url
}
