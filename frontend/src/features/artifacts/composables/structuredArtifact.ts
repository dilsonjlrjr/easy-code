/**
 * Typed reading of the structured header of the DBA, Designer and validation artifacts
 * (specs/dominio/artefatos/{dic,persist,telas,validacao}.schema.json). The header comes from the api
 * as the artifact front matter, with the schema field names; it is untrusted generated content
 * (RNF-05), so the screen shows it as text and only links ids that match the artifact id pattern.
 */

export interface DicColumn {
  nome: string
  tipo: string
  nulo: boolean
  padrao?: string | null
  descricao: string
  dado_pessoal: boolean
}

export interface DicConstraint {
  nome: string
  tipo: string
  definicao: string
  regra?: string
}

export interface DicTable {
  nome: string
  origem?: string[]
  descricao: string
  colunas: DicColumn[]
  constraints: DicConstraint[]
  indices: string[]
}

export interface PersonalData {
  campo: string
  finalidade: string
  base_legal: string
  retencao: string
}

export interface DicHeader {
  modelo_dbml?: string
  tabelas: DicTable[]
  dados_pessoais: PersonalData[]
}

export interface PersistIndex {
  nome: string
  tabela: string
  colunas: string[]
  tipo: string
  justificativa: string
  custo_escrita_aceito?: boolean
}

export interface PersistHeader {
  banco: { sgbd: string; versao: string }
  acesso: string
  concorrencia: Array<{ situacao: string; garantia: string }>
  indices: PersistIndex[]
  migracoes: { ferramenta: string; convencao: string }
  desempenho: Array<{
    consulta: string
    volume: string
    meta: string
    plano_conferido: boolean
    rnf?: string
  }>
  backup:
    | { rpo: string; rto: string; estrategia: string }
    | { sem_requisito: true; justificativa: string }
  permissoes: Array<{ papel: string; privilegios: string[]; ddl_em_producao: boolean }>
  protecao_dados_pessoais?: string
}

export interface ScreenEntry {
  id: string
  nome: string
  arquivo: string
  us: string[]
  criterios: string[]
  estados: string[]
  operacoes?: string[]
}

export interface TelasHeader {
  fluxo: string
  prototipo: string
  estilo: string
  telas: ScreenEntry[]
  acessibilidade: { nivel: string; verificado: boolean; observacoes?: string }
  lacunas_contrato: string[]
}

export interface ValidacaoHeader {
  conferencias: Array<{ par: string; resultado: string; inconsistencia?: string; achado?: string }>
  pareceres: Array<{ participante: string; parecer: string; ressalvas: string[]; onda?: string }>
  artefatos_conferidos?: Array<{ id: string; versao: number }>
  veredito: string
}

/** specs/dominio/artefatos/ct.schema.json (US-22). */
export interface CtHeader {
  cobre: string | string[]
  nivel: string
  tipo_caso: string
  precondicoes: string[]
  passos: string[]
  resultado_esperado: string[]
  execucao?: {
    teste?: string
    resultado: string
    evidencia?: string
    achado?: string
    onda?: string
  }
}

/** specs/dominio/artefatos/rel-qa.schema.json (US-22 CA-2). */
export interface RelQaHeader {
  execucao: number
  resumo: {
    ct_executados: number
    passaram: number
    falharam: number
    bloqueados: number
    ca_must_cobertos: number
    ca_must_total: number
    cobertura_codigo?: number
  }
  matriz: Array<{
    us: string
    criterio: string
    ct: string
    nivel: string
    resultado: string
    achado?: string
  }>
  sem_cobertura: Array<{ us: string; criterio: string; motivo: string }>
  comandos: unknown[]
  achados_abertos: string[]
  veredito: string
}

/** specs/dominio/artefatos/ameacas.schema.json (US-23 CA-1). */
export interface AmeacasHeader {
  ativos: unknown[]
  atores: unknown[]
  fluxo: string
  ameacas: Array<{
    id: string
    fronteira: string
    stride: string[]
    descricao: string
    probabilidade: string
    impacto: string
    severidade: string
    mitigacao?: string
    onde: string[]
    status: string
    achado?: string
  }>
}

export interface SeverityCount {
  abertos: number
  fechados: number
  aceitos: number
}

/** specs/dominio/artefatos/rel-seg.schema.json (US-23 CA-2, CA-3). */
export interface RelSegHeader {
  execucao: number
  resumo: Record<'critico' | 'alto' | 'medio' | 'baixo', SeverityCount>
  varreduras: Array<{
    ferramenta: string
    versao: string
    comando: string
    achados: string[]
    falsos_positivos: number
    resultados?: Array<{ id: string; achado?: string; falso_positivo?: boolean }>
  }>
  owasp: Array<{ categoria: string; verificado: boolean; resultado: string; achados: string[] }>
  ameacas_conferidas: unknown[]
  falsos_positivos: Array<{
    ferramenta: string
    regra: string
    local: string
    justificativa: string
  }>
  comandos: unknown[]
  veredito: string
}

export type StructuredArtifact =
  | { type: 'DIC'; header: DicHeader }
  | { type: 'PERSIST'; header: PersistHeader }
  | { type: 'TELAS'; header: TelasHeader }
  | { type: 'VALIDACAO'; header: ValidacaoHeader }
  | { type: 'CT'; header: CtHeader }
  | { type: 'REL-QA'; header: RelQaHeader }
  | { type: 'AMEACAS'; header: AmeacasHeader }
  | { type: 'REL-SEG'; header: RelSegHeader }

/** The QA and Security artifacts, shown by their own view (ESP-15, ESP-16). */
export const QUALITY_TYPES: ReadonlyArray<StructuredArtifact['type']> = [
  'CT',
  'REL-QA',
  'AMEACAS',
  'REL-SEG',
]

/** Header fields each structured view renders; the generic header list skips them. */
export const STRUCTURED_FIELDS: Record<StructuredArtifact['type'], string[]> = {
  DIC: ['modelo_dbml', 'tabelas', 'dados_pessoais'],
  PERSIST: [
    'banco',
    'acesso',
    'concorrencia',
    'indices',
    'migracoes',
    'desempenho',
    'backup',
    'permissoes',
    'protecao_dados_pessoais',
  ],
  TELAS: ['fluxo', 'prototipo', 'estilo', 'telas', 'acessibilidade', 'lacunas_contrato'],
  VALIDACAO: ['conferencias', 'pareceres', 'artefatos_conferidos', 'veredito'],
  CT: ['cobre', 'nivel', 'tipo_caso', 'precondicoes', 'passos', 'resultado_esperado', 'execucao'],
  'REL-QA': ['execucao', 'resumo', 'matriz', 'sem_cobertura', 'achados_abertos', 'veredito'],
  AMEACAS: ['ativos', 'atores', 'fluxo', 'ameacas'],
  'REL-SEG': ['execucao', 'resumo', 'varreduras', 'owasp', 'falsos_positivos', 'veredito'],
}

const REQUIRED: Record<StructuredArtifact['type'], string[]> = {
  DIC: ['tabelas', 'dados_pessoais'],
  PERSIST: ['banco', 'acesso', 'indices', 'migracoes', 'backup', 'permissoes'],
  TELAS: ['telas', 'acessibilidade'],
  VALIDACAO: ['conferencias', 'pareceres', 'veredito'],
  CT: ['cobre', 'nivel', 'passos', 'resultado_esperado'],
  'REL-QA': ['resumo', 'matriz', 'veredito'],
  AMEACAS: ['ameacas'],
  'REL-SEG': ['resumo', 'varreduras', 'owasp', 'veredito'],
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/**
 * The structured view of an artifact, when its type has one and the header carries the required
 * fields; otherwise undefined and the page falls back to the generic header list.
 */
export function structuredArtifact(
  type: string,
  header: Record<string, unknown> | undefined,
): StructuredArtifact | undefined {
  if (!isRecord(header) || !(type in REQUIRED)) return undefined
  const kind = type as StructuredArtifact['type']
  if (!REQUIRED[kind].every((field) => field in header)) return undefined
  return { type: kind, header } as unknown as StructuredArtifact
}

/** Foreign keys of every table, for the relationships list (ESP-09 3, cardinality in text). */
export function foreignKeys(
  header: DicHeader,
): Array<{ table: string; constraint: DicConstraint }> {
  return header.tabelas.flatMap((table) =>
    table.constraints
      .filter((constraint) => constraint.tipo === 'foreign_key')
      .map((constraint) => ({ table: table.nome, constraint })),
  )
}

const ARTIFACT_ID = /^(US|ADR|RNF|T|CT)-[0-9]{2,}$/

/** The artifact a reference points to (`US-03/CA-2` → `US-03`), or undefined when it is no id. */
export function referencedArtifact(reference: string): string | undefined {
  const id = reference.split('/')[0] ?? ''
  return ARTIFACT_ID.test(id) ? id : undefined
}
