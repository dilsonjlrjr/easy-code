import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi } from './support/fakeApi'

test('US-01 CA-1 / CA-02-1: without a session the address goes straight to the SSO and back to it', async ({
  page,
}) => {
  const api = new FakeApi()
  await api.install(page)

  await page.goto('/projects')

  await expect(page).toHaveURL(/\/projects$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Projetos' })).toBeVisible()
  // No intermediate login page: the only stop was startWebLogin with the requested path.
  expect(api.ssoRequests()).toEqual(['/session/login?callback=%2Fprojects'])
})

test('US-01 CA-3 / CA-4: signing out shows the login page with the reason; its button goes to the SSO', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  await api.install(page)

  await page.goto('/')
  await page.getByRole('button', { name: 'Conta de Dev' }).click()
  await page.getByText('Sair', { exact: true }).click()

  await expect(page).toHaveURL(/\/login\?reason=signed_out/)
  await expect(page.getByRole('heading', { level: 1, name: 'Entrar' })).toBeVisible()
  await expect(page.getByText('Você saiu da plataforma.')).toBeVisible()
  // CA-02-12: product name and subtitle in pt-BR.
  await expect(page.getByText('EasyCode', { exact: true })).toBeVisible()
  expect(api.signedIn).toBe(false)
  await expectNoSeriousA11yViolations(page)

  // CA-02-10: the focus starts on the sign-in button and Enter works.
  const signIn = page.getByRole('button', { name: 'Entrar com a conta da empresa' })
  await expect(signIn).toBeFocused()
  await page.keyboard.press('Enter')

  await expect(page).toHaveURL(/\/$/)
  // The Home page greets the person by name (canvas "Desktop · Início").
  await expect(page.getByRole('heading', { level: 1, name: /^Olá, / })).toBeVisible()
  expect(api.signedIn).toBe(true)
})

test('US-01 CA-2 / CA-02-4: an account without access sees "conta sem acesso" and no project data', async ({
  page,
}) => {
  const api = new FakeApi()
  api.accountWithoutAccess = true
  await api.install(page)

  await page.goto('/projects')

  await expect(page).toHaveURL(/\/access-denied$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Acesso negado' })).toBeVisible()
  await expect(page.getByRole('alert')).toContainText('Conta sem acesso.')
  await expect(page.getByRole('navigation')).toHaveCount(0)
  expect(api.requests.some((request) => request.path.startsWith('/projects'))).toBe(false)
  await expectNoSeriousA11yViolations(page)
})

test('ER-02-5 / CA-02-6: an expired session shows the notice and comes back to the same page', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  await api.install(page)

  await page.goto('/projects')
  await expect(page.getByRole('heading', { level: 1, name: 'Projetos' })).toBeVisible()
  api.expireSession()
  await page.getByRole('link', { name: 'Pendências' }).first().click()

  await expect(page).toHaveURL(/\/login\?reason=session_expired&return=(%2F|\/)pending/)
  await expect(page.getByTestId('session-expired')).toContainText('Sua sessão expirou')
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('button', { name: 'Entrar com a conta da empresa' }).click()
  await expect(page).toHaveURL(/\/pending$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Pendências' })).toBeVisible()
})

test('ESP-02 section 2: with an active session the login page leads to the return path', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  await api.install(page)

  await page.goto('/login?return=%2Fprojects')

  await expect(page).toHaveURL(/\/projects$/)
  expect(api.ssoRequests()).toEqual([])
})
