import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import {
  FakeApi,
  phaseOneWorkflow,
  waveDetail,
  waveSummary,
  type ProjectState,
} from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const WAVE = 'REQ-01'

function seed(
  api: FakeApi,
  state: string,
  actions: string[],
  overrides: Record<string, unknown> = {},
  project: Record<string, unknown> = {},
): ProjectState {
  return api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'ana', name: 'Ana Souza' },
      my_roles: ['member'],
      available_actions: [],
      last_sequence: 3,
      ...project,
    },
    workflow: phaseOneWorkflow('started'),
    waves: [waveSummary(WAVE, state)],
    waveDetails: {
      [WAVE]: waveDetail(WAVE, state, { available_actions: actions, ...overrides }),
    },
  })
}

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
}

function stateLabel(page: Page) {
  return page.locator('.state-label').first()
}

function button(page: Page, name: string) {
  return page.getByTestId('wave-actions').getByRole('button', { name, exact: true })
}

test('US-12 CA-1: pause goes to pausing, with no dialog and no lost cost', async ({ page }) => {
  const api = signedIn()
  const state = seed(api, 'running', ['pause', 'pause_now', 'cancel'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(stateLabel(page)).toContainText('Em execução')
  await button(page, 'Pausar').click()
  await expect(page.getByTestId('control-notice')).toContainText('próximo checkpoint')
  await expect(stateLabel(page)).toContainText('Pausando')
  await expect(page.getByTestId('pause-details')).toContainText('pedido de uma pessoa')
  // CA-18-32: while pausing only "pause now" and "cancel" remain.
  await expect(button(page, 'Pausar')).toHaveCount(0)
  await expect(button(page, 'Pausar agora')).toBeVisible()
  await expect(button(page, 'Cancelar onda')).toBeVisible()
  const request = api.requests.find((item) => item.path.endsWith('/pause'))
  expect(request?.body).toEqual({ mode: 'cooperative' })
  expect(request?.headers['idempotency-key']).toBeTruthy()
  expect(state.waveDetails[WAVE]?.cost).toMatchObject({ lost: { value: '0' } })
  await expectNoSeriousA11yViolations(page)
})

test('US-12 CA-2: pause now warns that the call in progress is lost and charged', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, 'running', ['pause', 'pause_now', 'cancel'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  const open = button(page, 'Pausar agora')
  await open.click()
  const dialog = page.getByRole('dialog', { name: `Pausar a onda ${WAVE} agora?` })
  await expect(dialog).toContainText('perdidos e cobrados')
  await expect(dialog.getByRole('button', { name: 'Voltar' })).toBeFocused()
  await expectNoSeriousA11yViolations(page)

  // CA-18-12: "Back" sends nothing and the wave stays running.
  await dialog.getByRole('button', { name: 'Voltar' }).click()
  await expect(dialog).toBeHidden()
  await expect(open).toBeFocused()
  expect(api.requests.some((item) => item.path.endsWith('/pause'))).toBe(false)

  await open.click()
  await dialog.getByRole('button', { name: 'Pausar agora' }).click()
  await expect(page.getByTestId('control-notice')).toContainText('Onda pausada.')
  await expect(stateLabel(page)).toContainText('Pausada')
  await expect(page.getByTestId('pause-details')).toContainText('0,004')
  const request = api.requests.find((item) => item.path.endsWith('/pause'))
  expect(request?.body).toEqual({ mode: 'immediate', confirm_lost_cost: true })
})

test('US-12 CA-3: a paused wave resumes from the last checkpoint', async ({ page }) => {
  const api = signedIn()
  seed(api, 'paused', ['resume', 'cancel'], {
    pause: { mode: 'cooperative', cause: 'user_request' },
    last_checkpoint: 4,
  })
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(stateLabel(page)).toContainText('Pausada')
  await expectNoSeriousA11yViolations(page)
  await button(page, 'Retomar').click()
  await expect(page.getByTestId('control-notice')).toContainText('último checkpoint')
  await expect(stateLabel(page)).toContainText('Em execução')
  expect(api.requests.some((item) => item.path.endsWith('/resume'))).toBe(true)
})

test('US-12 CA-4: pausing beyond the limit suggests pause now and nothing is aborted', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, 'pausing', ['pause_now', 'cancel'], {
    pause: { mode: 'cooperative', cause: 'user_request', pausing_limit_exceeded: true },
  })
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(page.getByTestId('pausing-too-long')).toContainText('Pausar agora')
  await expect(stateLabel(page)).toContainText('Pausando')
  await expectNoSeriousA11yViolations(page)
})

test('US-39 CA-2, CA-18-15: a wave paused by the budget shows the cause and no resume', async ({
  page,
}) => {
  const api = signedIn()
  seed(
    api,
    'paused',
    ['cancel'],
    { pause: { mode: 'cooperative', cause: 'budget_exhausted' } },
    { state: 'paused', pause_reason: 'budget' },
  )
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(page.getByTestId('pause-details')).toContainText('orçamento esgotado')
  await expect(page.getByTestId('resume-blocked')).toContainText('Amplie o limite de parada')
  await expect(button(page, 'Retomar')).toHaveCount(0)
})

test('US-13 CA-1: a failed wave is restarted after its dialog; the previous attempt stays', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, 'failed', ['restart', 'cancel'], { failure_cause: 'retries_exhausted' })
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(page.getByTestId('failure-cause')).toBeVisible()
  await button(page, 'Reiniciar').click()
  const dialog = page.getByRole('dialog', { name: `Reiniciar a onda ${WAVE}?` })
  await expect(dialog).toContainText('continua consultável')
  await expect(dialog.getByRole('button', { name: 'Voltar' })).toBeFocused()
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Reiniciar a onda' }).click()

  await expect(page.getByTestId('control-notice')).toContainText('está na fila')
  await expect(stateLabel(page)).toContainText('Na fila (reiniciada)')
  await page.getByTestId('wave-tab-attempts').click()
  await expect(page.getByTestId('wave-attempts')).toContainText('tentativa 1')
  await expect(page.getByTestId('wave-attempts')).toContainText('Falhou')
  const request = api.requests.find((item) => item.path.endsWith('/restart'))
  expect(request?.body).toEqual({ confirm: true })
  await expectNoSeriousA11yViolations(page)
})

for (const [state, label, actions] of [
  ['running', 'Em execução', ['pause', 'pause_now', 'cancel']],
  ['paused', 'Pausada', ['resume', 'cancel']],
  ['pausing', 'Pausando', ['pause_now', 'cancel']],
  ['completed', 'Concluída', []],
] as const) {
  test(`US-13 CA-2: a wave in ${state} offers no restart`, async ({ page }) => {
    const api = signedIn()
    seed(api, state, [...actions])
    await api.install(page)
    await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

    await expect(stateLabel(page)).toContainText(label)
    await expect(page.getByRole('button', { name: 'Reiniciar', exact: true })).toHaveCount(0)
  })
}

test('US-12 RN-1: a refused resume shows the reason and the real state', async ({ page }) => {
  const api = signedIn()
  const state = seed(api, 'paused', ['resume', 'cancel'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(button(page, 'Retomar')).toBeVisible()
  Object.assign(state.waveDetails[WAVE] ?? {}, { available_actions: ['cancel'] })
  await button(page, 'Retomar').click()
  await expect(page.getByTestId('control-failure')).toBeVisible()
  await expect(button(page, 'Retomar')).toHaveCount(0)
})
