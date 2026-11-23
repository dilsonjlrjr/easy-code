import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import {
  FakeApi,
  domainEvent,
  phaseOneWorkflow,
  problem,
  sseFrame,
  waveSummary,
  type ProjectState,
} from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const BRL = (value: string) => ({ value, currency: 'BRL' })
const ZERO_TOKENS = { input: 0, output: 0, read_cache: 0, write_cache: 0 }

function cost(useful: string, rework: string, lost: string, total: string, extra = {}) {
  return { useful: BRL(useful), rework: BRL(rework), lost: BRL(lost), total: BRL(total), ...extra }
}

function group(key: string, total: string, rework: string, waves: number, time = [0, 0]) {
  return {
    key,
    cost: cost(total, rework, '0', total),
    tokens: { input: 1000 * waves, output: 200 * waves, read_cache: 0, write_cache: 0 },
    time: { cycle_ms: time[0] ?? 0, execution_ms: time[1] ?? 0 },
    waves,
  }
}

const HOUR = 3_600_000
const BUDGET = { stop_limit: BRL('100.00'), balance: BRL('15.00'), used_percentage: 85 }

function byStage() {
  return {
    grouped_by: 'stage',
    total: cost('85.00', '20.00', '1.50', '86.50', {
      estimated_lost: BRL('0.50'),
      per_provider: [
        { provider: 'anthropic', total: BRL('60.00') },
        { provider: 'openai', total: BRL('26.50') },
      ],
    }),
    tokens: { input: 5000, output: 1000, read_cache: 300, write_cache: 40 },
    time: { cycle_ms: 30 * HOUR, execution_ms: 2 * HOUR },
    budget: BUDGET,
    groups: [
      group('requirements', '50.00', '20.00', 3, [26 * HOUR, HOUR]),
      group('architecture', '36.50', '0', 2, [4 * HOUR, HOUR]),
    ],
  }
}

function byAgent() {
  return {
    ...byStage(),
    grouped_by: 'agent',
    groups: [
      group('requirements-agent', '50.00', '20.00', 3),
      group('architect-agent', '36.50', '0', 2),
    ],
  }
}

function byModel() {
  return {
    ...byStage(),
    grouped_by: 'model',
    groups: [
      group('anthropic/model-a', '60.00', '20.00', 4),
      group('openai/model-b', '26.50', '0', 1),
    ],
  }
}

function seed(api: FakeApi, overrides: Partial<ProjectState> = {}, project = {}): ProjectState {
  const workflow = phaseOneWorkflow('started')
  const stages = workflow.stages as Array<Record<string, unknown>>
  if (stages[0]) stages[0].state = 'approved'
  if (stages[1]) stages[1].state = 'ready'
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
      summary: {
        approved_stages: 1,
        stages_total: 3,
        gates_awaiting: 0,
        open_questions: 0,
        accumulated_cost: BRL('85.00'),
        stop_limit: BRL('100.00'),
      },
      last_sequence: 3,
      ...project,
    },
    configuration: {
      model_pair: null,
      gate_approval: {},
      budget: { stop_limit: BRL('100.00'), percentage_alert_limits: [50, 80] },
      artifact_language: 'pt-BR',
    },
    workflow,
    waves: [
      { ...waveSummary('REQ-01', 'completed'), cost_total: BRL('30.00') },
      {
        ...waveSummary('REQ-02', 'completed'),
        purpose: 'rework',
        finding: 'ACH-01',
        cost_total: BRL('20.00'),
      },
      {
        ...waveSummary('ARQ-01', 'completed'),
        stage: 'architecture',
        agent: 'architect-agent',
      },
    ],
    costsByGrouping: { stage: byStage(), agent: byAgent(), model: byModel() },
    ...overrides,
  })
}

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
}

async function openCosts(page: Page, query = ''): Promise<void> {
  await page.goto(`/projects/${PROJECT_ID}/costs${query}`)
  await expect(page.getByTestId('costs-groups')).toBeVisible({ timeout: 30_000 })
}

test('US-38 CA-1/CA-2/CA-3: totals by stage, agent and model, rework apart, cycle and execution side by side', async ({
  page,
}) => {
  const api = signedIn()
  seed(api)
  await api.install(page)
  await openCosts(page)

  const table = page.getByTestId('costs-groups')
  // CA-25-1: one row per stage and the total row with the project total.
  await expect(page.getByTestId('costs-group-requirements')).toContainText('Requisitos')
  await expect(page.getByTestId('costs-group-architecture')).toContainText('Arquitetura')
  await expect(page.getByTestId('costs-total-row')).toContainText('86,50')
  // CA-2: rework in a column of its own.
  await expect(table.getByRole('columnheader', { name: 'Retrabalho' })).toBeVisible()
  await expect(page.getByTestId('costs-group-requirements')).toContainText('20,00')
  await expect(page.getByTestId('costs-total-rework')).toContainText('20,00')
  // CA-3: cycle time (with human wait) and execution time side by side, per stage.
  await expect(
    table.getByRole('columnheader', { name: 'Tempo de ciclo (com espera humana)' }),
  ).toBeVisible()
  await expect(table.getByRole('columnheader', { name: 'Tempo de execução' })).toBeVisible()
  await expect(page.getByTestId('costs-group-requirements')).toContainText(/1\s?dia/)
  // CA-25-4 / CA-25-5 / CA-25-8: per provider, lost cost with its estimated part.
  await expect(page.getByTestId('costs-providers')).toContainText('anthropic')
  await expect(page.getByTestId('costs-providers')).toContainText('openai')
  await expect(page.getByTestId('costs-total-lost')).toContainText('1,50')
  await expect(page.getByTestId('costs-estimated-lost')).toContainText('0,50 estimado')
  // CA-25-6: 85% used is past the 80% alert, in text.
  await expect(page.getByTestId('costs-budget-use')).toContainText('85%')
  await expect(page.getByTestId('costs-budget-alert')).toContainText('80%')
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('button', { name: 'Agente', exact: true }).click()
  await expect(page).toHaveURL(/group=agent/)
  await expect(page.getByTestId('costs-group-requirements-agent')).toContainText(
    'Agente de Requisitos',
  )
  await page.getByRole('button', { name: 'Modelo', exact: true }).click()
  await expect(page.getByTestId('costs-group-anthropic/model-a')).toBeVisible()
  await expect(page.getByTestId('costs-total-row')).toContainText('86,50')
  expect(api.requests.some((request) => request.path.includes('group_by=model'))).toBe(true)
})

test('CA-25-7: a stage row lists the waves of that stage, each to its wave page', async ({
  page,
}) => {
  const api = signedIn()
  seed(api)
  await api.install(page)
  await openCosts(page)

  await page.getByTestId('costs-group-requirements').getByRole('button').click()
  const waves = page.getByTestId('costs-group-waves')
  await expect(waves).toContainText('REQ-01')
  await expect(waves).toContainText('REQ-02')
  await expect(waves).not.toContainText('ARQ-01')
  await expectNoSeriousA11yViolations(page)
})

test('ESP-25 6: no wave yet and no budget', async ({ page }) => {
  const api = signedIn()
  seed(api, {
    waves: [],
    costsByGrouping: {
      stage: {
        grouped_by: 'stage',
        total: cost('0', '0', '0', '0'),
        tokens: ZERO_TOKENS,
        time: { cycle_ms: 0, execution_ms: 0 },
        budget: null,
        groups: [],
      },
    },
  })
  await api.install(page)
  await openCosts(page)
  await expect(page.getByTestId('costs-no-waves')).toBeVisible()
  await expect(page.getByTestId('costs-budget')).toContainText('Sem orçamento definido.')
})

test('US-40 CA-1/CA-3: the stage shows the estimate with its basis and estimated × performed', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, {
    estimates: {
      requirements: {
        stage: 'requirements',
        estimated: BRL('12.40'),
        no_history: false,
        basis: {
          input_size_tokens: 48210,
          price_table_version: '2026-11-01',
          input_price_per_million: BRL('15.00'),
          factor: '1.04',
          method: 'input_times_price_times_history',
          historical_average: BRL('11.90'),
          considered_waves: 3,
        },
        performed: BRL('13.10'),
      },
      architecture: {
        stage: 'architecture',
        estimated: BRL('8.00'),
        no_history: true,
        basis: { input_size_tokens: 12000, price_table_version: '2026-11-01' },
        performed: null,
      },
    },
  })
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/stages/requirements`)
  const estimate = page.getByTestId('stage-estimate')
  await expect(estimate).toContainText('12,40', { timeout: 30_000 })
  await expect(page.getByTestId('estimate-basis')).toContainText('48.210 tokens')
  await expect(page.getByTestId('estimate-basis')).toContainText('versão 2026-11-01')
  await expect(page.getByTestId('estimate-basis')).toContainText('11,90')
  await expect(page.getByTestId('estimate-basis')).toContainText('3 ondas consideradas')
  await expect(page.getByTestId('estimate-basis')).toContainText('15,00 por milhão de tokens')
  await expect(page.getByTestId('estimate-no-history')).toHaveCount(0)
  // CA-3: estimated × performed side by side.
  const performed = page.getByTestId('estimate-performed')
  await expect(performed.getByRole('columnheader', { name: 'Estimado' })).toBeVisible()
  await expect(performed.getByRole('columnheader', { name: 'Realizado' })).toBeVisible()
  await expect(performed).toContainText('13,10')
  await expectNoSeriousA11yViolations(page)

  // CA-2: a stage without history is marked and uses only size × price.
  await page.goto(`/projects/${PROJECT_ID}/stages/architecture`)
  await expect(page.getByTestId('estimate-no-history')).toContainText('sem histórico', {
    timeout: 30_000,
  })
  await expect(page.getByTestId('stage-estimate')).toContainText(
    'cálculo só com tamanho da entrada × preço vigente',
  )
  await expect(page.getByTestId('estimate-basis')).not.toContainText('Média histórica')
  await expect(page.getByTestId('estimate-performed')).toHaveCount(0)
})

test('US-40 / US-39: an estimate above the balance says the project would pause by budget', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, {
    estimates: {
      architecture: {
        stage: 'architecture',
        estimated: BRL('22.00'),
        no_history: true,
        basis: { input_size_tokens: 12000, price_table_version: '2026-11-01' },
        performed: null,
      },
    },
  })
  await api.install(page)

  // ESP-06 3.6: the project page shows the next stage (ready) with its estimate.
  await page.goto(`/projects/${PROJECT_ID}`)
  const next = page.getByTestId('next-stage')
  await expect(next).toContainText('Próxima etapa: Arquitetura', { timeout: 30_000 })
  await expect(next.getByTestId('estimate-exceeds-balance')).toContainText('15,00')
  await expect(page.getByTestId('cost-summary-rework')).toContainText('1 ondas de retrabalho')
  await expect(page.getByTestId('cost-summary-rework')).toContainText('ACH-01')
  await expect(page.getByTestId('cost-summary-cycle')).toBeVisible()
  await expectNoSeriousA11yViolations(page)
  await page.getByTestId('cost-summary-details').click()
  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/costs`))
})

test('US-40 ER-07-16: a model without a current price shows its message', async ({ page }) => {
  const api = signedIn()
  seed(api, {
    estimates: {
      requirements: { problem: problem('model-without-current-price', 409) },
    },
  })
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/stages/requirements`)
  await expect(page.getByTestId('stage-estimate')).toContainText(
    'O modelo não tem preço vigente na tabela de preços.',
    { timeout: 30_000 },
  )
})

test('US-39 estimate_exceeds_balance: the pause by budget says the estimate passed the balance', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api)
  state.streams.push({
    body: sseFrame(
      domainEvent(PROJECT_ID, 4, 'project.budget_exhausted', {
        project: PROJECT_ID,
        transition: { from_state: 'in_progress', to_state: 'paused' },
        actor: { type: 'orchestrator', id: 'orchestrator' },
        spend: BRL('85.00'),
        stop_limit: BRL('100.00'),
        cause: 'estimate_exceeds_balance',
        next_wave_estimate: BRL('22.00'),
      }),
    ),
    onServe: () => {
      state.project.state = 'paused'
      state.project.pause_reason = 'budget'
      state.project.available_actions = ['resume', 'cancel']
    },
  })
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}`)

  const paused = page.getByTestId('budget-paused')
  await expect(paused).toBeVisible({ timeout: 30_000 })
  await expect(page.getByTestId('budget-paused-estimate')).toContainText('22,00')
  await expect(page.getByTestId('budget-paused-estimate')).toContainText('15,00')
})

test('ESP-06 6 / US-39 CA-1: paused shows who and when; the last budget alert stays after a reload', async ({
  page,
}) => {
  const api = signedIn()
  seed(
    api,
    {},
    {
      state: 'paused',
      pause_reason: 'manual',
      available_actions: ['resume', 'cancel'],
      pause: {
        reason: 'manual',
        by: { type: 'human', id: 'ana', user: { id: 'ana', name: 'Ana Souza' } },
        at: '2026-11-22T14:30:00-03:00',
        origin: 'web',
      },
    },
  )
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}`)
  await expect(page.getByTestId('paused-by')).toContainText('Pausado por Ana Souza em 22/11/2026', {
    timeout: 30_000,
  })

  const project = api.project(PROJECT_ID).project
  project.state = 'in_progress'
  project.pause = null
  project.pause_reason = null
  ;(project.summary as Record<string, unknown>).last_budget_alert_percentage = 80
  await page.reload()
  await expect(page.getByTestId('budget-alert')).toContainText('80%', { timeout: 30_000 })
  await expect(page.getByTestId('paused-by')).toHaveCount(0)
})

test('US-39 estimate_exceeds_balance: after a reload the cause comes from the pause record', async ({
  page,
}) => {
  const api = signedIn()
  seed(
    api,
    {},
    {
      state: 'paused',
      pause_reason: 'budget',
      available_actions: ['resume', 'cancel'],
      pause: {
        reason: 'budget',
        by: { type: 'orchestrator', id: 'orchestrator' },
        at: '2026-11-22T14:30:00-03:00',
        origin: null,
        cause: 'estimate_exceeds_balance',
        next_wave_estimate: BRL('22.00'),
      },
    },
  )
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}`)
  await expect(page.getByTestId('budget-paused-estimate')).toContainText('22,00', {
    timeout: 30_000,
  })
  await expect(page.getByTestId('budget-paused-estimate')).toContainText('15,00')
  await expect(page.getByTestId('paused-by')).toContainText('Orquestrador')
})
