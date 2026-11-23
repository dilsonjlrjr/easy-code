import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi } from './support/fakeApi'

const FIRST = '11111111-1111-4111-8111-111111111111'
const SECOND = '22222222-2222-4222-8222-222222222222'

function signedIn(): FakeApi {
  const api = new FakeApi()
  api.signedIn = true
  api.user = { ...api.user, organization_roles: [] }
  return api
}

function sessions() {
  return [
    {
      id: FIRST,
      device: 'easycode-desktop/1.0.0 (linux; amd64)',
      started_at: '2026-11-22T09:00:00-03:00',
      last_use_at: '2026-11-23T08:30:00-03:00',
      ip_address: '10.0.0.15',
      acknowledged: true,
      current: false,
    },
    {
      id: SECOND,
      device: 'easycode-desktop/1.0.0 (darwin; arm64)',
      started_at: '2026-11-23T14:00:00-03:00',
      last_use_at: '2026-11-23T14:00:00-03:00',
      ip_address: '10.0.0.42',
      acknowledged: false,
      current: false,
    },
  ]
}

test('US-45 CA-5, CA-2: Minha conta › Cliente local lists my sessions and revokes one', async ({
  page,
}) => {
  // Two accessibility scans and several round trips: slow on the shared CI host.
  test.slow()
  const api = signedIn()
  api.localClientSessions = sessions()
  await api.install(page)
  await page.goto('/')

  // RN-3: open to every signed-in person, from the account menu.
  await page.getByTestId('account-menu').click({ timeout: 30_000 })
  await page.getByTestId('account-local-client').click()
  await expect(page.getByRole('heading', { level: 1, name: 'Cliente local' })).toBeVisible()
  const table = page.getByTestId('local-sessions')
  await expect(table.locator('tbody tr')).toHaveCount(2)
  await expect(table).toContainText('10.0.0.15')
  await expect(table).toContainText('easycode-desktop/1.0.0 (darwin; arm64)')
  await expect(page.getByText('nunca recebe chave de IA')).toBeVisible()
  // The code is confirmed in the SSO: the page links Keycloak's device page.
  await expect(page.getByTestId('sso-device-page')).toHaveAttribute(
    'href',
    'https://sso.example.test/realms/easycode/device',
  )
  await expectNoSeriousA11yViolations(page)

  await table
    .getByTestId(`session-${SECOND}`)
    .getByRole('button', { name: /Revogar a sessão/ })
    .click()
  const dialog = page.getByTestId('revoke-session-dialog')
  await expect(dialog).toContainText('IP 10.0.0.42')
  await expect(dialog).toContainText('em até 60 segundos')
  await expectNoSeriousA11yViolations(page)
  await dialog.getByRole('button', { name: 'Revogar' }).click()
  await expect(dialog).toBeHidden({ timeout: 30_000 })
  await expect(table.locator('tbody tr')).toHaveCount(1)
  expect(
    api.requests.some(
      (item) => item.method === 'DELETE' && item.path === `/me/local-client-sessions/${SECOND}`,
    ),
  ).toBe(true)
})

test('S-19: a new desktop session is flagged on Home until the person says it was them', async ({
  page,
}) => {
  test.slow()
  const api = signedIn()
  api.localClientSessions = sessions()
  await api.install(page)
  await page.goto('/')

  const banner = page.getByTestId('local-sessions-to-review')
  await expect(banner).toContainText('1 sessão nova do cliente local para conferir', {
    timeout: 30_000,
  })
  await banner.getByRole('link', { name: 'Conferir' }).click()
  const row = page.getByTestId(`session-${SECOND}`)
  await expect(row.getByTestId('session-new')).toContainText('confirme se foi você')
  await expect(page.getByTestId(`session-${FIRST}`).getByTestId('session-new')).toHaveCount(0)
  await row.getByRole('button', { name: /Foi eu: a sessão/ }).click()
  await expect(row.getByTestId('session-new')).toHaveCount(0, { timeout: 30_000 })
  expect(
    api.requests.some(
      (item) =>
        item.method === 'POST' &&
        item.path === `/me/local-client-sessions/${SECOND}/acknowledgement`,
    ),
  ).toBe(true)

  await page.goto('/')
  await expect(page.getByTestId('home-greeting')).toBeVisible({ timeout: 30_000 })
  await expect(page.getByTestId('local-sessions-to-review')).toHaveCount(0)
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
    { timeout: 30_000 },
  )
  await expect(page.getByTestId('no-local-sessions')).toContainText('escolha Entrar')
})
