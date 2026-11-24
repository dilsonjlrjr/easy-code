import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useInstallerStore } from '../installerStore'

type Handler = (
  body: unknown,
  headers: Record<string, string>,
) => { status: number; body?: unknown }

function stubApi(routes: Record<string, Handler>) {
  const calls: Array<{ route: string; body: unknown; headers: Record<string, string> }> = []
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string, init: RequestInit) => {
      const route = `${init.method ?? 'GET'} ${path}`
      const body: unknown = typeof init.body === 'string' ? JSON.parse(init.body) : undefined
      const headers = (init.headers ?? {}) as Record<string, string>
      calls.push({ route, body, headers })
      const handler = routes[route]
      const answer = handler ? handler(body, headers) : { status: 404, body: { code: 'not_found' } }
      return Promise.resolve(
        new Response(answer.status === 204 ? null : JSON.stringify(answer.body ?? {}), {
          status: answer.status,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    }),
  )
  return calls
}

const defaults = {
  bundled: true,
  database: {
    host: 'postgres',
    port: 5432,
    admin_user: 'easycode',
    admin_password: 'pw',
    name: 'easycode',
    ssl_mode: 'disable',
  },
  sso: {
    public_url: 'http://localhost:8180/auth',
    internal_url: 'http://keycloak:8080/auth',
    mode: 'new_realm',
    realm: 'easycode',
    admin_user: 'admin',
    admin_password: 'kc',
  },
  organization: {
    name: '',
    currency: 'BRL',
    public_origin: 'http://localhost:8080',
    locale: 'pt-BR',
  },
}

const welcomeRoutes: Record<string, Handler> = {
  'POST /api/token': (_b, h) =>
    h['X-Install-Token'] === 'tok'
      ? { status: 204 }
      : { status: 401, body: { code: 'invalid_token' } },
  'GET /api/defaults': () => ({ status: 200, body: defaults }),
  'POST /api/check/environment': () => ({
    status: 200,
    body: { checks: [{ name: 'config_volume', ok: true }], version: 'v1' },
  }),
  'GET /api/skills/native': () => ({
    status: 200,
    body: { skills: [{ name: 'go', description: 'Go' }] },
  }),
}

describe('installer store', () => {
  beforeEach(() => setActivePinia(createPinia()))
  afterEach(() => vi.unstubAllGlobals())

  it('refuses a wrong token and prefills the steps with the bundled defaults', async () => {
    stubApi(welcomeRoutes)
    const store = useInstallerStore()
    store.token = 'wrong'
    expect(await store.submitToken()).toBe(false)
    expect(store.fieldProblem('token')?.reason).toBe('invalid_token')
    expect(store.passed('welcome')).toBe(false)

    store.token = ' tok '
    expect(await store.submitToken()).toBe(true)
    expect(store.passed('welcome')).toBe(true)
    expect(store.database.host).toBe('postgres')
    expect(store.sso.internal_url).toBe('http://keycloak:8080/auth')
    expect(store.nativeSkills).toHaveLength(1)
    store.next()
    expect(store.current).toBe('database')
  })

  it('asks for a new test when the tested data changes', async () => {
    stubApi({
      ...welcomeRoutes,
      'POST /api/check/database': () => ({
        status: 200,
        body: {
          server_version: '17.6',
          can_create: true,
          exists: false,
          has_platform_tables: false,
        },
      }),
    })
    const store = useInstallerStore()
    store.token = 'tok'
    await store.submitToken()
    expect(await store.testDatabase()).toBe(true)
    expect(store.passed('database')).toBe(true)
    store.database.name = 'other'
    expect(store.passed('database')).toBe(false)
    expect(store.stale('database')).toBe(true)
  })

  it('shows the field problems of a refused check', async () => {
    stubApi({
      ...welcomeRoutes,
      'POST /api/check/database': () => ({
        status: 422,
        body: {
          code: 'invalid',
          errors: [{ field: 'database.name', reason: 'has_platform_tables' }],
          found: { exists: true, has_platform_tables: true },
        },
      }),
    })
    const store = useInstallerStore()
    store.token = 'tok'
    await store.submitToken()
    expect(await store.testDatabase()).toBe(false)
    expect(store.fieldProblem('database.name')?.reason).toBe('has_platform_tables')
    expect(store.databaseProbe?.has_platform_tables).toBe(true)
  })

  it('builds the plan with the skipped user and only the accepted skills', () => {
    stubApi(welcomeRoutes)
    const store = useInstallerStore()
    Object.assign(store.administrator, {
      username: 'ana',
      email: 'ana@example.com',
      first_name: 'Ana',
      password: 'temporary-password',
    })
    store.userSkipped = true
    store.skills = [
      { file_name: 'ok.zip', content: 'eg==', summary: { name: 'padrao', description: 'x' } },
      { file_name: 'bad.zip', content: 'eg==', problem: 'not_a_zip' },
    ]
    expect(store.plan.user).toBeNull()
    expect(store.plan.administrator.username).toBe('ana')
    expect(store.plan.skills.map((s) => s.file_name)).toEqual(['ok.zip'])
    expect(store.passed('user')).toBe(true)
    expect(store.passed('skills')).toBe(false)
  })

  it('sends the token and follows the steps until the lock', async () => {
    let polls = 0
    const calls = stubApi({
      ...welcomeRoutes,
      'POST /api/install': () => ({
        status: 200,
        body: { installed: false, running: true, steps: [] },
      }),
      'GET /api/install': () => {
        polls++
        return {
          status: 200,
          body: {
            installed: polls > 1,
            running: polls <= 1,
            steps: [{ name: 'lock', status: polls > 1 ? 'done' : 'running' }],
          },
        }
      },
    })
    vi.useFakeTimers()
    const store = useInstallerStore()
    store.token = 'tok'
    await store.submitToken()
    await store.install()
    expect(store.current).toBe('install')
    await vi.advanceTimersByTimeAsync(1100)
    expect(store.current).toBe('done')
    expect(
      calls.filter((c) => c.route === 'POST /api/install')[0]?.headers['X-Install-Token'],
    ).toBe('tok')
    vi.useRealTimers()
  })
})
