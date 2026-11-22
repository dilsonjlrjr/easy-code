import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, finding, phaseOneWorkflow } from './support/fakeApi'
import { chooseOption } from './support/select'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a51'
const FINDINGS_PATH = `/projects/${PROJECT_ID}/findings`

function securityWorkflow(): Record<string, unknown> {
  const workflow = phaseOneWorkflow('started')
  const stages = workflow.stages as Array<Record<string, unknown>>
  stages[0].state = 'approved'
  stages[1].state = 'approved'
  stages[2].state = 'approved'
  stages.push({
    id: 'security',
    name: 'Segurança',
    short_name: 'Segurança',
    type: 'stage',
    agent: 'security-agent',
    depends_on: ['implementation'],
    state: 'running',
    gate: 'gate.security',
    active_waves: 1,
  })
  return workflow
}

function apiWithFindings(myRoles: string[] = ['lead', 'approver']): FakeApi {
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
      my_roles: myRoles,
      available_actions: ['pause', 'cancel'],
      last_sequence: 9,
      summary: {
        approved_stages: 3,
        stages_total: 4,
        gates_awaiting: 0,
        open_questions: 0,
        open_findings: { critical: 0, high: 2, medium: 0, low: 0 },
        accumulated_cost: { value: '0', currency: 'BRL' },
      },
    },
    workflow: securityWorkflow(),
  })
  state.findings = [
    finding('ACH-07', {
      title: '<img src=x onerror=alert(1)> Senha em texto puro no log',
      status: 'escalated',
      cycles: 3,
      cycle_history: [1, 2, 3].map((number) => ({
        number,
        rework_wave: `DEV-0${String(number + 3)}`,
        revalidation_wave: `SEG-0${String(number + 1)}`,
        result: 'not_fixed',
        commit_or_version: `a1b2c3${String(number)}`,
      })),
      escalations: [{ when: '2026-11-22T09:50:00-03:00', reason: '3 ciclos sem correção' }],
      available_actions: ['authorize_new_rework', 'accept_risk', 'decide_scope_change', 'discard'],
    }),
    finding('ACH-08', {
      title: 'Dependência com CVE conhecida',
      status: 'in_rework',
      cycles: 1,
      cycle_history: [{ number: 1, rework_wave: 'DEV-08', result: null }],
      available_actions: ['accept_risk', 'discard'],
    }),
    finding('ACH-03', {
      title: 'Cabeçalho de segurança ausente',
      severity: 'medium',
      status: 'closed',
      blocking: false,
      resolution: 'fixed',
      available_actions: [],
    }),
    finding('ACH-02', {
      title: 'Token sem expiração',
      status: 'accepted',
      blocking: false,
      acceptance_expired: true,
      acceptance: {
        per: { id: 'ana', name: 'Ana Souza' },
        when: '2026-10-01T10:00:00-03:00',
        origin: 'web',
        justification: 'Compensado pelo proxy.',
        valid_until: '2026-11-01',
      },
      available_actions: ['accept_risk'],
    }),
  ]
  state.artifacts['ACH-07'] = {
    current: {
      id: 'ACH-07',
      type: 'ACH',
      title: 'Senha em texto puro no log',
      state: 'in_review',
      version: 1,
      stage: 'security',
      author: { type: 'agent', agent: 'security-agent' },
      wave: 'SEG-01',
      header: {
        evidencia: 'log.Info("login", "password", password)  // auth/handler.go:42',
        impacto: 'Senhas ficam no log central.',
        correcao_sugerida: 'Remover o campo do log.',
      },
      body: '',
      commit: null,
    },
    versions: [],
    byVersion: {},
  }
  state.artifacts['US-03'] = {
    current: {
      id: 'US-03',
      type: 'US',
      title: 'Reservar sala',
      state: 'approved',
      version: 1,
      stage: 'requirements',
      author: { type: 'agent', agent: 'requirements-agent' },
      wave: 'REQ-01',
      header: {},
      body: '',
      commit: null,
    },
    versions: [],
    byVersion: {},
  }
  return api
}

function decisions(api: FakeApi, path: string) {
  return api.requests.filter(
    (request) => request.method === 'POST' && request.path.endsWith(`/ACH-07/${path}`),
  )
}

test('ESP-30 / US-32 CA-4: the findings tab lists the open findings, blocking and escalated in text, with filters in the address', async ({
  page,
}) => {
  const api = apiWithFindings()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}`)
  await page.getByRole('tab', { name: 'Achados (2 em aberto)' }).click()
  await expect(page).toHaveURL(new RegExp(`${FINDINGS_PATH}$`))

  const table = page.getByTestId('findings-table')
  await expect(table.getByRole('row')).toHaveCount(3)
  const escalated = page.getByTestId('finding-ACH-07')
  // CA-30-10: generated title shows as text.
  await expect(escalated).toContainText('<img src=x onerror=alert(1)> Senha em texto puro no log')
  await expect(escalated).toContainText('Escalado')
  await expect(escalated).toContainText('espera decisão')
  await expect(escalated).toContainText('bloqueia o Gate de Segurança e a Release')
  await expect(escalated).toContainText('ciclo interno')
  await expect(escalated).toContainText('3 de 3')
  await expect(page.getByTestId('finding-ACH-08')).toContainText('Em retrabalho')
  await expectNoSeriousA11yViolations(page)

  // CA-30-2 / CA-30-12: "Todos" shows the ended ones; an expired acceptance blocks the release.
  await page.getByRole('button', { name: 'Todos' }).click()
  await expect(page).toHaveURL(/status_filter=all/)
  await expect(page.getByTestId('finding-ACH-03')).toContainText('Fechado')
  const expired = page.getByTestId('finding-ACH-02')
  await expect(expired).toContainText('Aceito (vencido)')
  await expect(expired).toContainText('bloqueia a Release até renovar o aceite')

  // CA-30-3: severity filter goes to the api and to the address.
  await page.getByRole('checkbox', { name: 'Médio' }).check()
  await expect(page).toHaveURL(/severity=medium/)
  await expect(table.getByRole('row')).toHaveCount(2)
  await expect(page.getByTestId('finding-ACH-03')).toBeVisible()
  expect(
    api.requests.some((request) => /\/findings\?severity=medium/.test(request.path)),
  ).toBeTruthy()

  // CA-30-6: "decidir" opens the decision of the escalated finding.
  await page.getByRole('button', { name: 'Limpar filtros' }).click()
  await page.getByRole('link', { name: 'Decidir o ACH-07' }).click()
  await expect(page).toHaveURL(new RegExp(`${FINDINGS_PATH}/ACH-07#decision$`))
})

test('US-33 CA-1 / CA-5 / CA-20-11 / CA-20-13: accepting a risk warns about separation, goes through the step-up and records who, when and why', async ({
  page,
}) => {
  const api = apiWithFindings()
  api.recentlyAuthenticated = false
  await api.install(page)
  await page.goto(`${FINDINGS_PATH}/ACH-07`)

  await expect(page.getByRole('heading', { level: 1 })).toContainText('ACH-07')
  await expect(page.getByTestId('finding-evidence')).toContainText('auth/handler.go:42')
  await expect(page.getByTestId('finding-cycles').getByRole('listitem')).toHaveCount(3)
  await expect(page.getByTestId('finding-cycle-type')).toContainText('Ciclo interno')
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('radio', { name: 'Aceitar o risco' }).check()
  await expect(page.getByTestId('risk-separation')).toContainText(
    'Quem aceita o risco não aprova a Release deste ciclo.',
  )
  // US-33 CA-2: without justification it is refused before sending.
  await page.getByRole('button', { name: 'Revisar a decisão' }).click()
  await expect(page.getByTestId('decision-errors')).toContainText('Escreva a justificativa')
  expect(decisions(api, 'risk-acceptance')).toHaveLength(0)

  await page
    .getByLabel('Justificativa (obrigatória)')
    .fill('Risco aceito pela segurança corporativa.')
  await page.getByLabel('Validade do aceite (opcional, dd/mm/aaaa)').fill('2026-12-31')
  await page.getByRole('button', { name: 'Revisar a decisão' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('decision-summary')).toContainText('Aceitar o risco')
  await expect(dialog).toContainText('não aprova a Release deste ciclo')
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Registrar a decisão' }).click()

  // Back from the SSO the decision kept is sent once.
  await expect(page.getByTestId('finding-result')).toContainText('Dev')
  await expect(page.getByTestId('finding-result')).toContainText(
    'Risco aceito pela segurança corporativa.',
  )
  await expect(page.getByTestId('finding-result')).toContainText('31/12/2026')
  await expect(page.getByRole('heading', { level: 1 }).locator('..')).toContainText('Aceito')
  await expect(page.getByTestId('finding-blocking')).toHaveText('não bloqueia')
  expect(api.ssoRequests()).toEqual([
    `/session/login?callback=${encodeURIComponent(`${FINDINGS_PATH}/ACH-07`)}&reauthenticate=true`,
  ])
  const sent = decisions(api, 'risk-acceptance')
  expect(sent).toHaveLength(2)
  for (const request of sent) {
    expect(request.body).toEqual({
      justification: 'Risco aceito pela segurança corporativa.',
      valid_until: '2026-12-31',
    })
    expect(request.headers['x-csrf-token']).toBe('csrf-dev')
  }
})

test('US-32 CA-6: the approver authorizes one more round and a new cycle appears', async ({
  page,
}) => {
  const api = apiWithFindings()
  await api.install(page)
  await page.goto(`${FINDINGS_PATH}/ACH-07`)

  await page.getByRole('radio', { name: 'Autorizar mais uma rodada' }).check()
  await page
    .getByLabel('Justificativa (obrigatória)')
    .fill('O desenvolvedor tem a correção pronta para a rodada extra.')
  await page.getByRole('button', { name: 'Revisar a decisão' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Registrar a decisão' }).click()

  await expect(page.getByTestId('finding-cycles').getByRole('listitem')).toHaveCount(4)
  await expect(page.getByTestId('finding-escalations')).toContainText('mais uma rodada autorizada')
  await expect(page.getByTestId('finding-escalations')).toContainText('por Dev')
})

test('US-32 CA-7 / CA-20-15: a scope change needs who changes and a requirements artifact, then the finding awaits the scope', async ({
  page,
}) => {
  const api = apiWithFindings()
  await api.install(page)
  await page.goto(`${FINDINGS_PATH}/ACH-07`)

  await page.getByRole('radio', { name: 'Mudar o escopo' }).check()
  await page.getByLabel('Justificativa (obrigatória)').fill('O requisito pede guardar a senha.')
  await page.getByRole('button', { name: 'Revisar a decisão' }).click()
  await expect(page.getByTestId('decision-errors')).toContainText(
    'Escolha quem altera os requisitos.',
  )
  await expect(page.getByTestId('decision-errors')).toContainText(
    'Escolha ao menos um artefato de requisitos.',
  )

  await page.getByRole('radio', { name: 'Onda do agente de Requisitos' }).check()
  await chooseOption(
    page,
    page,
    'Artefatos de requisitos afetados (ponto de partida)',
    /US-03 · Reservar sala/,
  )
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Revisar a decisão' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Registrar a decisão' }).click()

  await expect(page.getByTestId('finding-scope')).toContainText('Onda do agente de Requisitos')
  await expect(page.getByTestId('finding-scope')).toContainText('US-03')
  await expect(page.getByTestId('finding-blocking')).toContainText('bloqueia o Gate de Segurança')
  await expect(page.getByRole('heading', { name: 'Decisão', level: 2 })).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)
})

test('US-33 CA-3 / CA-20-10: a member sees the escalated finding without decision options', async ({
  page,
}) => {
  const api = apiWithFindings(['member'])
  await api.install(page)
  await page.goto(FINDINGS_PATH)
  await expect(page.getByTestId('finding-ACH-07')).toContainText('espera decisão')
  await expect(page.getByRole('link', { name: 'Decidir o ACH-07' })).toHaveCount(0)

  await page.goto(`${FINDINGS_PATH}/ACH-07`)
  await expect(page.getByTestId('finding-approver-only')).toContainText(
    'Só o aprovador do projeto decide este Achado.',
  )
  await expect(page.getByRole('radio')).toHaveCount(0)
})

test('US-33 RN-2 / ER-12-9: who accepted a risk in the cycle is refused on the release gate with the reason', async ({
  page,
}) => {
  const api = apiWithFindings()
  api.acceptedRiskThisCycle = true
  const state = api.project(PROJECT_ID)
  state.gates['gate.release'] = [
    {
      gate: 'gate.release',
      stage: 'release',
      round: 1,
      state: 'awaiting_human',
      opened_at: '2026-11-22T09:00:00-03:00',
      decision: null,
      approval_mode: 'human',
      outputs: [],
      checks: [],
      blocking_findings: [],
      non_blocking_findings: [],
      available_actions: ['approve', 'reject'],
    },
  ]
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/gates/gate.release/rounds/1`)
  await page.getByRole('button', { name: 'Aprovar' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Aprovar' }).click()
  await expect(page.getByTestId('decision-failure')).toContainText(
    'Você aceitou um risco neste ciclo e não aprova a Release do mesmo ciclo',
  )
})
