import { isApiError } from '@/shared/api/problemDetails'

/** Problem types with a message of their own in the catalog (`problems.<slug>`, openapi `Problem`). */
const KNOWN_SLUGS = new Set([
  'validation',
  'artifact-invalid',
  'confirmation-required',
  'not-authenticated',
  'session-expired',
  'forbidden',
  'account-without-access',
  'not-found',
  'state-conflict',
  'duplicate-name',
  'incomplete-configuration',
  'model-without-capability',
  'model-without-current-price',
  'effective-period-in-use',
  'invalid-key',
  'idempotency-conflict',
  'failed-precondition',
  'too-large-attachment',
  'required-precondition',
  'rate-limit',
  'internal',
  'service-unavailable',
])

const STATUS_FALLBACK: Record<number, string> = {
  401: 'not-authenticated',
  403: 'forbidden',
  404: 'not-found',
  409: 'state-conflict',
  412: 'failed-precondition',
  413: 'too-large-attachment',
  429: 'rate-limit',
  503: 'service-unavailable',
}

/** i18n key of the message for an error; unknown problems get the generic one (ER-00-12). */
export function problemMessageKey(error: unknown): string {
  if (!isApiError(error)) return 'problems.network'
  const slug = error.slug
  if (slug && KNOWN_SLUGS.has(slug)) return `problems.${slug}`
  const fallback = STATUS_FALLBACK[error.status]
  if (fallback) return `problems.${fallback}`
  return error.status >= 500 ? 'problems.internal' : 'problems.generic'
}
