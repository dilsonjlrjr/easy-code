import { expect, test, type Page } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import {
  FakeApi,
  finding,
  gateRound,
  releaseWorkflow,
  waveSummary,
  type ProjectState,
} from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const NAME = 'Reserva de salas'
const RELEASE_PATH = `/projects/${PROJECT_ID}/stages/release`
const BRL = (value: string) => ({ value, currency: 'BRL' })

function releaseRound(overrides: Record<string, unknown> = {}) {
  return gateRound('gate.release', 'release', 1, {
    outputs: [{ artifact: 'RELEASE-01', version: 1, type: 'RELEASE', state: 'in_review' }],
    checks: [
      {
        check: 'achado.sem_bloqueante_aberto',
        name: 'Nenhum achado bloqueante aberto',
        type: 'automatic',
        failure_severity: 'blocks',
        status: 'passed',
      },
      {
        check: 'release.entrega_conferida',
        name: 'Entrega conferida',
        type: 'human',
        failure_severity: 'blocks',
        status: 'pending',
      },
    ],
    ...overrides,
  })
}

const acceptedRisk = (id: string, overrides: Record<string, unknown> = {}) =>
  finding(id, {
    severity: 'medium',
    status: 'accepted',
    acceptance: {
      per: { id: 'ana', name: 'Ana Souza' },
      when: '2026-11-21T15:00:00-03:00',
      origin: 'web',
      justification: 'Mitigado por WAF até a próxima versão.',
      valid_until: '2026-12-31',
    },
    available_actions: [],
    ...overrides,
  })

function seed(
  api: FakeApi,
  options: { release?: string; security?: string; roles?: string[]; round?: boolean } = {},
): ProjectState {
  const state = api.addProject({
    project: {
      id: PROJECT_ID,
      name: NAME,
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-20T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: options.roles ?? ['lead', 'approver'],
      available_actions: ['pause', 'cancel'],
      summary: {
        approved_stages: 5,
        stages_total: 6,
        gates_awaiting: 1,
        open_questions: 0,
        accumulated_cost: BRL('42.00'),
        stop_limit: BRL('100.00'),
      },
      last_sequence: 3,
    },
    configuration: {
      model_pair: null,
      gate_approval: {},
      budget: { stop_limit: BRL('100.00'), percentage_alert_limits: [50, 80] },
      artifact_language: 'pt-BR',
    },
    workflow: releaseWorkflow(options.release ?? 'awaiting_gate', options.security),
    repository: {
      status: 'available',
      clone_url: `https://easycode.empresa.local/git/${PROJECT_ID}.git`,
      main_branch: 'main',
      last_commit: { sha: 'b'.repeat(40), when: '2026-11-22T09:00:00-03:00', wave: 'REL-01' },
      marks: [],
    },
  })
  if (options.round !== false) state.gates['gate.release'] = [releaseRound()]
  state.artifacts['RELEASE-01'] = {
    current: {
      id: 'RELEASE-01',
      type: 'RELEASE',
      title: 'Release 1.0 da reserva de salas',
      state: 'in_review',
      version: 1,
      stage: 'release',
      author: { type: 'orchestrator' },
      wave: 'REL-01',
    },
    versions: [],
    byVersion: {},
  }
  return state
}

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
}

function approveButton(page: Page) {
  return page.getByTestId('release-approve')
}

async function approveWithChecks(page: Page): Promise<void> {
  await approveButton(page).click()
  const dialog = page.getByRole('dialog', { name: 'Aprovar a Release' })
  await expect(dialog.getByRole('button', { name: /faltam 1 checagens/ })).toBeDisabled()
  await dialog.getByRole('checkbox', { name: 'Entrega conferida' }).check()
  await dialog.getByRole('button', { name: 'Aprovar a Release' }).click()
}

test('US-08 CA-1, US-33 CA-4: the consolidation shows gates, tests, findings, accepted risks and cost', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api)
  state.findings = [acceptedRisk('ACH-04'), finding('ACH-09', { severity: 'low' })]
  await api.install(page)
  // The stage card of the workflow opens the Release page.
  await page.goto(`/projects/${PROJECT_ID}/stages/release`)

  await expect(page.getByRole('heading', { level: 2, name: 'Release' })).toBeVisible()
  await expect(page.getByTestId('release-status')).toContainText('Pronta para a sua aprovação')
  await expect(page.getByTestId('release-gates')).toContainText('Segurança')
  await expect(page.getByTestId('release-tests')).toContainText('passaram 12')
  await expect(page.getByTestId('release-other-findings')).toContainText('ACH-09')
  const risk = page.getByTestId('accepted-risk-ACH-04')
  await expect(risk).toContainText('Aceito')
  await expect(risk).toContainText('Ana Souza')
  await expect(risk).toContainText('Mitigado por WAF até a próxima versão.')
  await expect(risk).toContainText('31/12/2026')
  await expect(page.getByTestId('release-cost')).toContainText('42%')
  await expect(page.getByTestId('release-blocks')).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('tab', { name: 'Notas' }).click()
  await expect(page).toHaveURL(/tab=notes/)
  await expect(page.getByTestId('release-artifact')).toContainText('RELEASE-01')
  await expectNoSeriousA11yViolations(page)
  await page.getByRole('tab', { name: 'Entrega' }).click()
  await expect(page.getByTestId('release-to-mark')).toContainText('bbbbbbbbbbbb')
  await expect(page.getByTestId('release-phase-note')).toContainText('Fase 3')
  await expect(page.getByTestId('condition-releaseApproved')).toContainText('pendente')
  await expectNoSeriousA11yViolations(page)
})

test('US-08 CA-1, CA-3: the approver approves; the repository gets the version mark', async ({
  page,
}) => {
  const api = signedIn()
  seed(api)
  await api.install(page)
  await page.goto(RELEASE_PATH)

  await approveWithChecks(page)
  await expect(page.getByTestId('release-status')).toContainText('Release aprovada')
  await expect(page.getByTestId('release-status')).toContainText('Dev')
  const approval = api.requests.find((request) => request.path.endsWith('/approval'))
  expect(approval?.body).toEqual({ confirmed_human_checks: ['release.entrega_conferida'] })
  expect(approval?.headers['x-csrf-token']).toBe('csrf-dev')

  await page.getByRole('tab', { name: 'Entrega' }).click()
  await expect(page.getByTestId('release-marks')).toContainText('v1.0.0')
  await expect(page.getByTestId('release-delivery')).toContainText('docs/release/')
  // CA-17-17: nothing is published from here in Phase 2.
  await expect(page.getByRole('button', { name: /Publicar/ })).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)
})

test('US-08 CA-2: an open High finding without acceptance blocks the approval and is the reason', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api)
  state.findings = [finding('ACH-07', { severity: 'high', status: 'open' })]
  await api.install(page)
  await page.goto(RELEASE_PATH)

  const blocks = page.getByTestId('release-blocks')
  await expect(blocks).toContainText('Achado bloqueante sem aceite')
  await expect(blocks.getByRole('link', { name: 'ACH-07' })).toBeVisible()
  await expect(page.getByTestId('release-blocking-findings')).toContainText('ACH-07')
  // ESP-17 section 8: still focusable, marked unavailable and described by the blocks.
  await expect(approveButton(page)).toHaveAttribute('aria-disabled', 'true')
  await expect(approveButton(page)).toHaveAttribute('aria-describedby', 'release-blocks')
  // Playwright treats aria-disabled as disabled; the keyboard proves it is still operable.
  await approveButton(page).focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(blocks).toBeFocused()
  expect(api.requests.some((request) => request.path.endsWith('/approval'))).toBe(false)
  await expectNoSeriousA11yViolations(page)
})

test('US-08 CA-4: an old authentication goes to the SSO and the approval is sent once back', async ({
  page,
}) => {
  const api = signedIn()
  seed(api)
  api.recentlyAuthenticated = false
  await api.install(page)
  await page.goto(RELEASE_PATH)

  await approveWithChecks(page)
  await expect(page.getByTestId('release-status')).toContainText('Release aprovada')
  expect(api.ssoRequests().some((path) => path.includes('reauthenticate=true'))).toBe(true)
  const approvals = api.requests.filter((request) => request.path.endsWith('/approval'))
  // The first try was refused (401), the one after the step-up was recorded.
  expect(approvals).toHaveLength(2)
  await expect(page).toHaveURL(new RegExp(`${RELEASE_PATH}$`))
})

test('US-08 CA-5: who accepted a risk in the cycle does not approve the Release', async ({
  page,
}) => {
  const api = signedIn()
  seed(api)
  api.acceptedRiskThisCycle = true
  await api.install(page)
  await page.goto(RELEASE_PATH)

  await approveWithChecks(page)
  await expect(page.getByTestId('release-decision-failure')).toContainText(
    'não aprova a Release do mesmo ciclo',
  )
  await expect(page.getByTestId('release-status')).toContainText('Pronta para a sua aprovação')
})

test('US-08 CA-6, CA-17-4: an expired acceptance blocks with the date and the renew shortcut', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api)
  state.findings = [
    acceptedRisk('ACH-05', {
      severity: 'high',
      acceptance_expired: true,
      acceptance: {
        per: { id: 'ana', name: 'Ana Souza' },
        when: '2026-10-01T10:00:00-03:00',
        origin: 'web',
        justification: 'Correção agendada.',
        valid_until: '2026-11-01',
      },
    }),
  ]
  await api.install(page)
  await page.goto(RELEASE_PATH)

  const block = page.getByTestId('release-block-ACH-05')
  await expect(block).toContainText('Aceite vencido')
  await expect(block).toContainText('01/11/2026')
  await expect(page.getByTestId('accepted-risk-ACH-05')).toContainText('Aceito (vencido)')
  await expect(approveButton(page)).toHaveAttribute('aria-disabled', 'true')
  await block.getByRole('link', { name: 'Renovar aceite' }).click()
  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/findings/ACH-05#decision$`))
})

test('US-08 CA-7, CA-17-20: a blocked release shows the partial consolidation and no decision', async ({
  page,
}) => {
  const api = signedIn()
  const state = seed(api, { release: 'blocked', security: 'awaiting_gate', round: false })
  state.waves = [waveSummary('SEG-01', 'running')]
  await api.install(page)
  await page.goto(RELEASE_PATH)

  await expect(page.getByTestId('release-status')).toContainText('espera a Segurança')
  await expect(page.getByTestId('release-gates')).toBeVisible()
  await expect(page.getByTestId('release-blocks')).toContainText('Onda ativa')
  await expect(approveButton(page)).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Reprovar a Release' })).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)
})

test('CA-17-9: with two approvers required the first approval is partial', async ({ page }) => {
  const api = signedIn()
  const state = seed(api)
  state.gates['gate.release'] = [releaseRound({ min_approvals: 2 })]
  await api.install(page)
  await page.goto(RELEASE_PATH)

  await expect(page.getByTestId('release-approvals')).toContainText('0 de 2')
  await approveWithChecks(page)
  await expect(page.getByTestId('release-approvals')).toContainText('1 de 2')
  await expect(page.getByTestId('release-status')).toContainText('falta outro aprovador')
  await expect(approveButton(page)).toHaveCount(0)
})

test('CA-17-11, CA-17-19: rejecting needs a reason and no step-up; the stage is returned', async ({
  page,
}) => {
  const api = signedIn()
  seed(api)
  api.recentlyAuthenticated = false
  await api.install(page)
  await page.goto(RELEASE_PATH)

  await page.getByRole('button', { name: 'Reprovar a Release' }).click()
  const dialog = page.getByRole('dialog', { name: 'Reprovar a Release' })
  await dialog.getByRole('button', { name: 'Reprovar a Release' }).click()
  await expect(dialog).toContainText('Escreva o motivo da reprovação.')
  await dialog.getByLabel('Motivo (obrigatório)').fill('Faltou a nota de migração.')
  await dialog.getByRole('button', { name: 'Reprovar a Release' }).click()
  await expect(page.getByTestId('release-status')).toContainText('Devolvida')
  await expect(page.getByTestId('release-status')).toContainText('Faltou a nota de migração.')
  expect(api.ssoRequests()).toHaveLength(0)
})

test('CA-17-16: a member reads the Release without approve, reject or conclude', async ({
  page,
}) => {
  const api = signedIn()
  seed(api, { roles: ['member'] })
  await api.install(page)
  await page.goto(RELEASE_PATH)

  await expect(page.getByTestId('release-status')).toContainText('Aguardando um aprovador')
  await expect(approveButton(page)).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Reprovar a Release' })).toHaveCount(0)
  await page.getByRole('tab', { name: 'Entrega' }).click()
  await expect(page.getByTestId('release-complete-action')).toHaveCount(0)
})

test('US-52 CA-1, CA-17-12: after the approval the project is concluded from the delivery tab', async ({
  page,
}) => {
  const api = signedIn()
  seed(api)
  await api.install(page)
  await page.goto(RELEASE_PATH)

  await approveWithChecks(page)
  await expect(page.getByTestId('release-status')).toContainText('Release aprovada')
  await page.getByRole('tab', { name: 'Entrega' }).click()
  await expect(page.getByTestId('condition-releaseApproved')).toContainText('cumprida')
  await page.getByTestId('release-complete-action').click()
  const dialog = page.getByRole('dialog', { name: `Concluir o projeto ${NAME}?` })
  await expect(dialog).toContainText('Nenhuma onda ativa: cumprida')
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Concluir o projeto' }).click()

  await expect(page.locator('.state-label').first()).toContainText('Concluído')
  await expect(page.getByTestId('read-only-notice')).toBeVisible()
  await expect(page.getByTestId('release-complete-action')).toHaveCount(0)
})
