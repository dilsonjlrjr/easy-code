import type {
  AgentConfiguration,
  NewAgentConfiguration,
  PinnedSkill,
  ProjectAgent,
  Skill,
} from '../api/agentsApi'

/**
 * Skill association is always a new agent configuration version (US-43, ESP-22 ER-22-1): the next
 * configuration keeps the instructions adjustment and the tools as they are and changes only the
 * pinned skill list.
 */
function nextConfiguration(
  current: AgentConfiguration,
  skills: Array<Pick<PinnedSkill, 'skill' | 'version'>>,
): NewAgentConfiguration {
  return {
    ...(current.adjustment_instructions !== undefined
      ? { adjustment_instructions: current.adjustment_instructions }
      : {}),
    tools: current.tools,
    skills: skills.map(({ skill, version }) => ({ skill, version })),
  }
}

/** Associate: the skill enters pinned at the chosen version (ESP-22 PC-37). */
export function withSkill(
  current: AgentConfiguration,
  skill: string,
  version: number,
): NewAgentConfiguration {
  const others = current.skills.filter((item) => item.skill !== skill)
  return nextConfiguration(current, [...others, { skill, version }])
}

/** Remove: the skill leaves the next configuration. */
export function withoutSkill(current: AgentConfiguration, skill: string): NewAgentConfiguration {
  return nextConfiguration(
    current,
    current.skills.filter((item) => item.skill !== skill),
  )
}

/** US-43 CA-3: accepting the newer version is saving the configuration with it. */
export function acceptingVersion(
  current: AgentConfiguration,
  skill: string,
): NewAgentConfiguration {
  return nextConfiguration(
    current,
    current.skills.map((item) =>
      item.skill === skill && item.available_version
        ? { skill: item.skill, version: item.available_version }
        : item,
    ),
  )
}

/** ESP-22 3.1: the agent list marks an agent whose pinned skill has a newer version. */
export function hasSkillUpdate(agent: Pick<ProjectAgent, 'configuration'>): boolean {
  return agent.configuration.skills.some((item) => (item.available_version ?? null) !== null)
}

/** CA-22-5, ER-22-3: only an `unblocked` version can be associated; one already pinned is not offered. */
export function canAssociate(skill: Skill, pinned: PinnedSkill[]): boolean {
  return skill.status === 'unblocked' && !pinned.some((item) => item.skill === skill.id)
}
