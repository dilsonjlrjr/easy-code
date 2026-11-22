import { describe, expect, it } from 'vitest'
import type { Artifact } from '@/features/artifacts/api/artifactsApi'
import { defaultTask, hasDeviation, inPlanOrder, taskFromArtifact } from '../composables/taskPlan'

function task(id: string, header: Record<string, unknown> = {}): Artifact {
  return {
    id,
    type: 'T',
    title: `Tarefa ${id}`,
    state: 'approved',
    version: 1,
    stage: 'architecture',
    author: { type: 'agent', agent: 'architect-agent' },
    wave: 'ARQ-01',
    header: { situacao: 'planejada', depende_de: [], segue: [], ...header },
    body: '',
    commit: null,
  }
}

describe('task plan (US-18, US-19)', () => {
  it('US-18 CA-1: reads what the T covers, follows and depends on from its header', () => {
    const view = taskFromArtifact(
      task('T-02', {
        cobre: [{ us: 'US-03', criterios: ['CA-1', 'CA-2'] }, { nope: true }],
        segue: ['ADR-01'],
        depende_de: ['T-01'],
        tamanho: 'm',
        escopo_arquivos: ['backend/'],
      }),
    )
    expect(view.covers).toEqual([{ us: 'US-03', criteria: ['CA-1', 'CA-2'] }])
    expect(view.follows).toEqual(['ADR-01'])
    expect(view.dependsOn).toEqual(['T-01'])
    expect(view.size).toBe('m')
    expect(view.situation).toBe('planejada')
  })

  it('drops header values outside the schema instead of showing them raw (RNF-05)', () => {
    const view = taskFromArtifact(task('T-01', { situacao: '<b>x</b>', segue: [1, 'ADR-02'] }))
    expect(view.situation).toBeUndefined()
    expect(view.follows).toEqual(['ADR-02'])
    expect(view.devRecord).toBeUndefined()
  })

  it('US-19 CA-1: keeps the commits, tests and deviations of the developer record', () => {
    const view = taskFromArtifact(
      task('T-01', {
        situacao: 'concluida',
        registro_dev: { commits: ['a'.repeat(40)], testes: ['booking_test.go'], desvios: 'nenhum' },
      }),
    )
    expect(view.devRecord).toEqual({
      commits: ['a'.repeat(40)],
      tests: ['booking_test.go'],
      deviations: 'nenhum',
    })
    expect(hasDeviation(view)).toBe(false)
    expect(
      hasDeviation(taskFromArtifact(task('T-03', { registro_dev: { desvios: 'trocou a lib' } }))),
    ).toBe(true)
  })

  it('orders the plan by dependency, then by id, and survives a cycle', () => {
    const order = inPlanOrder(
      [
        task('T-10'),
        task('T-02', { depende_de: ['T-03'] }),
        task('T-03'),
        task('T-01', { depende_de: ['T-01'] }),
      ].map(taskFromArtifact),
    ).map((item) => item.id)
    expect(order).toEqual(['T-01', 'T-03', 'T-02', 'T-10'])
  })

  it('selects the running task first, else the first one not done', () => {
    const views = [
      task('T-01', { situacao: 'concluida' }),
      task('T-02', { situacao: 'planejada' }),
      task('T-03', { situacao: 'em_andamento' }),
    ].map(taskFromArtifact)
    expect(defaultTask(views)?.id).toBe('T-03')
    expect(defaultTask(views.slice(0, 2))?.id).toBe('T-02')
    expect(defaultTask([])).toBeUndefined()
  })
})
