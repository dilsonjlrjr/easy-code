import { describe, expect, it } from 'vitest'
import { renderUntrustedMarkdown } from '../markdown'

describe('renderUntrustedMarkdown (RNF-05, ER-00-20, CA-00-18, CA-21-5)', () => {
  it('renders Markdown with headings shifted below the page headings', () => {
    const html = renderUntrustedMarkdown('# Visão\n\nTexto **forte**.')
    expect(html).toContain('<h3>Visão</h3>')
    expect(html).toContain('<strong>forte</strong>')
  })

  it('neutralizes raw HTML, scripts, event handlers and dangerous links', () => {
    const html = renderUntrustedMarkdown(
      '<script>alert(1)</script>\n\n<img src=x onerror="alert(1)">\n\n[clique](javascript:alert(1))\n\n![a](https://evil.test/x.png)',
    )
    expect(html).not.toContain('<script')
    expect(html).not.toContain('<img')
    expect(html).not.toMatch(/href="javascript:/i)
    expect(html).toContain('&lt;script&gt;')
  })

  it('opens links outside the platform without the opener', () => {
    const html = renderUntrustedMarkdown('[docs](https://example.test/a)')
    expect(html).toContain('rel="noopener noreferrer nofollow"')
    expect(html).toContain('target="_blank"')
  })
})
