import type { ProviderId } from '@/shared/api/contract'
import type { CredentialKind, Provider } from './api/providersApi'

/** The Claude Code of the server, run as the agent executor (US-58, ADR-23). */
export const CLAUDE_CODE: ProviderId = 'claude-code'

/** Prefix of the OAuth token made by `claude setup-token` (same rule as the api). */
const SUBSCRIPTION_PREFIX = 'sk-ant-oat'

/** Install command shown when the binary is not detected (US-58 CA-2). */
export const CLAUDE_CODE_INSTALL = 'curl -fsSL https://claude.ai/install.sh | bash'

/** Kind of a credential being typed, to warn before saving; the api derives it the same way. */
export function credentialKindOf(key: string): CredentialKind {
  return key.startsWith(SUBSCRIPTION_PREFIX) ? 'subscription_token' : 'api_key'
}

/** The active credential is a personal subscription (US-58 CA-4: individual use, cost covered). */
export function usesSubscription(provider: Provider | undefined): boolean {
  return (
    provider?.id === CLAUDE_CODE &&
    provider.key.status === 'active' &&
    provider.key.credential_kind === 'subscription_token'
  )
}

/**
 * Whether models of the provider may be chosen in a model pair: an active key, and for Claude Code
 * also the binary detected on the server (US-58 CA-2; the api refuses the same).
 */
export function providerUsable(provider: Provider): boolean {
  if (provider.key.status !== 'active') return false
  if (provider.id === CLAUDE_CODE) return provider.detection?.detected === true
  return true
}
