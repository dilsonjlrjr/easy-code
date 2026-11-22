import DOMPurify from 'dompurify'
import MarkdownIt from 'markdown-it'

/**
 * Agent-generated Markdown is untrusted (RNF-05, ADR-19, ESP-00 ER-00-20): raw HTML is escaped by the
 * parser (`html: false`), dangerous link schemes are refused by markdown-it, and the result still goes
 * through DOMPurify. Links open outside the platform without the opener. Headings start at level 3 so
 * they never break the page hierarchy (ESP-21 section 8).
 */
const parser = new MarkdownIt({ html: false, linkify: false, typographer: false })

const HEADING_OFFSET = 2
const MAX_HEADING_LEVEL = 6

parser.core.ruler.push('shift_headings', (state) => {
  for (const token of state.tokens) {
    if (token.type === 'heading_open' || token.type === 'heading_close') {
      const level = Math.min(Number(token.tag.slice(1)) + HEADING_OFFSET, MAX_HEADING_LEVEL)
      token.tag = `h${String(level)}`
    }
  }
})

const purifier = DOMPurify(window)
purifier.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName === 'A') {
    node.setAttribute('rel', 'noopener noreferrer nofollow')
    node.setAttribute('target', '_blank')
  }
})

export function renderUntrustedMarkdown(source: string): string {
  return purifier.sanitize(parser.render(source), {
    FORBID_TAGS: ['img', 'style', 'iframe', 'form', 'input', 'svg', 'math'],
    FORBID_ATTR: ['style'],
  })
}
