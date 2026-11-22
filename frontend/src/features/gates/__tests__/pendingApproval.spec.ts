import { beforeEach, describe, expect, it } from 'vitest'
import { savePendingApproval, takePendingApproval } from '../composables/pendingApproval'

const pending = {
  projectId: 'p1',
  gateId: 'gate.requirements',
  round: 2,
  decision: { confirmed_human_checks: ['us.criteria_testable'], comment: 'ok' },
}

describe('pending approval across the SSO step-up (US-06 CA-5)', () => {
  beforeEach(() => {
    window.sessionStorage.clear()
  })

  it('is taken once, for the same round only', () => {
    savePendingApproval(pending)
    expect(takePendingApproval('p1', 'gate.requirements', 2)).toEqual(pending)
    expect(takePendingApproval('p1', 'gate.requirements', 2)).toBeUndefined()
  })

  it('is dropped when the page is another round', () => {
    savePendingApproval(pending)
    expect(takePendingApproval('p1', 'gate.requirements', 3)).toBeUndefined()
    expect(takePendingApproval('p1', 'gate.requirements', 2)).toBeUndefined()
  })
})
