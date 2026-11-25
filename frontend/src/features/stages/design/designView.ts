/**
 * ESP-10 "Fluxo": the attachment of the TELAS version that holds the navigation flow. The header field
 * `fluxo` names it (normally `fluxo.md`, relative to the artifact folder); the attachment list may carry
 * it relative to the folder or as the repository path, so the match is by the whole path or its end.
 */
export function flowAttachment(
  attachments: Array<{ path: string }> | undefined,
  flow: string,
): string | undefined {
  const items = attachments ?? []
  const wanted = flow.trim().replace(/^\.\//, '')
  if (wanted) {
    const exact = items.find((item) => item.path === wanted)
    if (exact) return exact.path
    const ending = items.find(
      (item) => item.path.endsWith(`/${wanted}`) || wanted.endsWith(`/${item.path}`),
    )
    if (ending) return ending.path
    return undefined
  }
  return items.find((item) => /(^|\/)fluxo\.md$/.test(item.path))?.path
}
