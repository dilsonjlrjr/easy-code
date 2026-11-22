import { describe, expect, it } from 'vitest'
import { approverGraceUntil, sortedRoles, type Member } from '../api/membersApi'

const now = new Date('2026-11-22T12:00:00Z')

function member(overrides: Partial<Member>): Member {
  return {
    user: { id: 'ana', name: 'Ana' },
    roles: ['approver'],
    assigned_by: { id: 'ana', name: 'Ana' },
    assigned_at: '2026-11-22T10:00:00Z',
    ...overrides,
  }
}

describe('approverGraceUntil (US-02 CA-8, RN-4)', () => {
  it('gives the end of the 24 h grace of a self-assigned approver', () => {
    expect(approverGraceUntil(member({}), now)?.toISOString()).toBe('2026-11-23T10:00:00.000Z')
  })

  it('has no grace when someone else assigned the role, after 24 h or without approver', () => {
    expect(approverGraceUntil(member({ assigned_by: { id: 'dev', name: 'Dev' } }), now)).toBe(
      undefined,
    )
    expect(approverGraceUntil(member({ assigned_at: '2026-11-21T11:00:00Z' }), now)).toBe(undefined)
    expect(approverGraceUntil(member({ roles: ['member'] }), now)).toBe(undefined)
  })
})

describe('sortedRoles', () => {
  it('always names the lead first (ESP-26)', () => {
    expect(sortedRoles(['member', 'approver', 'lead'])).toEqual(['lead', 'approver', 'member'])
  })
})
