import { describe, expect, it } from 'vitest'
import type { Provider } from '../api/providersApi'
import { credentialKindOf, providerUsable, usesSubscription } from '../claudeCode'

function claudeCode(
  overrides: Partial<Provider> = {},
  key: Partial<Provider['key']> = {},
): Provider {
  return {
    id: 'claude-code',
    name: 'Claude Code',
    detection: { detected: true, version: '2.1.0', checked_at: '2026-11-25T10:00:00Z' },
    ...overrides,
    key: { status: 'active', last_4: 'wxyz', credential_kind: 'api_key', ...key },
  }
}

describe('Claude Code provider', () => {
  it('US-58 CA-3: tells a claude setup-token token from an API key by its prefix', () => {
    expect(credentialKindOf('sk-ant-oat01-abcdefgh')).toBe('subscription_token')
    expect(credentialKindOf('sk-ant-api03-abcdefgh')).toBe('api_key')
  })

  it('US-58 CA-2: Claude Code models need the binary detected and an active key', () => {
    expect(providerUsable(claudeCode())).toBe(true)
    expect(
      providerUsable(
        claudeCode({ detection: { detected: false, version: null, checked_at: null } }),
      ),
    ).toBe(false)
    expect(providerUsable(claudeCode({}, { status: 'missing' }))).toBe(false)
    expect(providerUsable(claudeCode({ detection: undefined }))).toBe(false)
  })

  it('other providers only need an active key', () => {
    const anthropic: Provider = { id: 'anthropic', name: 'Anthropic', key: { status: 'active' } }
    expect(providerUsable(anthropic)).toBe(true)
    expect(providerUsable({ ...anthropic, key: { status: 'testing' } })).toBe(false)
  })

  it('US-58 CA-4: only an active subscription token is individual use', () => {
    expect(usesSubscription(claudeCode({}, { credential_kind: 'subscription_token' }))).toBe(true)
    expect(usesSubscription(claudeCode())).toBe(false)
    expect(
      usesSubscription(
        claudeCode({}, { status: 'testing', credential_kind: 'subscription_token' }),
      ),
    ).toBe(false)
    expect(usesSubscription(undefined)).toBe(false)
  })
})
