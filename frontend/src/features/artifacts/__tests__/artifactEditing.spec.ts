import { describe, expect, it, vi } from 'vitest'
import {
  awaitingAgentReview,
  canEditArtifact,
  followArtifactEdit,
  isImposedAdr,
  newArtifactTemplate,
} from '../composables/artifactEditing'

const running = { state: 'in_progress' as const, my_roles: ['member' as const] }

describe('canEditArtifact (US-53 CA-1, CA-6, CA-7)', () => {
  it('lets any project role edit an editable type in draft or in review', () => {
    expect(canEditArtifact({ type: 'US', state: 'draft' }, running)).toBe(true)
    expect(canEditArtifact({ type: 'ADR', state: 'in_review' }, running)).toBe(true)
  })

  it('refuses an approved artifact unless a scope change finding opened it', () => {
    expect(canEditArtifact({ type: 'US', state: 'approved' }, running)).toBe(false)
    expect(canEditArtifact({ type: 'US', state: 'approved' }, running, 'ACH-03')).toBe(true)
    expect(canEditArtifact({ type: 'US', state: 'replaced' }, running, 'ACH-03')).toBe(false)
  })

  it('refuses other types, ended projects and people without a project role', () => {
    expect(canEditArtifact({ type: 'DIC', state: 'draft' }, running)).toBe(false)
    expect(
      canEditArtifact({ type: 'US', state: 'draft' }, { state: 'canceled', my_roles: ['lead'] }),
    ).toBe(false)
    expect(
      canEditArtifact({ type: 'US', state: 'draft' }, { state: 'in_progress', my_roles: [] }),
    ).toBe(false)
  })
})

describe('awaitingAgentReview (RN-1)', () => {
  it('is true for a human version the agent has not reviewed yet', () => {
    expect(awaitingAgentReview({ type: 'human', reviewed_by: null })).toBe(true)
    expect(awaitingAgentReview({ type: 'human', reviewed_by: 'requirements-agent' })).toBe(false)
    expect(awaitingAgentReview({ type: 'agent', agent: 'requirements-agent' })).toBe(false)
  })
})

describe('newArtifactTemplate', () => {
  it('starts an imposed ADR citing the constraint, without options to compare (CA-5, RN-3)', () => {
    const text = newArtifactTemplate('ADR', 'RESTR-02')
    expect(text).toContain('natureza: imposta')
    expect(text).toContain('- RESTR-02')
    expect(text.startsWith('---\ntipo: ADR')).toBe(true)
    expect(isImposedAdr({ natureza: 'imposta' })).toBe(true)
    expect(isImposedAdr({})).toBe(false)
  })
})

describe('followArtifactEdit (as built: pending, reviewing, applied or refused)', () => {
  const status = (state: 'pending' | 'reviewing' | 'applied' | 'refused') => ({
    id: 'e1',
    command: 'c1',
    operation: 'modify' as const,
    type: 'US',
    state,
  })

  it('waits while the state is one to wait for, then returns the settled edit', async () => {
    const read = vi
      .fn()
      .mockResolvedValueOnce(status('reviewing'))
      .mockResolvedValueOnce(status('applied'))
    const settled = await followArtifactEdit(
      status('pending'),
      read,
      (state) => state === 'pending' || state === 'reviewing',
      { sleep: () => Promise.resolve() },
    )
    expect(settled.state).toBe('applied')
    expect(read).toHaveBeenCalledTimes(2)
  })

  it('the dialog stops at the guard: reviewing is enough', async () => {
    const read = vi.fn().mockResolvedValue(status('reviewing'))
    const settled = await followArtifactEdit(
      status('pending'),
      read,
      (state) => state === 'pending',
      {
        sleep: () => Promise.resolve(),
      },
    )
    expect(settled.state).toBe('reviewing')
  })
})
