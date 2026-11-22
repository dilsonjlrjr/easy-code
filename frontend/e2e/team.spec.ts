import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, waveSummary, type ProjectState } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'

function stage(id: string, name: string, agent: string, dependsOn: string[], state: string) {
  return { id, name, short_name: name, type: 'stage', agent, depends_on: dependsOn, state }
}

/** The phase 2 workflow up to Implementation (workflow-padrao): Data and Design after Architecture. */
const WORKFLOW = {
  id: 'workflow-padrao',
  version: 1,
  mode: 'fixed',
  stages: [
    stage('requirements', 'Requisitos', 'requirements-agent', [], 'approved'),
    stage('architecture', 'Arquitetura', 'architect-agent', ['requirements'], 'approved'),
    stage('data', 'Modelagem de Dados', 'dba-agent', ['architecture'], 'running'),
    stage('design', 'Design', 'designer-agent', ['architecture'], 'running'),
    stage('validation', 'Validação técnica', 'architect-agent', ['data', 'design'], 'blocked'),
    stage('implementation', 'Implementação', 'developer-agent', ['validation'], 'blocked'),
  ],
}

function artifact(id: string, type: string, stageId: string, wave: string, header: object) {
  const current = {
    id,
    type,
    title: id,
    state: 'in_review',
    version: 1,
    stage: stageId,
    author: { type: 'agent', agent: stageId === 'design' ? 'designer-agent' : 'dba-agent' },
    wave,
    updated_at: '2026-11-22T10:00:00-03:00',
    header: { id, tipo: type, ...header },
    body: `# ${id}`,
    commit: 'b'.repeat(40),
  }
  return {
    current,
    versions: [{ version: 1, data: '2026-11-22', wave, description: 'criação' }],
    byVersion: { 1: current },
  }
}

const DIC = {
  modelo_dbml: 'docs/dados/modelo.dbml',
  tabelas: [
    {
      nome: 'bookings',
      origem: ['US-03'],
      descricao: 'Reservas de sala.',
      colunas: [
        { nome: 'id', tipo: 'uuid', nulo: false, descricao: 'Id.', dado_pessoal: false },
        {
          nome: 'requester_email',
          tipo: 'text',
          nulo: false,
          descricao: 'E-mail de quem reservou.',
          dado_pessoal: true,
        },
        { nome: 'room_id', tipo: 'uuid', nulo: false, descricao: 'Sala.', dado_pessoal: false },
      ],
      constraints: [
        { nome: 'bookings_pkey', tipo: 'primary_key', definicao: 'PRIMARY KEY (id)' },
        {
          nome: 'bookings_room_fk',
          tipo: 'foreign_key',
          definicao: 'room_id → rooms.id (N:1)',
          regra: 'US-03/RN-1',
        },
      ],
      indices: ['bookings_room_idx'],
    },
  ],
  dados_pessoais: [
    {
      campo: 'bookings.requester_email',
      finalidade: 'avisar quem reservou',
      base_legal: 'a_confirmar',
      retencao: '12 meses',
    },
  ],
}

const PERSIST = {
  banco: { sgbd: 'PostgreSQL', versao: '18' },
  acesso: 'pgx com consultas parametrizadas',
  concorrencia: [{ situacao: 'duas reservas na mesma sala', garantia: 'constraint de exclusão' }],
  indices: [
    {
      nome: 'bookings_room_idx',
      tabela: 'bookings',
      colunas: ['room_id', 'starts_at'],
      tipo: 'btree',
      justificativa: 'agenda da sala',
      custo_escrita_aceito: true,
    },
  ],
  migracoes: { ferramenta: 'goose', convencao: 'NNNN_nome.sql' },
  desempenho: [
    {
      consulta: 'agenda do dia',
      volume: '10 mil reservas',
      meta: 'p95 < 50 ms',
      plano_conferido: false,
      rnf: 'RNF-09',
    },
  ],
  backup: { rpo: '15 min', rto: '1 h', estrategia: 'WAL contínuo' },
  permissoes: [{ papel: 'app', privilegios: ['SELECT', 'INSERT'], ddl_em_producao: false }],
}

const TELAS = {
  fluxo: 'docs/design/fluxo.md',
  prototipo: 'docs/design/prototipo/index.html',
  estilo: 'docs/design/estilo.md',
  telas: [
    {
      id: 'TL-1',
      nome: 'Reservar sala',
      arquivo: 'docs/design/prototipo/reservar.html',
      us: ['US-03'],
      criterios: ['US-03/CA-1'],
      estados: ['vazio', 'carregando', 'erro', 'sucesso'],
    },
  ],
  acessibilidade: { nivel: 'wcag22_aa', verificado: true },
  lacunas_contrato: [],
}

const VALIDACAO = {
  autor: 'architect-agent',
  conferencias: [
    { par: 'arquitetura_dados', resultado: 'ok' },
    {
      par: 'api_telas',
      resultado: 'divergente',
      inconsistencia: 'Tela TL-1 usa campo ausente no contrato.',
      achado: 'ACH-04',
    },
  ],
  pareceres: [
    { participante: 'dba', parecer: 'reprovado', ressalvas: ['Índice ausente em room_id.'] },
    { participante: 'designer', parecer: 'aprovado', ressalvas: [] },
  ],
  veredito: 'devolvido',
}

function seed(): { api: FakeApi; state: ProjectState } {
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
      my_roles: ['member'],
      available_actions: [],
      last_sequence: 1,
    },
    workflow: WORKFLOW,
    waves: [
      { ...waveSummary('DADOS-01', 'running'), stage: 'data', agent: 'dba-agent' },
      { ...waveSummary('DESIGN-01', 'running'), stage: 'design', agent: 'designer-agent' },
    ],
    artifacts: {
      DIC: artifact('DIC', 'DIC', 'data', 'DADOS-01', DIC),
      PERSIST: artifact('PERSIST', 'PERSIST', 'data', 'DADOS-01', PERSIST),
      TELAS: artifact('TELAS', 'TELAS', 'design', 'DESIGN-01', TELAS),
      'VALIDACAO-01': artifact('VALIDACAO-01', 'VALIDACAO', 'validation', 'VAL-01', VALIDACAO),
    },
  })
  return { api, state }
}

test('US-20 CA-3, US-21: Data Modeling and Design run in parallel after Architecture, with the DBA and Designer', async ({
  page,
}) => {
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}`)

  const data = page.getByTestId('stage-data')
  await expect(data).toContainText('Agente DBA')
  await expect(data).toContainText('Em paralelo com: Design')
  await expect(data).toContainText('DADOS-01')
  const design = page.getByTestId('stage-design')
  await expect(design).toContainText('Agente de Design')
  await expect(design).toContainText('Em paralelo com: Modelagem de Dados')
  await expectNoSeriousA11yViolations(page)

  await data.getByRole('link', { name: 'Modelagem de Dados' }).click()
  await expect(page.getByTestId('stage-parallel')).toContainText('Design')
  await expect(page.getByTestId('stage-artifacts')).toContainText('DIC')
  await expect(page.getByTestId('stage-artifacts')).toContainText('PERSIST')
  await expect(page.getByTestId('stage-agent-link')).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/agents/dba-agent`,
  )
  await expectNoSeriousA11yViolations(page)

  await page.getByTestId('stage-parallel').getByRole('link', { name: 'Design' }).click()
  await expect(page.getByTestId('stage-artifacts')).toContainText('TELAS')
  await expect(page.getByTestId('stage-parallel')).toContainText('Modelagem de Dados')
})

test('US-20 CA-1: the DIC shows tables, dictionary, relationships and personal data', async ({
  page,
}) => {
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/DIC`)

  const view = page.getByTestId('structured-view')
  await expect(view.getByRole('heading', { name: 'Modelo de dados' })).toBeVisible()
  const table = page.getByTestId('dic-table-bookings')
  await expect(table).toContainText('Reservas de sala.')
  await expect(table.getByRole('link', { name: 'US-03' }).first()).toBeVisible()
  await expect(table.getByRole('row', { name: /requester_email/ })).toContainText('pessoal')
  await expect(table).toContainText('chave estrangeira')
  await expect(view).toContainText('room_id → rooms.id (N:1)')
  await expect(page.getByTestId('dic-personal-data')).toContainText('base legal a confirmar')
  await expect(page.getByTestId('dic-personal-data')).toContainText('12 meses')
  await expectNoSeriousA11yViolations(page)
})

test('US-20 CA-1: the PERSIST shows access, indexes with their reason, performance and backup', async ({
  page,
}) => {
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/PERSIST`)

  await expect(page.getByTestId('persist-summary')).toContainText('PostgreSQL 18')
  await expect(page.getByTestId('persist-summary')).toContainText('RPO 15 min · RTO 1 h')
  await expect(page.getByTestId('persist-indexes')).toContainText('agenda da sala')
  await expect(page.getByTestId('persist-indexes')).toContainText('custo de escrita aceito')
  await expect(page.getByTestId('structured-view')).toContainText('plano de execução não conferido')
  await expectNoSeriousA11yViolations(page)
})

test('US-21 CA-1: TELAS links every screen to its stories', async ({ page }) => {
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/TELAS`)

  const screen = page.getByTestId('screen-TL-1')
  await expect(screen).toContainText('Reservar sala')
  await expect(screen.getByRole('link', { name: 'US-03' })).toHaveAttribute(
    'href',
    `/projects/${PROJECT_ID}/artifacts/US-03`,
  )
  await expect(screen).toContainText('US-03/CA-1')
  await expect(page.getByTestId('structured-view')).toContainText('WCAG 2.2 AA · verificada')
  await expectNoSeriousA11yViolations(page)
})

test('US-20 CA-4, RN-2: VALIDACAO shows the cross checks, each opinion and the verdict', async ({
  page,
}) => {
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/artifacts/VALIDACAO-01`)

  await expect(page.getByTestId('validation-verdict')).toContainText('Veredito: devolvido')
  await expect(page.getByTestId('validation-opinions')).toContainText(
    'DBA · reprovado · Índice ausente em room_id.',
  )
  await expect(page.getByTestId('structured-view')).toContainText('contratos × telas · divergente')
  await expectNoSeriousA11yViolations(page)
})
