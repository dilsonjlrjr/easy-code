import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, finding, phaseOneWorkflow } from './support/fakeApi'
import { chooseOption } from './support/select'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a62'

type Json = Record<string, unknown>

function artifact(id: string, type: string, stage: string, header: Json, title = id): Json {
  return {
    id,
    type,
    title,
    state: 'approved',
    version: 1,
    stage,
    author: { type: 'agent', agent: stage === 'qa' ? 'qa-agent' : 'security-agent' },
    wave: stage === 'qa' ? 'QA-01' : 'SEG-01',
    header,
    body: '',
    commit: null,
  }
}

function apiWithQuality(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  const workflow = phaseOneWorkflow('started')
  const stages = workflow.stages as Json[]
  Object.assign(stages[0], { state: 'approved' })
  // US-34 CA-1: Architecture reopened by a Security finding; Implementation blocked.
  Object.assign(stages[1], { state: 'reopened', reopened_by_finding: 'ACH-09' })
  Object.assign(stages[2], { state: 'blocked' })
  stages.push({
    id: 'security',
    name: 'Segurança',
    short_name: 'Segurança',
    type: 'stage',
    agent: 'security-agent',
    depends_on: ['implementation'],
    state: 'running',
    active_waves: 1,
  })
  stages.push({
    id: 'security-threats',
    name: 'Segurança (ameaças)',
    short_name: 'Ameaças',
    type: 'parallel_activity',
    agent: 'security-agent',
    depends_on: ['architecture'],
    state: 'approved',
    approved_in_gate: 'gate.validation',
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
      my_roles: ['lead', 'approver'],
      available_actions: ['pause', 'cancel'],
      last_sequence: 9,
    },
    workflow,
  })
  const add = (item: Json) => {
    state.artifacts[String(item.id)] = { current: item, versions: [], byVersion: {} }
  }
  add(artifact('US-03', 'US', 'requirements', {}, 'Reservar sala'))
  add(
    artifact('REL-QA', 'REL-QA', 'qa', {
      execucao: 1,
      resumo: {
        ct_executados: 2,
        passaram: 1,
        falharam: 1,
        bloqueados: 0,
        ca_must_cobertos: 1,
        ca_must_total: 2,
      },
      matriz: [
        { us: 'US-03', criterio: 'CA-1', ct: 'CT-01', nivel: 'e2e', resultado: 'passou' },
        {
          us: 'US-03',
          criterio: 'CA-2',
          ct: 'CT-02',
          nivel: 'integracao',
          resultado: 'falhou',
          achado: 'ACH-05',
        },
      ],
      sem_cobertura: [{ us: 'US-04', criterio: 'CA-1', motivo: 'sem tela ainda' }],
      comandos: [],
      achados_abertos: ['ACH-05'],
      veredito: 'reprovado',
    }),
  )
  const severity = { abertos: 0, fechados: 0, aceitos: 0 }
  add(
    artifact('REL-SEG', 'REL-SEG', 'security', {
      execucao: 1,
      resumo: {
        critico: severity,
        alto: { abertos: 1, fechados: 0, aceitos: 0 },
        medio: severity,
        baixo: severity,
      },
      varreduras: [
        {
          ferramenta: 'trivy',
          versao: '0.58',
          comando: 'trivy fs .',
          achados: ['ACH-09'],
          falsos_positivos: 1,
        },
      ],
      owasp: [
        { categoria: 'A02', verificado: true, resultado: 'senha em log', achados: ['ACH-09'] },
      ],
      ameacas_conferidas: [],
      falsos_positivos: [
        { ferramenta: 'trivy', regra: 'CVE-1', local: 'go.sum', justificativa: 'só em teste' },
      ],
      comandos: [],
      veredito: 'reprovado',
    }),
  )
  state.findings = [
    finding('ACH-09', {
      title: 'Arquitetura guarda senha sem hash',
      destination_stage: 'architecture',
      status: 'in_rework',
      cycles: 1,
      cycle_history: [{ number: 1, rework_wave: 'ARQ-03', result: null }],
      affected_artifacts: [{ artifact: 'ARQ', item: 'componentes.auth' }],
    }),
  ]
  state.links['US-03'] = {
    root: 'US-03',
    nodes: [
      { id: 'US-03', type: 'US', title: 'Reservar sala', state: 'approved', exists: true },
      { id: 'T-02', type: 'T', title: 'Tela de reserva', state: 'approved', exists: true },
      { id: 'ADR-04', type: 'ADR', title: 'Banco relacional', state: 'approved', exists: true },
      { id: 'CT-01', type: 'CT', title: 'Reserva feliz', state: 'approved', exists: true },
      { id: 'CT-99', type: 'CT', exists: false },
      { id: 'a1b2c3d', type: 'commit', exists: true },
      { id: 'e2e/reserve.spec.ts', type: 'test', exists: true },
      { id: 'ACH-05', type: 'ACH', title: 'Falha na reserva', exists: true },
    ],
    edges: [
      { from: 'T-02', to: 'US-03', rule: 'E-20' },
      { from: 'ADR-04', to: 'US-03', rule: 'E-12' },
      { from: 'CT-01', to: 'US-03', rule: 'E-30' },
      { from: 'CT-99', to: 'US-03', rule: 'E-30' },
      { from: 'a1b2c3d', to: 'T-02', rule: 'E-40' },
      { from: 'ACH-05', to: 'CT-01', rule: 'E-50' },
      { from: 'CT-01', to: 'e2e/reserve.spec.ts', rule: 'E-41' },
    ],
  }
  state.coverage = [
    {
      us: 'US-03',
      priority: 'must',
      status: 'covered',
      criteria: [
        { ca: 'CA-1', cts: [{ ct: 'CT-01', result: 'passed' }] },
        { ca: 'CA-2', cts: [{ ct: 'CT-02', result: 'failed' }] },
      ],
    },
    { us: 'US-04', priority: 'must', status: 'gap', criteria: [{ ca: 'CA-1', cts: [] }] },
    { us: 'US-09', priority: 'should', status: 'gap', criteria: [] },
  ]
  return api
}

test('US-28 CA-1 / CA-3 / CA-24-1: the traceability of a US shows T, ADR, CT, commits and ACH, and the broken link', async ({
  page,
}) => {
  const api = apiWithQuality()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/US-03`)
  await page.getByTestId('artifact-traceability').click()
  await expect(page).toHaveURL(
    new RegExp(`/projects/${PROJECT_ID}/traceability/links\\?root=US-03$`),
  )

  await expect(page.getByTestId('trace-root')).toContainText('US-03')
  await expect(page.getByTestId('trace-group-T')).toContainText('Tarefas (T) (1)')
  await expect(page.getByTestId('trace-group-ADR')).toContainText('ADR-04')
  await expect(page.getByTestId('trace-group-CT')).toContainText('Cenários de teste (CT) (2)')
  await expect(page.getByTestId('trace-node-CT-99')).toContainText('id não encontrado')
  await expect(page.getByTestId('trace-node-CT-01')).toContainText('vínculos E-30, E-41, E-50')
  await expect(page.getByTestId('trace-group-commit')).toContainText('a1b2c3d')
  await expect(page.getByTestId('trace-group-test')).toContainText('e2e/reserve.spec.ts')
  await expect(page.getByTestId('trace-group-ACH')).toContainText('ACH-05')
  await expectNoSeriousA11yViolations(page)

  await chooseOption(page, page, 'Direção', /^saída$/)
  await expect(page).toHaveURL(/direction=output/)
  expect(
    api.requests.some((request) => request.path.includes('/links?depth=3&direction=output')),
  ).toBeTruthy()
})

test('US-28 CA-2 / US-22 CA-2 / CA-24-4: the coverage matrix shows Must US covered or as a gap, gaps first', async ({
  page,
}) => {
  const api = apiWithQuality()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}`)
  await page.getByRole('tab', { name: 'Rastreabilidade' }).click()
  await page.getByRole('button', { name: 'Matriz de cobertura' }).click()

  await expect(page.getByTestId('coverage-summary')).toHaveText('2 US: 1 cobertas, 1 lacunas.')
  const rows = page.getByTestId('coverage-table').locator('tbody tr')
  await expect(rows.first()).toContainText('US-04')
  await expect(rows.first()).toContainText('Lacuna')
  await expect(rows.first()).toContainText('sem cenário')
  await expect(page.getByTestId('coverage-US-03')).toContainText('Coberta')
  await expect(page.getByTestId('coverage-US-03')).toContainText('CT-02 falhou')
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('checkbox', { name: 'Should' }).check()
  await expect(page.getByTestId('coverage-US-09')).toContainText('Lacuna')
  await expect(page).toHaveURL(/priority=must&priority=should/)
})

test('US-22 CA-2 / US-23 CA-2: REL-QA and REL-SEG show their matrix, scans, OWASP and verdict', async ({
  page,
}) => {
  const api = apiWithQuality()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/REL-QA`)
  await expect(page.getByRole('heading', { name: 'Relatório de QA' })).toBeVisible()
  await expect(page.getByTestId('rel-qa-verdict')).toHaveText('Veredito: reprovado.')
  await expect(page.getByTestId('rel-qa-summary')).toContainText('Critérios Must cobertos: 1 de 2.')
  await expect(page.getByTestId('rel-qa-gaps')).toContainText('sem tela ainda')
  await expect(page.getByTestId('rel-qa-matrix')).toContainText('ponta a ponta')
  await expect(
    page.getByTestId('rel-qa-matrix').getByRole('link', { name: 'ACH-05' }),
  ).toBeVisible()
  await expectNoSeriousA11yViolations(page)

  await page.goto(`/projects/${PROJECT_ID}/artifacts/REL-SEG`)
  await expect(page.getByRole('heading', { name: 'Relatório de segurança' })).toBeVisible()
  await expect(page.getByTestId('rel-seg-summary')).toContainText('Alto')
  await expect(page.getByTestId('rel-seg-scans')).toContainText('trivy 0.58')
  await expect(page.getByTestId('rel-seg-owasp')).toContainText('Falhas criptográficas')
  await expectNoSeriousA11yViolations(page)
  await page.getByTestId('rel-seg-scans').getByRole('link', { name: 'ACH-09' }).click()
  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/findings/ACH-09$`))
})

test('US-34 CA-1 / CA-3 / CA-20-17: a finding that changes the approved architecture reopens it and blocks the later stages', async ({
  page,
}) => {
  const api = apiWithQuality()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}`)

  const architecture = page.getByTestId('stage-architecture')
  await expect(architecture).toContainText('Reaberta')
  await expect(architecture).toContainText('Reaberta pelo achado ACH-09')
  await expect(page.getByTestId('stage-implementation')).toContainText('Bloqueada')
  // US-23 CA-1: the threat modeling activity runs in parallel and is approved with gate.validation.
  await expect(page.getByTestId('activity-security-threats')).toContainText('Segurança (ameaças)')
  await expect(page.getByTestId('activity-security-threats')).toContainText(
    'aprovada no gate.validation',
  )
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('link', { name: 'Implementação' }).click()
  await expect(page.getByTestId('stage-blocked-reopening')).toContainText(
    'uma etapa da qual depende foi reaberta',
  )

  await page.goto(`/projects/${PROJECT_ID}/findings/ACH-09`)
  const reopening = page.getByTestId('finding-reopening')
  await expect(reopening).toContainText('Etapa reaberta: Arquitetura')
  await expect(reopening).toContainText('Implementação')
  // CA-20-19: the issuing stage (Security) is not listed as blocked.
  await expect(reopening).not.toContainText('Segurança')
  await expect(page.getByTestId('finding-affected')).toContainText('componentes.auth')
  await expect(page.getByTestId('finding-cycle-type')).toContainText(
    'O retrabalho reabre a etapa dona (Arquitetura).',
  )
  await expectNoSeriousA11yViolations(page)
})
