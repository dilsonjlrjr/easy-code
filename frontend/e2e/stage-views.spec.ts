import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, finding, waveDetail, waveSummary, type ProjectState } from './support/fakeApi'

const PROJECT_ID = '5b2d7c1e-3f4a-4b6c-9d8e-1a2b3c4d5e6f'
const LOAD_TIMEOUT = 30_000

function artifact(
  id: string,
  type: string,
  stage: string,
  agent: string,
  header: Record<string, unknown> = {},
  state = 'approved',
) {
  const current = {
    id,
    type,
    title: `${type} ${id}`,
    state,
    version: 1,
    stage,
    author: { type: 'agent', agent },
    wave: 'W-01',
    updated_at: '2026-11-22T10:00:00-03:00',
    header: { id, tipo: type, ...header },
    situation: null,
    body: '',
    commit: null,
  }
  return { current, versions: [], byVersion: { 1: current } }
}

function wave(
  id: string,
  stage: string,
  agent: string,
  state: string,
  extra: Record<string, unknown> = {},
) {
  return { ...waveSummary(id, state), stage, agent, ...extra }
}

function execution(id: string, category: string, extra: Record<string, unknown> = {}) {
  return {
    id,
    command: category === 'tests' ? 'go test ./...' : `make ${category}`,
    category,
    start: '2026-11-22T10:10:00-03:00',
    duration_ms: 4200,
    exit_code: 0,
    end_reason: 'completed',
    output_bytes: 0,
    ...extra,
  }
}

function workflow() {
  const stage = (
    id: string,
    name: string,
    agent: string,
    dependsOn: string[],
    state: string,
    type = 'stage',
  ) => ({
    id,
    name,
    short_name: name,
    type,
    agent,
    depends_on: dependsOn,
    required_capabilities: [],
    state,
    active_waves: 0,
    ...(id === 'qa-plan' ? { approved_in_gate: 'gate.validation' } : {}),
  })
  return {
    id: 'workflow-padrao',
    version: 1,
    mode: 'fixed',
    stages: [
      stage('requirements', 'Requisitos', 'requirements-agent', [], 'approved'),
      stage(
        'qa-plan',
        'Plano de testes',
        'qa-agent',
        ['requirements'],
        'approved',
        'parallel_activity',
      ),
      stage('architecture', 'Arquitetura', 'architect-agent', ['requirements'], 'approved'),
      stage('data', 'Modelagem de Dados', 'dba-agent', ['architecture'], 'awaiting_gate'),
      stage('implementation', 'Implementação', 'developer-agent', ['data'], 'approved'),
      stage('qa', 'QA', 'qa-agent', ['implementation'], 'running'),
    ],
  }
}

const column = (nome: string, tipo: string, extra: Record<string, unknown> = {}) => ({
  nome,
  tipo,
  nulo: false,
  descricao: nome,
  dado_pessoal: false,
  ...extra,
})

const DIC_HEADER = {
  modelo_dbml: 'docs/dados/modelo-dados.dbml',
  tabelas: [
    {
      nome: 'rooms',
      origem: ['US-01'],
      descricao: 'Salas de reunião.',
      colunas: [column('id', 'uuid'), column('name', 'text')],
      constraints: [{ nome: 'rooms_pkey', tipo: 'primary_key', definicao: 'PRIMARY KEY (id)' }],
      indices: [],
    },
    {
      nome: 'bookings',
      origem: ['US-03'],
      descricao: 'Reservas.',
      colunas: [
        column('id', 'uuid'),
        column('room_id', 'uuid'),
        column('owner_email', 'text', { dado_pessoal: true }),
      ],
      constraints: [
        { nome: 'bookings_pkey', tipo: 'primary_key', definicao: 'PRIMARY KEY (id)' },
        {
          nome: 'bookings_room_fk',
          tipo: 'foreign_key',
          definicao: 'FOREIGN KEY (room_id) REFERENCES easycode.rooms(id) ON DELETE RESTRICT',
        },
      ],
      indices: [],
    },
  ],
  dados_pessoais: [
    {
      campo: 'bookings.owner_email',
      finalidade: 'avisar',
      base_legal: 'execucao_contrato',
      retencao: '1 ano',
    },
  ],
}

const REL_QA_HEADER = {
  execucao: 1,
  resumo: {
    ct_executados: 2,
    passaram: 2,
    falharam: 0,
    bloqueados: 0,
    ca_must_cobertos: 2,
    ca_must_total: 3,
  },
  matriz: [
    { us: 'US-01', criterio: 'CA-1', ct: 'CT-01', nivel: 'e2e', resultado: 'passou' },
    {
      us: 'US-03',
      criterio: 'CA-1',
      ct: 'CT-02',
      nivel: 'integracao',
      resultado: 'passou',
      achado: 'ACH-03',
    },
  ],
  sem_cobertura: [{ us: 'US-03', criterio: 'CA-2', motivo: 'cenário bloqueado pelo ambiente' }],
  comandos: [],
  achados_abertos: [],
  veredito: 'reprovado',
}

function seed(api: FakeApi): ProjectState {
  return api.addProject({
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
      last_sequence: 3,
    },
    workflow: workflow(),
    waves: [
      wave('DBA-01', 'data', 'dba-agent', 'completed'),
      wave('DEV-01', 'implementation', 'developer-agent', 'completed', {
        task: 'T-01',
        created_at: '2026-11-22T10:00:00-03:00',
      }),
      wave('DEV-11', 'implementation', 'developer-agent', 'completed', {
        purpose: 'rework',
        finding: 'ACH-03',
        created_at: '2026-11-22T11:00:00-03:00',
      }),
      // ESP-15 3.4: a QA wave whose executions fail to load must not hide the others.
      wave('QA-00', 'qa', 'qa-agent', 'completed', { created_at: '2026-11-22T09:30:00-03:00' }),
      wave('QA-01', 'qa', 'qa-agent', 'completed', { created_at: '2026-11-22T10:30:00-03:00' }),
      wave('QA-02', 'qa', 'qa-agent', 'running', {
        purpose: 'revalidation',
        finding: 'ACH-03',
        created_at: '2026-11-22T12:00:00-03:00',
      }),
    ],
    // The fake serves a wave's sandbox executions only for a wave it knows in detail.
    waveDetails: {
      'DEV-11': waveDetail('DEV-11', 'completed'),
      'QA-01': waveDetail('QA-01', 'completed'),
      'QA-02': waveDetail('QA-02', 'running'),
    },
    executions: {
      'DEV-11': [
        execution('ex-1', 'build'),
        execution('ex-2', 'tests', { tests: { total: 12, passed: 12, failed: 0, ignored: 0 } }),
        execution('ex-3', 'lint', { exit_code: 1 }),
      ],
      'QA-02': [
        execution('ex-4', 'tests', {
          command: 'npx playwright test',
          tests: { total: 5, passed: 5, failed: 0, ignored: 0 },
        }),
      ],
    },
    artifacts: {
      DIC: artifact('DIC', 'DIC', 'data', 'dba-agent', DIC_HEADER, 'in_review'),
      'REL-QA-01': artifact('REL-QA-01', 'REL-QA', 'qa', 'qa-agent', REL_QA_HEADER),
      'CT-01': artifact('CT-01', 'CT', 'qa-plan', 'qa-agent'),
      'CT-02': (() => {
        // PL-63: the plan is an attachment of the CT versions, outside their folder.
        const ct = artifact('CT-02', 'CT', 'qa-plan', 'qa-agent')
        const current = {
          ...ct.current,
          attachments: [
            {
              path: 'docs/qa/plano.md',
              media_type: 'text/markdown',
              size_bytes: 64,
              sha256: 'e'.repeat(64),
            },
          ],
        }
        return { ...ct, current, byVersion: { 1: current } }
      })(),
    },
    attachmentContent: {
      'CT-02:1:docs/qa/plano.md': '# Plano de testes\n\nNíveis: unidade, integração e **e2e**.',
    },
    findings: [
      finding('ACH-03', {
        title: 'Reserva duplicada no reenvio',
        author: 'qa-agent',
        origin_stage: 'qa',
        category: 'defeito',
        status: 'revalidating',
        cycles: 1,
      }),
      finding('ACH-07', { status: 'escalated', cycles: 3, severity: 'critical' }),
      finding('ACH-09', { destination_stage: 'requirements', title: 'Outro destino' }),
    ],
    coverage: [
      {
        us: 'US-01',
        priority: 'must',
        status: 'covered',
        criteria: [{ ca: 'CA-1', cts: [{ ct: 'CT-01', result: 'passed' }] }],
      },
    ],
  })
}

test.beforeEach(async ({ page }) => {
  const api = new FakeApi()
  api.signedIn = true
  seed(api)
  await api.install(page)
})

test('ESP-09: the data stage shows the DIC entities with keys, personal data, origin and relationships', async ({
  page,
}) => {
  await page.goto(`/projects/${PROJECT_ID}/stages/data`)
  const entities = page.getByTestId('data-entities')
  await expect(page.getByTestId('entity-bookings')).toBeVisible({ timeout: LOAD_TIMEOUT })
  await expect(entities).toContainText('docs/dados/modelo-dados.dbml')
  await expect(page.getByTestId('entity-rooms')).toContainText('uuid · PK')
  const bookings = page.getByTestId('entity-bookings')
  await expect(bookings).toContainText('uuid · FK rooms')
  await expect(bookings).toContainText('text · pessoal')
  await expect(bookings.getByRole('link', { name: 'US-03' })).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/artifacts/US-03`,
  )
  await expect(page.getByTestId('data-relationships')).toHaveText('rooms 1 — N bookings (room_id)')
  await expect(entities.getByRole('link', { name: 'Abrir o DIC' })).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/artifacts/DIC`,
  )
  await expectNoSeriousA11yViolations(page)
})

test('ESP-14, US-41, US-32: the implementation stage shows build, tests and lint, and the rework list', async ({
  page,
}) => {
  await page.goto(`/projects/${PROJECT_ID}/stages/implementation`)
  const checks = page.getByTestId('impl-checks')
  await expect(page.getByTestId('impl-check-build')).toHaveText('build verde', {
    timeout: LOAD_TIMEOUT,
  })
  await expect(checks).toContainText('Últimos comandos da onda DEV-11')
  await expect(page.getByTestId('impl-check-tests')).toHaveText('12 testes passando')
  await expect(page.getByTestId('impl-check-lint')).toHaveText('lint falhou (código 1)')
  await expect(page.getByTestId('impl-evidence')).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/waves/DEV-11#evidence`,
  )

  // Only the findings sent to the Implementation, with their rework and revalidation waves.
  const rework = page.getByTestId('impl-rework')
  await expect(page.getByTestId('rework-ACH-03')).toBeVisible()
  await expect(rework).not.toContainText('ACH-09')
  const ach03 = page.getByTestId('rework-ACH-03')
  await expect(ach03).toContainText('ciclo 1 de 3')
  await expect(ach03).toContainText('Revalidando')
  await expect(ach03.getByRole('link', { name: 'DEV-11' })).toBeVisible()
  await expect(ach03.getByRole('link', { name: 'QA-02' })).toBeVisible()
  const ach07 = page.getByTestId('rework-ACH-07')
  await expect(ach07).toContainText('ciclo 3 de 3')
  await expect(ach07.getByRole('link', { name: 'Ver em Pendências' })).toBeVisible()
  await expectNoSeriousA11yViolations(page)
})

test('ESP-15, US-22: the QA stage shows the REL-QA, the matrix with gaps, the executions and the plan', async ({
  page,
}) => {
  await page.goto(`/projects/${PROJECT_ID}/stages/qa`)
  await expect(page.getByTestId('qa-verdict')).toHaveText('Veredito: reprovado.', {
    timeout: LOAD_TIMEOUT,
  })
  await expect(page.getByTestId('qa-open-report')).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/artifacts/REL-QA-01`,
  )

  const matrix = page.getByTestId('qa-matrix')
  await expect(page.getByTestId('qa-gaps')).toContainText('US-03 CA-2')
  await expect(page.getByTestId('qa-gaps')).toContainText('cenário bloqueado pelo ambiente')
  await expect(page.getByTestId('qa-row-2')).toContainText('passou após ACH-03')
  await expect(page.getByTestId('qa-row-2')).toContainText('integração')
  await expect(matrix.getByRole('link', { name: 'CT-01' })).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/artifacts/CT-01`,
  )
  await expect(page.getByTestId('qa-tab-result')).toContainText('2')
  await expectNoSeriousA11yViolations(page)

  // Tabs: arrows move between them (WAI-ARIA) and the route keeps the tab.
  await page.getByTestId('qa-tab-result').focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByTestId('qa-tab-executions')).toBeFocused()
  await expect(page).toHaveURL(/qaTab=executions/)
  const executions = page.getByTestId('qa-executions')
  await expect(page.getByTestId('qa-execution-ex-4')).toContainText('npx playwright test')
  await expect(page.getByTestId('qa-execution-ex-4')).toContainText('verde')
  await expect(page.getByTestId('qa-executions-failed-QA-00')).toContainText(
    'Não foi possível carregar as execuções da onda QA-00.',
  )
  await expect(
    executions.getByRole('link', { name: 'Ver evidência: comando 1 da onda QA-02' }),
  ).toHaveAttribute('href', `/projects/${PROJECT_ID}/waves/QA-02#evidence`)
  await expectNoSeriousA11yViolations(page)

  await page.getByTestId('qa-tab-plan').click()
  await expect(page.getByTestId('qa-plan-scenarios')).toHaveText('2 (CT-01 a CT-02)')
  await expect(page.getByTestId('qa-plan')).toContainText('gate.validation')
  await expect(page.getByTestId('qa-plan-file').getByRole('link')).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/artifacts/CT-02?version=1`,
  )
  await expect(page.getByTestId('qa-plan-file')).toHaveText('docs/qa/plano.md')
  const plan = page.getByTestId('qa-plan-content')
  await expect(plan.getByRole('heading', { name: 'Plano de testes' })).toBeVisible()
  await expect(plan.locator('strong')).toHaveText('e2e')
  await expect(page.getByTestId('qa-open-plan')).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/stages/qa-plan`,
  )
  await expectNoSeriousA11yViolations(page)
})
