import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { safeReturnPath } from '@/app/router/safeReturnPath'
import { ApiError } from '@/shared/api/problemDetails'
import { webLoginUrl, type SessionApi } from '../sessionApi'
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

  it('remembers that the session expired, so the login page can say why (ER-02-5)', async () => {
    const expired = new ApiError(401, {
      type: 'urn:easycode:problem:session-expired',
      status: 401,
    })
    setSessionApi(fakeApi({ getCurrentUser: vi.fn().mockRejectedValue(expired) }))
    const session = useSessionStore()
    await session.ensureLoaded()
    expect(session.status).toBe('anonymous')
    expect(session.anonymousReason).toBe('session-expired')
  })

  it('reads the user again after a step-up, with the new CSRF token (ADR-19)', async () => {
    const api = fakeApi({
      getCurrentUser: vi
        .fn()
        .mockResolvedValueOnce({ ...devUser, csrf_token: 'first' })
        .mockResolvedValue({ ...devUser, csrf_token: 'second' }),
    })
    setSessionApi(api)
    const session = useSessionStore()
    await session.ensureLoaded()
    expect(session.csrfToken).toBe('first')
    await session.refresh()
    expect(session.csrfToken).toBe('second')
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

describe('webLoginUrl (startWebLogin)', () => {
  it('goes to the SSO with the return path (US-01 CA-1)', () => {
    expect(webLoginUrl('/projects/p1/waves/REQ-01', {}, '/api/v1')).toBe(
      '/api/v1/session/login?callback=%2Fprojects%2Fp1%2Fwaves%2FREQ-01',
    )
  })

  it('asks for a fresh authentication for the step-up (US-06 CA-5)', () => {
    expect(webLoginUrl('/x', { reauthenticate: true }, '/api/v1')).toBe(
      '/api/v1/session/login?callback=%2Fx&reauthenticate=true',
    )
  })
})
