import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow } from './support/fakeApi'
import { chooseOption, openSelect } from './support/select'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'

function apiWithProject(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  api
    .withActiveKey()
    .withModel({ model: 'reasoning-model', display_name: 'Raciocínio X' })
    .withModel({
      model: 'no-tools-model',
      display_name: 'Sem ferramentas Z',
      capabilities: {
        tool_use: false,
        context_tokens: 100000,
        prompt_cache: false,
        image_input: false,
      },
    })
  api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'created',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: ['lead', 'approver'],
      available_actions: ['start', 'cancel'],
      last_sequence: 1,
    },
    workflow: phaseOneWorkflow(null),
  })
  return api
}

test('US-16 CA-1: the lead chooses the project model pair and the configuration shows it', async ({
  page,
}) => {
  const api = apiWithProject()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/configuration`)

  await expect(page.getByText('Par de modelos não definido')).toBeVisible()
  await expectNoSeriousA11yViolations(page)

  const form = page.locator('form').first()
  await chooseOption(page, form, 'Modelo de raciocínio', /Raciocínio X/)
  await chooseOption(page, form, 'Modelo de execução', /Raciocínio X/)
  await page.getByRole('button', { name: 'Salvar par do projeto' }).click()

  await expect(page.getByTestId('current-pair')).toContainText('Raciocínio X (Anthropic)')
})

test('US-16 CA-2: a model without the capability of the stage is refused with the capability name', async ({
  page,
}) => {
  const api = apiWithProject()
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/configuration`)

  // The implementation stage requires tool use: the model without it is not even offered (ER-29-25).
  const implementation = page.getByTestId('stage-pair-implementation')
  await expect(implementation).toContainText('Exige: uso de ferramentas')
  await openSelect(implementation, 'Modelo de execução')
  await expect(page.getByRole('option', { name: /Sem ferramentas Z/ })).toHaveCount(0)
  await page.keyboard.press('Escape')

  // For the whole project the API refuses it and the screen names the capability.
  const form = page.locator('form').first()
  await chooseOption(page, form, 'Modelo de raciocínio', /Raciocínio X/)
  await chooseOption(page, form, 'Modelo de execução', /Sem ferramentas Z/)
  await page.getByRole('button', { name: 'Salvar par do projeto' }).click()
  await expect(page.getByTestId('start-refusal')).toContainText(
    'o modelo não tem uso de ferramentas',
  )
})

test('US-54 CA-3: the lead changes the technical constraints and a new version is shown', async ({
  page,
}) => {
  const api = apiWithProject()
  api.project(PROJECT_ID).configuration.constraints = {
    version: 1,
    items: [{ id: 'RESTR-01', category: 'database', description: 'PostgreSQL 16 obrigatório' }],
    defined_by: { id: 'dev', name: 'Dev' },
    defined_at: '2026-11-21T10:00:00-03:00',
  }
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/configuration`)

  const section = page.getByTestId('constraints')
  await expect(section).toContainText('RESTR-01 · Banco de dados')
  await expect(section).toContainText('Versão 1, gravada por Dev')
  await section.getByRole('button', { name: 'Alterar restrições' }).click()
  await section.getByRole('button', { name: 'Adicionar restrição' }).click()
  await section.getByLabel('Descrição').nth(1).fill('Integração com o Active Directory')
  await expectNoSeriousA11yViolations(page)
  await section.getByRole('button', { name: 'Salvar restrições' }).click()

  await expect(section).toContainText('Versão 2, gravada por Dev')
  await expect(section).toContainText('RESTR-02 · Outra')
  const patch = api.requests.find(
    (request) => request.method === 'PATCH' && request.path.endsWith('/configuration'),
  )
  expect(patch?.body).toEqual({
    constraints: {
      items: [
        { id: 'RESTR-01', category: 'database', description: 'PostgreSQL 16 obrigatório' },
        { category: 'other', description: 'Integração com o Active Directory' },
      ],
    },
  })
})

test('US-54 CA-4 / CA-6: without the lead role, or with the project ended, the constraints are read only', async ({
  page,
}) => {
  const api = apiWithProject()
  const state = api.project(PROJECT_ID)
  state.project = { ...state.project, my_roles: ['approver'] }
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/configuration`)

  const section = page.getByTestId('constraints')
  await expect(section).toContainText('Nenhuma restrição técnica')
  await expect(section).toContainText('Só o responsável do projeto altera as restrições')
  await expect(section.getByRole('button', { name: 'Alterar restrições' })).toHaveCount(0)
})
