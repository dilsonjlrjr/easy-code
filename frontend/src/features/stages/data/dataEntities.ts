import type {
  DicConstraint,
  DicHeader,
  DicTable,
} from '@/features/artifacts/composables/structuredArtifact'

/**
 * Entities of the Data Modeling stage (ESP-09 section 3 "Modelo", canvas EtapaDados "Modelo de dados"),
 * read from the DIC header (`tabelas[]`, specs/dominio/artefatos/dic.schema.json). The constraint
 * `definicao` is free text written by the DBA agent (untrusted, RNF-05): it is only scanned for the
 * column list of a primary key, unique or foreign key, never evaluated or rendered as markup.
 */

export interface EntityColumn {
  name: string
  type: string
  nullable: boolean
  primaryKey: boolean
  unique: boolean
  /** Table the column points to through a foreign key, without the schema prefix. */
  references?: string
  personal: boolean
}

export interface Entity {
  name: string
  description: string
  /** US or ADR that requires the table (PL-32); empty when the DIC does not say. */
  origins: string[]
  columns: EntityColumn[]
}

/** A foreign key in text (ESP-09: cardinality written, besides any diagram). */
export interface Relationship {
  parent: string
  child: string
  columns: string[]
  /** `one_to_one` when the foreign key columns are also the child's primary key or a unique key. */
  cardinality: 'one_to_many' | 'one_to_one'
  constraint: string
}

const KEY_COLUMNS = /(PRIMARY\s+KEY|UNIQUE)\s*\(([^)]*)\)/i
const FOREIGN_KEY = /FOREIGN\s+KEY\s*\(([^)]*)\)\s*REFERENCES\s+([A-Za-z0-9_."]+)/i
/** Short form also seen in DIC headers: `room_id → rooms.id`. */
const ARROW_KEY = /^\s*([A-Za-z0-9_,\s]+?)\s*(?:→|->)\s*([A-Za-z0-9_."]+)/

function columnList(text: string): string[] {
  return text
    .split(',')
    .map((name) => name.trim().replace(/"/g, ''))
    .filter((name) => name.length > 0)
}

/** `easycode.users` → `users`; `rooms.id` (arrow form) → `rooms`. */
function tableName(reference: string, arrow: boolean): string {
  const parts = reference.replace(/"/g, '').split('.')
  if (arrow && parts.length > 1) parts.pop()
  return parts[parts.length - 1] ?? reference
}

/** Columns a primary key or unique constraint covers; empty when the definition says otherwise. */
export function keyColumns(constraint: DicConstraint): string[] {
  if (constraint.tipo !== 'primary_key' && constraint.tipo !== 'unique') return []
  const match = KEY_COLUMNS.exec(constraint.definicao)
  return match?.[2] ? columnList(match[2]) : []
}

/** The columns and the referenced table of a foreign key, when the definition can be read. */
export function foreignKey(
  constraint: DicConstraint,
): { columns: string[]; table: string } | undefined {
  if (constraint.tipo !== 'foreign_key') return undefined
  const sql = FOREIGN_KEY.exec(constraint.definicao)
  if (sql?.[1] && sql[2]) return { columns: columnList(sql[1]), table: tableName(sql[2], false) }
  const arrow = ARROW_KEY.exec(constraint.definicao)
  if (arrow?.[1] && arrow[2])
    return { columns: columnList(arrow[1]), table: tableName(arrow[2], true) }
  return undefined
}

function sameColumns(left: string[], right: string[]): boolean {
  return left.length === right.length && left.every((name) => right.includes(name))
}

export function entityOf(table: DicTable): Entity {
  const primary = table.constraints
    .filter((item) => item.tipo === 'primary_key')
    .flatMap(keyColumns)
  const uniqueSets = table.constraints
    .filter((item) => item.tipo === 'unique')
    .map(keyColumns)
    .filter((set) => set.length > 0)
  const references = new Map<string, string>()
  for (const constraint of table.constraints) {
    const key = foreignKey(constraint)
    for (const column of key?.columns ?? []) references.set(column, key?.table ?? '')
  }
  return {
    name: table.nome,
    description: table.descricao,
    origins: table.origem ?? [],
    columns: table.colunas.map((column) => ({
      name: column.nome,
      type: column.tipo,
      nullable: column.nulo,
      primaryKey: primary.includes(column.nome),
      unique: uniqueSets.some((set) => set.length === 1 && set[0] === column.nome),
      references: references.get(column.nome),
      personal: column.dado_pessoal,
    })),
  }
}

export function entities(header: DicHeader): Entity[] {
  return header.tabelas.map(entityOf)
}

export function relationships(header: DicHeader): Relationship[] {
  return header.tabelas.flatMap((table) => {
    const primary = table.constraints
      .filter((item) => item.tipo === 'primary_key')
      .flatMap(keyColumns)
    const uniqueSets = table.constraints.filter((item) => item.tipo === 'unique').map(keyColumns)
    return table.constraints.flatMap((constraint): Relationship[] => {
      const key = foreignKey(constraint)
      if (!key) return []
      const oneToOne =
        sameColumns(key.columns, primary) || uniqueSets.some((set) => sameColumns(key.columns, set))
      return [
        {
          parent: key.table,
          child: table.nome,
          columns: key.columns,
          cardinality: oneToOne ? 'one_to_one' : 'one_to_many',
          constraint: constraint.nome,
        },
      ]
    })
  })
}
