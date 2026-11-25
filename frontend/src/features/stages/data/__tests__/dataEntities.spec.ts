import { describe, expect, it } from 'vitest'
import type { DicHeader } from '@/features/artifacts/composables/structuredArtifact'
import { entities, foreignKey, keyColumns, relationships } from '../dataEntities'

const column = (
  nome: string,
  tipo: string,
  extra: { nulo?: boolean; dado_pessoal?: boolean } = {},
) => ({
  nome,
  tipo,
  nulo: extra.nulo ?? false,
  descricao: nome,
  dado_pessoal: extra.dado_pessoal ?? false,
})

const dic: DicHeader = {
  modelo_dbml: 'docs/dados/modelo-dados.dbml',
  tabelas: [
    {
      nome: 'companies',
      origem: ['US-01'],
      descricao: 'Empresas.',
      colunas: [
        column('id', 'uuid'),
        column('tax_id', 'char(14)'),
        column('contact_email', 'text', { nulo: true, dado_pessoal: true }),
      ],
      constraints: [
        { nome: 'companies_pkey', tipo: 'primary_key', definicao: 'PRIMARY KEY (id)' },
        { nome: 'companies_tax_id_key', tipo: 'unique', definicao: 'UNIQUE (tax_id)' },
      ],
      indices: [],
    },
    {
      nome: 'invoices',
      descricao: 'Guias.',
      colunas: [column('id', 'uuid'), column('company_id', 'uuid')],
      constraints: [
        { nome: 'invoices_pkey', tipo: 'primary_key', definicao: 'PRIMARY KEY (id)' },
        {
          nome: 'invoices_company_fk',
          tipo: 'foreign_key',
          definicao:
            'FOREIGN KEY (company_id) REFERENCES easycode.companies(id) ON DELETE RESTRICT',
        },
      ],
      indices: [],
    },
    {
      nome: 'payments',
      descricao: 'Pagamentos.',
      colunas: [column('invoice_id', 'uuid')],
      constraints: [
        { nome: 'payments_pkey', tipo: 'primary_key', definicao: 'PRIMARY KEY (invoice_id)' },
        { nome: 'payments_invoice_fk', tipo: 'foreign_key', definicao: 'invoice_id → invoices.id' },
      ],
      indices: [],
    },
  ],
  dados_pessoais: [],
}

describe('Data Modeling entities (ESP-09 "Modelo")', () => {
  it('reads key columns and foreign keys from the constraint definitions', () => {
    expect(
      keyColumns({ nome: 'k', tipo: 'primary_key', definicao: 'PRIMARY KEY (a, "b")' }),
    ).toEqual(['a', 'b'])
    expect(keyColumns({ nome: 'k', tipo: 'check', definicao: 'CHECK (a > 0)' })).toEqual([])
    expect(
      foreignKey({
        nome: 'f',
        tipo: 'foreign_key',
        definicao: 'FOREIGN KEY (artifact_id, type) REFERENCES easycode.artifacts(id, type)',
      }),
    ).toEqual({ columns: ['artifact_id', 'type'], table: 'artifacts' })
    expect(foreignKey({ nome: 'f', tipo: 'foreign_key', definicao: 'room_id → rooms.id' })).toEqual(
      {
        columns: ['room_id'],
        table: 'rooms',
      },
    )
    expect(
      foreignKey({ nome: 'f', tipo: 'foreign_key', definicao: 'see the model' }),
    ).toBeUndefined()
  })

  it('marks primary key, foreign key, unique, nullable and personal columns, with the origin', () => {
    const [companies, invoices] = entities(dic)
    expect(companies?.origins).toEqual(['US-01'])
    expect(companies?.columns).toEqual([
      expect.objectContaining({ name: 'id', primaryKey: true, unique: false }),
      expect.objectContaining({ name: 'tax_id', primaryKey: false, unique: true }),
      expect.objectContaining({ name: 'contact_email', nullable: true, personal: true }),
    ])
    expect(invoices?.origins).toEqual([])
    expect(invoices?.columns[1]).toEqual(
      expect.objectContaining({ name: 'company_id', references: 'companies' }),
    )
  })

  it('writes each foreign key as a cardinality between the tables', () => {
    expect(relationships(dic)).toEqual([
      {
        parent: 'companies',
        child: 'invoices',
        columns: ['company_id'],
        cardinality: 'one_to_many',
        constraint: 'invoices_company_fk',
      },
      {
        parent: 'invoices',
        child: 'payments',
        columns: ['invoice_id'],
        cardinality: 'one_to_one',
        constraint: 'payments_invoice_fk',
      },
    ])
  })
})
