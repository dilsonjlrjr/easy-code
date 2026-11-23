import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi } from './support/fakeApi'

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  api.user = { ...api.user, organization_roles: [] }
  return api
}

test('US-45 CA-5, CA-2: Minha conta › Cliente local lists my sessions and revokes one', async ({
  page,
}) => {
  // Two accessibility scans and several round trips: slow on the shared CI host.
  test.slow()
  const api = signedIn()
  api.localClientSessions = [
    {
      id: '11111111-1111-4111-8111-111111111111',
      started_at: '2026-11-22T09:00:00-03:00',
      last_use_at: '2026-11-23T08:30:00-03:00',
      ip_address: '10.0.0.15',
    },
    {
      id: '22222222-2222-4222-8222-222222222222',
      started_at: '2026-11-20T14:00:00-03:00',
      ip_address: '10.0.0.42',
    },
  ]
  await api.install(page)
  await page.goto('/')

  // RN-3: open to every signed-in person, from the account menu.
  await page.getByTestId('account-menu').click({ timeout: 30_000 })
  await page.getByTestId('account-local-client').click()
  await expect(page.getByRole('heading', { level: 1, name: 'Cliente local' })).toBeVisible()
  const table = page.getByTestId('local-sessions')
  await expect(table.locator('tbody tr')).toHaveCount(2)
  await expect(table).toContainText('10.0.0.15')
  await expect(table).toContainText('ainda não usada')
  await expect(page.getByText('nunca recebe chave de IA')).toBeVisible()
  await expectNoSeriousA11yViolations(page)

  await table
    .getByTestId('session-22222222-2222-4222-8222-222222222222')
    .getByRole('button', { name: /Revogar a sessão/ })
    .click()
  const dialog = page.getByTestId('revoke-session-dialog')
  await expect(dialog).toContainText('IP 10.0.0.42')
  await expect(dialog).toContainText('em até 60 segundos')
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Revogar' }).click()
  await expect(dialog).toBeHidden()
  await expect(table.locator('tbody tr')).toHaveCount(1)
  await expect(page.getByRole('status').filter({ hasText: 'Sessão revogada.' })).toBeAttached()
  expect(
    api.requests.some(
      (item) =>
        item.method === 'DELETE' &&
        item.path === '/me/local-client-sessions/22222222-2222-4222-8222-222222222222',
    ),
  ).toBe(true)
})

test('ESP-23 6: without sessions the page says how to connect the desktop app', async ({
  page,
}) => {
  const api = signedIn()
  await api.install(page)
  // The desktop app opens /account/local-client; the pt-BR address is an alias (ESP-23 2).
  await page.goto('/minha-conta/cliente-local')
  await expect(page.getByTestId('no-local-sessions')).toContainText(
    'Nenhum cliente local conectado.',
  )
  await expect(page.getByTestId('no-local-sessions')).toContainText('escolha Entrar')
})

test('US-45 CA-1 (S-19): the code page shows client, access and origin, then goes to the SSO', async ({
  page,
}) => {
  // Two accessibility scans and several round trips: slow on the shared CI host.
  test.slow()
  const api = signedIn()
  await api.install(page)
  await page.route('https://sso.example.test/**', (route) =>
    route.fulfill({ contentType: 'text/html', body: '<title>SSO</title><p>SSO</p>' }),
  )
  await page.goto('/device?user_code=wdjb-mjht')

  const request = page.getByTestId('device-request')
  await expect(request).toContainText('Easy Code desktop', { timeout: 30_000 })
  await expect(request).toContainText('easycode-desktop')
  await expect(request).toContainText('Só leitura')
  await expect(request).toContainText('O app desktop que mostrou este código')
  await expect(request).toContainText('Dev')
  await expect(request).toContainText('sso.example.test')
  await expect(page.getByTestId('device-warning')).toContainText('expira em 10 min')
  await expectNoSeriousA11yViolations(page)

  const field = page.getByLabel('Código mostrado no app')
  await expect(field).toHaveValue('wdjb-mjht')
  await field.fill('<x>')
  await field.blur()
  await expect(page.getByText('Isso não parece um código do app.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Continuar para aprovar no SSO' })).toBeDisabled()
  await field.fill('WDJB-MJHT')
  await page.getByRole('button', { name: 'Continuar para aprovar no SSO' }).click()
  await page.waitForURL('https://sso.example.test/realms/easycode/device?user_code=WDJB-MJHT')
})

test('US-45 CA-4: declining approves nothing', async ({ page }) => {
  const api = signedIn()
  await api.install(page)
  await page.goto('/device?user_code=ABCD-EFGH')
  await page.getByRole('button', { name: 'Não fui eu' }).click()
  await expect(page.getByTestId('device-declined')).toContainText('Nada foi aprovado.')
  await page.getByRole('link', { name: 'Ver minhas sessões do cliente local' }).click()
  await expect(page).toHaveURL(/\/account\/local-client$/)
})
