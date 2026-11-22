import { loadRuntimeConfig } from '@/shared/config/runtimeConfig'
import { i18n } from '@/shared/i18n'
import { createHttpClient, type HttpClient } from './httpClient'

/** The application's single API client. Feature `api/` modules build on it. */
export const apiClient: HttpClient = createHttpClient({
  baseUrl: loadRuntimeConfig().apiBaseUrl,
  currentLanguage: () => i18n.global.locale.value,
})
