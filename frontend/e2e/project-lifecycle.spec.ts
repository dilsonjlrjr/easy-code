import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import {
  domainEvent,
  FakeApi,
  finding,
  releaseWorkflow,
  sseFrame,
  waveSummary,
  type ProjectState,
} from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const NAME = 'Reserva de salas'
const BRL = (value: string) => ({ value, currency: 'BRL' })

function seed(
  api: FakeApi,
  project: Record<string, unknown> = {},
  release = 'approved',
): ProjectState {
  return api.addProject({
    project: {
      id: PROJECT_ID,
      name: NAME,
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: ['lead', 'approver'],
      available_actions: ['pause', 'cancel'],
      summary: {
        approved_stages: 6,
        stages_total: 6,
        gates_awaiting: 0,
        open_questions: 0,
        accumulated_cost: BRL('42.00'),
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
    workflow: releaseWorkflow(release),
  })
}

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
}

function actions(page: Page) {
  return page.getByTestId('project-actions')
}

async function openProject(page: Page): Promise<void> {
  await page.goto(`/projects/${PROJECT_ID}`)
  await expect(page.getByRole('heading', { level: 1, name: NAME })).toBeVisible()
}

test('US-51 CA-1: the lead pauses the project after a simple confirmation', async ({ page }) => {
  const api = signedIn()
  seed(api)
  await api.install(page)
  await openProject(page)

  await expect(page.getByTestId('budget-use')).toContainText('42%')
  const open = actions(page).getByRole('button', { name: 'Pausar projeto' })
  await open.click()
  const dialog = page.getByRole('dialog', { name: `Pausar o projeto ${NAME}?` })
  await expect(dialog).toContainText('próximo checkpoint')
  await expect(dialog.getByRole('button', { name: 'Voltar' })).toBeFocused()
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Pausar o projeto' }).click()

  await expect(page.getByTestId('project-action-notice')).toContainText('Projeto pausado.')
  await expect(page.locator('.state-label').first()).toContainText('Pausado (manual)')
  await expect(actions(page).getByRole('button', { name: 'Retomar projeto' })).toBeVisible()
  const request = api.requests.find((item) => item.path === `/projects/${PROJECT_ID}/pause`)
  expect(request?.method).toBe('POST')
  expect(request?.headers['idempotency-key']).toBeTruthy()
  await expectNoSeriousA11yViolations(page)
})

test('US-51 CA-2: a project paused by hand is resumed', async ({ page }) => {
  const api = signedIn()
  seed(api, { state: 'paused', pause_reason: 'manual', available_actions: ['resume', 'cancel'] })
  await api.install(page)
  await openProject(page)

  await actions(page).getByRole('button', { name: 'Retomar projeto' }).click()
  const dialog = page.getByRole('dialog', { name: `Retomar o projeto ${NAME}?` })
  await dialog.getByRole('button', { name: 'Retomar o projeto' }).click()
  await expect(page.locator('.state-label').first()).toContainText('Em andamento')
  await expect(actions(page).getByRole('button', { name: 'Pausar projeto' })).toBeVisible()
})

test('US-51 CA-3, US-39 CA-3: paused by the budget, resume is refused until the limit is raised', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, {
    state: 'paused',
    pause_reason: 'budget',
    available_actions: ['resume', 'cancel'],
    summary: {
      approved_stages: 3,
      stages_total: 6,
      gates_awaiting: 0,
      open_questions: 0,
      accumulated_cost: BRL('100.00'),
      stop_limit: BRL('100.00'),
    },
  })
  await api.install(page)
  await openProject(page)

  const banner = page.getByTestId('budget-paused')
  await expect(banner).toContainText('orçamento esgotado')
  await expect(banner).toContainText('100,00')
  await expect(banner.getByRole('link', { name: 'Ampliar o limite de parada' })).toBeVisible()
  await expectNoSeriousA11yViolations(page)

  await actions(page).getByRole('button', { name: 'Retomar projeto' }).click()
  const dialog = page.getByRole('dialog', { name: `Retomar o projeto ${NAME}?` })
  await expect(dialog).toContainText('limite de parada acima do gasto')
  await dialog.getByRole('button', { name: 'Retomar o projeto' }).click()
  await expect(dialog.getByRole('alert')).toContainText('amplie o limite de parada')
  // ER-06-8: there is no "authorize the excess"; the way out is the link to raise the limit.
  await dialog.getByRole('link', { name: 'Ampliar o limite de parada' }).click()
  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/configuration$`))
})

test('US-51 CA-4, RN-1: cancel needs the typed name; the project becomes read-only', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api)
  state.waves = [waveSummary('REQ-02', 'running')]
  await api.install(page)
  await openProject(page)

  await actions(page).getByRole('button', { name: 'Cancelar projeto' }).click()
  const dialog = page.getByRole('dialog', { name: `Cancelar o projeto ${NAME}?` })
  await expect(dialog).toContainText('definitivo')
  await expect(dialog).toContainText('custo perdido')
  const confirm = dialog.getByRole('button', { name: 'Cancelar o projeto' })
  await expect(confirm).toBeDisabled()
  const field = dialog.getByLabel(`Digite o nome do projeto (${NAME}) para confirmar`)
  await field.fill('Reserva')
  await expect(confirm).toBeDisabled()
  await expectNoSeriousA11yViolations(page)
  await field.fill(NAME)
  await confirm.click()

  await expect(page.locator('.state-label').first()).toContainText('Cancelado')
  await expect(page.getByTestId('read-only-notice')).toContainText('somente leitura')
  await expect(page.getByTestId('project-actions')).toHaveCount(0)
  const request = api.requests.find((item) => item.path.endsWith('/cancellation'))
  expect(request?.body).toEqual({ name_confirmation: NAME })
  expect(state.waves[0]?.state).toBe('canceled')
  await expectNoSeriousA11yViolations(page)
})

for (const ended of ['completed', 'canceled'] as const) {
  test(`US-51 CA-5, US-52 CA-4: a ${ended} project shows no lifecycle action`, async ({ page }) => {
    const api = signedIn()
    seed(api, { state: ended, available_actions: [] })
    await api.install(page)
    await openProject(page)

    await expect(page.getByTestId('read-only-notice')).toBeVisible()
    await expect(page.getByTestId('project-actions')).toHaveCount(0)
    // The configuration is read-only too (ER-29-17).
    await page.getByRole('tab', { name: 'Configuração' }).click()
    await expect(page.getByRole('button', { name: 'Salvar orçamento' })).toHaveCount(0)
  })
}

test('US-51 CA-6: a member or approver sees no pause, resume or cancel', async ({ page }) => {
  const api = signedIn()
  api.user = { id: 'bruno', name: 'Bruno Lima', organization_roles: [] }
  seed(api, { my_roles: ['member', 'approver'], available_actions: [] })
  await api.install(page)
  await openProject(page)

  await expect(page.getByTestId('project-actions')).toHaveCount(0)
})

test('US-52 CA-1: with the Release approved the project is completed and read-only', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, { available_actions: ['pause', 'cancel', 'record_publication'] })
  await api.install(page)
  await openProject(page)

  await actions(page).getByRole('button', { name: 'Concluir projeto' }).click()
  const dialog = page.getByRole('dialog', { name: `Concluir o projeto ${NAME}?` })
  await expect(dialog).toContainText('somente leitura')
  await dialog.getByRole('button', { name: 'Concluir o projeto' }).click()
  await expect(page.locator('.state-label').first()).toContainText('Concluído')
  await expect(page.getByTestId('read-only-notice')).toContainText('Projeto concluído')
  await expect(page.getByTestId('project-actions')).toHaveCount(0)
})

test('US-52 CA-2: without the Release approved the conclusion is refused with the reason', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, { available_actions: ['pause', 'cancel', 'record_publication'] }, 'awaiting_gate')
  await api.install(page)
  await openProject(page)

  await actions(page).getByRole('button', { name: 'Concluir projeto' }).click()
  const dialog = page.getByRole('dialog', { name: `Concluir o projeto ${NAME}?` })
  await dialog.getByRole('button', { name: 'Concluir o projeto' }).click()
  await expect(dialog.getByTestId('completion-reason')).toContainText(
    'A Release precisa estar aprovada.',
  )
})

test('US-52 CA-3: an open Critical finding without acceptance refuses the conclusion', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api, { available_actions: ['pause', 'cancel', 'record_publication'] })
  state.findings = [finding('ACH-07', { severity: 'critical', status: 'open' })]
  await api.install(page)
  await openProject(page)

  await actions(page).getByRole('button', { name: 'Concluir projeto' }).click()
  const dialog = page.getByRole('dialog', { name: `Concluir o projeto ${NAME}?` })
  await dialog.getByRole('button', { name: 'Concluir o projeto' }).click()
  await expect(dialog.getByTestId('problem-findings')).toContainText('ACH-07')
})

test('US-39 CA-1: a budget alert arrives live and the project state does not change', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api)
  state.streams.push({
    body: `retry: 60000\n\n${sseFrame(
      domainEvent(PROJECT_ID, 4, 'project.budget_alerted', {
        project: PROJECT_ID,
        transition: { machine: 'project', from_state: 'in_progress', to_state: 'in_progress' },
        spend: BRL('81.00'),
        alert_limit: BRL('80.00'),
        stop_limit: BRL('100.00'),
      }),
    )}`,
  })
  await api.install(page)
  await openProject(page)

  await expect(page.getByTestId('budget-alert')).toContainText('80%')
  await expect(page.getByTestId('budget-alert')).toContainText('81,00')
  await expect(page.locator('.state-label').first()).toContainText('Em andamento')
  await expectNoSeriousA11yViolations(page)
})

test('US-52 CA-3: an active wave refuses the conclusion and is named as the reason', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api, { available_actions: ['pause', 'cancel', 'record_publication'] })
  state.waves = [waveSummary('REL-02', 'running')]
  await api.install(page)
  await openProject(page)

  await actions(page).getByRole('button', { name: 'Concluir projeto' }).click()
  const dialog = page.getByRole('dialog', { name: `Concluir o projeto ${NAME}?` })
  await dialog.getByRole('button', { name: 'Concluir o projeto' }).click()
  await expect(dialog.getByTestId('completion-reason')).toContainText('Há onda ativa: REL-02.')
})
