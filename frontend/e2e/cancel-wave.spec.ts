import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import {
  domainEvent,
  FakeApi,
  phaseOneWorkflow,
  sseFrame,
  waveDetail,
  waveSummary,
  type ProjectState,
} from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const WAVE = 'REQ-01'
const BRL = (value: string) => ({ value, currency: 'BRL' })

function seed(
  api: FakeApi,
  state: string,
  actions: string[],
  roles: string[] = ['member'],
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
      my_roles: roles,
      available_actions: [],
      last_sequence: 3,
    },
    workflow: phaseOneWorkflow('started'),
    waves: [waveSummary(WAVE, state)],
    waveDetails: {
      [WAVE]: waveDetail(WAVE, state, {
        available_actions: actions,
        cost: {
          useful: BRL('0.0250'),
          lost: BRL('0.0100'),
          total: BRL('0.0350'),
          estimated_lost: BRL('0'),
        },
      }),
    },
  })
}

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
}

async function cancelAndConfirm(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Cancelar onda' }).click()
  const dialog = page.getByRole('dialog', { name: `Cancelar a onda ${WAVE}?` })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Cancelar a onda' }).click()
}

test('US-14 CA-1: cancel a running wave after the strong confirmation; it goes to canceled with no actions left', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, 'running', ['pause', 'pause_now', 'cancel'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(page.locator('.state-label').first()).toContainText('Em execução')
  const open = page.getByRole('button', { name: 'Cancelar onda' })
  await open.click()
  const dialog = page.getByRole('dialog', { name: `Cancelar a onda ${WAVE}?` })
  await expect(dialog).toContainText('O cancelamento é final')
  await expect(dialog).toContainText('perdidos e cobrados')
  // ESP-18 section 8: the confirm button is never the initial focus of the cancel dialog.
  await expect(dialog.getByRole('button', { name: 'Manter a onda' })).toBeFocused()
  await expectNoSeriousA11yViolations(page)

  // Esc closes it without sending anything and the focus goes back to the button.
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(open).toBeFocused()
  expect(api.requests.some((request) => request.path.endsWith('/cancellation'))).toBe(false)

  await cancelAndConfirm(page)
  await expect(page.getByTestId('control-notice')).toContainText('Onda cancelada.')
  await expect(page.locator('.state-label').first()).toContainText('Cancelada')
  await expect(page.getByTestId('canceled-notice')).toContainText('0,01')
  await expect(page.getByRole('button', { name: 'Cancelar onda' })).toHaveCount(0)
  const request = api.requests.find((item) => item.path.endsWith('/cancellation'))
  expect(request?.method).toBe('POST')
  expect(request?.body).toEqual({ confirm: true })
  expect(request?.headers['idempotency-key']).toBeTruthy()
  await expectNoSeriousA11yViolations(page)
})

for (const [state, label, actions] of [
  ['queued', 'Na fila', ['cancel']],
  ['awaiting_user', 'Aguardando usuário', ['answer', 'cancel']],
  ['pausing', 'Pausando', ['pause_now', 'cancel']],
  ['paused', 'Pausada', ['resume', 'cancel']],
] as const) {
  test(`US-14 CA-2: a wave in ${state} is canceled after confirming`, async ({ page }) => {
    const api = signedIn()
    seed(api, state, [...actions])
    await api.install(page)
    await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

    await expect(page.locator('.state-label').first()).toContainText(label)
    await cancelAndConfirm(page)
    await expect(page.locator('.state-label').first()).toContainText('Cancelada')
  })
}

test('US-14 CA-3: a completed or canceled wave offers no cancel', async ({ page }) => {
  const api = signedIn()
  seed(api, 'completed', [])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(page.locator('.state-label').first()).toContainText('Concluída')
  await expect(page.getByRole('button', { name: 'Cancelar onda' })).toHaveCount(0)
})

test('US-14 CA-4: a failed wave that will not be restarted is canceled and stops offering restart', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api, 'failed', ['restart', 'cancel'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(page.getByTestId('failure-cause')).toBeVisible()
  await cancelAndConfirm(page)
  await expect(page.locator('.state-label').first()).toContainText('Cancelada')
  expect(state.waveDetails[WAVE]?.available_actions).toEqual([])
})

test('US-14: without a project role that controls waves the action is not shown', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, 'running', ['pause', 'pause_now', 'cancel'], [])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(page.locator('.state-label').first()).toContainText('Em execução')
  await expect(page.getByRole('button', { name: 'Cancelar onda' })).toHaveCount(0)
})

test('US-14 CA-3: when the wave ended meanwhile, the refusal shows and the real state comes back', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api, 'running', ['pause', 'pause_now', 'cancel'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await page.getByRole('button', { name: 'Cancelar onda' }).click()
  Object.assign(state.waveDetails[WAVE] ?? {}, {
    state: 'completed',
    result: 'success',
    available_actions: [],
  })
  const dialog = page.getByRole('dialog', { name: `Cancelar a onda ${WAVE}?` })
  await dialog.getByRole('button', { name: 'Cancelar a onda' }).click()
  await expect(dialog.getByRole('alert')).toBeVisible()
  await dialog.getByRole('button', { name: 'Manter a onda' }).click()
  await expect(page.locator('.state-label').first()).toContainText('Concluída')
  await expect(page.getByRole('button', { name: 'Cancelar onda' })).toHaveCount(0)
})

test('US-14, RNF-10: a cancel made elsewhere reaches the open page live', async ({ page }) => {
  const api = signedIn()
  const state = seed(api, 'running', ['pause', 'pause_now', 'cancel'])
  state.streams.push({
    body: `retry: 60000\n\n${sseFrame(
      domainEvent(
        PROJECT_ID,
        4,
        'wave.canceled',
        {
          wave: WAVE,
          stage: 'requirements',
          agent: 'requirements-agent',
          purpose: 'execution',
          transition: { machine: 'wave', from_state: 'running', to_state: 'canceled' },
        },
        WAVE,
      ),
    )}`,
    onServe: () => {
      Object.assign(state.waveDetails[WAVE] ?? {}, {
        state: 'canceled',
        result: 'canceled',
        available_actions: [],
      })
    },
  })
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/${WAVE}`)

  await expect(page.locator('.state-label').first()).toContainText('Cancelada')
  await expect(page.getByRole('button', { name: 'Cancelar onda' })).toHaveCount(0)
})
