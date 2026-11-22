import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi } from './support/fakeApi'

const usedPrice = {
  id: 7,
  table_version: '7',
  effective_since: '2026-11-01T00:00:00-03:00',
  currency: 'BRL',
  per_million_tokens: { input: '15', output: '75', read_cache: '1.5', write_cache: '18.75' },
  source: 'Página oficial de preços, consultada em 01/11/2026',
  used: true,
  recorded_at: '2026-11-01T09:00:00-03:00',
  recorded_by: { id: 'dev', name: 'Dev' },
}

function signedInApi(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  return api
}

test('US-15 CA-1/CA-2/CA-3: the administrator saves a key, sees only its last 4 characters and the provider error of an invalid one', async ({
  page,
}) => {
  const api = signedInApi()
  await api.install(page)
  await page.goto('/settings/providers')

  const anthropic = page.getByTestId('provider-anthropic')
  await expect(anthropic).toContainText('sem chave')
  await expectNoSeriousA11yViolations(page)

  // Invalid key: the provider error is summarized and nothing is saved (CA-3).
  await anthropic.getByRole('button', { name: 'Cadastrar chave' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Chave de API').fill('sk-invalid-key-0000')
  await dialog.getByRole('button', { name: 'Testar e salvar' }).click()
  await expect(dialog.locator('.v-alert')).toContainText('Chave inválida: 401 invalid x-api-key')
  await expect(dialog.getByLabel('Chave de API')).toHaveValue('')
  await dialog.getByRole('button', { name: 'Cancelar' }).click()
  await expect(anthropic).toContainText('sem chave')

  // Valid key: tested, active, only the last 4 shown (CA-1, CA-2).
  await anthropic.getByRole('button', { name: 'Cadastrar chave' }).click()
  await page.getByRole('dialog').getByLabel('Chave de API').fill('sk-ant-secret-value-wxyz')
  await page.getByRole('dialog').getByRole('button', { name: 'Testar e salvar' }).click()
  await expect(anthropic).toContainText('ativa')
  await expect(anthropic).toContainText('final wxyz')
  await expect(page.locator('body')).not.toContainText('sk-ant-secret-value')
  await expect(anthropic.getByRole('button', { name: 'Trocar chave' })).toBeVisible()
})

test('CA-23-4: removing a key warns that waves of the provider stop running', async ({ page }) => {
  const api = signedInApi().withActiveKey()
  await api.install(page)
  await page.goto('/settings/providers')

  await page
    .getByTestId('provider-anthropic')
    .getByRole('button', { name: 'Remover chave' })
    .click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('provedor sem chave')
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Remover chave' }).click()
  await expect(page.getByTestId('provider-anthropic')).toContainText('sem chave')
})

test('US-37 CA-1/CA-3: a new price period goes on top, the used one stays and cannot be corrected', async ({
  page,
}) => {
  const api = signedInApi()
    .withActiveKey()
    .withModel({ model: 'reasoning-model', display_name: 'Raciocínio' }, usedPrice)
  await api.install(page)
  await page.goto('/settings/prices')

  const usedRow = page.getByTestId('price-period-7')
  await expect(usedRow).toContainText('já usada por onda')
  await expect(usedRow.getByRole('button', { name: 'Corrigir' })).toHaveCount(0)
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('button', { name: 'Registrar nova vigência' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Vigente desde').fill('2026-12-01T00:00')
  await dialog.getByLabel('Entrada').fill('12')
  await dialog.getByLabel('Saída').fill('60')
  await dialog.getByLabel('Leitura de cache').fill('1.2')
  await dialog.getByLabel('Escrita de cache').fill('15')
  await dialog.getByLabel('Fonte', { exact: true }).fill('Página oficial, consultada em 22/11/2026')
  await dialog.getByRole('button', { name: 'Continuar' }).click()
  await expect(dialog).toContainText('Confira a vigência antes de gravar')
  await dialog.getByRole('button', { name: 'Registrar vigência' }).click()

  const rows = page.locator('tbody tr')
  await expect(rows).toHaveCount(2)
  await expect(rows.first()).toContainText('ainda não usada')
  await expect(rows.first().getByRole('button', { name: 'Corrigir' })).toBeVisible()
  await expect(rows.nth(1)).toContainText('já usada por onda')
  const posted = api.requests.find(
    (request) => request.method === 'POST' && request.path.endsWith('/prices'),
  )
  expect(posted?.headers['idempotency-key']).toMatch(/^[0-9a-f-]{36}$/)
})

test('US-16 matrix: models show capabilities and "no current price"', async ({ page }) => {
  const api = signedInApi()
    .withActiveKey()
    .withModel({ model: 'execution-model', display_name: 'Execução' })
  await api.install(page)
  await page.goto('/settings/providers')

  const row = page.locator('tbody tr', { hasText: 'Execução' })
  await expect(row).toContainText('sem preço vigente')
  await expect(row).toContainText('200.000')
})

test('ER-23-15: someone who is not administrator sees "no permission" and no Settings in the menu', async ({
  page,
}) => {
  const api = signedInApi()
  api.user = { id: 'ana', name: 'Ana', organization_roles: [] }
  await api.install(page)
  await page.goto('/settings')

  await expect(page.getByRole('alert')).toContainText('Sem permissão')
  await expect(
    page
      .getByRole('navigation', { name: 'Principal' })
      .getByRole('link', { name: 'Configurações' }),
  ).toHaveCount(0)
})
