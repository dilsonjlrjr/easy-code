import { describe, expect, it } from 'vitest'
import type { Artifact } from '@/features/artifacts/api/artifactsApi'
import {
  assumptions,
  citations,
  fieldsComplete,
  fieldsOf,
  itemEdit,
  requirementItems,
  reviewHints,
  type StoryFields,
} from '../requirementsView'

function artifact(id: string, type: string, header: Record<string, unknown>): Artifact {
  return {
    id,
    type,
    title: `${type} ${id}`,
    state: 'in_review',
    version: 2,
    stage: 'requirements',
    author: { type: 'agent', agent: 'requirements-agent' },
    wave: 'REQ-01',
    header: { id, tipo: type, ...header },
    body: '# Corpo\n',
    commit: null,
  }
}

const us = artifact('US-01', 'US', {
  ator: 'Funcionário',
  quero: 'reservar uma sala',
  para: 'reunir o time',
  prioridade: 'must',
  origem: 'ata.md, página 2',
  criterios_aceite: [{ id: 'CA-1', dado: 'a', quando: 'b', entao: 'c' }],
  regras_negocio: [{ id: 'RN-1', descricao: 'Sem conflito de horário' }],
})
const rnf = artifact('RNF-01', 'RNF', { requisito: 'Responder rápido', categoria: 'desempenho' })
const visao = artifact('VISAO', 'VISAO', {
  atores: [{ nome: 'Funcionário', quem_e: 'quem reserva', necessidade: 'sala livre' }],
  glossario: [{ termo: 'Sala', definicao: 'Espaço reservável' }],
})
const backlog = artifact('BACKLOG', 'BACKLOG', {
  suposicoes: [{ id: 'SUP-1', descricao: 'Horário comercial', motivo: 'não informado' }],
})
const all = [us, rnf, visao, backlog]

function first<T>(items: T[]): T {
  const [item] = items
  if (item === undefined) throw new Error('no item')
  return item
}

describe('requirementsView (ESP-07)', () => {
  it('lists stories, NFRs (no metric in text), rules by story, actors and terms', () => {
    expect(requirementItems('stories', all)[0]).toMatchObject({
      label: 'US-01',
      story: { actor: 'Funcionário', want: 'reservar uma sala' },
      origin: 'ata.md, página 2',
    })
    expect(requirementItems('nfrs', all)[0]).toMatchObject({
      detail: { key: 'stageRequirements.noMetric' },
      needsAttention: true,
    })
    expect(requirementItems('rules', all)[0]).toMatchObject({
      label: 'RN-1',
      artifact: { id: 'US-01' },
      index: 0,
    })
    expect(requirementItems('actors', all)[0]).toMatchObject({
      label: 'Funcionário',
      artifact: { id: 'VISAO' },
    })
    expect(requirementItems('terms', all)[0]).toMatchObject({ label: 'Sala' })
  })

  it('CA-07-25: editing a criterion sends the whole file with the base version (US-53)', () => {
    const item = first(requirementItems('stories', all))
    const fields = fieldsOf(item) as StoryFields
    first(fields.criterios).entao = 'a reserva aparece na agenda'
    const edit = itemEdit('modify', 'stories', fields, 'ajuste do CA-1', item)
    expect(edit).toMatchObject({
      operation: 'modify',
      artifact: 'US-01',
      base_version: 2,
      reason: 'ajuste do CA-1',
    })
    expect(edit?.content).toContain('entao: "a reserva aparece na agenda"')
    expect(edit?.content).toContain('regras_negocio:')
    expect(edit?.content?.endsWith('---\n# Corpo\n')).toBe(true)
  })

  it('creates a story or NFR as a new artifact and removes it as a delete', () => {
    const create = itemEdit(
      'create',
      'stories',
      {
        ator: 'Gestor',
        quero: 'ver a agenda',
        para: 'planejar',
        prioridade: 'should',
        criterios: [{ id: 'CA-1', dado: 'a', quando: 'b', entao: 'c' }],
      },
      'nova história',
    )
    expect(create).toMatchObject({ operation: 'create', type: 'US' })
    expect(create?.content).toContain('titulo: "Gestor quer ver a agenda"')
    const item = first(requirementItems('nfrs', all))
    expect(itemEdit('delete', 'nfrs', fieldsOf(item), 'remover', item)).toEqual({
      operation: 'delete',
      artifact: 'RNF-01',
      base_version: 2,
      reason: 'remover',
    })
  })

  it('a rule, actor or term changes its artifact in a new version (PL-41)', () => {
    const rule = first(requirementItems('rules', all))
    const removed = itemEdit('delete', 'rules', fieldsOf(rule), 'remover regra', rule)
    expect(removed).toMatchObject({ operation: 'modify', artifact: 'US-01' })
    expect(removed?.content).not.toContain('Sem conflito')
    const added = itemEdit(
      'create',
      'rules',
      { descricao: 'Máximo de 4 horas' },
      'nova regra',
      undefined,
      us,
    )
    expect(added?.content).toContain('id: "RN-2"')
    const term = itemEdit(
      'create',
      'terms',
      { termo: 'Andar', definicao: 'Piso do prédio' },
      'novo termo',
      undefined,
      visao,
    )
    expect(term).toMatchObject({ operation: 'modify', artifact: 'VISAO' })
    expect(term?.content).toContain('termo: "Andar"')
  })

  it('CA-07-11, CA-07-12: hints for a vague criterion and an NFR without metric', () => {
    expect(
      reviewHints('stories', {
        ator: 'a',
        quero: 'b',
        para: 'c',
        prioridade: 'must',
        criterios: [{ id: 'CA-1', dado: 'x', quando: 'y', entao: 'é rápido' }],
      }),
    ).toEqual([{ key: 'stageRequirements.hints.vague', values: { id: 'CA-1' } }])
    expect(reviewHints('nfrs', fieldsOf(first(requirementItems('nfrs', all))))).toEqual([
      { key: 'stageRequirements.hints.metric' },
    ])
    expect(fieldsComplete('terms', { termo: 'x', definicao: '' })).toBe(false)
  })

  it('counts the citations of a source and reads the BACKLOG assumptions', () => {
    expect(citations('ata.md', all)).toBe(1)
    expect(assumptions(all)).toEqual([
      { id: 'SUP-1', descricao: 'Horário comercial', motivo: 'não informado' },
    ])
  })
})
