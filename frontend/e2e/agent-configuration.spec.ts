import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow, waveSummary, type ProjectState } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'

function dbaAgent(overrides: Record<string, unknown> = {}) {
  return {
    agent: 'dba-agent',
    name: 'DBA',
    stages: ['data'],
    allowed_by_role: {
      read_file: ['docs/**', 'specs/**'],
      write_file: ['docs/dados/**'],
      execute_command: { allowed: true, categories: ['validate_ddl'] },
      browser: true,
    },
    limits: [],
    configuration: {
      version: 1,
      instructions_version: 'v1.0',
      tools: {
        read_file: ['docs/**'],
        write_file: ['docs/dados/**'],
        execute_command: { allowed: true, categories: ['validate_ddl'] },
      },
      skills: [],
      output_criteria: ['dic.ddl_executes'],
      created_at: '2026-11-20T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
    },
    ...overrides,
  }
}

function seed(
  roles: string[] = ['lead', 'approver'],
  agent = dbaAgent(),
): { api: FakeApi; state: ProjectState } {
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
      my_roles: roles,
      available_actions: [],
      last_sequence: 1,
    },
    workflow: phaseOneWorkflow('started'),
    agents: [agent],
    waves: [
      {
        ...waveSummary('DBA-01', 'completed'),
        stage: 'data',
        agent: 'dba-agent',
        agent_configuration_version: 1,
      },
    ],
  })
  return { api, state }
}

test('US-25 CA-1, CA-2, CA-4: the lead adjusts instructions and tools; a new version is born', async ({
  page,
}) => {
  const { api } = seed()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/agents/dba-agent`)

  // CA-1: instructions version, tools, skills and output criteria.
  await expect(page.getByTestId('agent-header')).toContainText('Instruções v1.0')
  await expect(page.getByTestId('agent-tools')).toContainText('docs/dados/**')
  await expect(page.getByText('dic.ddl_executes')).toBeVisible()
  // CA-4: each wave with the configuration version it used.
  await expect(page.getByTestId('agent-waves')).toContainText('DBA-01')
  await expect(page.getByTestId('agent-waves')).toContainText('Configuração v1')

  await page.getByTestId('edit-agent').click()
  const editor = page.getByTestId('agent-editor')
  await editor.getByLabel('Ajuste das instruções do projeto').fill('Nomes de tabela no singular.')
  await editor.getByRole('checkbox', { name: 'specs/**' }).check()
  await expect(editor.getByTestId('unsaved-changes')).toBeVisible()
  await expectNoSeriousA11yViolations(page)

  await editor.getByRole('button', { name: 'Revisar e salvar' }).click()
  const review = page.getByTestId('agent-review-dialog')
  await expect(review).toContainText('Ajuste das instruções alterado.')
  await expect(review).toContainText('Autoriza Ler arquivo: specs/**.')
  await expect(review).toContainText('Nasce a configuração v2')
  await expectNoSeriousA11yViolations(page)
  await review.getByRole('button', { name: 'Salvar nova versão' }).click()

  await expect(page.getByTestId('agent-header')).toContainText('Configuração v2')
  await expect(page.getByTestId('agent-adjustment-text')).toHaveText('Nomes de tabela no singular.')
  await expect(page.getByTestId('agent-versions')).toContainText('Configuração v2')
  await expect(page.getByTestId('agent-versions')).toContainText('Configuração v1')
  // CA-2: the earlier wave keeps the version it ran with.
  await expect(page.getByTestId('agent-waves')).toContainText('Configuração v1')
  const put = api.requests.find((request) => request.method === 'PUT')
  expect(put?.body).toEqual({
    adjustment_instructions: 'Nomes de tabela no singular.',
    tools: {
      read_file: ['docs/**', 'specs/**'],
      write_file: ['docs/dados/**'],
      execute_command: { allowed: true, categories: ['validate_ddl'] },
    },
    skills: [],
  })
  expect(put?.headers['if-match']).toBe('"1"')
})

test('US-25 CA-3: a tool outside the role is refused with its name and the version stays', async ({
  page,
}) => {
  // The api's role no longer allows the browser, although the screen offered it.
  const { api } = seed(
    ['lead'],
    dbaAgent({ role_limit: { read_file: ['docs/**'], write_file: ['docs/dados/**'] } }),
  )
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/agents/dba-agent`)
  await page.getByTestId('edit-agent').click()
  await page.getByTestId('agent-editor').getByRole('checkbox', { name: 'Navegador' }).check()
  await page.getByRole('button', { name: 'Revisar e salvar' }).click()
  const review = page.getByTestId('agent-review-dialog')
  await review.getByRole('button', { name: 'Salvar nova versão' }).click()
  const alert = review.getByRole('alert')
  await expect(alert).toContainText('Ferramenta fora do permitido para o papel do agente.')
  await expect(alert).toContainText('Ferramenta: browser.')
  await review.getByRole('button', { name: 'Continuar editando' }).click()
  await expect(page.getByTestId('agent-header')).toContainText('Configuração v1')
})

test('US-25 / CA-22-9: a member reads the configuration without the edit action', async ({
  page,
}) => {
  const { api } = seed(['member'])
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/agents/dba-agent`)
  await expect(page.getByTestId('agent-header')).toContainText('Configuração v1')
  await expect(page.getByTestId('agent-history')).toBeVisible()
  await expect(page.getByTestId('edit-agent')).toHaveCount(0)
})
