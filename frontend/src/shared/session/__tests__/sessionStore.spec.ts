import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { safeReturnPath } from '@/app/router/safeReturnPath'
import { ApiError } from '@/shared/api/problemDetails'
import type { SessionApi } from '../sessionApi'
import { setSessionApi, useSessionStore } from '../sessionStore'

const devUser = {
  id: 'dev',
  name: 'Dev',
  organization_roles: ['administrator' as const],
  access_medium: 'web' as const,
}

function fakeApi(overrides: Partial<SessionApi> = {}): SessionApi {
  return {
    getCurrentUser: vi.fn().mockResolvedValue(devUser),
    startDevSession: vi.fn().mockResolvedValue(undefined),
    endSession: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

describe('session store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('is anonymous when the API answers 401 (US-01 CA-1: login and back)', async () => {
    setSessionApi(
      fakeApi({ getCurrentUser: vi.fn().mockRejectedValue(new ApiError(401, undefined)) }),
    )
    const session = useSessionStore()
    await session.ensureLoaded()
    expect(session.status).toBe('anonymous')
    expect(session.isAdministrator).toBe(false)
  })

  it('signs in with the fixed development identity and reads the current user (ER-02-9)', async () => {
    const api = fakeApi({
      getCurrentUser: vi
        .fn()
        .mockRejectedValueOnce(new ApiError(401, undefined))
        .mockResolvedValue(devUser),
    })
    setSessionApi(api)
    const session = useSessionStore()
    await session.ensureLoaded()

    await session.signInWithDevIdentity()

    expect(api.startDevSession).toHaveBeenCalledOnce()
    expect(session.status).toBe('authenticated')
    expect(session.isAdministrator).toBe(true)
  })

  it('reads the session only once for concurrent navigations', async () => {
    const api = fakeApi()
    setSessionApi(api)
    const session = useSessionStore()
    await Promise.all([session.ensureLoaded(), session.ensureLoaded()])
    expect(api.getCurrentUser).toHaveBeenCalledOnce()
  })

  it('signing out ends the session even if the API call fails (CA-00-16)', async () => {
    setSessionApi(fakeApi({ endSession: vi.fn().mockRejectedValue(new Error('offline')) }))
    const session = useSessionStore()
    await session.ensureLoaded()
    await expect(session.signOut()).rejects.toThrow('offline')
    expect(session.status).toBe('anonymous')
  })
})

describe('safeReturnPath (ESP-02 CA-02-3)', () => {
  it('accepts only paths of this application', () => {
    expect(safeReturnPath('/projects/p1/waves/REQ-01')).toBe('/projects/p1/waves/REQ-01')
    expect(safeReturnPath('https://evil.test/')).toBe('/')
    expect(safeReturnPath('//evil.test/')).toBe('/')
    expect(safeReturnPath(undefined)).toBe('/')
  })
})
