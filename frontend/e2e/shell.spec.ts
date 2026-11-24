import { expect, test } from '@playwright/test'
import { FakeApi } from './support/fakeApi'

test.beforeEach(async ({ page }) => {
  const api = new FakeApi()
  api.signedIn = true
  await api.install(page)
})

test('the shell opens in pt-BR and switches to English, keeping the choice', async ({ page }) => {
  await page.goto('/projects')

  const mainNavigation = page.getByRole('navigation', { name: 'Principal' })
  await expect(page.getByRole('banner')).toBeVisible()
  await expect(mainNavigation).toBeVisible()
  await expect(page.getByRole('main')).toBeVisible()
  await expect(page.getByRole('heading', { level: 1, name: 'Projetos' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'pt-BR')

  // The interface language lives in the account menu (canvas "Desktop · Início").
  await page.getByTestId('account-menu').click()
  await page.getByRole('menuitemradio', { name: 'English' }).click()

  await expect(page.getByRole('heading', { level: 1, name: 'Projects' })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Main' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  await expect(page).toHaveTitle('Projects · Easy Code')

  await page.reload()
  await expect(page.getByRole('heading', { level: 1, name: 'Projects' })).toBeVisible()
})

test('on a phone the main navigation becomes a bottom bar without horizontal scroll', async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 640 })
  await page.goto('/')

  const mainNavigation = page.getByRole('navigation', { name: 'Principal' })
  await expect(mainNavigation.getByRole('link')).toHaveCount(5)
  const hasHorizontalScroll = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  )
  expect(hasHorizontalScroll).toBe(false)
})
