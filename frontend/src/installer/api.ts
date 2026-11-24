import type {
  Database,
  DatabaseProbe,
  Defaults,
  DirectoryUser,
  FieldProblem,
  InstallStatus,
  Organization,
  Person,
  Plan,
  SkillPackage,
  SkillSummary,
  SSO,
  SSOProbe,
} from './types'

/** A refusal of the installer API: a code and, for a failed check, the fields and what it found. */
export class InstallerError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly errors: FieldProblem[] = [],
    readonly found?: unknown,
    readonly detail?: string,
  ) {
    super(code)
  }
}

export const TOKEN_HEADER = 'X-Install-Token'

async function request<T>(method: string, path: string, token: string, body?: unknown): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, {
      method,
      headers: {
        Accept: 'application/json',
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
        ...(token ? { [TOKEN_HEADER]: token } : {}),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new InstallerError(0, 'unreachable')
  }
  if (response.status === 204) return undefined as T
  const payload: unknown = await response.json().catch(() => ({}))
  if (!response.ok) {
    const problem = (payload ?? {}) as {
      code?: string
      errors?: FieldProblem[]
      found?: unknown
      message?: string
    }
    throw new InstallerError(
      response.status,
      problem.code ?? 'failed',
      problem.errors ?? [],
      problem.found,
      problem.message,
    )
  }
  return payload as T
}

export const installerApi = {
  state: () => request<{ installed: boolean; version: string }>('GET', '/api/state', ''),
  checkToken: (token: string) => request<undefined>('POST', '/api/token', token),
  defaults: (token: string) => request<Defaults>('GET', '/api/defaults', token),
  checkEnvironment: (token: string) =>
    request<{ checks: Array<{ name: string; ok: boolean }>; version: string }>(
      'POST',
      '/api/check/environment',
      token,
      {},
    ),
  checkDatabase: (token: string, database: Database) =>
    request<DatabaseProbe>('POST', '/api/check/database', token, { database }),
  checkOrganization: (token: string, organization: Organization) =>
    request<{ ok: boolean }>('POST', '/api/check/organization', token, { organization }),
  checkSSO: (token: string, sso: SSO, organization: Organization) =>
    request<SSOProbe>('POST', '/api/check/sso', token, { sso, organization }),
  searchUsers: (token: string, sso: SSO, query: string) =>
    request<{ users: DirectoryUser[] | null }>('POST', '/api/sso/users', token, { sso, query }),
  checkPerson: (token: string, sso: SSO, person: Person, field: 'administrator' | 'user') =>
    request<{ ok: boolean }>('POST', '/api/check/person', token, { sso, person, field }),
  nativeSkills: (token: string) =>
    request<{ skills: SkillSummary[] | null }>('GET', '/api/skills/native', token),
  checkSkill: (token: string, pkg: SkillPackage) =>
    request<SkillSummary>('POST', '/api/check/skill', token, pkg),
  install: (token: string, plan: Plan) =>
    request<InstallStatus>('POST', '/api/install', token, { plan }),
  status: (token: string) => request<InstallStatus>('GET', '/api/install', token),
  retry: (token: string) => request<InstallStatus>('POST', '/api/install/retry', token, {}),
  finish: (token: string) => request<undefined>('POST', '/api/finish', token, {}),
}
