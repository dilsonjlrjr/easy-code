/** Only same-application paths are accepted as return addresses (ESP-02 CA-02-3). */
export function safeReturnPath(candidate: unknown): string {
  if (typeof candidate !== 'string') return '/'
  if (!candidate.startsWith('/') || candidate.startsWith('//') || candidate.includes('\\'))
    return '/'
  return candidate
}
