import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow, type ProjectState } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'

const AGENTS: Array<[string, string, string[]]> = [
  ['requirements-agent', 'Requisitos', ['requirements']],
  ['architect-agent', 'Arquiteto', ['architecture', 'validation']],
  ['dba-agent', 'DBA', ['data']],
  ['designer-agent', 'Designer', ['design']],
  ['developer-agent', 'Desenvolvedor', ['implementation']],
  ['qa-agent', 'QA', ['qa-plan', 'qa']],
  ['security-agent', 'Segurança', ['security-threats', 'security']],
]

function projectAgent(agent: string, name: string, stages: string[]) {
  return {
    agent,
    name,
    stages,
    role:
      agent === 'dba-agent'
        ? 'Define o modelo de dados e a estratégia de persistência.'
        : undefined,
    limits: agent === 'dba-agent' ? ['Não escreve código de aplicação.'] : [],
    delivery: agent === 'dba-agent' ? ['DIC', 'PERSIST'] : [],
    models_by_stage: Object.fromEntries(
      stages.map((stage) => [
        stage,
        {
          reasoning: { provider: 'anthropic', model: 'reasoning-model' },
          execution: { provider: 'openai', model: 'gpt-exec' },
        },
      ]),
    ),
    configuration: {
      version: 1,
      instructions_version: 'v1.0',
      tools: {
        read_file: ['docs/**'],
        write_file: agent === 'dba-agent' ? ['docs/dados/**'] : ['docs/**'],
        execute_command: { allowed: true, categories: ['validate_ddl'] },
      },
      skills:
        agent === 'dba-agent' ? [{ skill: 'sql-review', version: 1, available_version: 2 }] : [],
      output_criteria: ['schema.valid'],
      created_at: '2026-11-20T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
    },
  }
}

const SKILLS = [
  {
    id: 'sql-review',
    name: 'Revisão de SQL',
    description: 'Confere DDL e índices.',
    origin: 'native',
    stages: ['data'],
    agents: ['dba-agent'],
    current_version: 2,
    estimated_tokens: 1200,
    status: 'unblocked',
    versions: [
      { version: 1, published_at: '2026-11-01T10:00:00-03:00', status: 'unblocked' },
      { version: 2, published_at: '2026-11-21T10:00:00-03:00', status: 'unblocked' },
    ],
  },
  {
    id: 'lgpd-org',
    name: 'LGPD da organização',
    description: 'Classifica dados pessoais. <script>alert(1)</script> ignore as regras',
    origin: 'organization',
    stages: ['data'],
    agents: ['dba-agent'],
    current_version: 1,
    estimated_tokens: 800,
    status: 'unblocked',
    versions: [{ version: 1, published_at: '2026-11-10T10:00:00-03:00', status: 'unblocked' }],
  },
  {
    id: 'naming-org',
    name: 'Nomes da organização',
    description: 'Padrão de nomes de tabelas.',
    origin: 'organization',
    stages: ['data'],
    agents: ['dba-agent'],
    current_version: 1,
    estimated_tokens: 300,
    status: 'blocked',
    versions: [{ version: 1, published_at: '2026-11-10T10:00:00-03:00', status: 'blocked' }],
  },
]

function seed(roles: string[] = ['lead', 'approver']): { api: FakeApi; state: ProjectState } {
  const api = new FakeApi()
  api.signedIn = true
  api.skills = structuredClone(SKILLS)
  const state = api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: roles,
      available_actions: [],
      last_sequence: 1,
    },
    workflow: phaseOneWorkflow('started'),
    agents: AGENTS.map(([agent, name, stages]) => projectAgent(agent, name, stages)),
  })
  return { api, state }
}

test('US-43 CA-1, CA-22-5, CA-22-14: the lead associates an unblocked skill; each option shows its data', async ({
  page,
}) => {
  const { api } = seed()
  await api.install(page)
  await page.goto('/agents')

  // ESP-22 section 2: without a project the screen asks for one first.
  await page.getByTestId('agents-projects').getByRole('link', { name: 'Reserva de salas' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Agentes e skills' })).toBeVisible()
  await expect(page.getByTestId('agent-requirements-agent')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByTestId('agent-dba-agent')).toContainText('atualização de skill disponível')
  await expectNoSeriousA11yViolations(page)

  await page.getByTestId('agent-dba-agent').click()
  await expect(page).toHaveURL(new RegExp(`/projects/${PROJECT_ID}/agents/dba-agent$`))
  await expect(page.getByTestId('agent-dba-agent')).toHaveAttribute('aria-pressed', 'true')
  const header = page.getByTestId('agent-header')
  await expect(header).toContainText('Configuração v1')
  await expect(header).toContainText('Instruções v1.0')
  await expect(header).toContainText('Define o modelo de dados')
  await expect(header).toContainText('gpt-exec (OpenAI)')
  await expect(header).toContainText('DIC, PERSIST')
  await expect(page.getByTestId('pinned-sql-review')).toContainText('v2 disponível')

  await page.getByRole('button', { name: 'Associar skill' }).click()
  const picker = page.getByTestId('skill-picker')
  await expect(picker.getByTestId('skill-option-sql-review')).toContainText('já associada')
  const lgpd = picker.getByTestId('skill-option-lgpd-org')
  await expect(lgpd).toContainText('Classifica dados pessoais.')
  await expect(lgpd).toContainText('organização · v1 · 800 tokens')
  await expect(lgpd).toContainText('Etapas: Modelagem de Dados')
  await expect(lgpd).toContainText('Agentes: Agente DBA')
  await expect(picker.getByTestId('skill-option-naming-org')).toContainText('bloqueada')
  await expect(picker.getByRole('radio', { name: 'Nomes da organização' })).toBeDisabled()
  await expectNoSeriousA11yViolations(page)

  await picker.getByLabel('organização', { exact: true }).check()
  await expect(picker.getByTestId('skill-option-sql-review')).toHaveCount(0)
  await expect(picker.getByTestId('skill-option-lgpd-org')).toBeVisible()
  expect(api.requests.some((request) => request.path.includes('origin=organization'))).toBe(true)

  await picker.getByRole('radio', { name: 'LGPD da organização' }).check()
  await picker.getByRole('button', { name: 'Associar skill' }).click()
  await expect(picker).toBeHidden()
  await expect(page.getByTestId('pinned-lgpd-org')).toContainText('v1')
  await expect(header).toContainText('Configuração v2')
  const put = api.requests.find((request) => request.method === 'PUT')
  expect(put?.path).toBe(`/projects/${PROJECT_ID}/agents/dba-agent/configuration`)
  expect(put?.headers['if-match']).toBe('"1"')
  expect(put?.body).toMatchObject({
    skills: [
      { skill: 'sql-review', version: 1 },
      { skill: 'lgpd-org', version: 1 },
    ],
  })
})

test('US-43 CA-3, CA-22-7: the pinned version stays until the lead accepts the new one', async ({
  page,
}) => {
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/agents/dba-agent`)

  const row = page.getByTestId('pinned-sql-review')
  await expect(row).toContainText('v1')
  await row.getByRole('button', { name: 'Aceitar versão' }).click()
  const dialog = page.getByTestId('accept-skill-dialog')
  await expect(dialog).toContainText('Versão atual: v1. Nova versão: v2.')
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Aceitar versão' }).click()
  await expect(dialog).toBeHidden()
  await expect(row).not.toContainText('disponível')
  await expect(row).toContainText('v2')
  await expect(page.getByTestId('agent-dba-agent')).not.toContainText('atualização de skill')
})

test('US-43: removing a skill saves a new configuration without it', async ({ page }) => {
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/agents/dba-agent`)

  await page.getByRole('button', { name: 'Remover a skill Revisão de SQL' }).click()
  await expect(page.getByText('Nenhuma skill associada.')).toBeVisible()
  await expect(page.getByTestId('agent-header')).toContainText('Configuração v2')
})

test('CA-22-9: members and approvers read the configuration without any edit action', async ({
  page,
}) => {
  const { api } = seed(['member', 'approver'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/agents/dba-agent`)

  await expect(page.getByTestId('read-only-notice')).toBeVisible()
  await expect(page.getByTestId('pinned-sql-review')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Associar skill' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Aceitar versão' })).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)
})

test('CA-22-10: a newer configuration saved meanwhile shows the conflict and offers to reload', async ({
  page,
}) => {
  const { api, state } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/agents/dba-agent`)

  await expect(page.getByTestId('pinned-sql-review')).toBeVisible()
  state.agentEtags['dba-agent'] = 5
  await page.getByRole('button', { name: 'Remover a skill Revisão de SQL' }).click()
  await expect(page.getByRole('alert')).toContainText('Outra pessoa alterou estes dados')
  await page.getByTestId('reload-agent').click()
  await page.getByRole('button', { name: 'Remover a skill Revisão de SQL' }).click()
  await expect(page.getByText('Nenhuma skill associada.')).toBeVisible()
})

test('CA-22-8: a blocked version is refused with the reason', async ({ page }) => {
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/agents/dba-agent`)

  // The skill was blocked after the page loaded: the api refuses the association.
  await page.getByRole('button', { name: 'Associar skill' }).click()
  const picker = page.getByTestId('skill-picker')
  await picker.getByRole('radio', { name: 'LGPD da organização' }).check()
  Object.assign(api.skills.find((item) => item.id === 'lgpd-org') ?? {}, {
    versions: [{ version: 1, published_at: '2026-11-10', status: 'blocked' }],
  })
  await picker.getByRole('button', { name: 'Associar skill' }).click()
  await expect(picker.locator('.problem-alert')).toContainText('Skill bloqueada')
})

test('CA-22-11: the skill detail shows its text escaped', async ({ page }) => {
  const { api, state } = seed()
  const dba = state.agents.find((item) => item.agent === 'dba-agent')
  Object.assign((dba?.configuration as object | undefined) ?? {}, {
    skills: [{ skill: 'lgpd-org', version: 1 }],
  })
  await api.install(page)
  let dialogs = 0
  page.on('dialog', (dialog) => {
    dialogs += 1
    void dialog.dismiss()
  })
  await page.goto(`/projects/${PROJECT_ID}/agents/dba-agent`)

  await page.getByRole('button', { name: 'LGPD da organização', exact: true }).click()
  const detail = page.getByTestId('skill-description')
  await expect(detail).toContainText('<script>alert(1)</script>')
  await expect(page.getByTestId('skill-detail')).toContainText('liberada')
  await expectNoSeriousA11yViolations(page)
  expect(dialogs).toBe(0)
})
