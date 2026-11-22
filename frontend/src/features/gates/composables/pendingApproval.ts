import type { ApprovalDecision } from '../api/gatesApi'

/**
 * Approval waiting for the step-up (US-06 CA-5, ADR-19): the browser leaves for the SSO and comes
 * back to the same round, so the decision the approver already confirmed is kept for this tab only
 * (`sessionStorage`) and sent once after the return. It holds no secret: check ids and the comment.
 */
export interface PendingApproval {
  projectId: string
  gateId: string
  round: number
  decision: ApprovalDecision
}

const STORAGE_KEY = 'easycode.pendingApproval'

function storage(): Storage | undefined {
  try {
    return window.sessionStorage
  } catch {
    return undefined
  }
}

export function savePendingApproval(pending: PendingApproval): void {
  try {
    storage()?.setItem(STORAGE_KEY, JSON.stringify(pending))
  } catch {
    // Without storage the approver confirms again after the return; nothing is approved silently.
  }
}

/** Takes (reads and forgets) the pending approval of this round, so it is sent at most once. */
export function takePendingApproval(
  projectId: string,
  gateId: string,
  round: number,
): PendingApproval | undefined {
  const store = storage()
  let raw: string | null | undefined
  try {
    raw = store?.getItem(STORAGE_KEY)
    store?.removeItem(STORAGE_KEY)
  } catch {
    return undefined
  }
  if (!raw) return undefined
  try {
    const pending = JSON.parse(raw) as PendingApproval
    const matches =
      pending.projectId === projectId && pending.gateId === gateId && pending.round === round
    return matches ? pending : undefined
  } catch {
    return undefined
  }
}
