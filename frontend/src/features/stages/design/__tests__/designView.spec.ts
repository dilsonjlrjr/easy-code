import { describe, expect, it } from 'vitest'
import { flowAttachment } from '../designView'

describe('flowAttachment (ESP-10 Fluxo)', () => {
  const attachments = [
    { path: 'prototipo/index.html' },
    { path: 'docs/design/fluxo.md' },
    { path: 'estilo.md' },
  ]

  it('finds the file the header names, relative or by repository path', () => {
    expect(flowAttachment(attachments, 'fluxo.md')).toBe('docs/design/fluxo.md')
    expect(flowAttachment(attachments, './estilo.md')).toBe('estilo.md')
    expect(flowAttachment([{ path: 'fluxo.md' }], 'docs/design/fluxo.md')).toBe('fluxo.md')
  })

  it('falls back to fluxo.md only when the header names no file', () => {
    expect(flowAttachment(attachments, '')).toBe('docs/design/fluxo.md')
    expect(flowAttachment(attachments, 'outro.md')).toBeUndefined()
    expect(flowAttachment(undefined, 'fluxo.md')).toBeUndefined()
  })
})
