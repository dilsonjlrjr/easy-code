import { describe, expect, it, vi } from 'vitest'
import type { HttpClient } from '@/shared/api/httpClient'
import {
  createLocalClientApi,
  normalizeUserCode,
  verificationUrl,
  type LocalClientConfiguration,
} from '../api/localClientApi'

const configuration: LocalClientConfiguration = {
  issuer: 'https://easycode.empresa.local/auth/realms/easycode',
  client_id: 'easycode-desktop',
  scopes: ['openid', 'easycode-read'],
}

describe('normalizeUserCode (RFC 8628 user_code)', () => {
  it('forgives spaces and case, refuses anything that is not a code', () => {
    expect(normalizeUserCode(' wdjb-mjht ')).toBe('WDJB-MJHT')
    expect(normalizeUserCode('ABCD1234')).toBe('ABCD1234')
    expect(normalizeUserCode('ABCD-')).toBeUndefined()
    expect(normalizeUserCode('<script>')).toBeUndefined()
    expect(normalizeUserCode('')).toBeUndefined()
  })
})

describe('verificationUrl', () => {
  it("uses Keycloak's device page under the issuer, with the code filled", () => {
    expect(verificationUrl(configuration, 'WDJB-MJHT')).toBe(
      'https://easycode.empresa.local/auth/realms/easycode/device?user_code=WDJB-MJHT',
    )
  })

  it('never loops back to the published verification_uri (it may be this web page)', () => {
    expect(
      verificationUrl(
        { ...configuration, verification_uri: 'https://easycode.empresa.local/device' },
        'AB-CD',
      ),
    ).toBe('https://easycode.empresa.local/auth/realms/easycode/device?user_code=AB-CD')
  })
})

describe('createLocalClientApi (US-45 CA-5)', () => {
  it('lists and revokes only the own sessions under /me', async () => {
    const get = vi.fn().mockResolvedValue({ items: [{ id: 's1', started_at: 'x' }] })
    const del = vi.fn().mockResolvedValue(undefined)
    const api = createLocalClientApi({ get, delete: del } as unknown as HttpClient)
    expect(await api.listSessions()).toEqual([{ id: 's1', started_at: 'x' }])
    await api.revokeSession('s1')
    expect(get).toHaveBeenCalledWith('/me/local-client-sessions')
    expect(del).toHaveBeenCalledWith('/me/local-client-sessions/s1')
  })
})
