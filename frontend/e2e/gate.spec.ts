import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, gateRound, phaseOneWorkflow } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a40'
const GATE_PATH = `/projects/${PROJECT_ID}/gates/gate.requirements/rounds/1`

function apiWithGate(myRoles: string[] = ['lead', 'approver']): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  const workflow = phaseOneWorkflow('started')
  const stages = workflow.stages as Array<Record<string, unknown>>
  Object.assign(stages[0], {
    state: 'awaiting_gate',
    gate: 'gate.requirements',
    current_round: 1,
    gate_state: 'awaiting_human',
  })
  const state = api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: myRoles,
      available_actions: ['pause', 'cancel'],
      last_sequence: 9,
    },
    workflow,
  })
  state.gates['gate.requirements'] = [gateRound('gate.requirements', 'requirements', 1)]
  return api
}

function decisions(api: FakeApi, action: 'approval' | 'rejection') {
  return api.requests.filter(
    (request) => request.method === 'POST' && request.path.endsWith(`/rounds/1/${action}`),
  )
}

test('US-06 CA-1 / CA-5: with an old authentication the approval goes through the SSO step-up and is recorded after it', async ({
  page,
}) => {
  const api = apiWithGate()
  api.recentlyAuthenticated = false
  await api.install(page)

  // ESP-12 section 2: the gate is reached from its stage in the workflow.
  await page.goto(`/projects/${PROJECT_ID}`)
  await page.getByRole('link', { name: 'Abrir o gate (rodada 1)' }).click()
  await expect(page).toHaveURL(new RegExp(`${GATE_PATH}$`))
  await expect(
    page.getByRole('heading', { level: 1, name: 'Gate da etapa Requisitos' }),
  ).toBeVisible()
  await expect(page.getByTestId('gate-waiting')).toContainText('Aguardando você')
  await expect(page.getByTestId('check-schema.valid')).toContainText('passou')
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('button', { name: 'Aprovar' }).click()
  const dialog = page.getByRole('dialog')
  const confirm = dialog.getByRole('button', { name: 'Aprovar' })
  // ER-12-5: Approve waits until every human check is ticked.
  await expect(confirm).toBeDisabled()
  await dialog.getByRole('checkbox', { name: 'Critérios de aceite testáveis' }).check()
  await dialog.getByLabel('Comentário (opcional)').fill('Escopo conferido.')
  await expectNoSeriousA11yViolations(page)
  await confirm.click()

  // Back from the SSO on the same round, the confirmed approval is sent once.
  await expect(page.getByTestId('gate-decision')).toContainText('Aprovado')
  await expect(page).toHaveURL(new RegExp(`${GATE_PATH}$`))
  await expect(page.getByTestId('gate-decision')).toContainText('Dev')
  await expect(page.getByTestId('gate-decision')).toContainText('pela web')
  expect(api.ssoRequests()).toEqual([
    `/session/login?callback=${encodeURIComponent(GATE_PATH)}&reauthenticate=true`,
  ])
  const sent = decisions(api, 'approval')
  expect(sent).toHaveLength(2)
  for (const request of sent) {
    expect(request.body).toEqual({
      confirmed_human_checks: ['us.criteria_testable'],
      comment: 'Escopo conferido.',
    })
    expect(request.headers['x-csrf-token']).toBe('csrf-dev')
    expect(request.headers['idempotency-key']).toMatch(/^[0-9a-f-]{36}$/)
  }
  // CA-4: the next stage is ready.
  await page.getByRole('link', { name: 'Reserva de salas' }).click()
  await expect(page.getByTestId('stage-architecture')).toContainText('Pronta')
})

test('US-06 CA-2 / CA-3: rejecting needs a reason; with it the gate is rejected and the reason is recorded', async ({
  page,
}) => {
  const api = apiWithGate()
  // CA-12-18: rejecting does not ask for a recent authentication.
  api.recentlyAuthenticated = false
  await api.install(page)
  await page.goto(GATE_PATH)

  await page.getByRole('button', { name: 'Reprovar' }).click()
  const dialog = page.getByRole('dialog')
  const reason = dialog.getByLabel('Motivo (obrigatório)')
  await expect(reason).toBeFocused()
  await reason.fill('   ')
  await dialog.getByRole('button', { name: 'Reprovar' }).click()
  await expect(dialog).toContainText('Escreva o motivo da reprovação.')
  expect(decisions(api, 'rejection')).toHaveLength(0)
  await expectNoSeriousA11yViolations(page)

  await reason.fill('Faltam os critérios de cancelamento de reserva.')
  await dialog.getByRole('button', { name: 'Reprovar' }).click()

  await expect(page.getByTestId('gate-decision')).toContainText('Reprovado')
  await expect(page.getByTestId('gate-decision')).toContainText(
    'Faltam os critérios de cancelamento de reserva.',
  )
  expect(api.ssoRequests()).toEqual([])
  expect(decisions(api, 'rejection')[0]?.headers['x-csrf-token']).toBe('csrf-dev')
})

test('US-02 CA-2 / CA-12-11: a member sees the round waiting for approval without decision actions', async ({
  page,
}) => {
  const api = apiWithGate(['member'])
  await api.install(page)
  await page.goto(GATE_PATH)

  await expect(page.getByTestId('gate-waiting')).toContainText('Aguardando aprovação')
  await expect(page.getByRole('button', { name: 'Aprovar' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Reprovar' })).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)
})

test('US-02 CA-8 / CA-12-12: a self-assigned approver within 24 h is refused and the gate does not change', async ({
  page,
}) => {
  const api = apiWithGate()
  api.selfAssignedApprover = true
  await api.install(page)
  await page.goto(GATE_PATH)

  await page.getByRole('button', { name: 'Aprovar' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('checkbox', { name: 'Critérios de aceite testáveis' }).check()
  await dialog.getByRole('button', { name: 'Aprovar' }).click()

  await expect(page.getByTestId('decision-failure')).toContainText(
    'Papel atribuído há menos de 24 horas',
  )
  await expect(page.getByTestId('gate-waiting')).toContainText('Aguardando você')
})

test('US-31 CA-1 / CA-2 / CA-12-8: a failed blocking check rejects the round without a human and shows why', async ({
  page,
}) => {
  const api = apiWithGate()
  const state = api.project(PROJECT_ID)
  state.gates['gate.requirements'] = [
    gateRound('gate.requirements', 'requirements', 1, {
      state: 'rejected',
      available_actions: [],
      checks: [
        {
          check: 'schema.valid',
          name: 'Artefatos válidos no esquema',
          type: 'automatic',
          failure_severity: 'blocks',
          status: 'failed',
          evidence: [{ reference: 'US-04', message: 'campo criterios_aceite ausente' }],
        },
        {
          check: 'us.size',
          name: 'Histórias de tamanho adequado',
          type: 'automatic',
          failure_severity: 'alert',
          status: 'failed',
          evidence: [{ reference: 'US-02', message: 'história grande' }],
        },
      ],
      decision: {
        result: 'rejected',
        record: { per: { type: 'orchestrator' }, when: '2026-11-22T09:05:00-03:00', origin: null },
        failed_checks: ['schema.valid'],
      },
    }),
  ]
  await api.install(page)
  await page.goto(GATE_PATH)

  await expect(page.getByTestId('check-schema.valid')).toContainText('falhou (bloqueia)')
  await expect(page.getByTestId('check-schema.valid')).toContainText(
    'campo criterios_aceite ausente',
  )
  await expect(page.getByTestId('check-us.size')).toContainText('alerta')
  await expect(page.getByTestId('check-counts')).toContainText(
    'Passaram: 0. Alertas: 1. Bloqueantes que falharam: 1.',
  )
  await expect(page.getByTestId('gate-decision')).toContainText('Reprovado')
  await expect(page.getByTestId('gate-decision')).toContainText('Orquestrador')
  await expect(page.getByTestId('decision-failed-checks')).toHaveText(
    'Artefatos válidos no esquema',
  )
  await expect(page.getByRole('button', { name: 'Aprovar' })).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)
})

test('US-31 CA-3 / CA-12-9: a critical or high finding opened while waiting makes the round evaluate and approval unavailable', async ({
  page,
}) => {
  const api = apiWithGate()
  const state = api.project(PROJECT_ID)
  state.gates['gate.requirements'] = [
    gateRound('gate.requirements', 'requirements', 1, {
      state: 'evaluating',
      available_actions: [],
      blocking_findings: [
        {
          id: 'ACH-04',
          title: 'Critério ambíguo',
          severity: 'high',
          category: 'requisito_ambiguo',
          status: 'open',
          origin_stage: 'requirements',
          destination_stage: 'requirements',
          author: 'qa-agent',
          blocking: true,
        },
      ],
    }),
  ]
  await api.install(page)
  await page.goto(GATE_PATH)

  await expect(page.getByTestId('gate-evaluating')).toContainText('Avaliando checagens')
  const blocked = page.getByTestId('gate-blocked')
  await expect(blocked).toContainText('A aprovação está indisponível.')
  await expect(blocked).toContainText('Não existe aprovar mesmo assim')
  await expect(page.getByRole('button', { name: 'Aprovar' })).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)
  await blocked.getByRole('link', { name: 'ACH-04' }).click()
  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/findings/ACH-04$`))
})
