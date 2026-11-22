import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { FakeApi } from './support/fakeApi'

test('without a session the address leads to the login page and, after the dev sign-in, back to it', async ({
  page,
}) => {
  const api = new FakeApi()
  await api.install(page)

  await page.goto('/projects')

  await expect(page).toHaveURL(/\/login\?return=(%2F|\/)projects/)
  await expect(page.getByRole('heading', { level: 1, name: 'Entrar no Easy Code' })).toBeVisible()
  await expectNoSeriousA11yViolations(page)

  await page.getByRole('button', { name: 'Entrar com a identidade de desenvolvimento' }).click()

  await expect(page).toHaveURL(/\/projects$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Projetos' })).toBeVisible()
  expect(
    api.requests.some((request) => request.method === 'POST' && request.path === '/dev/session'),
  ).toBe(true)
})

test('ER-02-9: the dev sign-in is offered only when the runtime configuration enables it', async ({
  page,
}) => {
  const api = new FakeApi()
  api.devSignIn = false
  await api.install(page)

  await page.goto('/login')

  await expect(page.getByRole('button', { name: 'Entrar pelo SSO' })).toBeDisabled()
  await expect(page.getByRole('button', { name: /identidade de desenvolvimento/ })).toHaveCount(0)
})

test('CA-00-16: signing out ends the session and the next page asks to sign in', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  await api.install(page)

  await page.goto('/')
  await page.getByRole('button', { name: 'Conta de Dev' }).click()
  await page.getByText('Sair', { exact: true }).click()

  await expect(page).toHaveURL(/\/login/)
  await page.goto('/projects')
  await expect(page).toHaveURL(/\/login\?return=/)
})
