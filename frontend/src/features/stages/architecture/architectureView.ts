/** ESP-08 (canvas EtapaArquitetura): what the Decisões and Desenho tabs read from the ADR and the ARQ. */

export interface AdrOption {
  nome: string
  pros: string[]
  contras: string[]
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function strings(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string')
    : []
}

function records(value: unknown): Array<Record<string, unknown>> {
  return Array.isArray(value)
    ? value.filter(
        (item): item is Record<string, unknown> => typeof item === 'object' && item !== null,
      )
    : []
}

/** The options of an ADR (`opcoes`), untrusted text. */
export function adrOptions(header: Record<string, unknown>): AdrOption[] {
  return records(header.opcoes).map((option) => ({
    nome: text(option.nome),
    pros: strings(option.pros),
    contras: strings(option.contras),
  }))
}

function normalize(value: string): string {
  return value.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase().trim()
}

/**
 * The chosen option (ESP-08 3: "a escolhida marcada por texto"): the schema keeps the choice in the text
 * of `decisao`, so the option is the one whose name it cites (the longest, when names overlap); -1 when
 * none or the ADR is imposed (a single imposed option is the decision itself).
 */
export function chosenOption(header: Record<string, unknown>): number {
  const options = adrOptions(header)
  if (header.natureza === 'imposta' && options.length === 1) return 0
  const decision = normalize(text(header.decisao))
  let chosen = -1
  let length = 0
  options.forEach((option, index) => {
    const name = normalize(option.nome)
    if (name && decision.includes(name) && name.length > length) {
      chosen = index
      length = name.length
    }
  })
  return chosen
}

export interface ApiOperation {
  method: string
  path: string
  summary: string
}

const METHODS = new Set(['get', 'put', 'post', 'delete', 'patch', 'head', 'options'])

function unquote(value: string): string {
  const trimmed = value.trim()
  if (
    (trimmed.startsWith('"') && trimmed.endsWith('"')) ||
    (trimmed.startsWith("'") && trimmed.endsWith("'"))
  )
    return trimmed.slice(1, -1)
  return trimmed
}

/**
 * The operations of the OpenAPI attachment (ESP-08 "contrato da API": method, path, summary). A small
 * reader of the block YAML of `paths` (no YAML library in the web); anything it does not understand is
 * left out, and the file itself stays one click away.
 */
export function apiOperations(source: string): ApiOperation[] {
  const operations: ApiOperation[] = []
  let inPaths = false
  let pathIndent = -1
  let currentPath = ''
  let methodIndent = -1
  let current: ApiOperation | undefined
  for (const raw of source.split(/\r?\n/)) {
    if (!raw.trim() || raw.trim().startsWith('#')) continue
    const indent = raw.length - raw.trimStart().length
    const line = raw.trim()
    if (indent === 0) {
      inPaths = line === 'paths:'
      pathIndent = -1
      continue
    }
    if (!inPaths) continue
    const pathMatch = /^(['"]?)(\/[^'"]*)\1:\s*(\{.*)?$/.exec(line)
    if (pathMatch && (pathIndent === -1 || indent === pathIndent)) {
      pathIndent = indent
      currentPath = pathMatch[2] ?? ''
      methodIndent = -1
      current = undefined
      continue
    }
    const methodMatch = /^([a-z]+):\s*(\{.*)?$/.exec(line)
    if (
      methodMatch &&
      METHODS.has(methodMatch[1] ?? '') &&
      indent > pathIndent &&
      (methodIndent === -1 || indent === methodIndent)
    ) {
      methodIndent = indent
      current = { method: (methodMatch[1] ?? '').toUpperCase(), path: currentPath, summary: '' }
      const inline = /summary:\s*([^,}]+)/.exec(methodMatch[2] ?? '')
      if (inline?.[1]) current.summary = unquote(inline[1])
      operations.push(current)
      continue
    }
    if (current && indent > methodIndent) {
      const summary = /^summary:\s*(.+)$/.exec(line)
      if (summary?.[1] && !current.summary) current.summary = unquote(summary[1])
    }
  }
  return operations
}

export interface C4Element {
  id: string
  kind: string
  label: string
  detail: string
  external: boolean
}

export interface C4Relation {
  from: string
  to: string
  label: string
}

/** The arguments of a C4-PlantUML/Mermaid macro call, quoted strings kept whole. */
function macroArguments(body: string): string[] {
  const out: string[] = []
  let currentArg = ''
  let quoted = false
  for (const char of body) {
    if (char === '"') {
      quoted = !quoted
      continue
    }
    if (char === ',' && !quoted) {
      out.push(currentArg.trim())
      currentArg = ''
      continue
    }
    currentArg += char
  }
  if (currentArg.trim()) out.push(currentArg.trim())
  return out
}

/**
 * The text alternative of the C4 diagram (ESP-08 3: "lista de elementos e relações; sistema externo
 * identificado por texto"), read from the Mermaid C4 source of `ARQ.c4`. The diagram is never rendered
 * as HTML (RNF-05).
 */
export function c4Model(source: string): { elements: C4Element[]; relations: C4Relation[] } {
  const elements: C4Element[] = []
  const relations: C4Relation[] = []
  for (const raw of source.split(/\r?\n/)) {
    const match = /^\s*([A-Za-z_]+)\s*\((.*)\)\s*$/.exec(raw)
    if (!match) continue
    const macro = match[1] ?? ''
    const args = macroArguments(match[2] ?? '')
    if (/^(Bi)?Rel(_[A-Za-z]+)?$/.test(macro)) {
      relations.push({
        from: args[0] ?? '',
        to: args[1] ?? '',
        label: [args[2], args[3]].filter(Boolean).join(' · '),
      })
      continue
    }
    if (/^(Person|System|Container|Component)(Db|Queue)?(_Ext)?$/.test(macro)) {
      elements.push({
        id: args[0] ?? '',
        kind: macro.replace(/_Ext$/, ''),
        label: args[1] ?? args[0] ?? '',
        detail: args.slice(2).filter(Boolean).join(' · '),
        external: macro.endsWith('_Ext'),
      })
    }
  }
  return { elements, relations }
}

/** ARQ `componentes[]` and `riscos[]` (ESP-08 Desenho), untrusted text. */
export function arqComponents(header: Record<string, unknown>) {
  return records(header.componentes).map((component) => ({
    nome: text(component.nome),
    responsabilidade: text(component.responsabilidade),
    tecnologia: text(component.tecnologia),
    atende: strings(component.atende),
  }))
}

export function arqRisks(header: Record<string, unknown>) {
  return records(header.riscos).map((risk) => ({
    id: text(risk.id),
    descricao: text(risk.descricao),
    impacto: text(risk.impacto),
    mitigacao: text(risk.mitigacao),
  }))
}

/** The OpenAPI attachment of the ARQ version (workflow: docs/arquitetura/api/openapi.yaml). */
export function openApiAttachment(
  attachments: Array<{ path: string }> | undefined,
): string | undefined {
  return attachments?.find((attachment) => /(^|\/)openapi\.ya?ml$/.test(attachment.path))?.path
}
