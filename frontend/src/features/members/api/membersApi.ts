import { apiClient } from '@/shared/api/apiClient'
import type { Page, ProjectRole, UserRef } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'

/** openapi `Member`: a person of the project and the roles assigned to them (US-02). */
export interface Member {
  user: UserRef
  roles: ProjectRole[]
  assigned_by?: UserRef
  assigned_at?: string
}

export interface MembersApi {
  listMembers: (projectId: string) => Promise<{ items: Member[] }>
  /** Replaces the person's roles (creates the membership); idempotent (ER-26-10). */
  setMemberRoles: (projectId: string, userId: string, roles: ProjectRole[]) => Promise<Member>
  removeMember: (projectId: string, userId: string) => Promise<void>
  searchUsers: (search: string, cursor?: string) => Promise<Page<UserRef>>
}

function membersPath(projectId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/members`
}

export function createMembersApi(client: HttpClient = apiClient): MembersApi {
  return {
    listMembers: (projectId) => client.get<{ items: Member[] }>(membersPath(projectId)),
    setMemberRoles: (projectId, userId, roles) =>
      client.put<Member>(`${membersPath(projectId)}/${encodeURIComponent(userId)}`, { roles }),
    removeMember: (projectId, userId) =>
      client.delete<undefined>(`${membersPath(projectId)}/${encodeURIComponent(userId)}`),
    searchUsers: (search, cursor) =>
      client.get<Page<UserRef>>('/users', { query: { search, cursor, limit: 20 } }),
  }
}

export const membersApi = createMembersApi()

/** Self-assigned approvers wait 24 h before deciding a gate (US-02 RN-4, CA-8). */
export const SELF_ASSIGNMENT_GRACE_MS = 24 * 60 * 60 * 1000

/** End of the grace period of a self-assigned approver, or undefined when there is none. */
export function approverGraceUntil(member: Member, now: Date = new Date()): Date | undefined {
  if (!member.roles.includes('approver') || !member.assigned_at || !member.assigned_by) return
  if (member.assigned_by.id !== member.user.id) return
  const until = new Date(new Date(member.assigned_at).getTime() + SELF_ASSIGNMENT_GRACE_MS)
  return until > now ? until : undefined
}

/** Roles shown in a fixed order, the lead first (ER-26: the lead is always identified). */
export function sortedRoles(roles: ProjectRole[]): ProjectRole[] {
  const order: ProjectRole[] = ['lead', 'approver', 'member']
  return order.filter((role) => roles.includes(role))
}
