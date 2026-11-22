import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { isApiError } from '@/shared/api/problemDetails'
import { createSessionApi, type CurrentUser, type SessionApi } from './sessionApi'

export type SessionStatus = 'unknown' | 'authenticated' | 'anonymous'

const UNAUTHORIZED = 401

let sessionApi: SessionApi = createSessionApi()

/** Tests replace the API; the application uses the default one. */
export function setSessionApi(api: SessionApi): void {
  sessionApi = api
}

export const useSessionStore = defineStore('session', () => {
  const user = ref<CurrentUser | null>(null)
  const status = ref<SessionStatus>('unknown')
  /** Problem slug of the 401 that left the session anonymous (`session-expired`, ER-02-5). */
  const anonymousReason = ref<string>()
  let loading: Promise<void> | undefined

  const isAdministrator = computed(
    () => user.value?.organization_roles.includes('administrator') ?? false,
  )
  const csrfToken = computed(() => user.value?.csrf_token)

  async function load(): Promise<void> {
    try {
      user.value = await sessionApi.getCurrentUser()
      status.value = 'authenticated'
      anonymousReason.value = undefined
    } catch (error) {
      if (isApiError(error) && error.status === UNAUTHORIZED) {
        markSignedOut(error.slug)
        return
      }
      throw error
    }
  }

  /** Reads the session once; later calls reuse the answer until the session changes. */
  async function ensureLoaded(): Promise<void> {
    if (status.value !== 'unknown') return
    loading ??= load().finally(() => {
      loading = undefined
    })
    await loading
  }

  /** Reads the user again (fresh `authenticated_at` and `csrf_token` after a step-up). */
  async function refresh(): Promise<void> {
    await load()
  }

  async function signOut(): Promise<void> {
    try {
      await sessionApi.endSession()
    } finally {
      markSignedOut()
    }
  }

  /** The API said the session is gone (401, `easycode.ended`). */
  function markSignedOut(reason?: string): void {
    user.value = null
    status.value = 'anonymous'
    anonymousReason.value = reason
  }

  return {
    user,
    status,
    anonymousReason,
    isAdministrator,
    csrfToken,
    ensureLoaded,
    refresh,
    signOut,
    markSignedOut,
  }
})
