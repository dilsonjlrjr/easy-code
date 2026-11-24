import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import {
  domainEvent,
  FakeApi,
  phaseOneWorkflow,
  sseFrame,
  waveDetail,
  waveSummary,
} from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a30'

function apiWithRunningProject(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  const state = api.addProject({
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
      last_sequence: 5,
    },
    workflow: phaseOneWorkflow('started'),
    waves: [
      waveSummary('REQ-01', 'completed'),
      { ...waveSummary('REQ-02', 'running'), purpose: 'rework' },
    ],
  })
  state.waveDetails['REQ-01'] = waveDetail('REQ-01', 'completed')
  state.stageDetails.requirements = {
    tokens: { input: 12000, output: 3400, read_cache: 0, write_cache: 0 },
    detailed_cost: {
      useful: { value: '1.20', currency: 'BRL' },
      lost: { value: '0', currency: 'BRL' },
      total: { value: '1.20', currency: 'BRL' },
    },
    time: { execution_ms: 125000, cycle_ms: 600000 },
  }
  return api
}

test('US-49 CA-1 / CA-2: the workflow lists the stages in order with their state; a stage opens its detail', async ({
  page,
}) => {
  const api = apiWithRunningProject()
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}`)
  const stages = page.locator('ol.pipeline > li')
  await expect(stages).toHaveCount(3)
  await expect(stages.nth(0)).toContainText('Etapa 1')
  await expect(stages.nth(0)).toContainText('Requisitos')
  await expect(stages.nth(0)).toContainText('Em execução')
  await expect(stages.nth(1)).toContainText('Arquitetura')
  await expect(stages.nth(1)).toContainText('Bloqueada')
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('link', { name: 'Requisitos', exact: true }).click()

  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/stages/requirements$`))
  await expect(page.getByRole('heading', { level: 2, name: 'Etapa Requisitos' })).toBeVisible()
  await expect(page.getByText('Agente de Requisitos')).toBeVisible()
  const summary = page.getByTestId('stage-summary')
  await expect(summary).toContainText('humana, por um aprovador do projeto')
  await expect(summary).toContainText('12.000 de entrada')
  await expect(summary).toContainText('R$')
  // Rework waves are listed with their purpose.
  await expect(page.getByTestId('stage-waves')).toContainText('REQ-02 · retrabalho')
  await expectNoSeriousA11yViolations(page)
})

test('US-49 CA-3: a stage changing state appears without reloading the page', async ({ page }) => {
  const api = apiWithRunningProject()
  const state = api.project(PROJECT_ID)
  const event = domainEvent(PROJECT_ID, 6, 'stage.submitted_to_gate', { stage: 'requirements' })
  state.streams.push({
    body: `retry: 500\n\n${sseFrame(event)}`,
    onServe: () => {
      const stages = state.workflow.stages as Array<Record<string, unknown>>
      stages[0].state = 'awaiting_gate'
      state.project = { ...state.project, last_sequence: 6 }
    },
  })
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/stages/requirements`)

  await expect(page.getByRole('heading', { level: 2, name: 'Etapa Requisitos' })).toBeVisible()
  await expect(page.getByTestId('stage-summary').locator('..')).toContainText('Aguardando gate')
  // The stage header no longer says running; its rework wave keeps its own state in the waves card.
  await expect(
    page.locator('.state-label[data-machine="stage"][data-state="running"]'),
  ).toHaveCount(0)
})
