import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, gateRound, waveSummary, type ProjectState } from './support/fakeApi'

const PROJECT_ID = '7c4e2a1b-9d3f-4e5a-8b6c-2f1e0d9c8b7a'
const LOAD_TIMEOUT = 30_000

function stage(id: string, name: string, agent: string, state: string, extra = {}) {
  return {
    id,
    name,
    short_name: name,
    type: 'stage',
    agent,
    depends_on: [],
    required_capabilities: [],
    state,
    gate: `gate.${id}`,
    active_waves: 0,
    ...extra,
  }
}

function seed(api: FakeApi): ProjectState {
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
      last_sequence: 9,
    },
    workflow: {
      id: 'workflow-padrao',
      version: 1,
      mode: 'fixed',
      stages: [
        stage('requirements', 'Requisitos', 'requirements-agent', 'awaiting_gate', {
          current_round: 1,
          gate_state: 'awaiting_human',
        }),
        stage('architecture', 'Arquitetura', 'architect-agent', 'running'),
        stage('validation', 'Validação técnica', 'architect-agent', 'approved', {
          current_round: 1,
          gate_state: 'approved',
        }),
      ],
    },
    waves: [
      { ...waveSummary('VAL-01', 'completed'), stage: 'validation', agent: 'architect-agent' },
      { ...waveSummary('DBA-04', 'completed'), stage: 'validation', agent: 'dba-agent' },
      { ...waveSummary('DSG-05', 'completed'), stage: 'validation', agent: 'designer-agent' },
    ],
    artifacts: {
      VALIDACAO: {
        current: {
          id: 'VALIDACAO',
          type: 'VALIDACAO',
          title: 'Validação técnica',
          state: 'approved',
          version: 1,
          stage: 'validation',
          author: { type: 'agent', agent: 'architect-agent' },
          wave: 'VAL-01',
          updated_at: '2026-11-22T10:00:00-03:00',
          header: {
            id: 'VALIDACAO',
            tipo: 'VALIDACAO',
            veredito: 'liberado',
            pareceres: [
              { participante: 'arquiteto', parecer: 'aprovado', ressalvas: [], onda: 'VAL-01' },
              { participante: 'dba', parecer: 'aprovado', ressalvas: [], onda: 'DBA-04' },
              {
                participante: 'designer',
                parecer: 'reprovado',
                ressalvas: ['A tela S-03 não mostra o estado vazio.'],
                onda: 'DSG-05',
              },
            ],
          },
          situation: null,
          body: '',
          commit: null,
        },
        versions: [],
        byVersion: {},
      },
    },
  })
  state.gates['gate.requirements'] = [
    gateRound('gate.requirements', 'requirements', 1, {
      checks: [
        {
          check: 'artifact.schema_valid',
          name: 'Cabeçalho YAML válido no esquema do tipo',
          type: 'automatic',
          failure_severity: 'blocks',
          status: 'passed',
          evidence: [{ reference: 'TELAS/S-01', path: '/telas', message: 'esquema conferido' }],
        },
        {
          check: 'finding.no_open_blocking',
          name: 'Nenhum ACH Crítico ou Alto ativo',
          type: 'automatic',
          failure_severity: 'alert',
          status: 'failed',
          evidence: [{ reference: 'ACH-04', message: 'achado médio em aberto' }],
        },
      ],
    }),
  ]
  state.gates['gate.validation'] = [
    gateRound('gate.validation', 'validation', 1, {
      state: 'approved',
      approval_mode: 'automatic',
      available_actions: [],
      decision: {
        result: 'approved',
        record: {
          per: { type: 'orchestrator', id: 'orchestrator' },
          when: '2026-11-22T10:05:00-03:00',
          origin: null,
        },
      },
      checks: [
        {
          check: 'validacao.review_opinions_recorded',
          name: 'Pareceres de arquiteto, DBA e designer registrados',
          type: 'automatic',
          failure_severity: 'blocks',
          status: 'passed',
          evidence: [],
        },
        {
          check: 'validacao.review_opinion_rejected',
          name: 'Liberação com parecer reprovado',
          type: 'automatic',
          failure_severity: 'alert',
          status: 'failed',
          evidence: [{ reference: 'VALIDACAO', message: 'opinion of designer rejected' }],
        },
      ],
    }),
  ]
  return state
}

test('PC-20, PL-46: before any round the stage exit criteria list the gate checks as not evaluated', async ({
  page,
}) => {
  const api = new FakeApi()
  seed(api)
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/stages/architecture`)
  const exit = page.getByTestId('stage-exit-criteria')
  await expect(exit).toBeVisible({ timeout: LOAD_TIMEOUT })
  await expect(page.getByRole('heading', { level: 3, name: 'Critério de saída' })).toBeVisible()
  await expect(page.getByTestId('exit-criteria-summary')).toHaveText(
    'Ainda não avaliado: a etapa não foi ao gate.',
  )
  await expect(page.getByTestId('exit-criterion-artifact.schema_valid')).toContainText(
    'Cabeçalho YAML válido no esquema do tipo · não avaliada',
  )
  await expect(page.getByTestId('exit-criterion-finding.no_open_blocking')).toContainText(
    'não avaliada',
  )
  await expect(page.getByTestId('exit-criteria-outcome')).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)
})

test('ESP-13, ESP-12: the validation stage shows each opinion, the round result and who approved the gate', async ({
  page,
}) => {
  const api = new FakeApi()
  seed(api)
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/stages/validation`)
  // The frame: results of the round and "gate aprovado por … (rodada N)" from decision.record.
  await expect(page.getByTestId('exit-criteria-outcome')).toHaveText(
    'Gate aprovado por Orquestrador (rodada 1).',
    { timeout: LOAD_TIMEOUT },
  )
  await expect(page.getByTestId('exit-criteria-summary')).toHaveText(
    'Resultado da rodada 1: 1 de 2 passaram.',
  )
  await expect(page.getByTestId('exit-criterion-validacao.review_opinion_rejected')).toContainText(
    'Liberação com parecer reprovado · alerta',
  )

  // Three fixed opinion cards (ESP-13 3), the designer's rejection with its effect (ER-13-5a).
  await expect(page.getByRole('heading', { level: 3, name: 'Pareceres' })).toBeVisible()
  const architect = page.getByTestId('opinion-arquiteto')
  await expect(architect).toContainText('Arquiteto (líder)')
  await expect(architect).toContainText('Aprovado')
  await expect(page.getByTestId('opinion-dba')).toContainText('Aprovado')
  const designer = page.getByTestId('opinion-designer')
  await expect(designer).toContainText('Reprovado')
  await expect(designer).toContainText('A tela S-03 não mostra o estado vazio.')
  await expect(page.getByTestId('opinion-effect-designer')).toHaveText(
    'Aprovação automática impedida: a decisão do gate é humana.',
  )
  await expect(designer.getByRole('link', { name: 'Onda DSG-05' })).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/waves/DSG-05`,
  )
  await expect(page.getByTestId('validation-opinion-checks')).toContainText(
    'Pareceres de arquiteto, DBA e designer registrados: passou',
  )
  await expectNoSeriousA11yViolations(page)
})

test('ESP-12: every evidence has a navigable reference; the recorded rejection leads back to the project', async ({
  page,
}) => {
  const api = new FakeApi()
  seed(api)
  api.reworkWaveOnRejection = 'REQ-02'
  await api.install(page)

  await page.goto(`/projects/${PROJECT_ID}/gates/gate.requirements/rounds/1`)
  const schema = page.getByTestId('evidence-artifact.schema_valid')
  await expect(schema).toContainText('TELAS item S-01 · /telas · esquema conferido', {
    timeout: LOAD_TIMEOUT,
  })
  await expect(schema.getByRole('link', { name: 'TELAS' })).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/artifacts/TELAS`,
  )
  await expect(
    page.getByTestId('evidence-finding.no_open_blocking').getByRole('link', { name: 'ACH-04' }),
  ).toHaveAttribute('href', `/projects/${PROJECT_ID}/findings/ACH-04`)
  await expect(page.getByTestId('gate-recorded')).toHaveCount(0)

  await page.getByRole('button', { name: 'Reprovar' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Motivo (obrigatório)').fill('Falta o critério de cancelamento.')
  await dialog.getByRole('button', { name: 'Reprovar' }).click()

  const recorded = page.getByTestId('gate-recorded')
  await expect(recorded).toContainText('Gate reprovado.')
  await expect(recorded).toContainText('Registrado: Dev')
  await expect(recorded.getByRole('link', { name: 'REQ-02' })).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/waves/REQ-02`,
  )
  await expect(page.getByTestId('decision-rework-wave')).toHaveText('REQ-02')
  await expectNoSeriousA11yViolations(page)
  await page.getByTestId('gate-back-to-project').click()
  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}$`))
})
