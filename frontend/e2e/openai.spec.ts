import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi, phaseOneWorkflow, waveDetail, waveSummary } from './support/fakeApi'
import { chooseOption } from './support/select'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'
const BRL = (value: string) => ({ value, currency: 'BRL' })

function price(id: number) {
  return {
    id,
    table_version: String(id),
    effective_since: '2026-11-01T00:00:00-03:00',
    currency: 'BRL',
    per_million_tokens: { input: '6', output: '24', read_cache: '0.6', write_cache: '0' },
    source: 'Página oficial de preços, consultada em 01/11/2026',
    used: false,
    recorded_at: '2026-11-01T09:00:00-03:00',
    recorded_by: { id: 'dev', name: 'Dev' },
  }
}

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
}

function withProject(api: FakeApi, state: string): void {
  api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state,
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      lead: { id: 'dev', name: 'Dev' },
      my_roles: ['lead', 'approver'],
      available_actions: state === 'created' ? ['start', 'cancel'] : [],
      last_sequence: 1,
    },
    workflow: phaseOneWorkflow(state === 'created' ? null : 'started'),
  })
}

test('US-24 CA-4: the administrator enables OpenAI and its models show the whole capability matrix', async ({
  page,
}) => {
  const api = signedIn()
  api.withModel(
    {
      provider: 'openai',
      model: 'gpt-exec',
      display_name: 'GPT Execução',
      capabilities: {
        tool_use: true,
        context_tokens: 400000,
        prompt_cache: true,
        image_input: true,
      },
    },
    price(11),
  )
  await api.install(page)
  await page.goto('/settings/providers')

  const openai = page.getByTestId('provider-openai')
  await expect(openai).toContainText('OpenAI')
  await expect(openai).toContainText('sem chave')
  await openai.getByRole('button', { name: 'Cadastrar chave' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Chave de API').fill('sk-openai-test-abcd')
  await dialog.getByRole('button', { name: 'Testar e salvar' }).click()
  await expect(openai).toContainText('ativa')
  await expect(openai).toContainText('final abcd')
  const saved = api.requests.find((request) => request.path === '/providers/openai/key')
  expect(saved?.method).toBe('PUT')

  const row = page.getByRole('row', { name: /GPT Execução/ })
  await expect(row).toContainText('OpenAI')
  await expect(row).toContainText('400.000')
  await expect(row.getByRole('cell').nth(2)).toHaveText('sim')
  await expect(row.getByRole('cell').nth(4)).toHaveText('sim')
  await expect(row.getByRole('cell').nth(5)).toHaveText('sim')
  await expect(row).toContainText('Entrada')
  await expect(row).toContainText('Saída')
  await expectNoSeriousA11yViolations(page)
})

test('US-24 CA-1, CA-3: the project pair mixes the Anthropic reasoning model with the OpenAI execution model', async ({
  page,
}) => {
  const api = signedIn()
  api
    .withActiveKey('anthropic', 'wxyz')
    .withActiveKey('openai', 'abcd')
    .withModel({ model: 'reasoning-model', display_name: 'Raciocínio X' }, price(1))
    .withModel({ provider: 'openai', model: 'gpt-exec', display_name: 'GPT Execução' }, price(11))
  withProject(api, 'created')
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/configuration`)

  const form = page.locator('form').first()
  await chooseOption(page, form, 'Modelo de raciocínio', /Raciocínio X/)
  await chooseOption(page, form, 'Modelo de execução', /GPT Execução/)
  await page.getByRole('button', { name: 'Salvar par do projeto' }).click()

  await expect(page.getByTestId('current-pair')).toContainText('Raciocínio X (Anthropic)')
  await expect(page.getByTestId('current-pair')).toContainText('GPT Execução (OpenAI)')
  const change = api.requests.find((request) => request.method === 'PATCH')
  expect(change?.body).toMatchObject({
    model_pair: {
      reasoning: { provider: 'anthropic', model: 'reasoning-model' },
      execution: { provider: 'openai', model: 'gpt-exec' },
    },
  })
  await expectNoSeriousA11yViolations(page)
})

test('US-24 CA-3: a wave with models of both providers shows the cost apart by provider', async ({
  page,
}) => {
  const api = signedIn()
  withProject(api, 'in_progress')
  const state = api.project(PROJECT_ID)
  state.waves = [waveSummary('REQ-01', 'completed')]
  state.waveDetails['REQ-01'] = waveDetail('REQ-01', 'completed', {
    result: 'success',
    available_actions: [],
    models: {
      reasoning: { provider: 'anthropic', model: 'reasoning-model' },
      execution: { provider: 'openai', model: 'gpt-exec' },
    },
    cost: {
      useful: BRL('0.0900'),
      lost: BRL('0'),
      total: BRL('0.0900'),
      estimated_lost: BRL('0'),
      per_provider: [
        { provider: 'anthropic', total: BRL('0.0600') },
        { provider: 'openai', total: BRL('0.0300') },
      ],
    },
  })
  await api.install(page)
  await page.goto(`/projects/${PROJECT_ID}/waves/REQ-01`)

  const cost = page.getByTestId('wave-cost')
  await expect(cost).toContainText('Anthropic · reasoning-model')
  await expect(cost).toContainText('OpenAI · gpt-exec')
  await expect(cost).toContainText('Total em Anthropic')
  await expect(cost).toContainText('Total em OpenAI')
  await expect(cost).toContainText('0,03')
  await expectNoSeriousA11yViolations(page)
})
