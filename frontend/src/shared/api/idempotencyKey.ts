/**
 * `Idempotency-Key` for commands and creations (openapi conventions, ESP-00 ER-00-11): one key per
 * user intention, reused when the same intention is retried, so a double click or a retry after a
 * network error never creates two resources.
 */
export function newIdempotencyKey(): string {
  return globalThis.crypto.randomUUID()
}

export function idempotencyHeaders(key: string): Record<string, string> {
  return { 'Idempotency-Key': key }
}
