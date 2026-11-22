import { describe, expect, it } from 'vitest'
import type { AgentConfiguration, Skill } from '../api/agentsApi'
import {
  acceptingVersion,
  canAssociate,
  hasSkillUpdate,
  withSkill,
  withoutSkill,
} from '../composables/skillAssociation'

const configuration: AgentConfiguration = {
  version: 3,
  instructions_version: 'v1.2',
  adjustment_instructions: 'Use the team glossary.',
  tools: { read_file: ['docs/**'], write_file: ['docs/dados/**'] },
  skills: [
    { skill: 'sql-review', version: 2, available_version: 3 },
    { skill: 'lgpd-check', version: 1, available_version: null },
  ],
  output_criteria: ['schema.valid'],
  created_at: '2026-11-22T10:00:00-03:00',
}

function skill(overrides: Partial<Skill>): Skill {
  return {
    id: 'naming',
    name: 'Naming',
    description: 'Naming rules',
    origin: 'organization',
    current_version: 1,
    status: 'unblocked',
    ...overrides,
  }
}

describe('skill association (US-43)', () => {
  it('keeps the tools and the adjustment and adds the skill pinned at its version', () => {
    expect(withSkill(configuration, 'naming', 4)).toEqual({
      adjustment_instructions: 'Use the team glossary.',
      tools: configuration.tools,
      skills: [
        { skill: 'sql-review', version: 2 },
        { skill: 'lgpd-check', version: 1 },
        { skill: 'naming', version: 4 },
      ],
    })
  })

  it('removes a skill from the next configuration', () => {
    expect(withoutSkill(configuration, 'sql-review').skills).toEqual([
      { skill: 'lgpd-check', version: 1 },
    ])
  })

  it('CA-3: accepting moves only that skill to the available version', () => {
    expect(acceptingVersion(configuration, 'sql-review').skills).toEqual([
      { skill: 'sql-review', version: 3 },
      { skill: 'lgpd-check', version: 1 },
    ])
  })

  it('CA-3: marks the agent with a skill update', () => {
    expect(hasSkillUpdate({ configuration })).toBe(true)
    expect(
      hasSkillUpdate({
        configuration: { ...configuration, skills: [{ skill: 'a', version: 1 }] },
      }),
    ).toBe(false)
  })

  it('CA-22-5: only unblocked and not yet pinned skills can be associated', () => {
    expect(canAssociate(skill({}), configuration.skills)).toBe(true)
    expect(canAssociate(skill({ status: 'blocked' }), configuration.skills)).toBe(false)
    expect(canAssociate(skill({ id: 'sql-review' }), configuration.skills)).toBe(false)
  })
})
