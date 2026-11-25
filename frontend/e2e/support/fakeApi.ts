import type { Page, Request, Route } from '@playwright/test'

/**
 * In-memory stand-in for easycode-api, served through Playwright route interception. It follows
 * docs/arquitetura/api/openapi.yaml (paths, fields, problem types) and sse.md (frames), so the screens
 * are exercised against the contract; the end-to-end run against the real backend comes later.
 */

type Json = Record<string, unknown>

const API = '/api/v1'
const PROBLEM = 'urn:easycode:problem:'

export interface FakeUser {
  id: string
  name: string
  organization_roles: string[]
}

export interface StreamScript {
  /** Raw SSE text served for this connection (the connection then closes). */
  body: string
  /** Changes the REST state as the real backend would when these events happen. */
  onServe?: () => void
}

export interface ProviderState {
  id: string
  name: string
  key: Json
  /** Only on `claude-code` (US-58). */
  detection?: Json
}

export interface ModelState {
  provider: string
  model: string
  display_name: string
  active: boolean
  capabilities: Json
  current_price?: Json | null
}

export interface ProjectState {
  project: Json
  configuration: Json
  etag: number
  workflow: Json
  waves: Json[]
  waveDetails: Partial<Record<string, Json>>
  calls: Record<string, Json[]>
  checkpoints: Record<string, Json[]>
  questions: Record<string, Json[]>
  /** `SandboxExecution` items by wave (US-41). */
  executions: Record<string, Json[]>
  /** Full output of each execution by its id (`downloadExecutionOutput`). */
  outputs: Partial<Record<string, string>>
  /** `Repository` (US-42), `not_created` until a wave commits; null answers 404 (no role). */
  repository: Json | null
  history: Json[]
  /** `Member` items (US-02); the creator starts as lead and approver (RN-1). */
  members: Json[]
  /** Gate rounds by gate id, newest last (`GateRound`, US-06). */
  gates: Partial<Record<string, Json[]>>
  /** Overrides of `getStage` by stage id (tokens, cost, findings...). */
  stageDetails: Partial<Record<string, Json>>
  artifacts: Partial<
    Record<string, { current: Json; versions: Json[]; byVersion: Partial<Record<number, Json>> }>
  >
  streams: StreamScript[]
  /** `ProjectAgent` items (US-43); each configuration version bumps the agent ETag. */
  agents: Json[]
  agentEtags: Record<string, number>
  /** `Finding` items (US-32, US-33, US-34), in the server order (severity, then opening). */
  findings: Json[]
  /** `Traceability` by root artifact id (US-28 CA-1). */
  links: Partial<Record<string, Json>>
  /** `CoverageMatrix` items of every priority (US-28 CA-2); the query filters them. */
  coverage: Json[]
  /** Parts of `getReleaseSummary` that replace the consolidation built from the state. */
  releaseOverrides?: Json
  /** `ProjectCosts` by `group_by` (US-38); without one, the costs built from the budget. */
  costsByGrouping?: Partial<Record<string, Json>>
  /** `Estimate` by stage (US-40), or a problem to answer with (`model-without-current-price`). */
  estimates?: Partial<Record<string, Json | { problem: Json }>>
  /** `AuditRecord` items of the project trail, oldest first (US-35). */
  audit?: Json[]
  /** `ArtifactComparison` by `<artifact>:<from>:<to>` (US-27). */
  comparisons?: Partial<Record<string, Json>>
  /** `Attachment` items of the business need (US-03, `listAttachments`). */
  attachments?: Json[]
  /** Text of the artifact attachments by `<artifact>:<version>:<path>` (`getArtifactAttachment`, PL-24). */
  attachmentContent?: Partial<Record<string, string>>
  /**
   * Prototype files of each TELAS version served by the content origin (getArtifactContentUrl, ADR-19),
   * by path relative to the folder that holds `prototipo/` (`prototipo/login.html`).
   */
  prototype?: Partial<Record<number, Record<string, string>>>
}

export function problem(slug: string, status: number, extra: Json = {}): Json {
  return {
    type: `${PROBLEM}${slug}`,
    title: slug,
    status,
    trace_id: '4bf92f3577b34da6a3ce929d0e0e4736',
    ...extra,
  }
}

export function sseFrame(event: Json, withId = true): string {
  const lines = []
  if (withId && typeof event.sequence === 'number') lines.push(`id: ${String(event.sequence)}`)
  lines.push(`event: ${String(event.type)}`)
  lines.push(`data: ${JSON.stringify(event)}`)
  return `${lines.join('\n')}\n\n`
}

export function domainEvent(
  projectId: string,
  sequence: number,
  type: string,
  data: Json,
  waveId?: string,
): Json {
  return {
    specversion: '1.0',
    id: `evt-${String(sequence)}`,
    source: `/projects/${projectId}`,
    type: `br.easycode.${type}.v1`,
    subject: waveId ? `waves/${waveId}` : 'project',
    time: '2026-11-22T10:00:00-03:00',
    datacontenttype: 'application/json',
    dataschema: `urn:easycode:schema:event:payload-${type.split('.')[0] ?? ''}:v1`,
    projectid: projectId,
    ...(waveId ? { waveid: waveId } : {}),
    correlationid: '0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b30',
    sequence,
    data,
  }
}

export const DEV_USER: FakeUser = {
  id: 'dev',
  name: 'Dev',
  organization_roles: ['administrator'],
}

export class FakeApi {
  signedIn = false
  user: FakeUser = DEV_USER
  /** The SSO refuses the account (US-01 CA-2): completeWebLogin sends the browser to /access-denied. */
  accountWithoutAccess = false
  /** Unauthenticated answers say `session-expired` instead of `not-authenticated` (ER-02-5). */
  sessionExpired = false
  /** `authenticated_at` within 10 min; otherwise approving a gate asks for the step-up (ADR-19). */
  recentlyAuthenticated = true
  /** The rework wave a gate rejection creates (`GateRound.rework_wave`, ER-12-14); none by default. */
  reworkWaveOnRejection: string | undefined = undefined
  /** The user assigned the approver role to themselves less than 24 h ago (US-02 CA-8). */
  selfAssignedApprover = false
  /** The user accepted a risk in this cycle: they do not approve gate.release (US-33 RN-2). */
  acceptedRiskThisCycle = false
  /** `today` of the fake, the floor of `valid_until` (PL-47). */
  today = '2026-11-22'
  /** Page size of `listFindings` (CA-30-9). */
  findingsPageSize = 50
  attachmentLimit = 1024 * 1024
  providers: ProviderState[] = [
    { id: 'anthropic', name: 'Anthropic', key: { status: 'missing', last_test: null } },
    { id: 'openai', name: 'OpenAI', key: { status: 'missing', last_test: null } },
  ]
  models: ModelState[] = []
  /** Skill library (`Skill`, US-43, US-44): native catalog and organization skills. */
  skills: Json[] = []
  prices: Record<string, Json[]> = {}
  projects: Partial<Record<string, ProjectState>> = {}
  commands: Record<string, Json> = {}
  pendingItems: Json[] = []
  /** `searchPlatform` answers 500 (US-56, error state of the header search). */
  searchFailure = false
  /** `CurrentUser.organization.currency` (budget currency); absent like older api builds. */
  organizationCurrency?: string
  /** `getUserCosts` (`/me/costs`, PL-02); null answers zero in the organization currency. */
  userCosts: Json | null = null
  /** `AuditRecord` items of the organization trail, oldest first (ESP-27). */
  organizationAudit: Json[] = []
  /** Page size of the audit trails and of listProjects, to exercise "load more". */
  auditPageSize = 50
  projectsPageSize = 20
  /** Scripted connections of `watchNotices` (`/me/events`). */
  noticeStreams: StreamScript[] = []
  /** Organization directory for `searchUsers` (US-02). */
  directory: Array<{ id: string; name: string }> = [
    { id: 'ana', name: 'Ana Souza' },
    { id: 'bruno', name: 'Bruno Lima' },
  ]
  /** `LocalClientSession` items of the signed-in person (US-45 CA-5). */
  localClientSessions: Json[] = []
  /** Human artifact edits by id (US-53): the status and the version the review wave records. */
  artifactEdits: Record<string, { status: Json; apply: () => number; agentRefuses: boolean }> = {}
  /** Skill imports by id (US-44): the answer of the next getSkillImport. */
  skillImports: Record<string, Json> = {}
  /** The git source of skill imports works (an installation with the egress gateway). */
  gitSkillImport = false
  /** Transport limit of a skill package (413 `too-large-package`, CA-3). */
  skillPackageLimit = 2 * 1024 * 1024
  /** Requests seen, for assertions (method, path, headers, body). */
  readonly requests: Array<{
    method: string
    path: string
    headers: Record<string, string>
    body: unknown
  }> = []
  private nextId = 1
  /** Content origin of the prototypes (ADR-19): another origin than the web, without cookie. */
  readonly contentOrigin = 'http://content.easycode.test'
  /** Requests the content origin answered (path and the Cookie header it got). */
  readonly contentRequests: Array<{ path: string; cookie: string | undefined }> = []
  /**
   * Content-Security-Policy violations of the web page (ADR-19): `vite preview` sends the policy of
   * the web image, so anything the app loads or injects that the policy blocks shows up here.
   */
  readonly cspViolations: string[] = []

  async install(page: Page): Promise<void> {
    await page.exposeFunction('__easyCodeCspViolation', (violation: string) => {
      this.cspViolations.push(violation)
    })
    await page.addInitScript(() => {
      // Only the web page: a prototype frame answers to the closed CSP of the content origin.
      if (window.self !== window.top) return
      document.addEventListener('securitypolicyviolation', (event) => {
        const report = (window as unknown as Record<string, ((text: string) => void) | undefined>)
          .__easyCodeCspViolation
        report?.(
          `${event.violatedDirective} ${event.blockedURI} at ${event.sourceFile}:${String(event.lineNumber)} ${event.sample}`,
        )
      })
    })
    // ER-11-12: the content origin answers with the closed CSP, never a cookie, and has no /api.
    await page.route(`${this.contentOrigin}/**`, async (route, request) => {
      const url = new URL(request.url())
      this.contentRequests.push({ path: url.pathname, cookie: request.headers().cookie })
      const [, prefix, token, ...rest] = url.pathname.split('/')
      const [projectId, version] = token.split('.')
      const file =
        prefix === 'c'
          ? this.projects[projectId]?.prototype?.[Number(version)]?.[rest.join('/')]
          : undefined
      await route.fulfill({
        status: file === undefined ? 404 : 200,
        headers: {
          'Content-Type':
            file === undefined ? 'text/plain; charset=utf-8' : 'text/html; charset=utf-8',
          'Content-Security-Policy':
            "sandbox allow-scripts; default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'none'; form-action 'none'",
          'X-Content-Type-Options': 'nosniff',
          'Referrer-Policy': 'no-referrer',
        },
        body: file ?? 'Not Found',
      })
    })
    await page.route('**/config.js', (route) =>
      route.fulfill({
        contentType: 'application/javascript',
        body: `window.__EASY_CODE_CONFIG__ = { apiBaseUrl: "${API}" }`,
      }),
    )
    await page.route(`**${API}/**`, async (route, request) => {
      try {
        await this.handle(route, request)
      } catch (error) {
        // A bug in the fake must fail loudly, never leave the request hanging.
        console.error('fake API failed on', request.method(), request.url(), error)
        await route.fulfill({ status: 500, body: String(error) })
      }
    })
  }

  /**
   * `searchPlatform` (US-56) over the seeded projects where the user has a role: project name, artifact
   * and finding title and id, case-insensitive substring, at most 10 per group with the total.
   */
  private search(raw: string): { status: number; body: Json } {
    const term = raw.trim()
    if (Array.from(term).length < 2 || Array.from(term).length > 100) {
      return {
        status: 400,
        body: problem('validation', 400, { errors: [{ field: 'search', message: 'length' }] }),
      }
    }
    if (this.searchFailure) return { status: 500, body: problem('internal', 500) }
    const needle = term.toLowerCase()
    const matches = (...texts: unknown[]) =>
      texts.some((text) => typeof text === 'string' && text.toLowerCase().includes(needle))
    const projects: Json[] = []
    const artifacts: Json[] = []
    const findings: Json[] = []
    for (const state of Object.values(this.projects)) {
      if (!state) continue
      const roles = state.project.my_roles as unknown[] | undefined
      if (roles && roles.length === 0) continue
      const ref = { id: state.project.id, name: state.project.name, state: state.project.state }
      if (matches(state.project.name)) projects.push(ref)
      for (const [id, artifact] of Object.entries(state.artifacts)) {
        const current = artifact?.current
        if (!current || current.type === 'ACH' || !matches(id, current.title)) continue
        artifacts.push({
          id,
          type: current.type,
          title: current.title,
          state: current.state,
          project: ref,
        })
      }
      for (const item of state.findings) {
        if (!matches(item.id, item.title)) continue
        findings.push({
          id: item.id,
          title: item.title,
          status: item.status,
          severity: item.severity,
          project: ref,
        })
      }
    }
    const group = (items: Json[]) => ({
      items: items.slice(0, 10),
      total: items.length,
      project_count: new Set(
        items.map((item) => String((item.project as Json | undefined)?.id ?? item.id)),
      ).size,
    })
    return {
      status: 200,
      body: { projects: group(projects), artifacts: group(artifacts), findings: group(findings) },
    }
  }

  // ------------------------------------------------------------------ seeding helpers

  /** Adds the Claude Code provider (US-58) with its detection on the server. */
  withClaudeCode(detected: boolean, version = '2.1.0 (Claude Code)'): this {
    this.providers.push({
      id: 'claude-code',
      name: 'Claude Code',
      key: { status: 'missing', last_test: null },
      detection: {
        detected,
        version: detected ? version : null,
        checked_at: '2026-11-25T10:00:00-03:00',
      },
    })
    return this
  }

  withActiveKey(providerId = 'anthropic', last4 = 'wxyz'): this {
    const provider = this.providers.find((item) => item.id === providerId)
    if (provider) {
      provider.key = {
        status: 'active',
        last_4: last4,
        saved_at: '2026-11-20T09:00:00-03:00',
        saved_by: { id: 'dev', name: 'Dev' },
        last_test: { result: 'valid', tested_at: '2026-11-20T09:00:00-03:00' },
      }
    }
    return this
  }

  withModel(model: Partial<ModelState> & { model: string }, price?: Json): this {
    const entry: ModelState = {
      provider: 'anthropic',
      display_name: model.model,
      active: true,
      capabilities: {
        tool_use: true,
        context_tokens: 200000,
        prompt_cache: true,
        image_input: false,
      },
      current_price: price ?? null,
      ...model,
    }
    this.models.push(entry)
    if (price) this.prices[`${entry.provider}/${entry.model}`] = [price]
    return this
  }

  addProject(state: Partial<ProjectState> & { project: Json }): ProjectState {
    const id = String(state.project.id)
    const full: ProjectState = {
      configuration: {
        model_pair: null,
        gate_approval: {},
        budget: null,
        artifact_language: 'pt-BR',
      },
      etag: 1,
      workflow: phaseOneWorkflow(null),
      waves: [],
      waveDetails: {},
      calls: {},
      checkpoints: {},
      questions: {},
      executions: {},
      outputs: {},
      repository: {
        status: 'not_created',
        clone_url: `https://easycode.empresa.local/git/${id}.git`,
        main_branch: 'main',
        last_commit: null,
        marks: [],
      },
      history: [],
      members: [
        {
          user: { id: 'dev', name: 'Dev' },
          roles: ['lead', 'approver'],
          assigned_by: { id: 'dev', name: 'Dev' },
          assigned_at: '2026-11-20T10:00:00-03:00',
        },
      ],
      gates: {},
      stageDetails: {},
      artifacts: {},
      streams: [],
      agents: [],
      agentEtags: {},
      findings: [],
      links: {},
      coverage: [],
      ...state,
    }
    this.projects[id] = full
    return full
  }

  /** Requests of the SSO sign-in (`startWebLogin`), with their query. */
  ssoRequests(): string[] {
    return this.requests
      .filter((request) => request.method === 'GET' && request.path.startsWith('/session/login'))
      .map((request) => request.path)
  }

  /** The session ends by inactivity: the next call answers 401 `session-expired`. */
  expireSession(): void {
    this.signedIn = false
    this.sessionExpired = true
  }

  /** The seeded state of a project (fails the test when it was not seeded). */
  project(projectId: string): ProjectState {
    const state = this.projects[projectId]
    if (!state) throw new Error(`project ${projectId} was not seeded`)
    return state
  }

  streamRequests(projectId: string): Array<{ url: string; lastEventId: string | undefined }> {
    return this.requests
      .filter(
        (request) =>
          request.method === 'GET' &&
          request.path.startsWith(`/projects/${projectId}/events`) &&
          !request.path.includes('/history'),
      )
      .map((request) => ({ url: request.path, lastEventId: request.headers['last-event-id'] }))
  }

  // ------------------------------------------------------------------ routing

  private async handle(route: Route, request: Request): Promise<void> {
    const url = new URL(request.url())
    const path = url.pathname.slice(url.pathname.indexOf(API) + API.length)
    const method = request.method()
    let body: unknown
    try {
      body = request.postDataJSON() as unknown
    } catch {
      body = request.postData()
    }
    this.requests.push({ method, path: `${path}${url.search}`, headers: request.headers(), body })

    // Stand-in for startWebLogin + Keycloak + completeWebLogin: the SSO accepts the account at once
    // and the api redirects back to `callback` with the session (or to /access-denied).
    if (method === 'GET' && path === '/session/login') {
      if (this.accountWithoutAccess) {
        return route.fulfill({ status: 302, headers: { Location: '/access-denied' } })
      }
      this.signedIn = true
      this.sessionExpired = false
      if (url.searchParams.get('reauthenticate') === 'true') this.recentlyAuthenticated = true
      const callback = url.searchParams.get('callback') ?? '/'
      return route.fulfill({ status: 302, headers: { Location: callback } })
    }
    if (!this.signedIn) {
      const slug = this.sessionExpired ? 'session-expired' : 'not-authenticated'
      return this.json(route, 401, problem(slug, 401))
    }

    const segments = path.split('/').filter(Boolean)
    const response = this.dispatch(method, segments, body, url, request.headers())
    if ('sse' in response) {
      return route.fulfill({
        status: 200,
        headers: {
          'Content-Type': 'text/event-stream; charset=utf-8',
          'Cache-Control': 'no-cache, no-transform',
        },
        body: response.sse,
      })
    }
    if ('text' in response) {
      return route.fulfill({
        status: 200,
        headers: {
          'Content-Type': 'text/plain; charset=utf-8',
          'Content-Disposition': 'attachment',
          'X-Content-Type-Options': 'nosniff',
        },
        body: response.text,
      })
    }
    if (response.status === 204) return route.fulfill({ status: 204 })
    return this.json(route, response.status, response.body, response.headers)
  }

  private json(
    route: Route,
    status: number,
    body: unknown,
    headers: Record<string, string> = {},
  ): Promise<void> {
    return route.fulfill({
      status,
      headers: {
        'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
        ...headers,
      },
      body: JSON.stringify(body),
    })
  }

  private dispatch(
    method: string,
    segments: string[],
    body: unknown,
    url: URL,
    headers: Record<string, string> = {},
  ):
    | { status: number; body?: unknown; headers?: Record<string, string> }
    | { sse: string }
    | { text: string } {
    const [first, second, third, fourth, fifth, sixth, seventh] = segments as Array<
      string | undefined
    >
    const input = (body ?? {}) as Json

    if (first === 'me' && !second) {
      return {
        status: 200,
        body: {
          ...this.user,
          access_medium: 'web',
          organization: {
            id: 'org-1',
            name: 'Organização de desenvolvimento',
            ...(this.organizationCurrency ? { currency: this.organizationCurrency } : {}),
          },
          attachment_limit_bytes: this.attachmentLimit,
          local_client_sessions_to_review: this.localClientSessions.filter(
            (item) => item.acknowledged === false,
          ).length,
          csrf_token: 'csrf-dev',
          authenticated_at: this.recentlyAuthenticated
            ? '2026-11-22T10:00:00-03:00'
            : '2026-11-22T08:00:00-03:00',
        },
      }
    }
    // US-45: the desktop app's device flow parameters and the person's own sessions.
    if (first === 'session' && second === 'local-client') {
      return {
        status: 200,
        body: {
          issuer: 'https://sso.example.test/realms/easycode',
          client_id: 'easycode-desktop',
          audience: 'easycode-api',
          scopes: ['openid', 'easycode-read'],
          code_validity_s: 600,
        },
      }
    }
    if (first === 'me' && second === 'local-client-sessions') {
      if (!third) return { status: 200, body: { items: this.localClientSessions } }
      const exists = this.localClientSessions.some((item) => item.id === third)
      if (!exists) return { status: 404, body: problem('not-found', 404) }
      if (method === 'POST' && fourth === 'acknowledgement') {
        this.localClientSessions = this.localClientSessions.map((item) =>
          item.id === third ? { ...item, acknowledged: true } : item,
        )
        return { status: 204 }
      }
      if (method !== 'DELETE') return { status: 404, body: problem('not-found', 404) }
      this.localClientSessions = this.localClientSessions.filter((item) => item.id !== third)
      return { status: 204 }
    }
    if (first === 'session' && second === 'logout') {
      this.signedIn = false
      return { status: 204 }
    }
    if (first === 'search' && !second) {
      return this.search(url.searchParams.get('search') ?? '')
    }
    if (first === 'pending-items') {
      const types = url.searchParams.getAll('type')
      const items = this.pendingItems.filter(
        (item) => !types.length || types.includes(String(item.type)),
      )
      return { status: 200, body: { items, next_cursor: null, total: items.length } }
    }
    if (first === 'me' && second === 'costs') {
      return {
        status: 200,
        body: this.userCosts ?? {
          from: '2026-11-01',
          until: '2026-12-01',
          total: {
            useful: { value: '0', currency: 'BRL' },
            lost: { value: '0', currency: 'BRL' },
            total: { value: '0', currency: 'BRL' },
          },
          rework_percentage: 0,
          largest_spend: [],
        },
      }
    }
    if (first === 'audit' && !second) {
      if (!this.user.organization_roles.includes('administrator'))
        return { status: 403, body: problem('forbidden', 403) }
      return this.auditPage(this.organizationAudit, url)
    }
    if (first === 'me' && second === 'events') {
      const script = this.noticeStreams.shift()
      script?.onServe?.()
      return { sse: script?.body ?? 'retry: 60000\n\n: hb\n\n' }
    }

    if (first === 'users' && !second) {
      const search = (url.searchParams.get('search') ?? '').toLowerCase()
      return {
        status: 200,
        body: {
          items: this.directory.filter((person) => person.name.toLowerCase().includes(search)),
          next_cursor: null,
        },
      }
    }

    // Providers, models and prices (US-15, US-37).
    if (first === 'providers' && !second) return { status: 200, body: { items: this.providers } }
    if (first === 'providers' && third === 'key') {
      const provider = this.providers.find((item) => item.id === second)
      if (!provider) return { status: 404, body: problem('not-found', 404) }
      if (method === 'DELETE') {
        provider.key = { status: 'missing', last_test: provider.key.last_test ?? null }
        return { status: 204 }
      }
      const key = typeof input.key === 'string' ? input.key : ''
      if (key.includes('invalid')) {
        return {
          status: 409,
          body: problem('invalid-key', 409, {
            detail: 'O provedor recusou a chave: 401 invalid x-api-key',
            provider_message: '401 invalid x-api-key',
          }),
        }
      }
      provider.key = {
        status: 'active',
        last_4: key.slice(-4),
        saved_at: '2026-11-22T10:00:00-03:00',
        saved_by: { id: 'dev', name: 'Dev' },
        last_test: { result: 'valid', tested_at: '2026-11-22T10:00:00-03:00' },
        // US-58: the api derives the kind of a claude-code credential from its prefix.
        ...(provider.id === 'claude-code'
          ? { credential_kind: key.startsWith('sk-ant-oat') ? 'subscription_token' : 'api_key' }
          : {}),
      }
      return { status: 200, body: provider }
    }
    if (first === 'models') {
      const active = url.searchParams.get('active')
      const items = this.models.filter(
        (model) => active === null || String(model.active) === active,
      )
      return { status: 200, body: { items } }
    }
    if (first === 'providers' && third === 'models' && fourth && !fifth) {
      const model: ModelState = {
        ...(input as unknown as ModelState),
        provider: second ?? '',
        model: decodeURIComponent(fourth),
      }
      const existing = this.models.find(
        (item) => item.provider === model.provider && item.model === model.model,
      )
      model.current_price = existing?.current_price ?? null
      this.models = [...this.models.filter((item) => item !== existing), model]
      return { status: 200, body: model }
    }
    if (first === 'providers' && third === 'models' && fourth && fifth === 'prices') {
      const key = `${second ?? ''}/${decodeURIComponent(fourth)}`
      const list = (this.prices[key] ??= [])
      if (method === 'GET') return { status: 200, body: { items: list } }
      if (method === 'POST') {
        const id = 100 + list.length
        const created = {
          ...input,
          id,
          table_version: String(id),
          used: false,
          recorded_at: '2026-11-22T10:00:00-03:00',
          recorded_by: { id: 'dev', name: 'Dev' },
        }
        list.push(created)
        const model = this.models.find((item) => `${item.provider}/${item.model}` === key)
        if (model) model.current_price = created
        return { status: 201, body: created }
      }
      if (method === 'PATCH' && sixth) {
        const period = list.find((item) => String(item.id) === sixth)
        if (!period) return { status: 404, body: problem('not-found', 404) }
        if (period.used) return { status: 409, body: problem('effective-period-in-use', 409) }
        Object.assign(period, input)
        return { status: 200, body: period }
      }
    }

    // Skill import and release (US-44).
    if (first === 'skills' && second === 'imports') return this.skillImport(method, third, body)
    if (first === 'skills' && second && third === 'versions' && fourth && fifth)
      return this.skillVersionAction(second, Number(fourth), fifth, input)
    // Skill library (US-43 CA-1).
    if (first === 'skills' && !second) {
      const agent = url.searchParams.get('agent')
      const origin = url.searchParams.get('origin')
      const items = this.skills.filter(
        (skill) =>
          (!origin || skill.origin === origin) &&
          (!agent || ((skill.agents as string[] | undefined) ?? []).includes(agent)),
      )
      return { status: 200, body: { items, next_cursor: null } }
    }
    if (first === 'skills' && second && !third) {
      const skill = this.skills.find((item) => item.id === second)
      return skill ? { status: 200, body: skill } : { status: 404, body: problem('not-found', 404) }
    }

    // Projects (US-03, US-05, US-16).
    if (first === 'projects' && !second) {
      if (method === 'POST') return this.createProject(input)
      const items = Object.values(this.projects).flatMap((state) => (state ? [state.project] : []))
      const counts: Record<string, number> = {}
      for (const item of items) counts[String(item.state)] = (counts[String(item.state)] ?? 0) + 1
      const filter = url.searchParams.getAll('state')
      const matching = filter.length
        ? items.filter((item) => filter.includes(String(item.state)))
        : items
      const offset = Number(url.searchParams.get('cursor') ?? '0')
      const limit = Number(url.searchParams.get('limit') ?? String(this.projectsPageSize))
      const end = offset + limit
      return {
        status: 200,
        body: {
          items: matching.slice(offset, end),
          next_cursor: end < matching.length ? String(end) : null,
          count_by_state: counts,
        },
      }
    }
    const state = second ? this.projects[second] : undefined
    if (first === 'projects' && second && !state)
      return { status: 404, body: problem('not-found', 404) }
    if (!state) return { status: 404, body: problem('not-found', 404) }
    if (!third) return { status: 200, body: state.project }
    if (third === 'attachments' && method === 'GET')
      return { status: 200, body: { items: state.attachments ?? [] } }
    if (third === 'attachments') {
      return {
        status: 201,
        body: {
          id: `att-${String(this.nextId++)}`,
          file_name: 'file',
          media_type: 'text/plain',
          size_bytes: 1,
          sha256: '0'.repeat(64),
          submitted_at: '2026-11-22T10:00:00-03:00',
          submitted_by: { id: 'dev', name: 'Dev' },
        },
      }
    }
    if (third === 'configuration') {
      if (method === 'PATCH') {
        const change: Json = { ...input }
        const pair = change.model_pair as { execution?: { model?: string } } | undefined
        const model = this.models.find((item) => item.model === pair?.execution?.model)
        if (model && model.capabilities.tool_use === false) {
          return {
            status: 409,
            body: problem('model-without-capability', 409, { missing_capacity: 'tool_use' }),
          }
        }
        if (['completed', 'canceled'].includes(String(state.project.state))) {
          return { status: 409, body: problem('state-conflict', 409) }
        }
        if ('budget' in change) {
          const refusal = this.budgetRefusal(state, change.budget as Json | null | undefined)
          if (refusal) return refusal
        }
        if (change.constraints) {
          change.constraints = this.versionConstraints(
            state.configuration.constraints as Json | null | undefined,
            change.constraints as Json,
          )
        }
        if (change.gate_approval) {
          // US-07 CA-3: only a gate that allows both modes takes `automatic`.
          const current = state.configuration.gate_approval as Record<string, Json>
          const next: Record<string, Json> = { ...current }
          for (const [gate, mode] of Object.entries(change.gate_approval as Json)) {
            const entry = current[gate] ?? { mode: 'human', allowed: ['human'] }
            if (!(entry.allowed as string[]).includes(String(mode)))
              return { status: 409, body: problem('approval-mode-not-allowed', 409) }
            next[gate] = { ...entry, mode }
          }
          change.gate_approval = next
        }
        const changes = (change.pair_by_stage as Json | undefined) ?? {}
        const current = (state.configuration.pair_by_stage as Json | undefined) ?? {}
        const pairByStage = Object.fromEntries(
          Object.entries({ ...current, ...changes }).filter(([, value]) => value !== null),
        )
        state.configuration = { ...state.configuration, ...change, pair_by_stage: pairByStage }
        state.etag += 1
        if (change.budget) this.applyBudget(state)
      }
      return {
        status: 200,
        body: state.configuration,
        headers: { ETag: `"${String(state.etag)}"` },
      }
    }
    if (third === 'findings')
      return this.findings(state, method, fourth, fifth, input, headers, url)
    if (third === 'coverage') {
      const priorities = url.searchParams.getAll('priority')
      const wanted = priorities.length ? priorities : ['must']
      return {
        status: 200,
        body: { items: state.coverage.filter((item) => wanted.includes(String(item.priority))) },
      }
    }
    if (third === 'artifacts' && fourth && fifth === 'links') {
      const graph = state.links[fourth]
      return graph ? { status: 200, body: graph } : { status: 404, body: problem('not-found', 404) }
    }
    if (third === 'gates' && fourth && fifth === 'rounds')
      return this.gateRounds(state, method, fourth, sixth, seventh, input, headers)
    if (third === 'deletion' && method === 'POST') return this.deleteProject(state, input)
    if (third === 'artifact-edits' && method === 'POST') return this.editArtifact(state, input)
    if (third === 'artifact-edits' && fourth) return this.readArtifactEdit(fourth)
    if (third === 'agents' && fifth === 'configuration' && sixth === 'versions') {
      const agent = state.agents.find((item) => item.agent === fourth)
      if (!agent) return { status: 404, body: problem('not-found', 404) }
      const history = (agent.history as Json[] | undefined) ?? []
      return { status: 200, body: { items: [agent.configuration, ...history] } }
    }
    if (third === 'agents') return this.agents(state, method, fourth, fifth, input, headers)
    if (third === 'members') return this.members(state, method, fourth, input)
    if (third === 'start') return this.startWorkflow(state)
    if (
      method === 'POST' &&
      !fourth &&
      (third === 'pause' ||
        third === 'resume' ||
        third === 'cancellation' ||
        third === 'completion')
    )
      return this.projectLifecycle(state, third, input)
    if (third === 'costs') {
      const grouping = url.searchParams.get('group_by') ?? 'stage'
      if (!['stage', 'agent', 'model', 'provider'].includes(grouping))
        return { status: 400, body: problem('validation', 400) }
      return { status: 200, body: state.costsByGrouping?.[grouping] ?? this.costs(state) }
    }
    if (third === 'audit' && !fourth) {
      const roles = (state.project.my_roles as string[] | undefined) ?? []
      if (!roles.includes('approver') && !roles.includes('lead'))
        return { status: 403, body: problem('forbidden', 403) }
      return this.auditPage(state.audit ?? [], url)
    }
    if (third === 'stages' && fourth && fifth === 'estimate') {
      const estimate = state.estimates?.[fourth]
      if (!estimate) return { status: 404, body: problem('not-found', 404) }
      if ('problem' in estimate) {
        const answer = estimate.problem as Json
        return { status: Number(answer.status), body: answer }
      }
      return { status: 200, body: estimate }
    }
    if (third === 'release' && !fourth) return { status: 200, body: this.releaseSummary(state) }
    if (third === 'commands' && fourth) return { status: 200, body: this.commands[fourth] }
    if (third === 'workflow') return { status: 200, body: state.workflow }
    if (third === 'stages' && fourth) return this.stage(state, fourth)
    if (third === 'events' && fourth === 'history') {
      const waveId = url.searchParams.get('waveId')
      const items = state.history.filter((event) => !waveId || event.waveid === waveId)
      const last = items.reduce((max, event) => Math.max(max, Number(event.sequence ?? 0)), 0)
      return { status: 200, body: { items, last_sequence: last, more: false } }
    }
    if (third === 'events') {
      const script = state.streams.shift()
      script?.onServe?.()
      // Without a script the stream closes and the client waits a minute to come back.
      return { sse: script?.body ?? 'retry: 60000\n\n: hb\n\n' }
    }
    if (third === 'repository') {
      return state.repository
        ? { status: 200, body: state.repository }
        : { status: 404, body: problem('not-found', 404) }
    }
    if (third === 'waves' && !fourth) {
      const stage = url.searchParams.get('stage')
      const task = url.searchParams.get('task')
      const agent = url.searchParams.get('agent')
      const purpose = url.searchParams.get('purpose')
      const items = state.waves.filter(
        (wave) =>
          (!stage || wave.stage === stage) &&
          (!task || wave.task === task) &&
          (!agent || wave.agent === agent) &&
          (!purpose || wave.purpose === purpose),
      )
      return { status: 200, body: { items, next_cursor: null } }
    }
    if (third === 'waves' && fourth) {
      const wave = state.waveDetails[fourth]
      if (!wave) return { status: 404, body: problem('not-found', 404) }
      if (!fifth) return { status: 200, body: wave }
      if (fifth === 'calls')
        return { status: 200, body: { items: state.calls[fourth] ?? [], next_cursor: null } }
      if (fifth === 'sandbox-executions' && !sixth) {
        return { status: 200, body: { items: state.executions[fourth] ?? [], next_cursor: null } }
      }
      if (fifth === 'sandbox-executions' && sixth && seventh === 'output') {
        const output = state.outputs[sixth]
        return output === undefined
          ? { status: 404, body: problem('not-found', 404) }
          : { text: output }
      }
      if (fifth === 'cancellation' && method === 'POST') return this.cancelWave(state, wave, input)
      if (fifth && ['pause', 'resume', 'restart'].includes(fifth) && method === 'POST')
        return this.controlWave(state, wave, fifth, input)
      if (fifth === 'checkpoints') {
        return { status: 200, body: { items: state.checkpoints[fourth] ?? [], next_cursor: null } }
      }
      if (fifth === 'questions' && !sixth)
        return { status: 200, body: { items: state.questions[fourth] ?? [] } }
      if (fifth === 'questions' && sixth && seventh === 'answer') {
        const question = (state.questions[fourth] ?? []).find((item) => item.id === sixth)
        if (question) {
          question.answer = {
            record: {
              per: { type: 'human', id: 'dev', name: 'Dev' },
              when: '2026-11-22T10:05:00-03:00',
              origin: 'web',
            },
            answers: input.answers,
          }
        }
        wave.state = 'running'
        wave.available_actions = ['pause', 'pause_now', 'cancel']
        wave.question_open = null
        const command = this.command('answer', 'wave', fourth, 'applied')
        return { status: 202, body: command }
      }
    }
    if (third === 'artifacts' && !fourth) {
      return {
        status: 200,
        body: {
          items: Object.values(state.artifacts).flatMap((item) =>
            item &&
            (!url.searchParams.get('type') || item.current.type === url.searchParams.get('type'))
              ? [item.current]
              : [],
          ),
          next_cursor: null,
        },
      }
    }
    if (third === 'artifacts' && fourth) {
      const artifact = state.artifacts[fourth]
      if (!artifact) return { status: 404, body: problem('not-found', 404) }
      if (!fifth) return { status: 200, body: artifact.current }
      // getArtifactContentUrl (ADR-19): a new signed URL of a prototype page that exists in the version.
      if (fifth === 'versions' && sixth && seventh === 'content-url') {
        const path = url.searchParams.get('path') ?? 'prototipo/index.html'
        if (!path.startsWith('prototipo/') || path.includes('..'))
          return { status: 400, body: problem('validation', 400) }
        if (state.prototype?.[Number(sixth)]?.[path] === undefined)
          return { status: 404, body: problem('not-found', 404) }
        const token = `${String(state.project.id)}.${sixth}.${String(this.nextId++)}`
        return {
          status: 200,
          body: {
            url: `${this.contentOrigin}/c/${token}/${path}`,
            prefix: 'prototipo/',
            expires_at: new Date(Date.now() + 5 * 60_000).toISOString(),
          },
        }
      }
      if (fifth === 'versions' && !sixth) return { status: 200, body: { items: artifact.versions } }
      if (fifth === 'versions' && sixth && seventh === 'attachment') {
        const path = url.searchParams.get('path') ?? ''
        const listed = (artifact.byVersion[Number(sixth)]?.attachments as Json[] | undefined) ?? []
        const entry = listed.find((item) => item.path === path)
        if (!entry) return { status: 404, body: problem('not-found', 404) }
        const content = state.attachmentContent?.[`${fourth}:${sixth}:${path}`]
        if (content === undefined || !/^text\/|yaml|json|sql/.test(String(entry.media_type)))
          return { status: 409, body: problem('state-conflict', 409) }
        return { text: content }
      }
      if (fifth === 'comparison') {
        const key = `${fourth}:${url.searchParams.get('from') ?? ''}:${url.searchParams.get('to') ?? ''}`
        const comparison = state.comparisons?.[key]
        return comparison
          ? { status: 200, body: comparison }
          : { status: 404, body: problem('not-found', 404) }
      }
      if (
        fifth === 'versions' &&
        sixth &&
        (headers.accept as string | undefined)?.includes('text/markdown')
      ) {
        const version = artifact.byVersion[Number(sixth)]
        if (!version) return { status: 404, body: problem('not-found', 404) }
        return {
          text: `---\nid: ${String(version.id)}\ntitulo: ${String(version.title)}\n---\n${typeof version.body === 'string' ? version.body : ''}`,
        }
      }
      if (fifth === 'versions' && sixth) {
        const version = artifact.byVersion[Number(sixth)]
        return version
          ? { status: 200, body: version }
          : { status: 404, body: problem('not-found', 404) }
      }
    }
    return { status: 404, body: problem('not-found', 404) }
  }

  private command(
    type: string,
    targetType: string,
    targetId: string,
    state: string,
    refusal?: Json,
  ): Json {
    const id = `00000000-0000-4000-8000-${String(this.nextId++).padStart(12, '0')}`
    const command = {
      id,
      type,
      target: { type: targetType, id: targetId },
      state,
      requested_by: { id: 'dev', name: 'Dev' },
      origin: 'web',
      requested_at: '2026-11-22T10:00:00-03:00',
      ...(refusal ? { refusal } : {}),
    }
    this.commands[id] = command
    return command
  }

  /**
   * listProjectAgents, getProjectAgent and modifyAgentConfiguration (US-43): only the lead writes;
   * `If-Match` must match (412); a skill version that is not `unblocked` → 409 `blocked-skill`.
   */
  private agents(
    state: ProjectState,
    method: string,
    agentId: string | undefined,
    action: string | undefined,
    input: Json,
    headers: Record<string, string>,
  ): { status: number; body?: unknown; headers?: Record<string, string> } {
    if (!agentId) return { status: 200, body: { items: state.agents } }
    const agent = state.agents.find((item) => item.agent === agentId)
    if (!agent) return { status: 404, body: problem('not-found', 404) }
    const etag = () => `"${String(state.agentEtags[agentId] ?? 1)}"`
    if (!action) return { status: 200, body: agent, headers: { ETag: etag() } }
    if (action !== 'configuration' || method !== 'PUT')
      return { status: 404, body: problem('not-found', 404) }
    const roles = (state.project.my_roles as string[] | undefined) ?? []
    if (!roles.includes('lead')) return { status: 403, body: problem('forbidden', 403) }
    if (!headers['if-match']) return { status: 428, body: problem('required-precondition', 428) }
    if (headers['if-match'] !== etag())
      return { status: 412, body: problem('failed-precondition', 412) }
    // US-25 CA-3: a tool outside the role → 409 `tool-not-allowed` with its name. The api's rule is
    // `role_limit` when seeded (a role narrowed after the screen loaded), else `allowed_by_role`.
    const limit = (agent.role_limit ?? agent.allowed_by_role) as Json | undefined
    if (limit && input.tools) {
      const tools = input.tools as Json
      for (const tool of ['read_file', 'write_file', 'git', 'scanners']) {
        const outside = ((tools[tool] as string[] | undefined) ?? []).find(
          (value) => !((limit[tool] as string[] | undefined) ?? []).includes(value),
        )
        if (outside) return { status: 409, body: problem('tool-not-allowed', 409, { tool }) }
      }
      if (tools.browser && !limit.browser)
        return { status: 409, body: problem('tool-not-allowed', 409, { tool: 'browser' }) }
    }
    const chosen = (input.skills as Array<{ skill: string; version: number }> | undefined) ?? []
    const pinned: Json[] = []
    for (const item of chosen) {
      const skill = this.skills.find((entry) => entry.id === item.skill)
      const versions = (skill?.versions as Json[] | undefined) ?? []
      const status =
        versions.find((entry) => entry.version === item.version)?.status ??
        (skill?.current_version === item.version ? skill.status : undefined)
      if (status !== 'unblocked')
        return { status: 409, body: problem('blocked-skill', 409, { skill: item.skill }) }
      const current = Number(skill?.current_version ?? item.version)
      pinned.push({
        skill: item.skill,
        version: item.version,
        available_version: current > item.version ? current : null,
      })
    }
    const previous = agent.configuration as Json
    agent.history = [previous, ...((agent.history as Json[] | undefined) ?? [])]
    const configuration = {
      ...previous,
      version: Number(previous.version) + 1,
      tools: input.tools ?? previous.tools,
      adjustment_instructions: input.adjustment_instructions,
      skills: pinned,
      created_at: '2026-11-22T10:20:00-03:00',
      created_by: { id: this.user.id, name: this.user.name },
    }
    agent.configuration = configuration
    state.agentEtags[agentId] = (state.agentEtags[agentId] ?? 1) + 1
    return { status: 200, body: configuration, headers: { ETag: etag() } }
  }

  /**
   * cancelWave (US-14): member, approver or lead; `confirm: true`; `completed` or `canceled` → 409
   * `state-conflict` (CA-3). Accepted as pending; the next getCommand finds it applied.
   */
  private cancelWave(
    state: ProjectState,
    wave: Json,
    input: Json,
  ): { status: number; body: unknown } {
    const roles = (state.project.my_roles as string[] | undefined) ?? []
    if (!roles.some((role) => ['member', 'approver', 'lead'].includes(role)))
      return { status: 403, body: problem('forbidden', 403) }
    if (input.confirm !== true) return { status: 400, body: problem('validation', 400) }
    if (['completed', 'canceled'].includes(String(wave.state)))
      return { status: 409, body: problem('state-conflict', 409, { current_state: wave.state }) }
    wave.state = 'canceled'
    wave.result = 'canceled'
    wave.available_actions = []
    wave.question_open = null
    const summary = state.waves.find((item) => item.id === wave.id)
    if (summary) summary.state = 'canceled'
    const command = this.command('cancel', 'wave', String(wave.id), 'applied')
    return { status: 202, body: { ...command, state: 'pending' } }
  }

  /**
   * deleteProject (US-04): only the lead; only `completed` or `canceled` (CA-2, 409
   * `project-not-ended`); the typed name must match (400 `confirmation-required`). Accepted (202),
   * the project leaves every list at once (CA-1).
   */
  private deleteProject(state: ProjectState, input: Json): { status: number; body?: unknown } {
    const roles = (state.project.my_roles as string[] | undefined) ?? []
    if (!roles.includes('lead')) return { status: 403, body: problem('forbidden', 403) }
    if (!['completed', 'canceled'].includes(String(state.project.state)))
      return { status: 409, body: problem('project-not-ended', 409) }
    if (input.name_confirmation !== state.project.name)
      return { status: 400, body: problem('confirmation-required', 400) }
    this.projects = Object.fromEntries(
      Object.entries(this.projects).filter(([id]) => id !== state.project.id),
    )
    // CA-3: the organization trail keeps who, when and the project id, no content.
    this.organizationAudit.push({
      id: `00000000-0000-4000-9000-${String(this.nextId++).padStart(12, '0')}`,
      when: '2026-11-23T10:00:00-03:00',
      actor: { type: 'human', user: { id: this.user.id, name: this.user.name } },
      origin: 'web',
      action: 'project.delete',
      target: { type: 'project', id: state.project.id },
      project: state.project.id,
      reason: null,
      lost_cost: null,
    })
    return { status: 202 }
  }

  /**
   * importSkill and getSkillImport (US-44). The zip file name drives the outcome: `big.zip` → 413
   * with the limit (CA-3), `missing.zip` → 400 `invalid-skill-package` (CA-2), `suspicious.zip` →
   * a version blocked for suspicious content (CA-4), anything else → version 1 unblocked (CA-1).
   * Accepted as `received`; the next read finds the version in its final state.
   */
  private skillImport(
    method: string,
    importId: string | undefined,
    body: unknown,
  ): { status: number; body?: unknown } {
    if (!this.user.organization_roles.includes('administrator'))
      return { status: 403, body: problem('forbidden', 403) }
    if (method === 'GET' && importId) {
      const found = this.skillImports[importId] as Json | undefined
      return found ? { status: 200, body: found } : { status: 404, body: problem('not-found', 404) }
    }
    const json = typeof body === 'object' && body !== null ? (body as Json) : undefined
    const raw = typeof body === 'string' ? body : ''
    const fileName = /filename="([^"]+)"/.exec(raw)?.[1] ?? ''
    if (fileName === 'big.zip')
      return {
        status: 413,
        body: problem('too-large-package', 413, { limit_bytes: this.skillPackageLimit }),
      }
    if (fileName === 'missing.zip')
      return {
        status: 400,
        body: problem('invalid-skill-package', 400, { reason: 'missing_skill_md' }),
      }
    // As built: the git source is refused until the egress gateway exists (ADR-16).
    if (json && !this.gitSkillImport)
      return {
        status: 400,
        body: problem('validation', 400, {
          errors: [{ field: 'git_url', message: 'host_not_allowed' }],
        }),
      }
    const skillId = json
      ? (String(json.git_url).split('/').pop() ?? 'skill').replace(/\.git$/, '')
      : fileName.replace(/\.zip$/, '')
    const suspicious = skillId === 'suspicious'
    const id = `00000000-0000-4000-a000-${String(this.nextId++).padStart(12, '0')}`
    const origin = json ? 'git_repository' : 'upload'
    const status = suspicious ? 'blocked' : 'unblocked'
    const reasons = suspicious ? ['malicious_content'] : []
    this.skills.push({
      id: skillId,
      name: `Skill ${skillId}`,
      description: 'Skill enviada pela organização.',
      origin: 'organization',
      stages: ['data'],
      agents: ['dba-agent'],
      current_version: 1,
      estimated_tokens: 500,
      status,
      versions: [
        {
          version: 1,
          published_at: '2026-11-23T10:00:00-03:00',
          status,
          submission_origin: origin,
          block_reasons: reasons,
        },
      ],
    })
    this.skillImports[id] = {
      id,
      state: 'version_created',
      origin,
      version_status: status,
      received_at: '2026-11-23T10:00:00-03:00',
      skill: skillId,
      version: 1,
      block_reasons: reasons,
    }
    return {
      status: 202,
      body: { id, state: 'received', origin, received_at: '2026-11-23T10:00:00-03:00' },
    }
  }

  /** unblockSkill and deactivateSkill (US-44 CA-4): administrator, justification 10 to 2000. */
  private skillVersionAction(
    skillId: string,
    version: number,
    action: string,
    input: Json,
  ): { status: number; body?: unknown } {
    if (!this.user.organization_roles.includes('administrator'))
      return { status: 403, body: problem('forbidden', 403) }
    const skill = this.skills.find((item) => item.id === skillId)
    const entry = ((skill?.versions as Json[] | undefined) ?? []).find(
      (item) => item.version === version,
    )
    if (!skill || !entry) return { status: 404, body: problem('not-found', 404) }
    const text = typeof input.justification === 'string' ? input.justification.trim() : ''
    if (text.length < 10) return { status: 400, body: problem('validation', 400) }
    const reasons = (entry.block_reasons as string[] | undefined) ?? []
    if (action === 'unblock') {
      if (entry.status !== 'blocked' || !reasons.includes('malicious_content'))
        return { status: 409, body: problem('state-conflict', 409) }
      entry.status = 'unblocked'
      entry.unblocked_by = { id: this.user.id, name: this.user.name }
    } else if (action === 'deactivation') {
      if (!['blocked', 'unblocked'].includes(String(entry.status)))
        return { status: 409, body: problem('state-conflict', 409) }
      entry.status = 'deactivated'
      entry.deactivated_by = { id: this.user.id, name: this.user.name }
      entry.deactivated_at = '2026-11-23T11:00:00-03:00'
    } else return { status: 404, body: problem('not-found', 404) }
    if (skill.current_version === version) skill.status = entry.status
    return { status: 200, body: skill }
  }

  /**
   * editArtifact and getArtifactEdit (US-53, as built): any project role; draft or in review, or
   * approved with the scope change finding (CA-6, CA-7); `base_version` must be the current one
   * (409 `outdated-version`). Content with `ONDA-ATIVA` is refused by the guard (`state-conflict`);
   * with `AGENTE-RECUSA` the review wave ends without a version (CA-2). Otherwise the first read
   * finds the `edit_review` wave running and the next one the validated version (RN-1).
   */
  private editArtifact(state: ProjectState, input: Json): { status: number; body?: unknown } {
    const roles = (state.project.my_roles as string[] | undefined) ?? []
    if (!roles.some((role) => ['member', 'approver', 'lead'].includes(role)))
      return { status: 403, body: problem('forbidden', 403) }
    const reason = typeof input.reason === 'string' ? input.reason.trim() : ''
    if (reason.length < 3) return { status: 400, body: problem('validation', 400) }
    const content = typeof input.content === 'string' ? input.content : ''
    if (input.operation !== 'create') {
      const artifact = state.artifacts[String(input.artifact)]
      if (!artifact) return { status: 404, body: problem('not-found', 404) }
      const current = artifact.current
      const opened = current.state === 'approved' && Boolean(input.finding)
      if (!['draft', 'in_review'].includes(String(current.state)) && !opened)
        return { status: 409, body: problem('state-conflict', 409) }
      if (input.base_version !== current.version)
        return {
          status: 409,
          body: problem('outdated-version', 409, { current_version: current.version }),
        }
    }
    const guardRefusal = content.includes('ONDA-ATIVA')
      ? problem('state-conflict', 409, { current_state: 'running' })
      : undefined
    const command = this.command(
      'edit_artifact',
      'artifact',
      String(input.artifact ?? input.type),
      guardRefusal ? 'refused' : 'applied',
      guardRefusal,
    )
    const editId = `00000000-0000-4000-b000-${String(this.nextId++).padStart(12, '0')}`
    this.artifactEdits[editId] = {
      status: {
        id: editId,
        command: command.id,
        operation: input.operation,
        type: input.type ?? state.artifacts[String(input.artifact)]?.current.type,
        artifact: input.artifact ?? null,
        state: guardRefusal ? 'refused' : 'reviewing',
        review_wave: guardRefusal ? null : 'REQ-09',
        version: null,
        refusal_reason: guardRefusal ? 'state-conflict' : null,
      },
      apply: () => this.applyArtifactEdit(state, input, reason, content),
      agentRefuses: content.includes('AGENTE-RECUSA'),
    }
    return { status: 202, body: { ...command, state: 'pending', edit: editId } }
  }

  private readArtifactEdit(editId: string): { status: number; body?: unknown } {
    const record = this.artifactEdits[editId] as
      { status: Json; apply: () => number; agentRefuses: boolean } | undefined
    if (!record) return { status: 404, body: problem('not-found', 404) }
    const answer = { ...record.status }
    if (record.status.state === 'reviewing') {
      // The next read finds the review wave finished.
      if (record.agentRefuses) {
        record.status = {
          ...record.status,
          state: 'refused',
          refusal_reason: 'not_validated_by_agent',
        }
      } else {
        record.status = { ...record.status, state: 'applied', version: record.apply() }
      }
    }
    return { status: 200, body: answer }
  }

  /** The version the review wave records: human author, the stage agent as reviewer (RN-1). */
  private applyArtifactEdit(
    state: ProjectState,
    input: Json,
    reason: string,
    content: string,
  ): number {
    const type = String(input.type ?? state.artifacts[String(input.artifact)]?.current.type)
    const author = {
      type: 'human',
      edited_by: { id: this.user.id, name: this.user.name },
      reviewed_by: type === 'ADR' ? 'architect-agent' : 'requirements-agent',
    }
    const body = content.replace(/^---[\s\S]*?---\n?/, '')
    if (input.operation === 'create') {
      const number =
        Object.keys(state.artifacts).filter((id) => id.startsWith(`${type}-`)).length + 1
      const id = `${type}-${String(number).padStart(2, '0')}`
      const created = {
        id,
        type,
        title: /titulo: (.*)/.exec(content)?.[1]?.trim() || id,
        state: 'draft',
        version: 1,
        stage: type === 'ADR' ? 'architecture' : 'requirements',
        author,
        wave: 'REQ-09',
        header: type === 'ADR' ? { natureza: 'imposta' } : {},
        body,
        commit: null,
      }
      state.artifacts[id] = {
        current: created,
        versions: [{ version: 1, data: '2026-11-23', wave: 'REQ-09', author, description: reason }],
        byVersion: { 1: created },
      }
      return 1
    }
    const artifact = state.artifacts[String(input.artifact)]
    if (!artifact) return 0
    const current = artifact.current
    const version = Number(current.version) + 1
    const next = {
      ...current,
      version,
      state: 'draft',
      author,
      wave: 'REQ-09',
      body: input.operation === 'delete' ? '' : body,
    }
    artifact.current = next
    artifact.byVersion[version] = next
    artifact.versions.push({
      version,
      data: '2026-11-23',
      wave: 'REQ-09',
      author,
      description: reason,
    })
    return version
  }

  /** Accumulated cost of the project (`ProjectSummary.accumulated_cost`), zero when not seeded. */
  private spend(state: ProjectState): Json {
    const summary = (state.project.summary as Json | undefined) ?? {}
    return (summary.accumulated_cost as Json | undefined) ?? { value: '0', currency: 'BRL' }
  }

  /** ESP-29 ER-29-9: the stop limit is required and never above the organization's ceiling. */
  private budgetRefusal(
    state: ProjectState,
    budget: Json | null | undefined,
  ): { status: number; body: unknown } | undefined {
    const limit = budget?.stop_limit as Json | undefined
    if (!limit || !/^[0-9]+(\.[0-9]{1,10})?$/.test(String(limit.value)))
      return {
        status: 400,
        body: problem('validation', 400, {
          errors: [{ field: 'budget.stop_limit', message: 'required' }],
        }),
      }
    const alerts = (budget?.percentage_alert_limits as number[] | undefined) ?? []
    if (alerts.some((value) => value < 1 || value > 99) || new Set(alerts).size !== alerts.length)
      return {
        status: 400,
        body: problem('validation', 400, {
          errors: [{ field: 'budget.percentage_alert_limits', message: 'invalid' }],
        }),
      }
    const ceiling = state.configuration.stop_limit_ceiling as Json | null | undefined
    if (ceiling && Number(limit.value) > Number(ceiling.value))
      return { status: 409, body: problem('budget-above-ceiling', 409) }
    return undefined
  }

  /** US-39 CA-5 / ER-29-21: a stop limit at or below the spend pauses the project by budget. */
  private applyBudget(state: ProjectState): void {
    const budget = state.configuration.budget as Json | null
    const limit = budget?.stop_limit as Json | undefined
    const summary = (state.project.summary as Json | undefined) ?? {}
    state.project.summary = { ...summary, stop_limit: limit ?? null }
    if (!limit || state.project.state !== 'in_progress') return
    if (Number(this.spend(state).value) >= Number(limit.value)) {
      state.project.state = 'paused'
      state.project.pause_reason = 'budget'
      state.project.available_actions = ['resume', 'cancel']
    }
  }

  /** getProjectCosts (US-38, US-39): the total and the budget with its balance. */
  /**
   * listProjectAudit and listOrganizationAudit (US-35): filters by action and period, `order`
   * (`asc` by default, the screen asks `desc`), cursor pages. There is no write on the trail.
   */
  private auditPage(records: Json[], url: URL): { status: number; body: unknown } {
    const actions = url.searchParams.getAll('action')
    const from = url.searchParams.get('from')
    const until = url.searchParams.get('until')
    if (from && until && Date.parse(from) > Date.parse(until))
      return { status: 400, body: problem('validation', 400) }
    const matching = records.filter((record) => {
      const when = Date.parse(String(record.when))
      return (
        (!actions.length || actions.includes(String(record.action))) &&
        (!from || when >= Date.parse(from)) &&
        (!until || when <= Date.parse(until))
      )
    })
    if (url.searchParams.get('order') === 'desc') matching.reverse()
    const offset = Number(url.searchParams.get('cursor') ?? '0')
    const limit = Math.min(
      Number(url.searchParams.get('limit') ?? String(this.auditPageSize)),
      this.auditPageSize,
    )
    const end = offset + limit
    return {
      status: 200,
      body: {
        items: matching.slice(offset, end),
        next_cursor: end < matching.length ? String(end) : null,
      },
    }
  }

  private costs(state: ProjectState): Json {
    const spend = this.spend(state)
    const limit = (state.configuration.budget as Json | null)?.stop_limit as Json | undefined
    const zero = { value: '0', currency: spend.currency }
    return {
      grouped_by: 'stage',
      total: { useful: spend, lost: zero, total: spend },
      tokens: { input: 0, output: 0, read_cache: 0, write_cache: 0 },
      time: { execution_ms: 0, cycle_ms: 0 },
      budget: limit
        ? {
            stop_limit: limit,
            balance: {
              value: String(Math.max(0, Number(limit.value) - Number(spend.value))),
              currency: limit.currency,
            },
            used_percentage: (Number(spend.value) / Number(limit.value)) * 100,
          }
        : null,
      groups: [],
    }
  }

  /**
   * pauseProject, resumeProject, cancelProject and completeProject (US-51, US-52) with the guards of
   * the project machine: lead or administrator (lead or approver to complete), the state, the typed
   * name, the budget and the Release.
   */
  private projectLifecycle(
    state: ProjectState,
    action: string,
    input: Json,
  ): { status: number; body: unknown } {
    const project = state.project
    const roles = (project.my_roles as string[] | undefined) ?? []
    const administrator = this.user.organization_roles.includes('administrator')
    const allowed =
      action === 'completion'
        ? roles.includes('lead') || roles.includes('approver')
        : roles.includes('lead') || administrator
    if (!allowed) return { status: 403, body: problem('forbidden', 403) }
    const current = String(project.state)
    const conflict = {
      status: 409,
      body: problem('state-conflict', 409, { current_state: current }),
    }
    const activeWaves = state.waves.filter((wave) =>
      ['queued', 'running', 'awaiting_user', 'pausing', 'paused'].includes(String(wave.state)),
    )
    let type = action
    if (action === 'pause') {
      if (current !== 'in_progress') return conflict
      project.state = 'paused'
      project.pause_reason = 'manual'
      project.available_actions = ['resume', 'cancel']
    } else if (action === 'resume') {
      if (current !== 'paused') return conflict
      const limit = (state.configuration.budget as Json | null)?.stop_limit as Json | undefined
      if (
        project.pause_reason === 'budget' &&
        (!limit || Number(this.spend(state).value) >= Number(limit.value))
      )
        return { status: 409, body: problem('budget-exhausted', 409) }
      project.state = 'in_progress'
      project.pause_reason = null
      project.available_actions = ['pause', 'cancel']
    } else if (action === 'cancellation') {
      type = 'cancel'
      if (!['created', 'in_progress', 'paused'].includes(current)) return conflict
      if (input.name_confirmation !== project.name)
        return { status: 400, body: problem('confirmation-required', 400) }
      project.state = 'canceled'
      project.pause_reason = null
      project.available_actions = []
      for (const wave of activeWaves) {
        wave.state = 'canceled'
        const detail = state.waveDetails[String(wave.id)]
        if (detail) Object.assign(detail, { state: 'canceled', available_actions: [] })
      }
    } else {
      type = 'record_publication'
      if (current !== 'in_progress') return conflict
      const stages = (state.workflow.stages as Json[] | undefined) ?? []
      const release = stages.find((stage) => stage.id === 'release')
      if (release?.state !== 'approved')
        return {
          status: 409,
          body: problem('state-conflict', 409, {
            current_state: current,
            reason: 'release_not_approved',
          }),
        }
      const blocking = state.findings.filter(
        (item) =>
          ['critical', 'high'].includes(String(item.severity)) &&
          !['accepted', 'closed', 'discarded'].includes(String(item.status)),
      )
      if (blocking.length > 0)
        return {
          status: 409,
          body: problem('blocking-finding', 409, { findings: blocking.map((item) => item.id) }),
        }
      if (activeWaves.length > 0)
        return {
          status: 409,
          body: problem('state-conflict', 409, {
            current_state: current,
            reason: 'active_wave',
            waves: activeWaves.map((wave) => wave.id),
          }),
        }
      project.state = 'completed'
      project.available_actions = []
    }
    const command = this.command(type, 'project', String(project.id), 'applied')
    return { status: 202, body: { ...command, state: 'pending' } }
  }

  /**
   * pauseWave, resumeWave and restartWave (US-12, US-13) with the guards of the wave machine: the
   * action must be in `available_actions`; `immediate` needs `confirm_lost_cost`; resume needs the
   * project in progress (409 `budget-exhausted` when it is paused by the budget).
   */
  private controlWave(
    state: ProjectState,
    wave: Json,
    action: string,
    input: Json,
  ): { status: number; body: unknown } {
    const roles = (state.project.my_roles as string[] | undefined) ?? []
    if (!roles.some((role) => ['member', 'approver', 'lead'].includes(role)))
      return { status: 403, body: problem('forbidden', 403) }
    const actions = (wave.available_actions as string[] | undefined) ?? []
    const wanted = action === 'pause' && input.mode === 'immediate' ? 'pause_now' : action
    if (wanted === 'pause_now' && input.confirm_lost_cost !== true)
      return { status: 400, body: problem('confirmation-required', 400) }
    if (action === 'restart' && input.confirm !== true)
      return { status: 400, body: problem('validation', 400) }
    if (!actions.includes(wanted))
      return { status: 409, body: problem('state-conflict', 409, { current_state: wave.state }) }
    if (action === 'resume' && state.project.state === 'paused') {
      const slug = state.project.pause_reason === 'budget' ? 'budget-exhausted' : 'state-conflict'
      return { status: 409, body: problem(slug, 409) }
    }
    const summary = state.waves.find((item) => item.id === wave.id)
    const setState = (next: string, available: string[]) => {
      wave.state = next
      wave.available_actions = available
      if (summary) summary.state = next
    }
    if (wanted === 'pause') {
      setState('pausing', ['pause_now', 'cancel'])
      wave.pause = {
        mode: 'cooperative',
        cause: 'user_request',
        requested_at: '2026-11-22T10:06:00-03:00',
      }
    } else if (wanted === 'pause_now') {
      setState('paused', ['resume', 'cancel'])
      wave.pause = {
        mode: 'immediate',
        cause: 'user_request',
        requested_at: '2026-11-22T10:06:00-03:00',
      }
      const cost = wave.cost as Json
      cost.lost = { value: '0.0040', currency: 'BRL' }
    } else if (wanted === 'resume') {
      setState('running', ['pause', 'pause_now', 'cancel'])
      wave.pause = null
    } else {
      const attempts = (wave.attempts as Json[] | undefined) ?? []
      wave.attempts = [
        ...attempts,
        {
          number: wave.attempt,
          final_state: 'failed',
          failure_cause: wave.failure_cause,
          start: '2026-11-22T09:00:00-03:00',
          end: '2026-11-22T09:30:00-03:00',
          cost: wave.cost,
          initial_sequence: 1,
          final_sequence: 9,
        },
      ]
      wave.attempt = Number(wave.attempt) + 1
      if (summary) summary.attempt = wave.attempt
      wave.failure_cause = null
      wave.result = null
      setState('queued', ['cancel'])
    }
    const command = this.command(wanted, 'wave', String(wave.id), 'applied')
    return { status: 202, body: { ...command, state: 'pending' } }
  }

  /** listGateRounds, getGateRound, approveGate and rejectGate with the rules of US-06 and ADR-19. */
  private gateRounds(
    state: ProjectState,
    method: string,
    gateId: string,
    roundText: string | undefined,
    action: string | undefined,
    input: Json,
    headers: Record<string, string>,
  ): { status: number; body?: unknown } {
    const rounds = state.gates[gateId] ?? []
    if (!roundText) {
      const items = [...rounds].reverse().map((item) => ({
        gate: item.gate,
        stage: item.stage,
        round: item.round,
        state: item.state,
        opened_at: item.opened_at,
        decision: item.decision ?? null,
      }))
      return { status: 200, body: { items } }
    }
    const round = rounds.find((item) => String(item.round) === roundText)
    if (!round) return { status: 404, body: problem('not-found', 404) }
    if (method === 'GET' && !action) return { status: 200, body: round }
    const roles = (state.project.my_roles as string[] | undefined) ?? []
    if (!roles.includes('approver')) return { status: 403, body: problem('forbidden', 403) }
    if (headers['x-csrf-token'] !== 'csrf-dev')
      return { status: 403, body: problem('invalid-csrf', 403) }
    if (round.state !== 'awaiting_human')
      return { status: 409, body: problem('state-conflict', 409) }
    if (this.selfAssignedApprover)
      return {
        status: 409,
        body: problem('separation-of-duties', 409, { reason: 'approver_grace_period' }),
      }
    if (gateId === 'gate.release' && action === 'approval' && this.acceptedRiskThisCycle)
      return {
        status: 409,
        body: problem('separation-of-duties', 409, { reason: 'risk_acceptance_same_cycle' }),
      }
    const checks = (round.checks as Json[] | undefined) ?? []
    const human = checks.filter((check) => check.type === 'human').map((check) => check.check)
    const record = {
      per: { type: 'human', id: this.user.id, user: { id: this.user.id, name: this.user.name } },
      when: '2026-11-22T10:10:00-03:00',
      origin: 'web',
    }
    const stages = (state.workflow.stages as Json[] | undefined) ?? []
    const stageIndex = stages.findIndex((stage) => stage.id === round.stage)
    if (action === 'approval') {
      if (!this.recentlyAuthenticated)
        return { status: 401, body: problem('reauthentication-required', 401) }
      const confirmed = (input.confirmed_human_checks as string[] | undefined) ?? []
      const missing = human.filter((check) => !confirmed.includes(check))
      if (missing.length > 0) return { status: 400, body: problem('pending-human-check', 400) }
      // ER-17-10: below the minimum of distinct approvers the approval stays partial.
      const partial = (round.partial_approvals as Json[] | undefined) ?? []
      if (partial.length + 1 < Number(round.min_approvals ?? 1)) {
        round.partial_approvals = [...partial, record]
        return { status: 200, body: round }
      }
      if (gateId === 'gate.release') this.markRelease(state)
      round.state = 'approved'
      round.decision = { result: 'approved', record, comment: input.comment }
      for (const check of checks) if (check.type === 'human') check.status = 'passed'
      const stage = stages[stageIndex] as Json | undefined
      if (stage) stage.state = 'approved'
      const next = stages[stageIndex + 1] as Json | undefined
      if (next) next.state = 'ready'
    } else if (action === 'rejection') {
      const reason = typeof input.reason === 'string' ? input.reason.trim() : ''
      if (!reason) return { status: 400, body: problem('validation', 400) }
      // US-57 (PD-25 option a): the prototype annotations go only with the rejection of gate.design.
      const annotations = Array.isArray(input.annotations) ? (input.annotations as Json[]) : []
      if (annotations.length > 0 && gateId !== 'gate.design')
        return { status: 400, body: problem('validation', 400) }
      const telas = ((round.outputs as Json[] | undefined) ?? []).find(
        (output) => output.type === 'TELAS',
      )
      round.state = 'rejected'
      round.decision = { result: 'rejected', record, reason }
      if (gateId === 'gate.design')
        (round.decision as Json).annotations = annotations.map((item, index) => ({
          ...item,
          number: index + 1,
          artifact: 'TELAS',
          version: telas?.version ?? 1,
        }))
      // ER-12-14: the rejection creates a rework wave of the stage agent (openapi `GateRound.rework_wave`).
      round.rework_wave = this.reworkWaveOnRejection ?? null
      const stage = stages[stageIndex] as Json | undefined
      if (stage) stage.state = 'returned'
    } else {
      return { status: 404, body: problem('not-found', 404) }
    }
    round.available_actions = []
    this.pendingItems = this.pendingItems.filter(
      (item) => (item.target as Json).id !== `${gateId}/${String(round.round)}`,
    )
    return { status: 200, body: round }
  }

  /** US-08 CA-3: the approved Release marks the version in the repository; the lead may conclude. */
  private markRelease(state: ProjectState): void {
    const commit = 'a'.repeat(40)
    const repository = state.repository ?? {
      status: 'available',
      clone_url: `https://easycode.empresa.local/git/${String(state.project.id)}.git`,
      main_branch: 'main',
    }
    state.repository = {
      ...repository,
      status: 'available',
      marks: [{ name: 'release-01', commit, when: '2026-11-22T10:10:00-03:00' }],
    }
    const actions = (state.project.available_actions as string[] | undefined) ?? []
    const roles = (state.project.my_roles as string[] | undefined) ?? []
    if (roles.includes('lead') || roles.includes('approver'))
      state.project.available_actions = [...new Set([...actions, 'record_publication'])]
  }

  /**
   * getReleaseSummary (US-08, US-33 CA-4): built from the seeded gates, findings and waves, as the
   * api consolidates them; `releaseOverrides` replaces any part of it.
   */
  private releaseSummary(state: ProjectState): Json {
    const stages = (state.workflow.stages as Json[] | undefined) ?? []
    const gates = stages
      .filter((stage) => stage.id !== 'release')
      .map((stage) => {
        const gate = `gate.${String(stage.id)}`
        const rounds = state.gates[gate] ?? []
        const last = rounds.at(-1)
        return {
          gate,
          state: last ? last.state : stage.state === 'approved' ? 'approved' : null,
          decision: last?.decision ?? null,
        }
      })
    const SUMMARY = [
      'id',
      'title',
      'severity',
      'category',
      'status',
      'destination_stage',
      'author',
      'blocking',
      'acceptance_expired',
    ]
    const summaryOf = (item: Json) => Object.fromEntries(SUMMARY.map((key) => [key, item[key]]))
    const open = state.findings.filter(
      (item) => !['accepted', 'closed', 'discarded'].includes(String(item.status)),
    )
    const accepted = state.findings.filter((item) => item.status === 'accepted' && item.acceptance)
    const blocks: Json[] = [
      ...open
        .filter((item) => ['critical', 'high'].includes(String(item.severity)))
        .map((item) => ({ type: 'blocking_finding_no_acceptance', reference: item.id })),
      ...accepted
        .filter((item) => item.acceptance_expired === true)
        .map((item) => ({
          type: 'acceptance_expired',
          reference: item.id,
          valid_until: (item.acceptance as Json).valid_until ?? null,
        })),
      ...state.waves
        .filter((wave) =>
          ['queued', 'running', 'awaiting_user', 'pausing', 'paused'].includes(String(wave.state)),
        )
        .map((wave) => ({ type: 'active_wave', reference: wave.id })),
    ]
    const releaseStage = stages.find((stage) => stage.id === 'release')
    return {
      stage_state: releaseStage?.state ?? null,
      gates,
      tests: { ct_total: 12, passed: 12, failed: 0, report: 'REL-QA-01' },
      open_findings: open.map(summaryOf),
      accepted_risks: accepted.map((item) => ({
        finding: summaryOf(item),
        acceptance: item.acceptance,
      })),
      cost_total: this.costs(state).total,
      blocks,
      release_artifact: null,
      mark_name: 'release-01',
      ...(state.releaseOverrides ?? {}),
    }
  }

  /**
   * listFindings, getFinding and the decisions (US-32, US-33): approver only; risk acceptance, extra
   * round and scope change need the anti-CSRF token and a recent authentication (ADR-19).
   */
  private findings(
    state: ProjectState,
    method: string,
    findingId: string | undefined,
    action: string | undefined,
    input: Json,
    headers: Record<string, string>,
    url: URL,
  ): { status: number; body?: unknown } {
    const SUMMARY = [
      'id',
      'title',
      'severity',
      'category',
      'status',
      'origin_stage',
      'destination_stage',
      'author',
      'blocking',
      'cycles',
      'acceptance_expired',
      'opened_at',
    ]
    if (!findingId) {
      const statuses = url.searchParams.getAll('status')
      const severities = url.searchParams.getAll('severity')
      const destination = url.searchParams.get('destination_stage')
      const filtered = state.findings.filter(
        (item) =>
          (!statuses.length || statuses.includes(String(item.status))) &&
          (!severities.length || severities.includes(String(item.severity))) &&
          (!destination || item.destination_stage === destination),
      )
      const start = Number(url.searchParams.get('cursor') ?? 0)
      const page = filtered.slice(start, start + this.findingsPageSize)
      const next = start + this.findingsPageSize
      return {
        status: 200,
        body: {
          items: page.map((item) =>
            Object.fromEntries(Object.entries(item).filter(([key]) => SUMMARY.includes(key))),
          ),
          next_cursor: next < filtered.length ? String(next) : null,
        },
      }
    }
    const finding = state.findings.find((item) => item.id === findingId)
    if (!finding) return { status: 404, body: problem('not-found', 404) }
    if (method === 'GET') return { status: 200, body: finding }
    const roles = (state.project.my_roles as string[] | undefined) ?? []
    if (!roles.includes('approver')) return { status: 403, body: problem('forbidden', 403) }
    const event: Record<string, string> = {
      'risk-acceptance': 'accept_risk',
      'extra-round': 'authorize_new_rework',
      'scope-change': 'decide_scope_change',
      discard: 'discard',
    }
    const decision = action ? event[action] : undefined
    if (!decision) return { status: 404, body: problem('not-found', 404) }
    if (decision !== 'discard') {
      if (headers['x-csrf-token'] !== 'csrf-dev')
        return { status: 403, body: problem('invalid-csrf', 403) }
      if (!this.recentlyAuthenticated)
        return { status: 401, body: problem('reauthentication-required', 401) }
    }
    const actions = (finding.available_actions as string[] | undefined) ?? []
    if (!actions.includes(decision))
      return {
        status: 409,
        body: problem('state-conflict', 409, { current_state: finding.status }),
      }
    const justification = typeof input.justification === 'string' ? input.justification.trim() : ''
    if (justification.length < 10)
      return {
        status: 400,
        body: problem('validation', 400, {
          errors: [{ field: 'justification', message: 'required' }],
        }),
      }
    const who = { id: this.user.id, name: this.user.name }
    const when = '2026-11-22T10:20:00-03:00'
    const escalations = (finding.escalations as Json[] | undefined) ?? []
    const lastEscalation = escalations.at(-1)
    if (decision === 'accept_risk') {
      const validUntil = typeof input.valid_until === 'string' ? input.valid_until : null
      if (validUntil && validUntil < this.today)
        return {
          status: 400,
          body: problem('validation', 400, {
            errors: [{ field: 'valid_until', message: 'before today' }],
          }),
        }
      if (finding.status === 'escalated' && lastEscalation) {
        Object.assign(lastEscalation, {
          decision: 'accept_risk',
          authorized_by: who,
          decided_at: when,
          justification,
        })
      }
      finding.status = 'accepted'
      finding.blocking = false
      finding.acceptance_expired = false
      finding.acceptance = { per: who, when, origin: 'web', justification, valid_until: validUntil }
      finding.available_actions = ['accept_risk']
      this.acceptedRiskThisCycle = true
    } else if (decision === 'authorize_new_rework') {
      if (lastEscalation) {
        Object.assign(lastEscalation, {
          decision: 'new_rework',
          authorized_by: who,
          decided_at: when,
          justification,
        })
      }
      const cycles = (finding.cycle_history as Json[] | undefined) ?? []
      const number = cycles.length + 1
      const wave = `DEV-${String(10 + number).padStart(2, '0')}`
      finding.cycle_history = [...cycles, { number, rework_wave: wave, result: null }]
      finding.cycles = number
      finding.status = 'in_rework'
      finding.available_actions = ['accept_risk', 'discard']
    } else if (decision === 'decide_scope_change') {
      if (lastEscalation) {
        Object.assign(lastEscalation, {
          decision: 'scope_change',
          authorized_by: who,
          decided_at: when,
          justification,
          modified_by: input.modified_by,
          starting_point_artifacts: input.starting_point_artifacts,
        })
      }
      finding.status = 'awaiting_scope'
      finding.available_actions = []
    } else {
      finding.status = 'discarded'
      finding.blocking = false
      finding.discard_reason =
        input.reason === 'duplicate' ? `duplicate of ${String(input.duplicate_of)}` : 'no_evidence'
      finding.available_actions = []
    }
    this.pendingItems = this.pendingItems.filter((item) => (item.target as Json).id !== findingId)
    return { status: 200, body: finding }
  }

  /** US-54 CA-3 / PL-13: the whole list becomes the next version; new items get the next RESTR-NN. */
  private versionConstraints(previous: Json | null | undefined, input: Json): Json {
    const known = ((previous?.items as Json[] | undefined) ?? []).map((item) =>
      Number(String(item.id).slice('RESTR-'.length)),
    )
    let next = Math.max(0, ...known)
    const items = ((input.items as Json[] | undefined) ?? []).map((item) =>
      item.id ? item : { ...item, id: `RESTR-${String(++next).padStart(2, '0')}` },
    )
    return {
      version: Number(previous?.version ?? 0) + 1,
      items,
      defined_by: { id: this.user.id, name: this.user.name },
      defined_at: '2026-11-22T10:00:00-03:00',
    }
  }

  /** getStage (US-49 CA-2): the workflow item plus its waves, artifacts and totals. */
  private stage(state: ProjectState, stageId: string): { status: number; body: unknown } {
    const stages = (state.workflow.stages as Json[] | undefined) ?? []
    const summary = stages.find((item) => item.id === stageId)
    if (!summary) return { status: 404, body: problem('not-found', 404) }
    const zero = { value: '0', currency: 'BRL' }
    return {
      status: 200,
      body: {
        ...summary,
        waves: state.waves.filter((wave) => wave.stage === stageId),
        artifacts: Object.values(state.artifacts).flatMap((item) =>
          item && item.current.stage === stageId ? [item.current] : [],
        ),
        findings: [],
        tokens: { input: 0, output: 0, read_cache: 0, write_cache: 0 },
        detailed_cost: { useful: zero, lost: zero, total: zero },
        time: { execution_ms: 0, cycle_ms: 0 },
        approval: { mode: 'human', required_role: 'approver' },
        // openapi `Stage.gate_checks` (PC-20, PL-46): the checks of the stage gate, before any round.
        gate_checks: summary.gate
          ? [
              {
                check: 'artifact.schema_valid',
                name: 'Cabeçalho YAML válido no esquema do tipo',
                type: 'automatic',
                failure_severity: 'blocks',
              },
              {
                check: 'finding.no_open_blocking',
                name: 'Nenhum ACH Crítico ou Alto ativo',
                type: 'automatic',
                failure_severity: 'blocks',
              },
            ]
          : [],
        ...(state.stageDetails[stageId] ?? {}),
      },
    }
  }

  /** listMembers, setMemberRoles and removeMember with the rules of US-02 (RN-2, RN-3, ER-26-4). */
  private members(
    state: ProjectState,
    method: string,
    userId: string | undefined,
    input: Json,
  ): { status: number; body?: unknown } {
    if (!userId) return { status: 200, body: { items: state.members } }
    const roles = (input.roles as string[] | undefined) ?? []
    const current = state.members.find((member) => (member.user as Json).id === userId)
    const isAdministrator = this.user.organization_roles.includes('administrator')
    const approversAfter = (next: Json[]) =>
      next.filter((member) => (member.roles as string[]).includes('approver')).length
    if (method === 'DELETE') {
      if (!current) return { status: 404, body: problem('not-found', 404) }
      if ((current.roles as string[]).includes('lead'))
        return { status: 409, body: problem('lead-required', 409) }
      const next = state.members.filter((member) => member !== current)
      if (approversAfter(next) === 0) return { status: 409, body: problem('last-approver', 409) }
      state.members = next
      return { status: 204 }
    }
    const alreadyLead = (current?.roles as string[] | undefined)?.includes('lead') ?? false
    if (roles.includes('lead') && !alreadyLead) {
      if (!isAdministrator) return { status: 403, body: problem('forbidden', 403) }
      if (userId === this.user.id)
        return { status: 409, body: problem('separation-of-duties', 409) }
    }
    if (current && (current.roles as string[]).includes('lead') && !roles.includes('lead'))
      return { status: 409, body: problem('lead-required', 409) }
    const person = this.directory.find((item) => item.id === userId) ?? current?.user
    if (!person) return { status: 404, body: problem('not-found', 404) }
    const updated: Json = {
      user: person,
      roles,
      assigned_by: { id: this.user.id, name: this.user.name },
      assigned_at: '2026-11-22T10:00:00-03:00',
    }
    let next = current
      ? state.members.map((member) => (member === current ? updated : member))
      : [...state.members, updated]
    if (roles.includes('lead') && !alreadyLead) {
      // US-02 RN-3: the previous lead loses the role in the same operation.
      next = next.map((member) =>
        member === updated
          ? member
          : { ...member, roles: (member.roles as string[]).filter((role) => role !== 'lead') },
      )
      state.project = { ...state.project, lead: person }
    }
    if (approversAfter(next) === 0) return { status: 409, body: problem('last-approver', 409) }
    state.members = next
    return { status: 200, body: updated }
  }

  private createProject(input: Json): { status: number; body: unknown } {
    if (input.provider_submission_acknowledgment !== true) {
      return { status: 400, body: problem('validation', 400) }
    }
    const name = typeof input.name === 'string' ? input.name : ''
    if (Object.values(this.projects).some((state) => state?.project.name === name)) {
      return { status: 409, body: problem('duplicate-name', 409) }
    }
    const id = `3f6c1e2a-8b4d-4c9e-a1f7-${String(this.nextId++).padStart(12, '0')}`
    const state = this.addProject({
      project: {
        id,
        name,
        state: 'created',
        business_need: input.business_need,
        workflow: 'workflow-padrao',
        created_at: '2026-11-22T10:00:00-03:00',
        created_by: { id: 'dev', name: 'Dev' },
        lead: { id: 'dev', name: 'Dev' },
        my_roles: ['lead', 'approver'],
        available_actions: ['start', 'cancel'],
        last_sequence: 1,
      },
    })
    if (input.constraints) {
      state.configuration.constraints = this.versionConstraints(null, input.constraints as Json)
    }
    if (input.budget) {
      const refusal = this.budgetRefusal(state, input.budget as Json)
      if (refusal) {
        this.projects = Object.fromEntries(
          Object.entries(this.projects).filter(([key]) => key !== id),
        )
        return refusal
      }
      state.configuration.budget = input.budget
    }
    return { status: 201, body: state.project }
  }

  /** US-05: refused with the missing list without a valid key or pair; otherwise the first wave is queued. */
  private startWorkflow(state: ProjectState): { status: number; body: unknown } {
    const projectId = String(state.project.id)
    const missing: string[] = []
    if (!this.providers.some((provider) => provider.key.status === 'active'))
      missing.push('provider_key')
    if (!state.configuration.model_pair) missing.push('model_pair')
    // US-39 CA-4: the stop limit is required to start.
    if (!state.configuration.budget) missing.push('budget')
    if (missing.length > 0) {
      const refusal = problem('incomplete-configuration', 409, { missing })
      return { status: 202, body: this.command('start', 'project', projectId, 'refused', refusal) }
    }
    state.project = {
      ...state.project,
      state: 'in_progress',
      available_actions: ['pause', 'cancel'],
      summary: {
        approved_stages: 0,
        stages_total: 3,
        gates_awaiting: 0,
        open_questions: 0,
        accumulated_cost: { value: '0', currency: 'BRL' },
        stop_limit: (state.configuration.budget as Json | null)?.stop_limit ?? null,
        active_waves: [{ wave: 'REQ-01', agent: 'requirements-agent', state: 'queued' }],
      },
    }
    state.workflow = phaseOneWorkflow('started')
    const wave = waveSummary('REQ-01', 'queued')
    state.waves = [wave]
    state.waveDetails['REQ-01'] = waveDetail('REQ-01', 'queued')
    // Accepted as pending; the next getCommand finds it applied, as the worker would.
    const command = this.command('start', 'project', projectId, 'applied')
    return { status: 202, body: { ...command, state: 'pending' } }
  }
}

export function phaseOneWorkflow(mode: null | 'started'): Json {
  const stage = (
    id: string,
    name: string,
    agent: string,
    dependsOn: string[],
    state: string | null,
  ) => ({
    id,
    name,
    short_name: name,
    type: 'stage',
    agent,
    depends_on: dependsOn,
    required_capabilities: id === 'implementation' ? ['tool_use'] : [],
    state,
    active_waves: id === 'requirements' && state ? 1 : 0,
  })
  return {
    id: 'workflow-padrao',
    version: 1,
    mode: 'fixed',
    stages: [
      stage('requirements', 'Requisitos', 'requirements-agent', [], mode ? 'running' : null),
      stage(
        'architecture',
        'Arquitetura',
        'architect-agent',
        ['requirements'],
        mode ? 'blocked' : null,
      ),
      stage(
        'implementation',
        'Implementação',
        'developer-agent',
        ['architecture'],
        mode ? 'blocked' : null,
      ),
    ],
  }
}

export function waveSummary(id: string, state: string): Json {
  return {
    id,
    stage: 'requirements',
    agent: 'requirements-agent',
    state,
    purpose: 'execution',
    attempt: 1,
    created_at: '2026-11-22T10:00:00-03:00',
    cost_total: { value: '0', currency: 'BRL' },
    tokens: { input: 0, output: 0, read_cache: 0, write_cache: 0 },
  }
}

export function waveDetail(id: string, state: string, overrides: Json = {}): Json {
  return {
    ...waveSummary(id, state),
    models: {
      reasoning: { provider: 'anthropic', model: 'reasoning-model' },
      execution: { provider: 'anthropic', model: 'execution-model' },
    },
    tokens: { input: 0, output: 0, read_cache: 0, write_cache: 0 },
    cost: {
      useful: { value: '0', currency: 'BRL' },
      lost: { value: '0', currency: 'BRL' },
      total: { value: '0', currency: 'BRL' },
      estimated_lost: { value: '0', currency: 'BRL' },
    },
    time: { execution_ms: 0, cycle_ms: 0 },
    result: null,
    attempts: [],
    available_actions: state === 'awaiting_user' ? ['answer', 'cancel'] : ['cancel'],
    last_checkpoint: null,
    expected_outputs: [],
    ...overrides,
  }
}

/** A gate round awaiting a human decision, with one automatic and one human check (ESP-12). */
export function gateRound(gate: string, stage: string, round: number, overrides: Json = {}): Json {
  return {
    gate,
    stage,
    round,
    state: 'awaiting_human',
    opened_at: '2026-11-22T09:00:00-03:00',
    decision: null,
    approval_mode: 'human',
    min_approvals: 1,
    outputs: [{ artifact: 'VISAO', version: 2, type: 'VISAO', state: 'in_review' }],
    checks: [
      {
        check: 'schema.valid',
        name: 'Artefatos válidos no esquema',
        type: 'automatic',
        failure_severity: 'blocks',
        status: 'passed',
        evidence: [{ reference: 'VISAO', message: 'esquema conferido' }],
      },
      {
        check: 'us.criteria_testable',
        name: 'Critérios de aceite testáveis',
        type: 'human',
        failure_severity: 'blocks',
        status: 'pending',
      },
    ],
    blocking_findings: [],
    non_blocking_findings: [],
    available_actions: ['approve', 'reject'],
    ...overrides,
  }
}

/** A finding (`Finding`) of the Security agent, open and blocking (ESP-20). */
export function finding(id: string, overrides: Json = {}): Json {
  return {
    id,
    title: 'Senha em texto puro no log de autenticação',
    severity: 'high',
    category: 'vulnerabilidade',
    status: 'open',
    origin_stage: 'security',
    destination_stage: 'implementation',
    author: 'security-agent',
    blocking: true,
    cycles: 0,
    acceptance_expired: false,
    opened_at: '2026-11-22T09:30:00-03:00',
    affected_artifacts: [{ artifact: 'T-03', item: 'internal/auth/handler.go:42' }],
    origin_wave: 'SEG-01',
    cycle_history: [],
    escalations: [],
    acceptance: null,
    resolution: null,
    modified_artifacts: [],
    discard_reason: null,
    artifact: id,
    available_actions: ['accept_risk', 'discard'],
    ...overrides,
  }
}

/** The Phase 2 default workflow up to the Release; `release` is the state of the last stage. */
export function releaseWorkflow(release: string, security = 'approved'): Json {
  const stage = (id: string, name: string, agent: string, dependsOn: string[], state: string) => ({
    id,
    name,
    short_name: name,
    type: 'stage',
    agent,
    depends_on: dependsOn,
    required_capabilities: [],
    state,
    gate: `gate.${id}`,
    active_waves: 0,
  })
  return {
    id: 'workflow-padrao',
    version: 1,
    mode: 'fixed',
    stages: [
      stage('requirements', 'Requisitos', 'requirements-agent', [], 'approved'),
      stage('architecture', 'Arquitetura', 'architect-agent', ['requirements'], 'approved'),
      stage('implementation', 'Implementação', 'developer-agent', ['architecture'], 'approved'),
      stage('qa', 'QA', 'qa-agent', ['implementation'], 'approved'),
      stage('security', 'Segurança', 'security-agent', ['qa'], security),
      stage('release', 'Release', 'orchestrator', ['security'], release),
    ],
  }
}
