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
  history: Json[]
  artifacts: Partial<
    Record<string, { current: Json; versions: Json[]; byVersion: Partial<Record<number, Json>> }>
  >
  streams: StreamScript[]
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
  devSignIn = true
  attachmentLimit = 1024 * 1024
  providers: ProviderState[] = [
    { id: 'anthropic', name: 'Anthropic', key: { status: 'missing', last_test: null } },
    { id: 'openai', name: 'OpenAI', key: { status: 'missing', last_test: null } },
  ]
  models: ModelState[] = []
  prices: Record<string, Json[]> = {}
  projects: Partial<Record<string, ProjectState>> = {}
  commands: Record<string, Json> = {}
  pendingItems: Json[] = []
  /** Requests seen, for assertions (method, path, headers, body). */
  readonly requests: Array<{
    method: string
    path: string
    headers: Record<string, string>
    body: unknown
  }> = []
  private nextId = 1

  async install(page: Page): Promise<void> {
    await page.route('**/config.js', (route) =>
      route.fulfill({
        contentType: 'application/javascript',
        body: `window.__EASY_CODE_CONFIG__ = { apiBaseUrl: "${API}", devSignIn: ${String(this.devSignIn)} }`,
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

  // ------------------------------------------------------------------ seeding helpers

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
      history: [],
      artifacts: {},
      streams: [],
      ...state,
    }
    this.projects[id] = full
    return full
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

    if (method === 'POST' && path === '/dev/session') {
      this.signedIn = true
      return route.fulfill({ status: 204 })
    }
    if (!this.signedIn) return this.json(route, 401, problem('not-authenticated', 401))

    const segments = path.split('/').filter(Boolean)
    const response = this.dispatch(method, segments, body, url)
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
  ): { status: number; body?: unknown; headers?: Record<string, string> } | { sse: string } {
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
          organization: { id: 'org-1', name: 'Organização de desenvolvimento' },
          attachment_limit_bytes: this.attachmentLimit,
          csrf_token: 'csrf-dev',
        },
      }
    }
    if (first === 'session' && second === 'logout') {
      this.signedIn = false
      return { status: 204 }
    }
    if (first === 'pending-items') {
      return {
        status: 200,
        body: { items: this.pendingItems, next_cursor: null, total: this.pendingItems.length },
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

    // Projects (US-03, US-05, US-16).
    if (first === 'projects' && !second) {
      if (method === 'POST') return this.createProject(input)
      const items = Object.values(this.projects).flatMap((state) => (state ? [state.project] : []))
      const counts: Record<string, number> = {}
      for (const item of items) counts[String(item.state)] = (counts[String(item.state)] ?? 0) + 1
      const filter = url.searchParams.getAll('state')
      return {
        status: 200,
        body: {
          items: filter.length
            ? items.filter((item) => filter.includes(String(item.state)))
            : items,
          next_cursor: null,
          count_by_state: counts,
        },
      }
    }
    const state = second ? this.projects[second] : undefined
    if (first === 'projects' && second && !state)
      return { status: 404, body: problem('not-found', 404) }
    if (!state) return { status: 404, body: problem('not-found', 404) }
    if (!third) return { status: 200, body: state.project }
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
        const change = input
        const pair = change.model_pair as { execution?: { model?: string } } | undefined
        const model = this.models.find((item) => item.model === pair?.execution?.model)
        if (model && model.capabilities.tool_use === false) {
          return {
            status: 409,
            body: problem('model-without-capability', 409, { missing_capacity: 'tool_use' }),
          }
        }
        const changes = (change.pair_by_stage as Json | undefined) ?? {}
        const current = (state.configuration.pair_by_stage as Json | undefined) ?? {}
        const pairByStage = Object.fromEntries(
          Object.entries({ ...current, ...changes }).filter(([, value]) => value !== null),
        )
        state.configuration = { ...state.configuration, ...change, pair_by_stage: pairByStage }
        state.etag += 1
      }
      return {
        status: 200,
        body: state.configuration,
        headers: { ETag: `"${String(state.etag)}"` },
      }
    }
    if (third === 'start') return this.startWorkflow(state)
    if (third === 'commands' && fourth) return { status: 200, body: this.commands[fourth] }
    if (third === 'workflow') return { status: 200, body: state.workflow }
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
    if (third === 'waves' && !fourth)
      return { status: 200, body: { items: state.waves, next_cursor: null } }
    if (third === 'waves' && fourth) {
      const wave = state.waveDetails[fourth]
      if (!wave) return { status: 404, body: problem('not-found', 404) }
      if (!fifth) return { status: 200, body: wave }
      if (fifth === 'calls')
        return { status: 200, body: { items: state.calls[fourth] ?? [], next_cursor: null } }
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
          items: Object.values(state.artifacts).flatMap((item) => (item ? [item.current] : [])),
          next_cursor: null,
        },
      }
    }
    if (third === 'artifacts' && fourth) {
      const artifact = state.artifacts[fourth]
      if (!artifact) return { status: 404, body: problem('not-found', 404) }
      if (!fifth) return { status: 200, body: artifact.current }
      if (fifth === 'versions' && !sixth) return { status: 200, body: { items: artifact.versions } }
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
    return { status: 201, body: state.project }
  }

  /** US-05: refused with the missing list without a valid key or pair; otherwise the first wave is queued. */
  private startWorkflow(state: ProjectState): { status: number; body: unknown } {
    const projectId = String(state.project.id)
    const missing: string[] = []
    if (!this.providers.some((provider) => provider.key.status === 'active'))
      missing.push('provider_key')
    if (!state.configuration.model_pair) missing.push('model_pair')
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
