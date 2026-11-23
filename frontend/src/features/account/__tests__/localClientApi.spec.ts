import { describe, expect, it, vi } from 'vitest'
import type { HttpClient } from '@/shared/api/httpClient'
import {
  createLocalClientApi,
  ssoDevicePage,
  type LocalClientConfiguration,
} from '../api/localClientApi'

const configuration: LocalClientConfiguration = {
  issuer: 'https://easycode.empresa.local/auth/realms/easycode',
  client_id: 'easycode-desktop',
  scopes: ['openid', 'easycode-read'],
}

describe('ssoDevicePage', () => {
  it("is Keycloak's device page under the issuer", () => {
    expect(ssoDevicePage(configuration)).toBe(
      'https://easycode.empresa.local/auth/realms/easycode/device',
    )
    expect(ssoDevicePage({ ...configuration, issuer: `${configuration.issuer}/` })).toBe(
      'https://easycode.empresa.local/auth/realms/easycode/device',
    )
  })
})

describe('createLocalClientApi (US-45 CA-5)', () => {
  it('lists and revokes only the own sessions under /me', async () => {
    const get = vi.fn().mockResolvedValue({ items: [{ id: 's1', started_at: 'x' }] })
    const del = vi.fn().mockResolvedValue(undefined)
    const post = vi.fn().mockResolvedValue(undefined)
    const api = createLocalClientApi({ get, delete: del, post } as unknown as HttpClient)
    expect(await api.listSessions()).toEqual([{ id: 's1', started_at: 'x' }])
    await api.revokeSession('s1')
    await api.acknowledgeSession('s1')
    expect(post).toHaveBeenCalledWith('/me/local-client-sessions/s1/acknowledgement')
    expect(get).toHaveBeenCalledWith('/me/local-client-sessions')
    expect(del).toHaveBeenCalledWith('/me/local-client-sessions/s1')
  })
})
