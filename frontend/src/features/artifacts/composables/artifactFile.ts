/**
 * The Markdown file of an artifact rebuilt from its header (JSON of the YAML front matter) and body, for
 * the human edits of US-53 made from structured fields (ESP-07 editor). Strings go double-quoted (JSON
 * escapes are valid YAML), so any text the person types stays text; the stage agent validates and
 * formats the file before the version counts (RN-1).
 */

const PLAIN_KEY = /^[A-Za-z_][A-Za-z0-9_-]*$/

function key(name: string): string {
  return PLAIN_KEY.test(name) ? name : JSON.stringify(name)
}

function scalar(value: unknown): string {
  if (value === null || value === undefined) return 'null'
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  if (typeof value === 'string') return JSON.stringify(value)
  return JSON.stringify(value)
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function lines(value: unknown, indent: string): string[] {
  if (Array.isArray(value)) {
    if (value.length === 0) return []
    return value.flatMap((item) => {
      if (isObject(item) && Object.keys(item).length > 0) {
        const [first, ...rest] = objectLines(item, `${indent}  `)
        return [`${indent}- ${(first ?? '').trimStart()}`, ...rest]
      }
      if (Array.isArray(item) && item.length > 0)
        return [`${indent}-`, ...lines(item, `${indent}  `)]
      return [`${indent}- ${inline(item)}`]
    })
  }
  if (isObject(value)) return objectLines(value, indent)
  return [`${indent}${scalar(value)}`]
}

function inline(value: unknown): string {
  if (Array.isArray(value) && value.length === 0) return '[]'
  if (isObject(value) && Object.keys(value).length === 0) return '{}'
  return scalar(value)
}

function objectLines(value: Record<string, unknown>, indent: string): string[] {
  return Object.entries(value).flatMap(([name, item]) => {
    const nested =
      (Array.isArray(item) && item.length > 0) || (isObject(item) && Object.keys(item).length > 0)
    if (!nested) return [`${indent}${key(name)}: ${inline(item)}`]
    return [`${indent}${key(name)}:`, ...lines(item, `${indent}  `)]
  })
}

/** YAML text of a header (block style, quoted strings). */
export function toYaml(header: Record<string, unknown>): string {
  const text = objectLines(header, '').join('\n')
  return text ? `${text}\n` : ''
}

/** The whole artifact file: front matter and body. */
export function artifactFile(header: Record<string, unknown>, body: string): string {
  const rest = body.startsWith('\n') ? body : `\n${body}`
  return `---\n${toYaml(header)}---${rest}`
}
