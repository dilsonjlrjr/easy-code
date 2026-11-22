import { describe, expect, it } from 'vitest'
import {
  foreignKeys,
  referencedArtifact,
  structuredArtifact,
  type DicHeader,
} from '../composables/structuredArtifact'

const dic: DicHeader = {
  tabelas: [
    {
      nome: 'bookings',
      descricao: 'Reservas.',
      colunas: [],
      constraints: [
        { nome: 'bookings_pkey', tipo: 'primary_key', definicao: 'PRIMARY KEY (id)' },
        { nome: 'bookings_room_fk', tipo: 'foreign_key', definicao: 'room_id → rooms.id' },
      ],
      indices: [],
    },
  ],
  dados_pessoais: [],
}

describe('structured artifacts (US-20, US-21)', () => {
  it('recognizes the DBA, Designer and validation types with their required fields', () => {
    expect(structuredArtifact('DIC', { ...dic })?.type).toBe('DIC')
    expect(
      structuredArtifact('TELAS', { telas: [], acessibilidade: { nivel: 'wcag22_aa' } })?.type,
    ).toBe('TELAS')
    expect(
      structuredArtifact('VALIDACAO', { conferencias: [], pareceres: [], veredito: 'liberado' })
        ?.type,
    ).toBe('VALIDACAO')
  })

  it('falls back to the generic header for other types or an incomplete header', () => {
    expect(structuredArtifact('US', { id: 'US-01' })).toBeUndefined()
    expect(structuredArtifact('DIC', { tabelas: [] })).toBeUndefined()
    expect(structuredArtifact('PERSIST', undefined)).toBeUndefined()
  })

  it('lists the foreign keys as relationships', () => {
    expect(foreignKeys(dic).map((item) => item.constraint.nome)).toEqual(['bookings_room_fk'])
  })

  it('links only references that are artifact ids', () => {
    expect(referencedArtifact('US-03/CA-2')).toBe('US-03')
    expect(referencedArtifact('ADR-07')).toBe('ADR-07')
    expect(referencedArtifact('javascript:alert(1)')).toBeUndefined()
  })
})
