import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import {
  FakeApi,
  gateRound,
  phaseOneWorkflow,
  waveSummary,
  type ProjectState,
} from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const SHA = '9c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d'
const LOAD_TIMEOUT = 30_000
const CLONE_URL = `https://easycode.empresa.local/git/${PROJECT_ID}.git`

function artifact(id: string, type: string, title: string, header: Record<string, unknown> = {}) {
  const current = {
    id,
    type,
    title,
    state: 'approved',
    version: 1,
    stage: type === 'US' ? 'requirements' : 'architecture',
    author: { type: 'agent', agent: type === 'US' ? 'requirements-agent' : 'architect-agent' },
    wave: type === 'US' ? 'REQ-01' : 'ARQ-01',
    header: { id, tipo: type, ...header },
    situation: type === 'T' ? (header.situacao ?? null) : null,
    body: '',
    commit: SHA,
  }
  return { current, versions: [], byVersion: { 1: current } }
}

function wave(id: string, stage: string, agent: string, state: string, task?: string) {
  return { ...waveSummary(id, state), stage, agent, ...(task ? { task } : {}) }
}

function workflow() {
  const base = phaseOneWorkflow('started') as { stages: Array<Record<string, unknown>> }
  const states: Record<string, string> = {
    requirements: 'approved',
    architecture: 'approved',
    implementation: 'running',
  }
  return {
    ...base,
    stages: base.stages.map((stage) => ({ ...stage, state: states[String(stage.id)] })),
  }
}

function seed(api: FakeApi, extra: Partial<ProjectState> = {}): ProjectState {
  return api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: ['lead', 'approver'],
      available_actions: ['pause', 'cancel'],
      last_sequence: 3,
    },
    workflow: workflow(),
    waves: [
      wave('ARQ-01', 'architecture', 'architect-agent', 'completed'),
      wave('DEV-01', 'implementation', 'developer-agent', 'completed', 'T-01'),
      wave('DEV-02', 'implementation', 'developer-agent', 'running', 'T-02'),
    ],
    artifacts: {
      'US-03': artifact('US-03', 'US', 'Reservar sala'),
      'ADR-01': artifact('ADR-01', 'ADR', 'Go com PostgreSQL'),
      'T-01': artifact('T-01', 'T', 'Esqueleto da API', {
        natureza: 'esqueleto',
        tamanho: 'p',
        segue: ['ADR-01'],
        depende_de: [],
        escopo_arquivos: ['backend/'],
        situacao: 'concluida',
        registro_dev: { commits: [SHA], testes: ['backend/health_test.go'], desvios: 'nenhum' },
      }),
      'T-02': artifact('T-02', 'T', 'Reserva de sala', {
        tamanho: 'm',
        cobre: [{ us: 'US-03', criterios: ['CA-1', 'CA-2'] }],
        segue: ['ADR-01'],
        depende_de: ['T-01'],
        escopo_arquivos: ['backend/booking/'],
        situacao: 'em_andamento',
      }),
    },
    repository: {
      status: 'available',
      clone_url: CLONE_URL,
      main_branch: 'main',
      last_commit: { sha: SHA, when: '2026-11-22T10:30:00-03:00', wave: 'DEV-01' },
      marks: [],
    },
    ...extra,
  })
}

async function stubClipboard(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const copied: string[] = []
    Object.defineProperty(window, '__copied', { value: copied })
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: (text: string) => Promise.resolve(void copied.push(text)) },
    })
  })
}

test('US-19 CA-1, US-41, US-42 CA-1: the implementation stage shows each task, its waves and evidence, and the clone address', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api)
  await api.install(page)
  await stubClipboard(page)

  await page.goto(`/projects/${PROJECT_ID}/stages/implementation`)
  const plan = page.getByTestId('task-plan')
  await expect(page.getByTestId('task-T-02')).toBeVisible({ timeout: LOAD_TIMEOUT })
  // Plan order: T-01 before T-02, which depends on it; the running task opens selected.
  await expect(plan.getByRole('listitem').first()).toContainText('T-01')
  await expect(page.getByTestId('task-T-01')).toContainText('Concluída')
  await expect(page.getByTestId('task-T-02')).toHaveAttribute('aria-current', 'true')
  await expect(page.getByTestId('task-detail')).toContainText('Em andamento')
  await expect(page.getByTestId('task-waves')).toContainText('DEV-02')
  await expect(page.getByTestId('task-waves')).not.toContainText('DEV-01')
  // The list comes from the summaries (`situation`); only the selected T is read in full.
  const taskReads = () =>
    api.requests.filter((request) => /\/artifacts\/T-\d+$/.test(request.path)).length
  expect(taskReads()).toBe(1)

  // CA-14-2, CA-14-24: the done task shows its commit, tests, deviations and only its waves.
  await page.getByTestId('task-T-01').click()
  await expect(page.getByTestId('task-T-01')).toHaveAttribute('aria-current', 'true')
  await expect(page.getByTestId('task-commits')).toHaveText(SHA.slice(0, 12))
  await expect(page.getByTestId('task-deviations')).toHaveText('nenhum')
  await expect(page.getByTestId('task-waves')).toContainText('DEV-01')
  await expect(page.getByTestId('task-waves')).not.toContainText('DEV-02')
  await expect(page.getByTestId('task-evidence-link')).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/waves/DEV-01#evidence`,
  )
  await expect(page.getByTestId('stage-evidence-DEV-02')).toBeVisible()

  // US-42: the platform clone address, copyable, with how to authenticate the clone (PT-37).
  const repository = page.getByTestId('repository-panel')
  await expect(repository.getByRole('textbox', { name: 'Endereço de clone' })).toHaveValue(
    CLONE_URL,
  )
  await expect(repository).toContainText('token de acesso do cliente local')
  await expect(page.getByTestId('repository-last-commit')).toContainText(SHA.slice(0, 12))
  await expectNoSeriousA11yViolations(page)

  await repository.getByRole('button', { name: 'Copiar endereço de clone' }).click()
  await expect(repository.getByRole('status')).toHaveText('Endereço de clone copiado.')
  expect(await page.evaluate(() => (window as unknown as { __copied: string[] }).__copied)).toEqual(
    [CLONE_URL],
  )
})

test('US-18 CA-1: the architecture stage shows the task plan with what each task covers and follows', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api)
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/stages/architecture`)
  // The plan comes after the stage, the T list and each T: three round trips on a loaded CI host.
  await expect(page.getByTestId('task-T-01')).toBeVisible({ timeout: LOAD_TIMEOUT })
  await page.getByTestId('task-T-02').click()
  const detail = page.getByTestId('task-detail')
  await expect(detail).toContainText('tamanho média')
  await expect(detail.getByRole('link', { name: 'US-03' })).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/artifacts/US-03`,
  )
  await expect(detail).toContainText('(CA-1, CA-2)')
  await expect(detail.getByRole('link', { name: 'ADR-01' })).toBeVisible()
  await expect(detail).toContainText('T-01')
  // The architecture stage plans; the execution blocks belong to the implementation stage.
  await expect(page.getByTestId('task-waves')).toHaveCount(0)
  await expect(page.getByTestId('repository-panel')).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)
})

test('US-42: the project overview shows the clone address, or says the repository is not there yet', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  const state = seed(api)
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}`)
  await expect(
    page.getByTestId('repository-panel').getByRole('textbox', { name: 'Endereço de clone' }),
  ).toHaveValue(CLONE_URL)
  await expectNoSeriousA11yViolations(page)

  state.repository = { ...state.repository, status: 'not_created', last_commit: null }
  await page.reload()
  await expect(page.getByTestId('repository-unavailable')).toHaveText(
    'O repositório do projeto ainda não está disponível.',
  )
})

test('US-41: a gate check whose evidence is a wave links to the build and test evidence', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  const state = seed(api)
  state.gates['gate.implementation'] = [
    gateRound('gate.implementation', 'implementation', 1, {
      outputs: [],
      checks: [
        {
          check: 'code.build_green',
          name: 'Build verde',
          type: 'automatic',
          failure_severity: 'blocks',
          status: 'passed',
          evidence: [{ reference: 'DEV-01', message: 'build verde no sandbox' }],
        },
      ],
    }),
  ]
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/gates/gate.implementation/rounds/1`)
  const link = page.getByRole('link', { name: 'Evidência de build e testes da onda DEV-01' })
  await expect(link).toHaveAttribute('href', `/projects/${PROJECT_ID}/waves/DEV-01#evidence`)
  await expectNoSeriousA11yViolations(page)
})
