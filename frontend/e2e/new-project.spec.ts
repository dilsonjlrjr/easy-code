import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi } from './support/fakeApi'
import { chooseOption } from './support/select'

const price = {
  id: 1,
  effective_since: '2026-11-01T00:00:00-03:00',
  currency: 'BRL',
  per_million_tokens: { input: '1', output: '2', read_cache: '0.1', write_cache: '0.2' },
  source: 'página oficial',
  used: false,
  recorded_at: '2026-11-01T00:00:00-03:00',
}

function configuredApi(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
    .withActiveKey()
    .withModel({ model: 'reasoning-model', display_name: 'Modelo de raciocínio X' }, price)
    .withModel({ model: 'execution-model', display_name: 'Modelo de execução Y' }, price)
}

test('US-03 CA-1/CA-3, US-05 CA-1/CA-5, US-54 CA-1: create the project, acknowledge the provider and start the Requirements wave', async ({
  page,
}) => {
  const api = configuredApi()
  await api.install(page)
  await page.goto('/projects/new')

  await expect(page.getByRole('heading', { level: 1, name: 'Novo projeto' })).toBeVisible()
  await expect(page.locator('[aria-current="step"]')).toContainText('Passo 1 de 3')
  await expectNoSeriousA11yViolations(page)

  await page.getByLabel('Nome do projeto').fill('Reserva de salas')
  await page
    .getByLabel('Necessidade de negócio')
    .fill('Reservar salas de reunião sem conflito de horário.')
  await page.getByRole('button', { name: 'Continuar' }).click()

  await chooseOption(page, page, 'Modelo de raciocínio', /Modelo de raciocínio X/)
  await chooseOption(page, page, 'Modelo de execução', /Modelo de execução Y/)
  // US-54 CA-1: optional technical constraints go with the project as version 1.
  await page.getByRole('button', { name: 'Adicionar restrição' }).click()
  await page.getByLabel('Descrição').fill('Banco PostgreSQL da empresa')
  // US-39 CA-4, CA-05-8: the stop limit is required; leaving it empty points the field out.
  await page.getByRole('button', { name: 'Continuar' }).click()
  await expect(page.getByText('O limite de parada é obrigatório.')).toBeVisible()
  await expect(page.getByLabel('Limite de parada (BRL)')).toBeFocused()
  await page.getByLabel('Limite de parada (BRL)').fill('150,00')
  await expectNoSeriousA11yViolations(page)
  await page.getByRole('button', { name: 'Continuar' }).click()
  await expect(page.getByTestId('review-stop-limit')).toContainText('150,00')

  // CA-5: phase 1 shows only Requirements, Architecture and Implementation.
  const stages = page.locator('ol li', { hasText: /^(Requisitos|Arquitetura|Implementação)$/ })
  await expect(stages).toHaveCount(3)
  const create = page.getByRole('button', { name: 'Criar e iniciar o workflow' })
  await expect(create).toBeDisabled()
  await page.getByLabel(/Estou ciente/).check()
  await expectNoSeriousA11yViolations(page)
  await create.click()

  await expect(page.getByRole('heading', { level: 1, name: 'Reserva de salas' })).toBeVisible()
  await expect(page.getByTestId('stage-requirements')).toContainText('Em execução')
  await expect(page.getByTestId('stage-architecture')).toContainText('Bloqueada')
  await expect(
    page.getByTestId('stage-requirements').getByRole('link', { name: 'REQ-01' }),
  ).toBeVisible()
  await expectNoSeriousA11yViolations(page)

  const created = api.requests.find(
    (request) => request.method === 'POST' && request.path === '/projects',
  )
  expect(created?.body).toMatchObject({
    provider_submission_acknowledgment: true,
    constraints: { items: [{ category: 'other', description: 'Banco PostgreSQL da empresa' }] },
    // CA-05-9: alerts stay at 50% and 80% when untouched.
    budget: {
      stop_limit: { value: '150.00', currency: 'BRL' },
      percentage_alert_limits: [50, 80],
    },
  })
  const patch = api.requests.find((request) => request.method === 'PATCH')
  expect(patch?.headers['if-match']).toBe('"1"')
  expect(patch?.headers['content-type']).toContain('application/merge-patch+json')
})

test('US-03 CA-2/CA-4: an empty need is pointed out and a too large attachment is refused at once, keeping the form', async ({
  page,
}) => {
  const api = configuredApi()
  api.attachmentLimit = 10
  await api.install(page)
  await page.goto('/projects/new')

  await page.getByLabel('Nome do projeto').fill('Reserva de salas')
  await page.locator('#project-attachments').setInputFiles({
    name: 'ata-grande.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('conteúdo acima de dez bytes'),
  })
  await expect(page.getByTestId('attachment-refusal')).toContainText('ata-grande.txt')
  await expect(page.getByTestId('attachment-refusal')).toContainText('10 byte')

  await page.getByRole('button', { name: 'Continuar' }).click()
  await expect(page.getByText('A necessidade de negócio é obrigatória.').first()).toBeVisible()
  await expect(page.getByLabel('Nome do projeto')).toHaveValue('Reserva de salas')
  expect(
    api.requests.some((request) => request.method === 'POST' && request.path === '/projects'),
  ).toBe(false)
})

test('US-05 CA-2: without a valid key the start is refused and the project page lists what is missing', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  await api.install(page)
  await page.goto('/projects/new')

  await page.getByLabel('Nome do projeto').fill('Sem chave')
  await page.getByLabel('Necessidade de negócio').fill('Algo a fazer.')
  await page.getByRole('button', { name: 'Continuar' }).click()
  await expect(page.getByText('Não há modelo disponível')).toBeVisible()
  await page.getByLabel(/Limite de parada/).fill('100')
  await page.getByRole('button', { name: 'Continuar' }).click()
  await page.getByLabel(/Estou ciente/).check()
  await page.getByRole('button', { name: 'Criar e iniciar o workflow' }).click()

  await expect(page.getByRole('heading', { level: 1, name: 'Sem chave' })).toBeVisible()
  const refusal = page.getByTestId('start-refusal')
  await expect(refusal).toContainText('chave válida de provedor de IA')
  await expect(refusal).toContainText('par de modelos do projeto')
  // Without any model price the currency is unknown, the budget is not sent and the start lists it.
  await expect(refusal).toContainText('orçamento do projeto')
  await expect(page.getByTestId('stage-requirements')).toContainText('Não iniciada')
  await expectNoSeriousA11yViolations(page)
})

test('US-39 RN-1, RN-2, CA-05-8: a limit above zero and alerts from 1 to 99 without repeats', async ({
  page,
}) => {
  const api = configuredApi()
  await api.install(page)
  await page.goto('/projects/new')

  await page.getByLabel('Nome do projeto').fill('Reserva de salas')
  await page.getByLabel('Necessidade de negócio').fill('Reservar salas.')
  await page.getByRole('button', { name: 'Continuar' }).click()
  await chooseOption(page, page, 'Modelo de execução', /Modelo de execução Y/)
  await page.getByLabel('Limite de parada (BRL)').fill('0')
  await page.getByLabel('Alerta 2 (%)').fill('50')
  await page.getByRole('button', { name: 'Continuar' }).click()
  await expect(page.getByText('Informe um valor maior que zero')).toBeVisible()
  await expect(page.getByText('Os alertas não podem se repetir.')).toBeVisible()
  await page.getByRole('button', { name: 'remover alerta de 50%' }).last().click()
  await page.getByLabel('Limite de parada (BRL)').fill('80')
  await page.getByRole('button', { name: 'Continuar' }).click()
  await expect(page.getByTestId('review-stop-limit')).toContainText('80,00')
})
