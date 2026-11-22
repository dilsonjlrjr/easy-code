import { describe, expect, it } from 'vitest'
import type { StageSummary } from '../api/projectsApi'
import { parallelStages } from '../composables/parallelStages'

function stage(
  id: string,
  dependsOn: string[],
  type: StageSummary['type'] = 'stage',
): StageSummary {
  return { id, name: id, type, agent: 'dba-agent', depends_on: dependsOn } as StageSummary
}

const stages = [
  stage('requirements', []),
  stage('architecture', ['requirements']),
  stage('data', ['architecture']),
  stage('design', ['architecture']),
  stage('qa-plan', ['architecture'], 'parallel_activity'),
  stage('validation', ['data', 'design']),
]

describe('parallelStages (US-20 CA-3)', () => {
  it('pairs Data Modeling and Design, which wait on the same stage', () => {
    expect(parallelStages(stages, 'data').map((item) => item.id)).toEqual(['design'])
    expect(parallelStages(stages, 'design').map((item) => item.id)).toEqual(['data'])
  })

  it('finds none for a stage alone in its level or without dependencies', () => {
    expect(parallelStages(stages, 'validation')).toEqual([])
    expect(parallelStages(stages, 'requirements')).toEqual([])
    expect(parallelStages(stages, 'unknown')).toEqual([])
  })
})
