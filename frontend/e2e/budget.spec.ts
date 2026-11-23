import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow, type ProjectState } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const NAME = 'Reserva de salas'
const BRL = (value: string) => ({ value, currency: 'BRL' })

function seed(
  api: FakeApi,
  options: {
    project?: Record<string, unknown>
    spend?: string
    limit?: string
    ceiling?: string
  } = {},
): ProjectState {
  const limit = options.limit ?? '100.00'
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
        approved_stages: 1,
        stages_total: 3,
        gates_awaiting: 0,
        open_questions: 0,
        accumulated_cost: BRL(options.spend ?? '70.00'),
        stop_limit: BRL(limit),
      },
      last_sequence: 3,
      ...options.project,
    },
    configuration: {
      model_pair: null,
      gate_approval: {},
      budget: { stop_limit: BRL(limit), percentage_alert_limits: [50, 80] },
      stop_limit_ceiling: options.ceiling ? BRL(options.ceiling) : null,
      artifact_language: 'pt-BR',
    },
    workflow: phaseOneWorkflow('started'),
  })
}

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
}

function budget(page: Page) {
  return page.getByTestId('budget')
}

async function openConfiguration(page: Page): Promise<void> {
  await page.goto(`/projects/${PROJECT_ID}/configuration`)
  // The configuration tab also reads the models; the shared CI host can be slow to render it.
  await expect(budget(page)).toBeVisible({ timeout: 30_000 })
}

test('ESP-29 3.3: the budget shows the limit, the alerts, the spend and the balance', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, { ceiling: '500.00' })
  await api.install(page)
  await openConfiguration(page)

  const current = page.getByTestId('budget-current')
  await expect(current).toContainText('100,00')
  await expect(current).toContainText('50%, 80%')
  await expect(current).toContainText('500,00')
  await expect(page.getByTestId('budget-spent')).toContainText('70,00')
  await expect(page.getByTestId('budget-spent')).toContainText('70% do limite')
  await expect(current).toContainText('30')
  await expectNoSeriousA11yViolations(page)
})

test('US-39 RN-1, CA-29-9: an empty limit or one above the ceiling is refused in the field', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, { ceiling: '500.00' })
  await api.install(page)
  await openConfiguration(page)

  await budget(page).getByRole('button', { name: 'Alterar orçamento' }).click()
  const field = page.getByLabel('Limite de parada (BRL)')
  await field.fill('')
  await budget(page).getByRole('button', { name: 'Salvar orçamento' }).click()
  await expect(budget(page)).toContainText('O limite de parada é obrigatório.')
  await field.fill('600')
  await expect(budget(page)).toContainText('passa do teto da organização')
  await expectNoSeriousA11yViolations(page)
  expect(api.requests.some((request) => request.method === 'PATCH')).toBe(false)
})

test('US-39 RN-2, CA-29-10: alerts change from 50% and 80% to 60% and 90%', async ({ page }) => {
  const api = signedIn()
  const state = seed(api)
  await api.install(page)
  await openConfiguration(page)

  await budget(page).getByRole('button', { name: 'Alterar orçamento' }).click()
  await page.getByLabel('Alerta 1 (%)').fill('60')
  await page.getByLabel('Alerta 2 (%)').fill('90')
  await budget(page).getByRole('button', { name: 'Salvar orçamento' }).click()
  await expect(page.getByTestId('budget-current')).toContainText('60%, 90%')
  const patch = api.requests.find((request) => request.method === 'PATCH')
  expect(patch?.body).toEqual({
    budget: { stop_limit: BRL('100.00'), percentage_alert_limits: [60, 90] },
  })
  expect(patch?.headers['if-match']).toBe('"1"')
  expect(state.project.state).toBe('in_progress')
})

test('US-39 CA-5, CA-29-22: a limit below the spend is warned before saving and pauses the project', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api)
  await api.install(page)
  await openConfiguration(page)

  await budget(page).getByRole('button', { name: 'Alterar orçamento' }).click()
  await page.getByLabel('Limite de parada (BRL)').fill('50')
  await budget(page).getByRole('button', { name: 'Salvar orçamento' }).click()
  const dialog = page.getByRole('dialog', { name: 'Limite abaixo do gasto' })
  await expect(dialog).toContainText('pausa por orçamento')
  await expect(dialog.getByRole('button', { name: 'Voltar' })).toBeFocused()
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Salvar e pausar' }).click()

  await expect(dialog).toBeHidden()
  await expect(page.locator('.state-label').first()).toContainText('Pausado (orçamento)')
  await expect(page.getByTestId('budget-paused')).toBeVisible()
  await expect(page.getByTestId('budget-section-paused')).toContainText('Amplie o limite')
  expect(state.project.pause_reason).toBe('budget')
})

test('US-39 CA-3, CA-29-11: raising the limit keeps the project paused until a person resumes it', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, {
    spend: '100.00',
    project: { state: 'paused', pause_reason: 'budget', available_actions: ['resume', 'cancel'] },
  })
  await api.install(page)
  await openConfiguration(page)

  await expect(page.getByTestId('budget-section-paused')).toContainText('Amplie o limite')
  await budget(page).getByRole('button', { name: 'Alterar orçamento' }).click()
  await page.getByLabel('Limite de parada (BRL)').fill('200,00')
  await budget(page).getByRole('button', { name: 'Salvar orçamento' }).click()

  await expect(page.getByTestId('budget-ready-to-resume')).toBeVisible()
  await expect(page.locator('.state-label').first()).toContainText('Pausado (orçamento)')
  await page.getByTestId('project-actions').getByRole('button', { name: 'Retomar projeto' }).click()
  const dialog = page.getByRole('dialog', { name: `Retomar o projeto ${NAME}?` })
  await dialog.getByRole('button', { name: 'Retomar o projeto' }).click()
  await expect(page.locator('.state-label').first()).toContainText('Em andamento')
  await expect(page.getByTestId('budget-section-paused')).toHaveCount(0)
})

test('ER-29-1: a member reads the budget without controls', async ({ page }) => {
  const api = signedIn()
  seed(api, { project: { my_roles: ['member'], available_actions: [] } })
  await api.install(page)
  await openConfiguration(page)

  await expect(page.getByTestId('budget-current')).toContainText('100,00')
  await expect(budget(page).getByRole('button', { name: 'Alterar orçamento' })).toHaveCount(0)
  await expect(budget(page)).toContainText('Só o responsável do projeto altera o orçamento.')
})
